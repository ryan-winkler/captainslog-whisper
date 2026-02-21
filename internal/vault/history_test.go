package vault

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testLogger returns a no-op logger for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- Scan tests ---

func TestScanEmptyDir(t *testing.T) {
	entries, err := Scan("", 100, testLogger())
	if err != nil || entries != nil {
		t.Errorf("Scan empty dir: got entries=%v err=%v, want nil/nil", entries, err)
	}
}

func TestScanNonexistentDir(t *testing.T) {
	_, err := Scan("/nonexistent/path/that/does/not/exist", 100, testLogger())
	if err == nil {
		t.Error("Scan nonexistent dir should return error")
	}
}

func TestScanNotADirectory(t *testing.T) {
	f, _ := os.CreateTemp("", "notadir*.md")
	f.Close()
	defer os.Remove(f.Name())

	_, err := Scan(f.Name(), 100, testLogger())
	if err == nil {
		t.Error("Scan on a file (not dir) should return error")
	}
}

func TestScanValidEntries(t *testing.T) {
	dir := t.TempDir()

	files := []struct {
		name    string
		content string
	}{
		{"entry1.md", "---\ntitle: Test\ndate: 2026-02-20T10:00:00\nlanguage: en\ntags: [test]\n---\n\nFirst entry text.\n"},
		{"entry2.md", "---\ntitle: Test\ndate: 2026-02-21T10:00:00\n---\n\nSecond entry text.\n"},
		{"entry3.md", "---\ntitle: Test\ndate: 2026-02-19T10:00:00\n---\n\nThird entry text.\n"},
	}

	for _, f := range files {
		os.WriteFile(filepath.Join(dir, f.name), []byte(f.content), 0644)
	}

	entries, err := Scan(dir, 100, testLogger())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(entries))
	}

	// Should be sorted newest first
	if !strings.Contains(entries[0].Text, "Second") {
		t.Errorf("First entry should be newest (Second), got %q", entries[0].Text)
	}
	if !strings.Contains(entries[2].Text, "Third") {
		t.Errorf("Last entry should be oldest (Third), got %q", entries[2].Text)
	}

	// Entry with language=en is entry1 (2026-02-20), which sorts to index 1
	if entries[1].Language != "en" {
		t.Errorf("Entry with language should have Language=en, got %q", entries[1].Language)
	}
}

func TestScanMaxEntries(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 5; i++ {
		content := "---\ntitle: Test\ndate: 2026-02-20T10:00:0" + string(rune('0'+i)) + "\n---\n\nEntry text here.\n"
		os.WriteFile(filepath.Join(dir, "entry"+string(rune('0'+i))+".md"), []byte(content), 0644)
	}

	entries, err := Scan(dir, 3, testLogger())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("Expected 3 entries (capped), got %d", len(entries))
	}
}

func TestScanSkipsEmptyFiles(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "valid.md"),
		[]byte("---\ntitle: Test\ndate: 2026-02-20\n---\n\nValid content.\n"), 0644)
	os.WriteFile(filepath.Join(dir, "empty.md"),
		[]byte("---\ntitle: Test\ndate: 2026-02-20\n---\n\n"), 0644)

	entries, err := Scan(dir, 100, testLogger())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry (empty body skipped), got %d", len(entries))
	}
}

func TestScanSkipsNonMdFiles(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "valid.md"),
		[]byte("---\ntitle: Test\ndate: 2026-02-20\n---\n\nContent.\n"), 0644)
	os.WriteFile(filepath.Join(dir, "not-markdown.txt"),
		[]byte("This is a text file"), 0644)
	os.WriteFile(filepath.Join(dir, "image.png"),
		[]byte{0x89, 0x50, 0x4e, 0x47}, 0644)

	entries, err := Scan(dir, 100, testLogger())
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Expected 1 .md entry, got %d", len(entries))
	}
}

// --- parseVaultFile tests ---

func TestParseVaultFileValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	os.WriteFile(path, []byte("---\ntitle: Dictation\ndate: 2026-02-21T11:44:58\nlanguage: en\ntags: [dictation, auto-generated]\n---\n\nHello world, this is a test.\n"), 0644)

	entry, err := parseVaultFile(path)
	if err != nil {
		t.Fatalf("parseVaultFile failed: %v", err)
	}

	if entry.Title != "Dictation" {
		t.Errorf("Title = %q, want Dictation", entry.Title)
	}
	if entry.Language != "en" {
		t.Errorf("Language = %q, want en", entry.Language)
	}
	if !strings.Contains(entry.Text, "Hello world") {
		t.Errorf("Text should contain 'Hello world', got %q", entry.Text)
	}
	if entry.File != path {
		t.Errorf("File = %q, want %q", entry.File, path)
	}
}

func TestParseVaultFileMissingDate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodate.md")
	os.WriteFile(path, []byte("---\ntitle: Test\n---\n\nSome content.\n"), 0644)

	entry, err := parseVaultFile(path)
	if err != nil {
		t.Fatalf("parseVaultFile failed: %v", err)
	}

	if entry.Timestamp == "" {
		t.Error("Timestamp should fallback to file mod time, got empty")
	}
	if !strings.Contains(entry.Timestamp, "T") {
		t.Errorf("Timestamp should be RFC3339 format, got %q", entry.Timestamp)
	}
}

func TestParseVaultFileEmptyBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.md")
	os.WriteFile(path, []byte("---\ntitle: Test\ndate: 2026-02-20\n---\n"), 0644)

	_, err := parseVaultFile(path)
	if err == nil {
		t.Error("parseVaultFile with empty body should return error")
	}
}

func TestParseVaultFileNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.md")
	os.WriteFile(path, []byte("Just plain text without frontmatter.\n"), 0644)

	_, err := parseVaultFile(path)
	if err == nil {
		t.Error("parseVaultFile with no frontmatter should return error (empty body)")
	}
}

func TestParseVaultFileUnicode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unicode.md")
	content := "---\ntitle: 記録\ndate: 2026-02-21\nlanguage: ja\n---\n\nこんにちは世界。日本語テスト。🎙️ 録音テスト。\n"
	os.WriteFile(path, []byte(content), 0644)

	entry, err := parseVaultFile(path)
	if err != nil {
		t.Fatalf("parseVaultFile failed: %v", err)
	}

	if entry.Title != "記録" {
		t.Errorf("Title = %q, want 記録", entry.Title)
	}
	if !strings.Contains(entry.Text, "こんにちは世界") {
		t.Errorf("Text should contain Japanese text, got %q", entry.Text)
	}
}

func TestParseVaultFileBodyCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.md")

	longText := strings.Repeat("abcdefghij", 100) // 1000 chars
	content := "---\ntitle: Long\ndate: 2026-02-21\n---\n\n" + longText + "\n"
	os.WriteFile(path, []byte(content), 0644)

	entry, err := parseVaultFile(path)
	if err != nil {
		t.Fatalf("parseVaultFile failed: %v", err)
	}

	if len([]rune(entry.Text)) > maxBodyRunes {
		t.Errorf("Text should be capped at %d runes, got %d", maxBodyRunes, len([]rune(entry.Text)))
	}
}

// --- cleanMarkdown tests ---

func TestCleanMarkdownHeaders(t *testing.T) {
	input := "# Main Title\n## Sub Title\n### Section\nActual content here."
	result := cleanMarkdown(input)

	if strings.Contains(result, "#") {
		t.Errorf("Should strip # prefixes, got %q", result)
	}
	if !strings.Contains(result, "Main Title") {
		t.Errorf("Should preserve header text, got %q", result)
	}
	if !strings.Contains(result, "Actual content") {
		t.Errorf("Should preserve body text, got %q", result)
	}
}

func TestCleanMarkdownHorizontalRules(t *testing.T) {
	input := "First paragraph.\n---\nSecond paragraph."
	result := cleanMarkdown(input)

	if strings.Contains(result, "---") {
		t.Errorf("Should strip horizontal rules, got %q", result)
	}
	if !strings.Contains(result, "First") || !strings.Contains(result, "Second") {
		t.Errorf("Should preserve paragraphs, got %q", result)
	}
}

func TestCleanMarkdownBlockquotes(t *testing.T) {
	input := "> Quoted text\nNormal text"
	result := cleanMarkdown(input)

	if strings.Contains(result, "> ") {
		t.Errorf("Should strip blockquote prefix, got %q", result)
	}
	if !strings.Contains(result, "Quoted text") {
		t.Errorf("Should preserve quoted text content, got %q", result)
	}
}

func TestCleanMarkdownEmpty(t *testing.T) {
	result := cleanMarkdown("\n\n\n")
	if result != "" {
		t.Errorf("Empty input should return empty, got %q", result)
	}
}

// --- normalizeTimestamp tests ---

func TestNormalizeTimestampRFC3339(t *testing.T) {
	input := "2026-02-21T11:44:58Z"
	result := normalizeTimestamp(input)
	if result != input {
		t.Errorf("RFC3339 should pass through, got %q", result)
	}
}

func TestNormalizeTimestampISO(t *testing.T) {
	result := normalizeTimestamp("2026-02-21T11:44:58")
	if !strings.Contains(result, "2026-02-21") {
		t.Errorf("ISO datetime should normalize, got %q", result)
	}
}

func TestNormalizeTimestampDateOnly(t *testing.T) {
	result := normalizeTimestamp("2026-02-21")
	if !strings.HasPrefix(result, "2026-02-21") {
		t.Errorf("Date-only should normalize to RFC3339, got %q", result)
	}
}

func TestNormalizeTimestampUnparseable(t *testing.T) {
	input := "not-a-date"
	result := normalizeTimestamp(input)
	if result != input {
		t.Errorf("Unparseable should return as-is, got %q", result)
	}
}

// --- ExpandDir tests ---

func TestExpandDirEmpty(t *testing.T) {
	result := ExpandDir("")
	if result != "" {
		t.Errorf("Empty should return empty, got %q", result)
	}
}

func TestExpandDirAbsolute(t *testing.T) {
	result := ExpandDir("/tmp/test")
	if result != "/tmp/test" {
		t.Errorf("Absolute path should pass through, got %q", result)
	}
}

func TestExpandDirTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	result := ExpandDir("~/Documents")
	expected := filepath.Join(home, "Documents")
	if result != expected {
		t.Errorf("Tilde should expand, got %q want %q", result, expected)
	}
}

// --- Daily aggregate file tests ---

func TestParseVaultFileDailyAggregate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-02-20.md")
	content := "---\ntags: [dictation, auto-generated]\ndate: 2026-02-20\n---\n\n# 🎙️ Dictation — 2026-02-20\n\n## 12:52:05 (en)\n\nHello, hello, hello, hello, hello.\n\n---\n\n## 14:06:46 (en)\n\nsecurity audit test\n\n---\n\n## 14:12:44 (en)\n\nQoL hardening test\n"
	os.WriteFile(path, []byte(content), 0644)

	entry, err := parseVaultFile(path)
	if err != nil {
		t.Fatalf("parseVaultFile failed: %v", err)
	}

	if !strings.Contains(entry.Text, "Hello") {
		t.Errorf("Should contain first entry text, got %q", entry.Text)
	}
	if !strings.Contains(entry.Text, "security audit test") {
		t.Errorf("Should contain second entry text, got %q", entry.Text)
	}
}
