package vault

import (
	"os"
	"strings"
	"testing"
)

// --- cleanMarkdown tests ---

func TestCleanMarkdown_Headers(t *testing.T) {
	input := "# Title\n## Subtitle\n### Section\nActual content here"
	got := cleanMarkdown(input)
	if strings.Contains(got, "#") {
		t.Errorf("should strip headers, got %q", got)
	}
	if !strings.Contains(got, "Title") {
		t.Error("should keep header text after stripping #")
	}
	if !strings.Contains(got, "Actual content here") {
		t.Error("should keep body text")
	}
}

func TestCleanMarkdown_HorizontalRules(t *testing.T) {
	input := "Before\n---\nAfter"
	got := cleanMarkdown(input)
	if strings.Contains(got, "---") {
		t.Errorf("should strip horizontal rules, got %q", got)
	}
	if !strings.Contains(got, "Before") || !strings.Contains(got, "After") {
		t.Errorf("should keep surrounding text, got %q", got)
	}
}

func TestCleanMarkdown_Blockquotes(t *testing.T) {
	input := "> This is a quote\nNormal text"
	got := cleanMarkdown(input)
	if strings.HasPrefix(got, ">") {
		t.Errorf("should strip blockquote prefix, got %q", got)
	}
	if !strings.Contains(got, "This is a quote") {
		t.Error("should keep blockquote text")
	}
}

func TestCleanMarkdown_EmptyLines(t *testing.T) {
	input := "First\n\n\n\nSecond"
	got := cleanMarkdown(input)
	if got != "First Second" {
		t.Errorf("should collapse empty lines, got %q", got)
	}
}

func TestCleanMarkdown_ShortLines(t *testing.T) {
	// Single-character lines should be dropped (emoji/bullets)
	// But multi-character short words should be kept
	input := "🎙\nOK\nNo\nYes\nHi"
	got := cleanMarkdown(input)
	if !strings.Contains(got, "OK") {
		t.Errorf("should keep 'OK', got %q", got)
	}
	if !strings.Contains(got, "No") {
		t.Errorf("should keep 'No', got %q", got)
	}
}

func TestCleanMarkdown_Empty(t *testing.T) {
	if got := cleanMarkdown(""); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestCleanMarkdown_OnlyMarkdown(t *testing.T) {
	input := "# \n---\n> \n"
	got := cleanMarkdown(input)
	if got != "" {
		t.Errorf("markdown-only input should return empty, got %q", got)
	}
}

// --- normalizeTimestamp tests ---

func TestNormalizeTimestamp_RFC3339(t *testing.T) {
	input := "2026-02-21T12:00:00Z"
	got := normalizeTimestamp(input)
	if got != input {
		t.Errorf("RFC3339 should pass through, got %q", got)
	}
}

func TestNormalizeTimestamp_DateTimeNoTZ(t *testing.T) {
	input := "2026-02-21T12:00:00"
	got := normalizeTimestamp(input)
	if !strings.HasPrefix(got, "2026-02-21") {
		t.Errorf("should normalize to RFC3339, got %q", got)
	}
}

func TestNormalizeTimestamp_DateTimeSpace(t *testing.T) {
	input := "2026-02-21 12:00:00"
	got := normalizeTimestamp(input)
	if !strings.HasPrefix(got, "2026-02-21") {
		t.Errorf("should normalize space-separated datetime, got %q", got)
	}
}

func TestNormalizeTimestamp_DateOnly(t *testing.T) {
	input := "2026-02-21"
	got := normalizeTimestamp(input)
	if !strings.HasPrefix(got, "2026-02-21") {
		t.Errorf("should normalize date-only, got %q", got)
	}
}

func TestNormalizeTimestamp_Unparseable(t *testing.T) {
	input := "not-a-date"
	got := normalizeTimestamp(input)
	if got != input {
		t.Errorf("unparseable should return as-is, got %q", got)
	}
}

// --- UTF-8 truncation tests ---

func TestParseVaultFile_UTF8Truncation(t *testing.T) {
	// Create a test file with emoji content that would break at byte boundary
	content := "---\ntitle: Test\ndate: 2026-02-21T12:00:00\n---\n" +
		strings.Repeat("🎵", 300) // 300 emoji = 1200 bytes but 300 runes

	dir := t.TempDir()
	path := dir + "/test.md"
	if err := writeTestFile(path, content); err != nil {
		t.Fatal(err)
	}

	entry, err := parseVaultFile(path)
	if err != nil {
		t.Fatalf("parseVaultFile: %v", err)
	}

	// With 300 emoji (each 4 bytes), byte slicing at 500 would split an emoji.
	// Rune slicing should give us exactly 300 runes (< 500 cap, so no truncation).
	runes := []rune(entry.Text)
	for i, r := range runes {
		if r == '\uFFFD' { // Unicode replacement character = broken UTF-8
			t.Fatalf("found broken UTF-8 at rune position %d", i)
		}
	}
}

func TestParseVaultFile_LongText(t *testing.T) {
	// 600 ASCII chars should be capped at 500 runes
	longText := strings.Repeat("a", 600)
	content := "---\ntitle: Test\ndate: 2026-02-21\n---\n" + longText

	dir := t.TempDir()
	path := dir + "/test.md"
	if err := writeTestFile(path, content); err != nil {
		t.Fatal(err)
	}

	entry, err := parseVaultFile(path)
	if err != nil {
		t.Fatalf("parseVaultFile: %v", err)
	}
	if len([]rune(entry.Text)) > 500 {
		t.Errorf("text should be capped at 500 runes, got %d", len([]rune(entry.Text)))
	}
}

// --- Scan tests ---

func TestScan_EmptyDir(t *testing.T) {
	entries, err := Scan("", 10)
	if err != nil || entries != nil {
		t.Errorf("Scan('') should return nil,nil — got %v,%v", entries, err)
	}
}

func TestScan_NonexistentDir(t *testing.T) {
	entries, err := Scan("/nonexistent/path", 10)
	if err != nil {
		t.Errorf("Scan of nonexistent dir should not error, got %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Scan of nonexistent dir should return 0 entries, got %d", len(entries))
	}
}

func TestScan_MaxEntries(t *testing.T) {
	dir := t.TempDir()
	// Create 5 files
	for i := 0; i < 5; i++ {
		content := "---\ntitle: Test\ndate: 2026-02-21T12:00:00\n---\nEntry content number " + strings.Repeat("x", 10)
		writeTestFile(dir+"/test"+string(rune('A'+i))+".md", content)
	}

	entries, err := Scan(dir, 3)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("Scan with maxEntries=3 should return 3 entries, got %d", len(entries))
	}
}

func TestScan_SkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	// File with no body
	writeTestFile(dir+"/empty.md", "---\ntitle: Test\ndate: 2026-02-21\n---\n\n")
	// File with body
	writeTestFile(dir+"/full.md", "---\ntitle: Test\ndate: 2026-02-21\n---\nHello world content")

	entries, err := Scan(dir, 10)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("should skip empty body files, got %d entries", len(entries))
	}
}

// --- helper ---

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
