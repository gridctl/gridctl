package api

import (
	"net/http"

	"github.com/gridctl/gridctl/pkg/agent/dev/devserver"
)

// SetAgentDevServer wires the agent IDE dev-server endpoints into the
// daemon's API surface. When set, /api/agent/dev/* routes serve parsed
// skill graphs and file-watcher events from the configured project
// root. Passing nil disables the routes — they 503.
//
// The daemon path is convenience: `gridctl agent dev` already stands
// up its own listener on port 8181 for standalone authoring. When the
// operator wants the project-aware IDE inside the same web UI as the
// gateway dashboard, they can wire a dev server here at apply time
// (or via a future serve flag).
//
// SetAgentRuntime takes precedence over this setter at read time;
// retained for test fixtures and the current daemon path that builds
// the dev server out-of-band.
func (s *Server) SetAgentDevServer(dev *devserver.Server) {
	s.agentDevServer = dev
}

// handleAgentDev routes /api/agent/dev/* requests to the configured
// devserver. When no dev server is wired, every method returns 503
// with a clear "not configured" message so frontend clients can
// gracefully degrade rather than silently fail.
func (s *Server) handleAgentDev(w http.ResponseWriter, r *http.Request) {
	dev := s.devServer()
	if dev == nil {
		writeJSONError(w, "agent dev server not configured", http.StatusServiceUnavailable)
		return
	}
	dev.Handler().ServeHTTP(w, r)
}
