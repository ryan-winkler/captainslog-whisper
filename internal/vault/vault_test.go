package vault

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewEmpty(t *testing.T) {
	v := New("", "", "", slog.Default())
	if v != nil {
		t.Error("New with empty dir should return nil")
	}
}

func TestNewDefaults(t *testing.T) {
	v := New("/tmp/test", "", "", slog.Default())
	if v == nil {
		t.Fatal("New with valid dir should not return nil")
	}
	if v.dateFormat != "2006-01-02" {
		t.Errorf("dateFormat = %q, want default", v.dateFormat)
	}
	if v.fileTitle != "Dictation" {
		t.Errorf("fileTitle = %q, want default", v.fileTitle)
	}
}

func TestSaveNil(t *testing.T) {
	var v *Vault
	file, err := v.Save("test", "en")
	if err != nil || file != "" {
		t.Errorf("Save on nil vault should return empty, got file=%q err=%v", file, err)
	}
}

func TestSaveEmpty(t *testing.T) {
	v := New("/tmp/test-vault", "", "", slog.Default())
	file, err := v.Save("", "en")
	if err != nil || file != "" {
		t.Errorf("Save with empty text should return empty, got file=%q err=%v", file, err)
	}
}

func TestSaveCreatesFile(t *testing.T) {
	dir := t.TempDir()
	v := New(dir, "2006-01-02", "Test Log", slog.Default())

	file, err := v.Save("Hello world", "en")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if file == "" {
		t.Fatal("Save returned empty filename")
	}

	// Check file exists
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("File not created: %v", err)
	}

	// Check content
	content, _ := os.ReadFile(file)
	s := string(content)
	if !strings.Contains(s, "Test Log") {
		t.Error("File should contain custom title")
	}
	if !strings.Contains(s, "Hello world") {
		t.Error("File should contain transcription text")
	}
	if !strings.Contains(s, "tags: [dictation, auto-generated]") {
		t.Error("File should contain YAML frontmatter")
	}
}

func TestSaveCreatesIndividualFiles(t *testing.T) {
	dir := t.TempDir()
	v := New(dir, "2006-01-02", "Dictation", slog.Default())

	file1, _ := v.Save("First entry", "en")
	// Small delay to ensure different timestamp in filename
	time.Sleep(1100 * time.Millisecond)
	file2, _ := v.Save("Second entry", "en")

	// Should create 2 separate files
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	if len(files) != 2 {
		t.Fatalf("Expected 2 files (one per transcription), got %d", len(files))
	}

	content1, _ := os.ReadFile(file1)
	if !strings.Contains(string(content1), "First entry") {
		t.Error("First file should contain first entry")
	}

	content2, _ := os.ReadFile(file2)
	if !strings.Contains(string(content2), "Second entry") {
		t.Error("Second file should contain second entry")
	}
}

func TestCustomDateFormat(t *testing.T) {
	dir := t.TempDir()
	v := New(dir, "02-01-2006", "Notes", slog.Default())

	file, err := v.Save("test", "en")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Filename should use EU date format
	base := filepath.Base(file)
	if !strings.HasSuffix(base, ".md") {
		t.Errorf("Expected .md extension, got %q", base)
	}
	// Should NOT be in ISO format (2026-xx-xx)
	if strings.HasPrefix(base, "2026-") {
		t.Errorf("Expected EU date format, got %q", base)
	}
}
