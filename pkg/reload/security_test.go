package reload

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestSecurityPreflight_ObservableStoreRotation(t *testing.T) {
	dir := t.TempDir()
	writer := vault.NewStore(dir)
	if err := writer.Set("LIFECYCLE_FIXTURE", rand.Text()); err != nil {
		t.Fatal(err)
	}
	reader := vault.NewStore(dir)
	if err := reader.Load(); err != nil {
		t.Fatal(err)
	}
	path := writeStackFile(t, "name: fixture\ngateway:\n  auth:\n    type: bearer\n    token: ${var:LIFECYCLE_FIXTURE}\n")
	baseline, err := config.LoadStack(path, config.WithVault(reader))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(path, baseline, nil, nil, 8180, 9000, reader, nil)
	h.SetSecurityPreflight(NewSecurityPreflight(baseline, "", false))
	// Different size guarantees observability through the store's documented
	// mtime/size reload gate even on coarse filesystem timestamps.
	if err := writer.Set("LIFECYCLE_FIXTURE", rand.Text()+rand.Text()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result, err := h.Reload(t.Context())
		if err != nil || result.Success || result.Code != "restart_required" || !slices.Contains(result.ChangedFields, "gateway.auth.token") {
			t.Fatal("store rotation not detected")
		}
		if h.CurrentConfig() != baseline {
			t.Fatal("baseline replaced")
		}
	}
	if err := writer.Delete("LIFECYCLE_FIXTURE"); err != nil {
		t.Fatal(err)
	}
	result, err := h.Reload(t.Context())
	if err != nil || result.Success || result.Code != "invalid_candidate" || h.CurrentConfig() != baseline {
		t.Fatal("failed resolution changed active state")
	}
	t.Setenv("LIFECYCLE_EQUIVALENT", baseline.Gateway.Auth.Token)
	if err := os.WriteFile(path, []byte("name: fixture\ngateway:\n  auth:\n    type: bearer\n    token: ${LIFECYCLE_EQUIVALENT}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = h.Reload(t.Context())
	if err != nil || !result.Success {
		t.Fatal("equivalent resolved template was rejected")
	}
}

func TestEffectiveListenerOverrides(t *testing.T) {
	stack := &config.Stack{Gateway: &config.GatewayConfig{Bind: "0.0.0.0", InsecureAllowUnauthenticated: true}}
	if EffectiveBind(nil, "") != "127.0.0.1" || EffectiveBind(stack, "") != "0.0.0.0" || EffectiveBind(stack, "::1") != "::1" {
		t.Fatal("bind precedence mismatch")
	}
	if AllowUnauthenticated(nil, false) || !AllowUnauthenticated(nil, true) || !AllowUnauthenticated(stack, false) {
		t.Fatal("override precedence mismatch")
	}
}

func TestHandler_StaticSecurityMatrix(t *testing.T) {
	t.Setenv("LIFECYCLE_BASE", rand.Text())
	t.Setenv("LIFECYCLE_NEXT", rand.Text())
	auth := "  auth:\n    type: api_key\n    token: ${LIFECYCLE_BASE}\n"
	for _, tc := range []struct{ name, gateway string }{
		{"remove", ""},
		{"token", strings.Replace(auth, "LIFECYCLE_BASE", "LIFECYCLE_NEXT", 1)},
		{"type", strings.Replace(auth, "api_key", "bearer", 1)},
		{"header", auth + "    header: X-Credential\n"},
		{"bind", auth + "  bind: 0.0.0.0\n"},
		{"hosts", auth + "  allowed_hosts: [gateway.example]\n"},
		{"origins", auth + "  allowed_origins: [https://gateway.example]\n"},
		{"override", auth + "  insecure_allow_unauthenticated: true\n"},
	} {
		for _, initialize := range []bool{false, true} {
			for _, mixed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/initialize=%t/mixed=%t", tc.name, initialize, mixed), func(t *testing.T) {
					path := writeStackFile(t, "name: active\ngateway:\n"+auth)
					baseline, err := config.LoadStack(path)
					if err != nil {
						t.Fatal(err)
					}
					h, rt := setupHandler(t, path, baseline)
					if initialize {
						h.stackPath = ""
					}
					acceptedPath := h.stackPath
					h.SetSecurityPreflight(NewSecurityPreflight(baseline, "", false))
					h.SetOnConfigApplied(func(*config.Stack) { t.Fatal("rejected candidate applied callback") })
					rt.startFn = func(context.Context, runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
						t.Fatal("rejected candidate started a workload")
						return nil, nil
					}
					candidate := "name: active\ngateway:\n" + tc.gateway
					if mixed {
						candidate += "mcp-servers:\n  - name: added\n    image: example.invalid/must-not-start\n    transport: stdio\n"
					}
					if err := os.WriteFile(path, []byte(candidate), 0600); err != nil {
						t.Fatal(err)
					}
					for range 2 {
						var result *ReloadResult
						if initialize {
							result, err = h.Initialize(t.Context(), path)
						} else {
							result, err = h.Reload(t.Context())
						}
						if err != nil || result.Success || result.Code != "restart_required" {
							t.Fatal("static security change accepted")
						}
						if h.CurrentConfig() != baseline || h.stackPath != acceptedPath || len(rt.ensureNetworkLog) != 0 {
							t.Fatal("preflight changed accepted state or networks")
						}
					}
				})
			}
		}
	}
}

func TestHandler_SecurityPreflightAtomic(t *testing.T) {
	for _, initialize := range []bool{false, true} {
		for _, mixed := range []bool{false, true} {
			t.Run(fmt.Sprintf("initialize=%t/mixed=%t", initialize, mixed), func(t *testing.T) {
				token := rand.Text()
				t.Setenv("GRIDCTL_TEST_CANDIDATE", token)
				path := writeStackFile(t, "name: candidate\ngateway:\n  auth:\n    type: bearer\n    token: ${GRIDCTL_TEST_CANDIDATE}\n")
				if mixed {
					f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, err = f.WriteString("mcp-servers:\n  - name: backend\n    image: example.invalid/backend\n    transport: stdio\n")
					if closeErr := f.Close(); closeErr != nil {
						t.Fatal(closeErr)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				baseline := &config.Stack{Name: "active"}
				h, rt := setupHandler(t, path, baseline)
				if initialize {
					h.stackPath = ""
				}
				oldPath := h.stackPath
				h.SetSecurityPreflight(NewSecurityPreflight(baseline, "", false))
				h.SetOnConfigApplied(func(*config.Stack) { t.Fatal("callback ran") })
				rt.startFn = func(context.Context, runtime.WorkloadConfig) (*runtime.WorkloadStatus, error) {
					t.Fatal("workload started")
					return nil, nil
				}
				for range 2 {
					var result *ReloadResult
					var err error
					if initialize {
						result, err = h.Initialize(t.Context(), path)
					} else {
						result, err = h.Reload(t.Context())
					}
					if err != nil || result.Success || result.Code != "restart_required" {
						t.Fatal("candidate was not rejected with restart_required")
					}
					if h.CurrentConfig() != baseline || h.stackPath != oldPath || len(rt.ensureNetworkLog) != 0 {
						t.Fatal("rejection changed active state")
					}
					body, err := json.Marshal(result)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(body), token) {
						t.Fatal("result disclosed credential")
					}
				}
			})
		}
	}
}

func TestSecurityPreflight(t *testing.T) {
	token := rand.Text()
	base := func() *config.Stack {
		return &config.Stack{Gateway: &config.GatewayConfig{Auth: &config.AuthConfig{Type: "api_key", Token: token}}}
	}
	for _, tc := range []struct {
		name  string
		field string
		edit  func(*config.Stack)
	}{
		{"remove auth", "gateway.auth.enabled", func(s *config.Stack) { s.Gateway.Auth = nil }},
		{"token", "gateway.auth.token", func(s *config.Stack) { s.Gateway.Auth.Token = rand.Text() }},
		{"type", "gateway.auth.type", func(s *config.Stack) { s.Gateway.Auth.Type = "bearer" }},
		{"header", "gateway.auth.header", func(s *config.Stack) { s.Gateway.Auth.Header = "X-Credential" }},
		{"bind", "gateway.bind", func(s *config.Stack) { s.Gateway.Bind = "0.0.0.0" }},
		{"hosts", "gateway.allowed_hosts", func(s *config.Stack) { s.Gateway.AllowedHosts = []string{"gateway.example"} }},
		{"origins", "gateway.allowed_origins", func(s *config.Stack) { s.Gateway.AllowedOrigins = []string{"https://gateway.example"} }},
		{"override", "gateway.insecure_allow_unauthenticated", func(s *config.Stack) { s.Gateway.InsecureAllowUnauthenticated = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := NewSecurityPreflight(base(), "", false)
			candidate := base()
			tc.edit(candidate)
			for range 2 {
				if fields := check(candidate); !slices.Contains(fields, tc.field) {
					t.Fatalf("missing changed field %s", tc.field)
				}
			}
		})
	}
	t.Run("equivalence and immutable ownership", func(t *testing.T) {
		startup := base()
		startup.Gateway.AllowedHosts = []string{"EXAMPLE.com", "other.example"}
		startup.Gateway.AllowedOrigins = []string{"https://example.com", "https://other.example"}
		check := NewSecurityPreflight(startup, "127.0.0.1", true)
		candidate := base()
		candidate.Gateway.Auth.Header = "aUtHoRiZaTiOn"
		candidate.Gateway.Bind = "0.0.0.0"
		candidate.Gateway.AllowedHosts = []string{"other.example", "example.com:443", "example.com"}
		candidate.Gateway.AllowedOrigins = []string{"https://other.example", "https://example.com"}
		startup.Gateway.Auth.Token = rand.Text()
		startup.Gateway.AllowedHosts[0] = "changed.example"
		startup.Gateway.AllowedOrigins[0] = "https://changed.example"
		if fields := check(candidate); len(fields) != 0 {
			t.Fatalf("equivalent candidate rejected: %v", fields)
		}
	})
	t.Run("stackless defaults", func(t *testing.T) {
		check := NewSecurityPreflight(nil, "", false)
		if fields := check(&config.Stack{}); len(fields) != 0 {
			t.Fatalf("default mismatch: %v", fields)
		}
		if fields := check(base()); !slices.Contains(fields, "gateway.auth.enabled") {
			t.Fatal("auth addition accepted")
		}
	})
}
