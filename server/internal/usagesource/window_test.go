package usagesource

import "testing"

// maxWindowMinutes mirrors the one-year cap KindMinutes puts on a window length.
const maxWindowMinutes = 366 * 1440

func TestKindMinutesCapsTheLengthNotTheCount(t *testing.T) {
	for _, kind := range []string{"100000w", "527040w", "527040h", "53w", "8785h", "367d", "527041m", "99999999999999999999m"} {
		if got, ok := KindMinutes(kind); ok {
			t.Errorf("%q → %d, longer than a year should not be a window", kind, got)
		}
	}
	for kind, want := range map[string]int{
		"52w": 52 * 10080, "8784h": 8784 * 60, "366d": 366 * 1440, "527040m": 527040, "5h": 300, "weekly_opus": 10080,
	} {
		if got, ok := KindMinutes(kind); !ok || got != want {
			t.Errorf("%q → %d %v, want %d", kind, got, ok, want)
		}
	}
}

func FuzzKindMinutes(f *testing.F) {
	for _, seed := range []string{
		"five_hour", "5h", "weekly", "7d", "seven_day", "hourly", "daily", "monthly", "weekly_fable", "gemini_argon_weekly",
		"45m", "2w", "Five-Hour", "credits", "blocked", "", "gemini", "0h", "weird", "100000w", "53w", "8785h", "527041m",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, kind string) {
		if got, ok := KindMinutes(kind); ok && (got <= 0 || got > maxWindowMinutes) {
			t.Fatalf("%q → %d ok, want 0 < minutes <= %d", kind, got, maxWindowMinutes)
		}
	})
}
