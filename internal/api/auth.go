package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// authMiddleware returns middleware that validates bearer tokens or API keys.
// If token is empty, all requests pass through (no auth configured).
func authMiddleware(authType, token, header string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	if header == "" {
		header = "Authorization"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health/ready and CORS preflight
		if r.URL.Path == "/health" || r.URL.Path == "/ready" || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		val := r.Header.Get(header)
		var provided string
		switch authType {
		case "bearer":
			if !strings.HasPrefix(val, "Bearer ") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			provided = val[len("Bearer "):]
		default:
			provided = val
		}

		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
