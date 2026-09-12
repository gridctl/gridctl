package probe

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestProbe_RejectsInapplicableExecutionBeforeContact(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	_, err := NewProber(nil).Probe(t.Context(), config.MCPServer{Name: "fixture", URL: server.URL, Execution: &config.ExecutionConfig{Mode: "local"}})
	if err == nil || requests.Load() != 0 {
		t.Fatal("inapplicable execution reached external probe")
	}
}
