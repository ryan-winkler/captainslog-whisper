// Package vault provides optional autosave of transcriptions to a local directory.
// Each transcription is saved as its own file for compatibility with Obsidian, Logseq, and other PKM tools.
package vault

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Vault manages saving transcriptions to a local directory.
type Vault struct {
	dir        string
	dateFormat string
	fileTitle  string
	logger     *slog.Logger
}

// New creates a new Vault saver. Returns nil if dir is empty (disabled).
func New(dir, dateFormat, fileTitle string, logger *slog.Logger) *Vault {
	if dir == "" {
		return nil
	}
	if dateFormat == "" {
		dateFormat = "2006-01-02"
	}
	if fileTitle == "" {
		fileTitle = "Dictation"
	}
	return &Vault{dir: dir, dateFormat: dateFormat, fileTitle: fileTitle, logger: logger}
}

// Save writes a transcription to its own file.
// Filename: {fileTitle} {date} {time}.md — one file per transcription.
func (v *Vault) Save(text, language string) (string, error) {
	if v == nil || text == "" {
		return "", nil
	}

	if err := os.MkdirAll(v.dir, 0755); err != nil {
		return "", fmt.Errorf("create vault dir: %w", err)
	}

	now := time.Now()
	date := now.Format(v.dateFormat)
	timeStr := now.Format("15-04-05")

	// Sanitize file title for filesystem safety
	safeTitle := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '-'
		}
		return r
	}, v.fileTitle)

	filename := filepath.Join(v.dir, fmt.Sprintf("%s %s %s.md", safeTitle, date, timeStr))

	// Build compact markdown content
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %s\n", safeTitle))
	b.WriteString(fmt.Sprintf("date: %s\n", now.Format("2006-01-02T15:04:05")))
	if language != "" && language != "und" {
		b.WriteString(fmt.Sprintf("language: %s\n", language))
	}
	b.WriteString("tags: [dictation, auto-generated]\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(text))
	b.WriteString("\n")

	if err := os.WriteFile(filename, []byte(b.String()), 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	v.logger.Info("transcription saved", "file", filename)
	return filename, nil
}
