package limits

import "time"

// BudgetStatus is one budget's consumption within its current window. All
// numeric fields are always present: a zero spent_usd is a real zero, not
// an unknown (the UI's em-dash convention is for absent data only).
type BudgetStatus struct {
	MaxUSD        float64   `json:"max_usd"`
	SpentUSD      float64   `json:"spent_usd"`
	Percent       float64   `json:"percent"`
	Period        string    `json:"period"`
	WarnAtPercent int       `json:"warn_at_percent,omitempty"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
}

// RateStatus is one rate limit's configuration snapshot.
type RateStatus struct {
	CallsPerMinute int `json:"calls_per_minute"`
	Burst          int `json:"burst"`
}

// EntryStatus is one limit's snapshot, shared by GET /api/limits and
// `gridctl limits`. Exactly one of Budget or Rate is set, matching Kind.
type EntryStatus struct {
	// Kind is "budget" or "rate".
	Kind string `json:"kind"`
	// Scope is "client", "server", or "tool"; Key is the configured value.
	Scope string `json:"scope"`
	Key   string `json:"key"`
	// State is "ok", "warn" (budget past its warn threshold), or "exceeded".
	State string `json:"state"`

	Budget *BudgetStatus `json:"budget,omitempty"`
	Rate   *RateStatus   `json:"rate,omitempty"`
}

// StatusReport is the full limits status payload.
type StatusReport struct {
	Configured bool          `json:"configured"`
	Entries    []EntryStatus `json:"entries"`
}

// Status snapshots every configured limit. Budget windows are rolled first
// so a report requested after midnight never shows yesterday's spend. A nil
// policy reports Configured: false with an empty (non-nil) entry list.
func (p *Policy) Status() StatusReport {
	report := StatusReport{Entries: []EntryStatus{}}
	if p == nil {
		return report
	}
	report.Configured = true
	now := p.now()
	for _, e := range p.budgets {
		e.mu.Lock()
		if e.roll(now) {
			p.markDirty()
		}
		spent, maxM, warned := e.spentMicro, e.maxMicro, e.warned
		ws := e.windowStart
		e.mu.Unlock()
		budget := &BudgetStatus{
			MaxUSD:        float64(maxM) / microPerUSD,
			SpentUSD:      float64(spent) / microPerUSD,
			Period:        string(e.period),
			WarnAtPercent: e.warnAtPercent,
			WindowStart:   ws,
			WindowEnd:     windowEnd(e.period, ws),
		}
		if maxM > 0 {
			budget.Percent = float64(spent) / float64(maxM) * 100
		}
		st := EntryStatus{Kind: "budget", Scope: e.scope, Key: e.rawKey, State: "ok", Budget: budget}
		switch {
		case spent >= maxM:
			st.State = "exceeded"
		case warned:
			st.State = "warn"
		}
		report.Entries = append(report.Entries, st)
	}
	for _, e := range p.rates {
		st := EntryStatus{
			Kind:  "rate",
			Scope: e.scope,
			Key:   e.rawKey,
			State: "ok",
			Rate:  &RateStatus{CallsPerMinute: e.perMinute, Burst: e.burst},
		}
		if e.limiter.Tokens() < 1 {
			st.State = "exceeded"
		}
		report.Entries = append(report.Entries, st)
	}
	return report
}
