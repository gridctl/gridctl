package limits

// RateStatus is one rate limit's configuration snapshot.
type RateStatus struct {
	CallsPerMinute int `json:"calls_per_minute"`
	Burst          int `json:"burst"`
}

// EntryStatus is one limit's snapshot, shared by GET /api/limits and
// `gridctl limits`. Kind is always "rate"; it stays on the wire so consumers
// written against the mixed budget/rate era keep parsing.
type EntryStatus struct {
	Kind string `json:"kind"`
	// Scope is "client", "server", or "tool"; Key is the configured value.
	Scope string `json:"scope"`
	Key   string `json:"key"`
	// State is "ok" or "exceeded" (the bucket is currently empty).
	State string `json:"state"`

	Rate *RateStatus `json:"rate,omitempty"`
}

// StatusReport is the full limits status payload.
type StatusReport struct {
	Configured bool          `json:"configured"`
	Entries    []EntryStatus `json:"entries"`
}

// Status snapshots every configured limit. A nil policy reports
// Configured: false with an empty (non-nil) entry list.
func (p *Policy) Status() StatusReport {
	report := StatusReport{Entries: []EntryStatus{}}
	if p == nil {
		return report
	}
	report.Configured = true
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
