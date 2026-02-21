// Package vault — history scanning.
// Reads saved transcription files from the vault directory and returns
// structured entries for the frontend history list.
//
// Design constraints:
//   - Memory bounded: body text capped at maxBodyRunes, scanner limited to 256KB/line
//   - Error surfacing: parse errors logged (not silently dropped)
//   - Performance: sort AFTER filtering, file stat batched with parse
package vault

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// maxBodyRunes caps body text per entry to prevent oversized responses.
	// Matches the localStorage entry cap in the frontend.
	maxBodyRunes = 500

	// maxScannerBytes limits bufio.Scanner line buffer to prevent OOM on
	// binary files or extremely long lines accidentally placed in the vault.
	maxScannerBytes = 256 * 1024 // 256KB

	// maxBodyLines stops reading body lines after this count.
	// A 200-line transcription at ~80 chars/line ≈ 16KB — more than enough
	// for the 500-rune preview. Early exit saves memory on large files.
	maxBodyLines = 200
)

// Entry represents a single transcription file from the vault directory.
type Entry struct {
	// File is the absolute path to the vault file.
	File string `json:"vault_file"`

	// Text is the transcription content (body after frontmatter).
	Text string `json:"text"`

	// Timestamp is the ISO-8601 date from frontmatter, or file mod time.
	Timestamp string `json:"timestamp"`

	// Language detected during transcription (from frontmatter).
	Language string `json:"language,omitempty"`

	// Title from frontmatter (e.g. "Dictation").
	Title string `json:"title,omitempty"`
}

// ExpandDir resolves ~/ to the user's home directory and returns the
// absolute path. Exported so callers (main.go) don't duplicate this logic.
func ExpandDir(dir string) string {
	if dir == "" {
		return ""
	}
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

// Scan reads all .md files in dir, parses YAML frontmatter, and returns
// entries sorted by date (newest first). Returns at most maxEntries results.
//
// Parse errors for individual files are logged and counted — never silently
// dropped. If dir is empty or doesn't exist, returns nil without error.
func Scan(dir string, maxEntries int, logger *slog.Logger) ([]Entry, error) {
	if dir == "" {
		return nil, nil
	}

	dir = ExpandDir(dir)

	// Verify directory exists before globbing — fail fast with clear error
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("vault dir stat: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("vault path is not a directory: %s", dir)
	}

	start := time.Now()

	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, fmt.Errorf("glob vault dir: %w", err)
	}

	entries := make([]Entry, 0, min(len(matches), maxEntries))
	var parseErrors int

	for _, path := range matches {
		entry, err := parseVaultFile(path)
		if err != nil {
			parseErrors++
			logger.Debug("skipping vault file", "path", filepath.Base(path), "error", err)
			continue
		}
		entries = append(entries, entry)
	}

	// Sort newest first
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp > entries[j].Timestamp
	})

	if maxEntries > 0 && len(entries) > maxEntries {
		entries = entries[:maxEntries]
	}

	logger.Info("vault scan complete",
		"dir", dir,
		"files_found", len(matches),
		"entries_parsed", len(entries),
		"parse_errors", parseErrors,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return entries, nil
}

// parseVaultFile reads a single .md file with YAML frontmatter.
// Expected format:
//
//	---
//	title: Dictation
//	date: 2026-02-21T11:44:58
//	language: en
//	tags: [dictation, auto-generated]
//	---
//
//	Transcription text here.
//
// Memory bounded: stops reading body after maxBodyLines and caps text at
// maxBodyRunes. Scanner buffer limited to maxScannerBytes.
func parseVaultFile(path string) (Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return Entry{}, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), maxScannerBytes)

	entry := Entry{File: path}

	// State machine: 0=before frontmatter, 1=in frontmatter, 2=body
	state := 0
	var bodyBuilder strings.Builder
	bodyLineCount := 0

	for scanner.Scan() {
		line := scanner.Text()

		switch state {
		case 0:
			if strings.TrimSpace(line) == "---" {
				state = 1
			}
		case 1:
			if strings.TrimSpace(line) == "---" {
				state = 2
				continue
			}
			parseFrontmatterLine(line, &entry)
		case 2:
			bodyLineCount++
			if bodyLineCount > maxBodyLines {
				// Early exit — we have more than enough for a preview
				goto done
			}
			bodyBuilder.WriteString(line)
			bodyBuilder.WriteByte('\n')
		}
	}

done:
	if err := scanner.Err(); err != nil {
		return Entry{}, fmt.Errorf("scan: %w", err)
	}

	// Extract body text — strip markdown formatting for clean preview
	body := cleanMarkdown(bodyBuilder.String())

	// Skip empty files
	if body == "" {
		return Entry{}, fmt.Errorf("empty body in %s", filepath.Base(path))
	}

	// Cap text at maxBodyRunes.
	// WHY runes not bytes? Byte slicing at position 500 can split a multi-byte
	// UTF-8 character (emoji, CJK), producing invalid UTF-8 in the response.
	runes := []rune(body)
	if len(runes) > maxBodyRunes {
		body = string(runes[:maxBodyRunes])
	}
	entry.Text = body

	// Fallback: if no date in frontmatter, use file modification time
	if entry.Timestamp == "" {
		if info, err := os.Stat(path); err == nil {
			entry.Timestamp = info.ModTime().Format(time.RFC3339)
		}
	}

	// Normalize timestamp to RFC3339
	entry.Timestamp = normalizeTimestamp(entry.Timestamp)

	return entry, nil
}

// parseFrontmatterLine extracts a key: value pair from a YAML frontmatter line.
func parseFrontmatterLine(line string, entry *Entry) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	switch key {
	case "title":
		entry.Title = val
	case "date":
		entry.Timestamp = val
	case "language":
		entry.Language = val
	}
}

// cleanMarkdown strips markdown formatting for clean history preview text.
// Removes headers (#), horizontal rules (---), blockquotes (>), and collapses whitespace.
func cleanMarkdown(text string) string {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines and horizontal rules
		if trimmed == "" || trimmed == "---" {
			continue
		}
		// Strip heading prefixes: # ## ### etc.
		if strings.HasPrefix(trimmed, "#") {
			trimmed = strings.TrimLeft(trimmed, "# ")
			trimmed = strings.TrimSpace(trimmed)
			if trimmed == "" {
				continue
			}
		}
		// Strip blockquote prefixes
		trimmed = strings.TrimPrefix(trimmed, "> ")
		// Skip single-rune lines (e.g. stray emoji)
		if len([]rune(trimmed)) <= 1 {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(trimmed)
	}
	return strings.TrimSpace(b.String())
}

// normalizeTimestamp converts various date formats to RFC3339.
func normalizeTimestamp(ts string) string {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return ts // Return as-is if unparseable
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
