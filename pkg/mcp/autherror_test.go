package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newChallengeServer(t *testing.T, status int, challenge string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte("denied"))
	}))
}

func TestSendHTTP_TypedAuthError(t *testing.T) {
	challenge := `Bearer resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`
	ts := newChallengeServer(t, http.StatusUnauthorized, challenge)
	defer ts.Close()

	c := NewClient("test", ts.URL)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error from 401")
	}

	var authErr *AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthRequiredError, got %T: %v", err, err)
	}
	if authErr.Status != http.StatusUnauthorized {
		t.Errorf("Status = %d, want 401", authErr.Status)
	}
	if authErr.Challenge != challenge {
		t.Errorf("Challenge = %q, want %q", authErr.Challenge, challenge)
	}
}

func TestSendHTTP_Plain403StaysGeneric(t *testing.T) {
	ts := newChallengeServer(t, http.StatusForbidden, "")
	defer ts.Close()

	c := NewClient("test", ts.URL)
	err := c.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error from 403")
	}
	var authErr *AuthRequiredError
	if errors.As(err, &authErr) {
		t.Fatalf("plain 403 without a challenge must not be an AuthRequiredError: %v", err)
	}
}

func TestPing_AuthChallengeSurfaces(t *testing.T) {
	ts := newChallengeServer(t, http.StatusUnauthorized, "Bearer")
	defer ts.Close()

	c := NewClient("test", ts.URL)
	err := c.Ping(context.Background())
	var authErr *AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthRequiredError from ping, got %v", err)
	}
}

func TestPing_NonAuthStatusStillPasses(t *testing.T) {
	ts := newChallengeServer(t, http.StatusMethodNotAllowed, "")
	defer ts.Close()

	c := NewClient("test", ts.URL)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping must ignore non-auth statuses, got %v", err)
	}
}

func TestWaitForHTTPServer_AuthChallengeAbortsEarly(t *testing.T) {
	ts := newChallengeServer(t, http.StatusUnauthorized, "Bearer")
	defer ts.Close()

	g := NewGateway()
	c := NewClient("test", ts.URL)

	start := time.Now()
	err := g.waitForHTTPServer(context.Background(), c, 30*time.Second)
	elapsed := time.Since(start)

	var authErr *AuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected AuthRequiredError, got %v", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("expected early abort, waited %s", elapsed)
	}
}

func TestRecordRegistrationFailure_AuthErrorBecomesNeedsAuth(t *testing.T) {
	g := NewGateway()

	wrapped := &AuthRequiredError{Status: 401, Challenge: "Bearer"}
	g.RecordRegistrationFailure("notion", wrapped)

	st, ok := g.ServerAuthState("notion")
	if !ok || st.Status != AuthStatusNeedsAuth {
		t.Fatalf("expected needs_auth state, got %+v (ok=%v)", st, ok)
	}

	statuses := g.Status()
	var found *MCPServerStatus
	for i := range statuses {
		if statuses[i].Name == "notion" {
			found = &statuses[i]
		}
	}
	if found == nil {
		t.Fatal("needs-auth server missing from Status()")
	}
	if found.RegistrationFailed {
		t.Error("needs-auth server must not report RegistrationFailed")
	}
	if found.Healthy != nil {
		t.Error("needs-auth server must not report a health verdict")
	}
	if found.AuthStatus != AuthStatusNeedsAuth {
		t.Errorf("AuthStatus = %q, want %q", found.AuthStatus, AuthStatusNeedsAuth)
	}
}

func TestRecordRegistrationFailure_NonAuthStillFails(t *testing.T) {
	g := NewGateway()
	g.RecordRegistrationFailure("broken", errors.New("connection refused"))

	if _, ok := g.ServerAuthState("broken"); ok {
		t.Fatal("non-auth failure must not create auth state")
	}
	statuses := g.Status()
	for _, st := range statuses {
		if st.Name == "broken" && !st.RegistrationFailed {
			t.Error("non-auth failure must report RegistrationFailed")
		}
	}
}

func TestServerAuthStateLifecycle(t *testing.T) {
	g := NewGateway()
	expiry := time.Now().Add(time.Hour)
	g.SetServerAuthState("s", ServerAuthState{Status: AuthStatusAuthorized, Issuer: "https://as.example.com", Expiry: &expiry})

	st, ok := g.ServerAuthState("s")
	if !ok || st.Status != AuthStatusAuthorized || st.Issuer != "https://as.example.com" {
		t.Fatalf("unexpected state: %+v (ok=%v)", st, ok)
	}

	g.ClearServerAuthState("s")
	if _, ok := g.ServerAuthState("s"); ok {
		t.Fatal("state not cleared")
	}
}
