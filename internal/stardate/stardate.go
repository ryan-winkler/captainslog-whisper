// Package stardate provides TNG-era stardate calculation.
// Stardates follow the format used in Star Trek: The Next Generation onward.
//
// The formula converts real Earth dates to stardates:
//
//	stardate = 100 * (year - 2323) + (dayOfYear / daysInYear * 1000)
//
// This gives stardates that increase by ~1000 per year, matching the
// on-screen progression in TNG/DS9/VOY.
package stardate

import (
	"fmt"
	"time"
)

// FromTime converts a Go time.Time to a TNG-era stardate string.
// Returns a formatted string like "103452.7".
func FromTime(t time.Time) string {
	year := t.Year()
	dayOfYear := float64(t.YearDay())

	// Days in this year
	daysInYear := 365.0
	if isLeapYear(year) {
		daysInYear = 366.0
	}

	// TNG stardate: 100 * (year - 2323) + fraction of year * 1000
	sd := float64(100*(year-2323)) + (dayOfYear/daysInYear)*1000.0

	// Add time-of-day precision
	hourFraction := (float64(t.Hour())*3600 + float64(t.Minute())*60 + float64(t.Second())) / 86400.0
	sd += hourFraction * (1000.0 / daysInYear)

	return fmt.Sprintf("%.1f", sd)
}

// Now returns the current stardate.
func Now() string {
	return FromTime(time.Now())
}

// Format returns a "Captain's log, stardate XXXXX.X" string.
func Format(t time.Time) string {
	return fmt.Sprintf("Captain's log, stardate %s", FromTime(t))
}

func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
