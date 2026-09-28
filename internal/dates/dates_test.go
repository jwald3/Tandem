package dates

import (
	"testing"
	"time"
)

// pin fixes "today" at Sunday 2026-09-27 for the duration of a test.
func pin(t *testing.T) {
	t.Helper()
	fixed := time.Date(2026, 9, 27, 10, 0, 0, 0, time.Local)
	now = func() time.Time { return fixed }
	t.Cleanup(func() { now = time.Now })
}

func TestNormalizeTime(t *testing.T) {
	for in, want := range map[string]string{
		"":        "",
		"14:00":   "14:00",
		"9:30":    "09:30",
		"2pm":     "14:00",
		"2:30 PM": "14:30",
		"12am":    "00:00",
		"9":       "09:00",
	} {
		got, err := NormalizeTime(in)
		if err != nil || got != want {
			t.Errorf("NormalizeTime(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"25:00", "noon-ish", "14:99"} {
		if _, err := NormalizeTime(bad); err == nil {
			t.Errorf("NormalizeTime(%q) should fail", bad)
		}
	}
}

func TestRelativeAndPretty(t *testing.T) {
	pin(t)
	for in, want := range map[string]string{
		"2026-09-27": "today",
		"2026-09-28": "tomorrow",
		"2026-09-26": "yesterday",
		"2026-10-02": "in 5 days",
		"2026-09-24": "3 days ago",
		"2026-12-25": "Fri, Dec 25",
		"2027-01-04": "Mon, Jan 4, 2027",
	} {
		if got := Relative(in); got != want {
			t.Errorf("Relative(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Long("2026-09-28"); got != "Monday, 2026-09-28" {
		t.Errorf("Long = %q", got)
	}
	if got := Clock("14:05"); got != "2:05 PM" {
		t.Errorf("Clock = %q", got)
	}
	if Valid("2026-02-30") || !Valid("2026-02-28") {
		t.Error("Valid should reject impossible dates")
	}
}
