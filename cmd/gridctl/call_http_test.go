package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/state"
)

func TestOriginDo_RefusesRedirect(t *testing.T) {
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
		if r.Header.Get("Authorization") != "" {
			t.Error("redirect target received credentials")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/tools/call", http.StatusFound)
	}))
	defer origin.Close()

	port := origin.Listener.Addr().(*net.TCPAddr).Port
	api := newDaemonAPIFor(state.DaemonState{Port: port, AuthToken: "secret", AuthType: "bearer"}, 0)
	_, err := api.originDo(context.Background(), http.MethodPost, "/api/tools/call", nil, "application/json")
	if !errors.Is(err, errRedirectRefused) {
		t.Fatalf("err = %v", err)
	}
	if redirected {
		t.Fatal("redirect was followed")
	}
}
