// Package vault — history scanning.
// Reads saved transcription files from the vault directory and returns
// structured entries for the frontend history list.
package vault

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// Scan reads all .md files in dir, parses YAML frontmatter, and returns
// entries sorted by date (newest first). Returns at most maxEntries results.
// If dir is empty or doesn't exist, returns nil without error.
func Scan(dir string, maxEntries int) ([]Entry, error) {
	if dir == "" {
		return nil, nil
	}

	// Expand ~/
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(matches))
	for _, path := range matches {
		entry, err := parseVaultFile(path)
		if err != nil {
			continue // Skip unparseable files
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
func parseVaultFile(path string) (Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	entry := Entry{File: path}

	// State machine: 0=before frontmatter, 1=in frontmatter, 2=body
	state := 0
	var bodyLines []string

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
			// Parse simple YAML key: value
			if idx := strings.Index(line, ":"); idx > 0 {
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
		case 2:
			bodyLines = append(bodyLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return Entry{}, err
	}

	// Extract body text — strip markdown formatting for clean preview
	body := cleanMarkdown(strings.Join(bodyLines, "\n"))

	// Skip empty files
	if body == "" {
		return Entry{}, fmt.Errorf("empty body")
	}

	// Cap text at 500 chars to match localStorage entries
	if len(body) > 500 {
		body = body[:500]
	}
	entry.Text = body

	// Fallback: if no date in frontmatter, use file modification time
	if entry.Timestamp == "" {
		if info, err := os.Stat(path); err == nil {
			entry.Timestamp = info.ModTime().Format(time.RFC3339)
		}
	}

	// Normalize timestamp to RFC3339 if it's just a date or datetime
	entry.Timestamp = normalizeTimestamp(entry.Timestamp)

	return entry, nil
}

// cleanMarkdown strips markdown formatting for clean history preview text.
// Removes headers (#), horizontal rules (---), blockquotes (>), and collapses whitespace.
func cleanMarkdown(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip empty lines, horizontal rules, and markdown headers
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
		if strings.HasPrefix(trimmed, "> ") {
			trimmed = strings.TrimPrefix(trimmed, "> ")
		}
		// Strip emoji-only lines (like 🎙️ headers)
		if len(trimmed) <= 4 {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

// normalizeTimestamp converts various date formats to ISO-8601.
func normalizeTimestamp(ts string) string {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, fmt := range formats {
		if t, err := time.Parse(fmt, ts); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return ts // Return as-is if unparseable
}
