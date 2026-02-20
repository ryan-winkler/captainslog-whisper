package stardate

import (
	"strings"
	"testing"
	"time"
)

func TestNow(t *testing.T) {
	sd := Now()
	if sd == "" {
		t.Error("Now() returned empty string")
	}
	// Should contain a decimal point
	if !strings.Contains(sd, ".") {
		t.Errorf("Now() = %q, expected decimal format", sd)
	}
}

func TestFormat(t *testing.T) {
	now := time.Now()
	formatted := Format(now)
	if !strings.HasPrefix(formatted, "Captain's log, stardate") {
		t.Errorf("Format() = %q, expected prefix 'Captain's log, stardate'", formatted)
	}
}

func TestFromTimeKnownDate(t *testing.T) {
	// For 2026: 100 * (2026 - 2323) = -29700 + fraction -> negative
	date := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sd := FromTime(date)
	if sd == "" {
		t.Error("FromTime returned empty")
	}
	// Should start with "-" for pre-TNG era
	if sd[0] != '-' {
		t.Errorf("FromTime(2026-01-01) = %q, expected negative for pre-TNG era", sd)
	}
}
