// Package vault provides optional autosave of transcriptions to a local directory.
// Designed for integration with Obsidian vaults but works with any filesystem path.
package vault

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Vault manages saving transcriptions to a local directory.
type Vault struct {
	dir       string
	dateFormat string
	fileTitle  string
	logger    *slog.Logger
}

// New creates a new Vault saver. Returns nil if dir is empty (disabled).
// dateFormat uses Go time layout (e.g. "2006-01-02").
// fileTitle is the heading prefix (e.g. "Dictation", "Meeting Notes").
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

// Save appends a transcription to the daily file.
// File format: {dateFormat}.md with timestamped entries and YAML frontmatter.
func (v *Vault) Save(text, language string) (string, error) {
	if v == nil || text == "" {
		return "", nil
	}

	if err := os.MkdirAll(v.dir, 0755); err != nil {
		return "", fmt.Errorf("create vault dir: %w", err)
	}

	now := time.Now()
	today := now.Format(v.dateFormat)
	filename := filepath.Join(v.dir, today+".md")

	// Create file with header if new
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		header := fmt.Sprintf("---\ntags: [dictation, auto-generated]\ndate: %s\n---\n\n# 🎙️ %s — %s\n",
			now.Format("2006-01-02"), v.fileTitle, today)
		if err := os.WriteFile(filename, []byte(header), 0644); err != nil {
			return "", fmt.Errorf("create file: %w", err)
		}
	}

	// Append entry
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	timestamp := now.Format("15:04:05")
	entry := fmt.Sprintf("\n## %s", timestamp)
	if language != "" && language != "und" {
		entry += fmt.Sprintf(" (%s)", language)
	}
	entry += fmt.Sprintf("\n\n%s\n\n---\n", text)

	if _, err := f.WriteString(entry); err != nil {
		return "", fmt.Errorf("write entry: %w", err)
	}

	v.logger.Info("transcription saved to vault", "file", filename)
	return filename, nil
}
