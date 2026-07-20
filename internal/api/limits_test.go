package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/limits"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getLimits(t *testing.T, s *Server) (int, limits.StatusReport) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/limits", nil)
	w := httptest.NewRecorder()
	s.handleLimits(w, req)
	var report limits.StatusReport
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	}
	return w.Code, report
}

func TestHandleLimits_Unwired(t *testing.T) {
	code, report := getLimits(t, &Server{})

	assert.Equal(t, http.StatusOK, code)
	assert.False(t, report.Configured)
	assert.NotNil(t, report.Entries)
	assert.Empty(t, report.Entries)
}

func TestHandleLimits_ReturnsStatusSnapshot(t *testing.T) {
	s := &Server{}
	snapshot := limits.StatusReport{
		Configured: true,
		Entries: []limits.EntryStatus{
			{Kind: "budget", Scope: "client", Key: "claude-code", State: "ok",
				Budget: &limits.BudgetStatus{MaxUSD: 5, SpentUSD: 1.25, Percent: 25, Period: "daily"}},
			{Kind: "rate", Scope: "server", Key: "github", State: "exceeded",
				Rate: &limits.RateStatus{CallsPerMinute: 30, Burst: 10}},
		},
	}
	s.SetLimitsStatusFunc(func() limits.StatusReport { return snapshot })

	code, report := getLimits(t, s)

	assert.Equal(t, http.StatusOK, code)
	assert.True(t, report.Configured)
	require.Len(t, report.Entries, 2)
	assert.Equal(t, "budget", report.Entries[0].Kind)
	require.NotNil(t, report.Entries[0].Budget)
	assert.Equal(t, 1.25, report.Entries[0].Budget.SpentUSD)
	assert.Equal(t, "exceeded", report.Entries[1].State)
}

func TestHandleLimits_ReflectsLivePolicySwaps(t *testing.T) {
	s := &Server{}
	current := limits.StatusReport{Configured: false, Entries: []limits.EntryStatus{}}
	s.SetLimitsStatusFunc(func() limits.StatusReport { return current })

	_, report := getLimits(t, s)
	assert.False(t, report.Configured)

	// Simulate a hot reload installing a policy: the closure must see it.
	current = limits.StatusReport{
		Configured: true,
		Entries: []limits.EntryStatus{{Kind: "rate", Scope: "tool", Key: "github__search", State: "ok",
			Rate: &limits.RateStatus{CallsPerMinute: 6}}},
	}
	_, report = getLimits(t, s)
	assert.True(t, report.Configured)
	require.Len(t, report.Entries, 1)
	assert.Equal(t, "github__search", report.Entries[0].Key)
}
