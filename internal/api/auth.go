package api

import (
	"crypto/subtle"
	"net/http"
	"path"
	"strings"
)

// authMiddleware returns middleware that validates bearer tokens or API keys.
// If token is empty, all requests pass through (no auth configured).
// Auth is only enforced on protected paths (API, MCP, A2A endpoints).
// Static web UI files are served without authentication.
func authMiddleware(authType, token, header string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	if header == "" {
		header = "Authorization"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check the decoded path and its redirect target. Keep the original
		// check too: ServeMux treats escaped dot segments as literal segments.
		// CORS preflight terminates in the outer CORS layer, never here.
		if !isProtectedPath(r.URL.Path) && !isProtectedPath(path.Clean(r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}

		val := r.Header.Get(header)
		var provided string
		switch authType {
		case "bearer":
			if !strings.HasPrefix(val, "Bearer ") {
				w.Header().Set("Gridctl-Auth-Rejected", "1")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			provided = val[len("Bearer "):]
		default:
			provided = val
		}

		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			w.Header().Set("Gridctl-Auth-Rejected", "1")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isProtectedPath returns true for paths that require authentication:
// API, MCP (including groups), SSE, A2A, and well-known endpoints.
func isProtectedPath(path string) bool {
	switch {
	case strings.HasPrefix(path, "/api/"):
		return true
	case strings.HasPrefix(path, "/groups/"):
		return true
	case path == "/mcp":
		return true
	case path == "/sse":
		return true
	case path == "/message":
		return true
	case strings.HasPrefix(path, "/a2a/"):
		return true
	case strings.HasPrefix(path, "/.well-known/"):
		return true
	default:
		return false
	}
}
