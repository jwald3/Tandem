// Package dates holds the app's calendar helpers. Every date the app stores or
// compares is a local-time YYYY-MM-DD string, and every time of day is a
// 24-hour HH:MM string, so these parse, validate and format those forms.
package dates

import (
	"fmt"
	"strings"
	"time"
)

// Layout is the YYYY-MM-DD format used for every stored date.
const Layout = "2006-01-02"

// TimeLayout is the HH:MM (24-hour) format used for every stored time of day.
const TimeLayout = "15:04"

// now is swappable so tests can pin "today".
var now = time.Now

// Today returns the current local date as YYYY-MM-DD.
func Today() string { return now().Format(Layout) }

// DaysFromToday returns the date n days after today (negative n for before).
func DaysFromToday(n int) string {
	return now().AddDate(0, 0, n).Format(Layout)
}

// Parse parses a YYYY-MM-DD date in local time.
func Parse(s string) (time.Time, error) {
	return time.ParseInLocation(Layout, s, time.Local)
}

// Valid reports whether s is a real YYYY-MM-DD date.
func Valid(s string) bool {
	_, err := Parse(s)
	return err == nil
}

// NormalizeTime accepts "14:00", "9:30", "2pm", "2:30 PM" and returns the
// canonical HH:MM, or an error. An empty string stays empty (no time).
func NormalizeTime(s string) (string, error) {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if s == "" {
		return "", nil
	}
	for _, layout := range []string{"15:04", "3:04PM", "3PM", "15"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(TimeLayout), nil
		}
	}
	return "", fmt.Errorf("unrecognized time %q (use HH:MM, 24-hour)", s)
}

// Weekday returns the full weekday name of a YYYY-MM-DD date, or "".
func Weekday(s string) string {
	t, err := Parse(s)
	if err != nil {
		return ""
	}
	return t.Weekday().String()
}

// Pretty renders a date as "Mon, Sep 28", adding the year when it isn't the
// current one. Unparseable input is returned unchanged.
func Pretty(s string) string {
	t, err := Parse(s)
	if err != nil {
		return s
	}
	if t.Year() != now().Year() {
		return t.Format("Mon, Jan 2, 2006")
	}
	return t.Format("Mon, Jan 2")
}

// Long renders a date as "Monday, 2026-09-28" — unambiguous for the model.
func Long(s string) string {
	if wd := Weekday(s); wd != "" {
		return wd + ", " + s
	}
	return s
}

// Clock renders an HH:MM time as "2:00 PM", or "" for no time.
func Clock(hhmm string) string {
	t, err := time.Parse(TimeLayout, hhmm)
	if err != nil {
		return hhmm
	}
	return t.Format("3:04 PM")
}

// DaysUntil returns how many calendar days from today until date (negative
// when it's in the past). ok is false for an unparseable date.
func DaysUntil(date string) (days int, ok bool) {
	t, err := Parse(date)
	if err != nil {
		return 0, false
	}
	today, _ := Parse(Today())
	// Round rather than truncate so DST shifts don't cost a day.
	return int(t.Sub(today).Round(24*time.Hour).Hours() / 24), true
}

// Relative renders a date relative to today: "today", "tomorrow",
// "yesterday", "in 5 days", "3 days ago". Beyond two weeks it falls back to
// Pretty.
func Relative(date string) string {
	d, ok := DaysUntil(date)
	if !ok {
		return date
	}
	switch {
	case d == 0:
		return "today"
	case d == 1:
		return "tomorrow"
	case d == -1:
		return "yesterday"
	case d > 1 && d <= 14:
		return fmt.Sprintf("in %d days", d)
	case d < -1 && d >= -14:
		return fmt.Sprintf("%d days ago", -d)
	}
	return Pretty(date)
}
