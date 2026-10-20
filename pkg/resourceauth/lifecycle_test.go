package resourceauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVerifier_CloseCancelsKeyFetch(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer srv.Close()
	v, err := New(t.Context(), Config{Issuer: srv.URL, Audience: "gateway", JWKSURL: srv.URL}, nil)
	require.NoError(t, err)
	defer v.Close()
	done := make(chan error, 1)
	go func() {
		_, err := v.keySet.VerifySignature(context.Background(), "eyJhbGciOiJSUzI1NiJ9.e30.c2ln")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("key fetch did not start")
	}
	v.Close()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("key fetch did not stop")
	}
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("verification did not stop")
	}
}

func TestVerifier_ExactIssuerAndCancellation(t *testing.T) {
	key := rsaKey(t, "issuer-key")
	iss := newTestIssuer(t, key)
	v, err := New(t.Context(), Config{Issuer: "https://accounts.google.com", Audience: "gridctl", JWKSURL: iss.srv.URL + "/jwks"}, nil)
	require.NoError(t, err)
	defer v.Close()
	_, err = v.Verify(t.Context(), iss.mint(t, key, tokenSpec{iss: "accounts.google.com"}))
	var invalid *TokenError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, CategoryIssuer, invalid.Category)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = v.Verify(ctx, iss.mint(t, key, tokenSpec{iss: "https://accounts.google.com"}))
	require.ErrorIs(t, err, context.Canceled)
}
