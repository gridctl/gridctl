package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

// TestDaemonAPI_AttachesRecordedCredentials is the regression case: before
// this, no CLI subcommand sent an auth header, so enabling gateway.auth
// returned 401 from every command that calls the local API.
func TestDaemonAPI_AttachesRecordedCredentials(t *testing.T) {
	tests := []struct {
		name       string
		st         state.DaemonState
		wantHeader string
		wantValue  string
	}{
		{
			name:       "bearer type gets the Bearer prefix",
			st:         state.DaemonState{AuthToken: "secret", AuthType: "bearer"},
			wantHeader: "Authorization",
			wantValue:  "Bearer secret",
		},
		{
			name:       "empty type defaults to bearer",
			st:         state.DaemonState{AuthToken: "secret"},
			wantHeader: "Authorization",
			wantValue:  "Bearer secret",
		},
		{
			name:       "api-key type sends the raw value",
			st:         state.DaemonState{AuthToken: "secret", AuthType: "api_key"},
			wantHeader: "Authorization",
			wantValue:  "secret",
		},
		{
			name:       "custom header name is honored",
			st:         state.DaemonState{AuthToken: "secret", AuthType: "api_key", AuthHeader: "X-Api-Key"},
			wantHeader: "X-Api-Key",
			wantValue:  "secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
			}))
			defer srv.Close()

			api := newDaemonAPIFor(tt.st, 2*time.Second)
			resp, err := api.Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if v := got.Get(tt.wantHeader); v != tt.wantValue {
				t.Errorf("%s = %q, want %q", tt.wantHeader, v, tt.wantValue)
			}
		})
	}
}

func TestDaemonAPI_NoTokenSendsNoHeader(t *testing.T) {
	// Auth is opt-in; with none configured the CLI must behave exactly as
	// it did before, sending nothing.
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer srv.Close()

	api := newDaemonAPIFor(state.DaemonState{}, 2*time.Second)
	resp, err := api.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if v := got.Get("Authorization"); v != "" {
		t.Errorf("expected no Authorization header, got %q", v)
	}
}

func TestDaemonAPI_DoAttachesOnNonGET(t *testing.T) {
	// POST and DELETE paths build their own requests, so they need the same
	// treatment as Get — this is the shape that would silently regress.
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var got http.Header
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Clone()
			}))
			defer srv.Close()

			api := newDaemonAPIFor(state.DaemonState{AuthToken: "t", AuthType: "bearer"}, 2*time.Second)
			resp, err := api.Do(method, srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			if v := got.Get("Authorization"); v != "Bearer t" {
				t.Errorf("%s Authorization = %q, want %q", method, v, "Bearer t")
			}
		})
	}
}

func TestDaemonAPI_URL(t *testing.T) {
	api := newDaemonAPIFor(state.DaemonState{Port: 8180}, time.Second)
	if got := api.URL("/api/status"); got != "http://localhost:8180/api/status" {
		t.Errorf("URL = %q", got)
	}
}
