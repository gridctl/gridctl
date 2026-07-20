package limits

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("timezone database unavailable for %s: %v", name, err)
	}
	return loc
}

func TestWindowStart(t *testing.T) {
	ny := mustLoc(t, "America/New_York")

	tests := []struct {
		name   string
		period Period
		now    time.Time
		want   time.Time
	}{
		{
			"daily plain",
			PeriodDaily,
			time.Date(2026, 7, 20, 15, 30, 0, 0, ny),
			time.Date(2026, 7, 20, 0, 0, 0, 0, ny),
		},
		{
			// 2026-03-08 is the US spring-forward day; midnight exists but the
			// day is 23 hours long. The window must still start at 00:00.
			"daily on DST spring-forward day",
			PeriodDaily,
			time.Date(2026, 3, 8, 12, 0, 0, 0, ny),
			time.Date(2026, 3, 8, 0, 0, 0, 0, ny),
		},
		{
			// 2026-07-20 is a Monday; the weekly window starts that same day.
			"weekly on a Monday",
			PeriodWeekly,
			time.Date(2026, 7, 20, 9, 0, 0, 0, ny),
			time.Date(2026, 7, 20, 0, 0, 0, 0, ny),
		},
		{
			// 2026-07-26 is a Sunday; the week began the prior Monday.
			"weekly on a Sunday",
			PeriodWeekly,
			time.Date(2026, 7, 26, 23, 59, 0, 0, ny),
			time.Date(2026, 7, 20, 0, 0, 0, 0, ny),
		},
		{
			// A week that crosses spring-forward: Sunday 2026-03-08 belongs to
			// the week starting Monday 2026-03-02.
			"weekly across DST",
			PeriodWeekly,
			time.Date(2026, 3, 8, 12, 0, 0, 0, ny),
			time.Date(2026, 3, 2, 0, 0, 0, 0, ny),
		},
		{
			"monthly mid-month",
			PeriodMonthly,
			time.Date(2026, 2, 28, 12, 0, 0, 0, ny),
			time.Date(2026, 2, 1, 0, 0, 0, 0, ny),
		},
		{
			"monthly on the 31st",
			PeriodMonthly,
			time.Date(2026, 1, 31, 23, 0, 0, 0, ny),
			time.Date(2026, 1, 1, 0, 0, 0, 0, ny),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := windowStart(tc.period, tc.now)
			if !got.Equal(tc.want) {
				t.Errorf("windowStart(%s, %s) = %s, want %s", tc.period, tc.now, got, tc.want)
			}
		})
	}
}

func TestWindowEnd(t *testing.T) {
	ny := mustLoc(t, "America/New_York")

	tests := []struct {
		name   string
		period Period
		start  time.Time
		want   time.Time
	}{
		{
			"daily",
			PeriodDaily,
			time.Date(2026, 7, 20, 0, 0, 0, 0, ny),
			time.Date(2026, 7, 21, 0, 0, 0, 0, ny),
		},
		{
			// The spring-forward day is 23 hours; AddDate still lands on the
			// next calendar midnight, not 24 wall-clock hours later.
			"daily across DST",
			PeriodDaily,
			time.Date(2026, 3, 8, 0, 0, 0, 0, ny),
			time.Date(2026, 3, 9, 0, 0, 0, 0, ny),
		},
		{
			"weekly",
			PeriodWeekly,
			time.Date(2026, 7, 20, 0, 0, 0, 0, ny),
			time.Date(2026, 7, 27, 0, 0, 0, 0, ny),
		},
		{
			// January 31 + one month must land on March 1 for the NEXT window
			// start only when starting from the 1st; window starts are always
			// the 1st, so this is the well-defined case.
			"monthly January",
			PeriodMonthly,
			time.Date(2026, 1, 1, 0, 0, 0, 0, ny),
			time.Date(2026, 2, 1, 0, 0, 0, 0, ny),
		},
		{
			"monthly February non-leap",
			PeriodMonthly,
			time.Date(2026, 2, 1, 0, 0, 0, 0, ny),
			time.Date(2026, 3, 1, 0, 0, 0, 0, ny),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := windowEnd(tc.period, tc.start)
			if !got.Equal(tc.want) {
				t.Errorf("windowEnd(%s, %s) = %s, want %s", tc.period, tc.start, got, tc.want)
			}
		})
	}
}
