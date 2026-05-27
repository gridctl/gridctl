package api

import (
	"net/http"
	"time"
)

// skillUsageStat is the wire shape for one skill's usage in
// GET /api/skills/usage. LastCalledAt is a pointer rendered as null (not
// omitted) so the documented { calls, lastCalledAt } shape is stable for the
// frontend join even for a skill with a recorded count but no stamped time.
type skillUsageStat struct {
	Calls        int64      `json:"calls"`
	LastCalledAt *time.Time `json:"lastCalledAt"`
}

// skillUsageResponse is the GET /api/skills/usage envelope. Skills maps each
// registry skill name to its cumulative prompts/get usage. ObservedSince is
// when this gateway process began recording; with metrics persistence enabled
// the restored counts may predate it, so the Skills Library uses ObservedSince
// to label the young-tracking-window case rather than calling a skill unused.
// It is rendered as null (not omitted) when no observation window exists.
type skillUsageResponse struct {
	ObservedSince *time.Time                `json:"observedSince"`
	Skills        map[string]skillUsageStat `json:"skills"`
}

// handleSkillsUsage serves GET /api/skills/usage: per-skill cumulative
// prompts/get counts and last-called timestamps from the metrics accumulator.
// The data is seeded from disk on startup
// (telemetry.MetricsFlusher.SeedPromptUsageFromFile) so it survives gateway
// restarts when metrics persistence is enabled. Returns 503 when no
// accumulator is wired, mirroring GET /api/tools/usage and GET /api/optimize.
//
// Skills is always a non-nil object so the JSON body is "{}" rather than null
// when nothing has been served. Usage is joined to the registry list by name
// on the frontend, so the registry list payload stays unchanged.
func (s *Server) handleSkillsUsage(w http.ResponseWriter, _ *http.Request) {
	if s.metricsAccumulator == nil {
		writeJSONError(w, "metrics accumulator not configured", http.StatusServiceUnavailable)
		return
	}

	resp := skillUsageResponse{Skills: map[string]skillUsageStat{}}
	if started := s.metricsAccumulator.StartedAt(); !started.IsZero() {
		t := started.UTC()
		resp.ObservedSince = &t
	}
	for name, stat := range s.metricsAccumulator.PromptUsageSnapshot() {
		entry := skillUsageStat{Calls: stat.Calls}
		if !stat.LastCalledAt.IsZero() {
			t := stat.LastCalledAt.UTC()
			entry.LastCalledAt = &t
		}
		resp.Skills[name] = entry
	}
	writeJSON(w, resp)
}
