package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware_NoToken(t *testing.T) {
	// When no token is configured, all requests pass through
	handler := authMiddleware("bearer", "", "", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddleware_BearerToken(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{
			name:       "valid bearer token",
			header:     "Bearer mysecret",
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing header",
			header:     "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong token",
			header:     "Bearer wrongtoken",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing Bearer prefix",
			header:     "mysecret",
			wantStatus: http.StatusUnauthorized,
		},
	}

	handler := authMiddleware("bearer", "mysecret", "", okHandler())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_APIKey(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		headerName string
		value      string
		wantStatus int
	}{
		{
			name:       "valid api key default header",
			headerName: "",
			header:     "Authorization",
			value:      "myapikey",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid api key custom header",
			headerName: "X-API-Key",
			header:     "X-API-Key",
			value:      "myapikey",
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing api key",
			headerName: "X-API-Key",
			header:     "",
			value:      "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong api key",
			headerName: "X-API-Key",
			header:     "X-API-Key",
			value:      "wrongkey",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := authMiddleware("api_key", "myapikey", tt.headerName, okHandler())

			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			if tt.header != "" {
				req.Header.Set(tt.header, tt.value)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_HealthBypass(t *testing.T) {
	handler := authMiddleware("bearer", "mysecret", "", okHandler())

	paths := []string{"/health", "/ready"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			// No auth header set
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 for %s without auth, got %d", path, rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_ProtectedPaths(t *testing.T) {
	handler := authMiddleware("bearer", "mysecret", "", okHandler())

	paths := []string{"/api/status", "/mcp", "/sse", "/api/tools", "/api/mcp-servers"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			// No auth header
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 for %s without auth, got %d", path, rec.Code)
			}
		})
	}
}

func TestAuthMiddleware_OptionsPassthrough(t *testing.T) {
	handler := authMiddleware("bearer", "mysecret", "", okHandler())

	req := httptest.NewRequest(http.MethodOptions, "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for OPTIONS without auth, got %d", rec.Code)
	}
}

func TestAuthMiddleware_BearerEmptyToken(t *testing.T) {
	handler := authMiddleware("bearer", "mysecret", "", okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty bearer token, got %d", rec.Code)
	}
}

func TestAuthMiddleware_TimingSafe(t *testing.T) {
	// Verify that partial matches are rejected (not vulnerable to prefix attacks)
	handler := authMiddleware("api_key", "mysecretkey", "", okHandler())

	partials := []string{"mysecret", "mysecretke", "mysecretkey!", ""}
	for _, partial := range partials {
		t.Run("partial_"+partial, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			req.Header.Set("Authorization", partial)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 for partial token %q, got %d", partial, rec.Code)
			}
		})
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
