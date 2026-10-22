package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/stretchr/testify/require"
)

func TestDaemonAPI_EnvironmentBearerPrecedence(t *testing.T) {
	credential := rand.Text()
	t.Setenv(gatewayTokenEnv, credential)
	for _, mode := range []string{"", "bearer", "api_key", "oauth"} {
		t.Run(mode, func(t *testing.T) {
			api := newDaemonAPIFor(state.DaemonState{AuthType: mode, AuthToken: rand.Text(), AuthHeader: "X-Key"}, time.Second)
			r := httptest.NewRequest(http.MethodGet, "http://localhost/api/status", nil)
			api.authorize(r)
			require.Equal(t, "Bearer "+credential, r.Header.Get("Authorization"))
			require.Empty(t, r.Header.Get("X-Key"))
		})
	}
}

func TestDaemonAPI_OAuthCommandDiagnostics(t *testing.T) {
	t.Setenv("GRIDCTL_HOME", t.TempDir())
	t.Setenv(gatewayTokenEnv, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("Gridctl-Auth-Rejected", "1")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	st := &state.DaemonState{StackName: "oauth", Port: port, PID: os.Getpid(), AuthType: "oauth", AuthIssuerHost: "auth.example.com"}
	require.NoError(t, state.Save(st))
	want := "set GRIDCTL_GATEWAY_TOKEN to a token issued by auth.example.com"
	t.Run("status output retains gateway", func(t *testing.T) {
		stdout, stderr := captureOutput(t, func() error { return runStatus("oauth", false, false, true) })
		require.Contains(t, stdout+stderr, want)
		require.Contains(t, stdout, "oauth")
		require.Contains(t, stdout, "running")
	})
	t.Run("call fails with credential hint", func(t *testing.T) {
		cmd := *callCmd
		cmd.SetContext(context.Background())
		require.EqualError(t, runCall(&cmd, []string{"alpha__echo"}), want)
	})
	t.Run("runs fails with credential hint", func(t *testing.T) {
		require.EqualError(t, runRunsListLive("json", runs.Filter{}, nil), want)
	})
	_, _, _, _, _, warning := queryGatewayStatus(port)
	require.Equal(t, want, warning)
	for name, call := range map[string]func() error{
		"groups":   func() error { _, err := fetchGroupsReport(port); return err },
		"limits":   func() error { _, err := fetchLimitsReport(port); return err },
		"traces":   func() error { _, err := fetchTraces(port); return err },
		"auth":     func() error { _, err := fetchAuthServers(port); return err },
		"pins":     func() error { _, err := fetchPinsRecords(st, ""); return err },
		"reload":   func() error { _, err := postReload(st); return err },
		"override": func() error { _, err := daemonOverride(st); return err },
		"activate": func() error { _, _, _, err := postActivate(srv.URL, "skill"); return err },
	} {
		t.Run(name, func(t *testing.T) { require.EqualError(t, call(), want) })
	}
	api := newDaemonAPIFor(*st, time.Second)
	resp, err := api.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.EqualError(t, classifyHTTPFailureFor(api, resp, nil, false, "cli", "tool"), want)
}

func TestLink_OAuthOutput(t *testing.T) {
	for _, mode := range []string{"oauth", "bearer", "missing"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("GRIDCTL_HOME", filepath.Join(home, "gridctl"))
			if mode != "missing" {
				require.NoError(t, state.Save(&state.DaemonState{Port: 8180, AuthType: mode, AuthIssuerHost: "auth.example.com"}))
			}
			prov, ok := provisioner.NewRegistry().FindBySlug("cursor")
			require.True(t, ok)
			path := filepath.Join(home, "cursor.json")
			opts := provisioner.LinkOptions{GatewayURL: "http://localhost:8180/mcp?client=cursor", ServerName: "gridctl"}
			var buf bytes.Buffer
			link := func() string {
				buf.Reset()
				require.NoError(t, doLink(context.Background(), output.NewWithWriter(&buf), prov, path, opts))
				return buf.String()
			}
			checkNote := func(got string) {
				if mode == "oauth" {
					require.Contains(t, got, "OAuth login")
					require.Contains(t, got, "no credential")
					require.Contains(t, got, "display-only")
				} else {
					require.NotContains(t, got, "OAuth login")
				}
			}
			checkNote(link())
			unchanged := link()
			require.Contains(t, unchanged, "already linked")
			require.NotContains(t, unchanged, "OAuth login")
			opts.GatewayURL = "http://localhost:8180/groups/dev/mcp?client=cursor"
			checkNote(link())
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(data), opts.GatewayURL)
			require.NotContains(t, string(data), "Authorization")
			adoptPath := filepath.Join(home, "adopted.json")
			require.NoError(t, os.WriteFile(adoptPath, data, 0600))
			path = adoptPath
			t.Setenv("GRIDCTL_HOME", filepath.Join(home, "adopt-state"))
			if mode != "missing" {
				require.NoError(t, state.Save(&state.DaemonState{Port: 8180, AuthType: mode, AuthIssuerHost: "auth.example.com"}))
			}
			adopted := link()
			require.Contains(t, adopted, "recorded ownership")
			require.NotContains(t, adopted, "OAuth login")
		})
	}
}

func TestDaemonAPI_OAuthUnauthorized(t *testing.T) {
	t.Setenv(gatewayTokenEnv, "")
	for _, mode := range []string{"oauth", "bearer", "api_key"} {
		for _, marker := range []string{"", "1"} {
			api := newDaemonAPIFor(state.DaemonState{AuthType: mode, AuthIssuerHost: "auth.example.com"}, time.Second)
			resp := &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}}
			resp.Header.Set("Gridctl-Auth-Rejected", marker)
			err := api.describeUnauthorized(resp)
			if mode == "oauth" && marker == "1" {
				require.EqualError(t, err, "set GRIDCTL_GATEWAY_TOKEN to a token issued by auth.example.com")
			} else {
				require.NoError(t, err)
			}
		}
	}
	api := newDaemonAPIFor(state.DaemonState{AuthType: "oauth"}, time.Second)
	r := httptest.NewRequest(http.MethodGet, "http://localhost/api/status", nil)
	api.authorize(r)
	require.Empty(t, r.Header.Get("Authorization"))
}
