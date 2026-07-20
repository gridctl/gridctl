package limits

import "time"

// Period is a budget's calendar reset cadence.
type Period string

// Budget reset periods. Windows are calendar-aligned in the supplied time's
// location (in production, the daemon's local timezone): daily resets at
// midnight, weekly on Monday 00:00, monthly on the 1st at 00:00. This is a
// deliberate divergence from cloud gateways' UTC alignment: a single
// operator's "five dollars a day" means their day.
const (
	PeriodDaily   Period = "daily"
	PeriodWeekly  Period = "weekly"
	PeriodMonthly Period = "monthly"
)

// windowStart returns the start of the calendar window containing now.
// time.Date normalizes out-of-range days (and DST-nonexistent midnights), so
// month rolls and spring-forward days resolve to real instants.
func windowStart(period Period, now time.Time) time.Time {
	y, m, d := now.Date()
	loc := now.Location()
	switch period {
	case PeriodWeekly:
		// Back up to Monday. Weekday() is Sunday=0, so shift to Monday=0.
		back := (int(now.Weekday()) + 6) % 7
		return time.Date(y, m, d-back, 0, 0, 0, 0, loc)
	case PeriodMonthly:
		return time.Date(y, m, 1, 0, 0, 0, 0, loc)
	default: // PeriodDaily
		return time.Date(y, m, d, 0, 0, 0, 0, loc)
	}
}

// windowEnd returns the start of the window after the one beginning at
// start. AddDate handles DST transitions and month lengths.
func windowEnd(period Period, start time.Time) time.Time {
	switch period {
	case PeriodWeekly:
		return start.AddDate(0, 0, 7)
	case PeriodMonthly:
		return start.AddDate(0, 1, 0)
	default: // PeriodDaily
		return start.AddDate(0, 0, 1)
	}
}
