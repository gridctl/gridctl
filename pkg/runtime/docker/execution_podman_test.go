//go:build linux

package docker

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
)

type executionSocketEngine struct {
	*executionEngine
	host string
}

func (e *executionSocketEngine) DaemonHost() string { return e.host }

func TestExecution_PodmanNativePreflight(t *testing.T) {
	const supported = `{"host":{"cgroupVersion":"v2","cgroupManager":"systemd","cgroupControllers":["cpu","memory","pids"]},"version":{"Version":"4.9.3"}}`
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{name: "v2 despite false compatibility flags", body: supported},
		{name: "missing cpu delegation", body: strings.Replace(supported, `"cpu",`, "", 1), wantError: true},
		{name: "missing memory delegation", body: strings.Replace(supported, `"memory",`, "", 1), wantError: true},
		{name: "missing pids delegation", body: strings.Replace(supported, `,"pids"`, "", 1), wantError: true},
		{name: "v1", body: strings.Replace(supported, "v2", "v1", 1), wantError: true},
		{name: "missing native evidence", body: `{}`, wantError: true},
		{name: "missing cgroup manager", body: strings.Replace(supported, `"systemd"`, `""`, 1), wantError: true},
		{name: "malformed", body: `{"host":`, wantError: true},
		{name: "oversized", body: supported + strings.Repeat(" ", 1<<20), wantError: true},
		{name: "unsupported endpoint", status: http.StatusNotFound, body: "private engine detail", wantError: true},
		{name: "redirect", status: http.StatusTemporaryRedirect, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "api.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v4.0.0/libpod/info" || r.Method != http.MethodGet {
					t.Errorf("unexpected native request: %s %s", r.Method, r.URL.Path)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.body))
			})}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			engine := &executionSocketEngine{executionEngine: &executionEngine{MockDockerClient: &MockDockerClient{}, info: system.Info{CgroupVersion: "2", PidsLimit: true, SecurityOptions: []string{"name=seccomp,profile=default"}}}, host: "unix://" + socket}
			err = NewWithClient(engine).PreflightExecution(t.Context(), executionTestContract(t))
			if (err != nil) != tc.wantError {
				t.Fatalf("preflight: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private engine detail") {
				t.Fatal("native error disclosed")
			}
			if !tc.wantError {
				contract := executionTestContract(t)
				c, h := &container.Config{}, &container.HostConfig{}
				applyExecution(contract, c, h)
				engine.ContainerDetails = map[string]container.InspectResponse{"fixture": {ContainerJSONBase: &container.ContainerJSONBase{HostConfig: h, State: &container.State{Running: true}}, Config: c}}
				rt := NewWithClient(engine)
				if err := rt.CheckExecutionBeforeStart(t.Context(), "fixture", contract); err != nil {
					t.Fatalf("matching pre-start inspection: %v", err)
				}
				if report, err := rt.CheckExecution(t.Context(), "fixture", contract); err == nil || report.Eligible || report.Outcome != "unknown" {
					t.Fatalf("native info replaced kernel evidence: %+v %v", report, err)
				}
				h.MemorySwap = -1
				if err := rt.CheckExecutionBeforeStart(t.Context(), "fixture", contract); err == nil {
					t.Fatal("native info bypassed inspected swap refusal")
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := NewWithClient(engine).PreflightExecution(ctx, executionTestContract(t)); err == nil {
				t.Fatal("canceled native request admitted")
			}
		})
	}
}

func TestExecution_PodmanExpandedCapabilityDrops(t *testing.T) {
	id := strings.Repeat("a", 64)
	supported := `{"Id":"` + id + `","EffectiveCaps":[],"BoundingCaps":[]}`
	for _, tc := range []struct {
		name, body  string
		status      int
		wantOutcome string
		add         bool
	}{
		{name: "empty actual sets", body: supported},
		{name: "bounding capability retained", body: strings.Replace(supported, `"BoundingCaps":[]`, `"BoundingCaps":["CAP_CHOWN"]`, 1), wantOutcome: "mismatch"},
		{name: "effective capability retained", body: strings.Replace(supported, `"EffectiveCaps":[]`, `"EffectiveCaps":["CAP_SYS_ADMIN"]`, 1), wantOutcome: "mismatch"},
		{name: "missing sets", body: `{"Id":"` + id + `"}`, wantOutcome: "unknown"},
		{name: "null set", body: strings.Replace(supported, `"BoundingCaps":[]`, `"BoundingCaps":null`, 1), wantOutcome: "unknown"},
		{name: "wrong instance", body: strings.Replace(supported, id, strings.Repeat("b", 64), 1), wantOutcome: "unknown"},
		{name: "malformed", body: `{`, wantOutcome: "unknown"},
		{name: "oversized", body: supported + strings.Repeat(" ", 1<<20), wantOutcome: "unknown"},
		{name: "unsupported endpoint", status: http.StatusNotFound, body: "private-engine-detail", wantOutcome: "unknown"},
		{name: "redirect", status: http.StatusTemporaryRedirect, wantOutcome: "unknown"},
		{name: "additions remain refused", body: supported, add: true, wantOutcome: "mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "api.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v4.0.0/libpod/containers/"+id+"/json" || r.Method != http.MethodGet {
					t.Errorf("unexpected native request: %s %s", r.Method, r.URL.Path)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.body))
			})}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			contract := executionTestContract(t)
			c, h := &container.Config{}, &container.HostConfig{}
			applyExecution(contract, c, h)
			// Podman reports the default-set difference, not the ALL sentinel.
			h.CapDrop = []string{"CAP_CHOWN", "CAP_DAC_OVERRIDE", "CAP_FOWNER", "CAP_FSETID", "CAP_KILL", "CAP_NET_BIND_SERVICE", "CAP_SETFCAP", "CAP_SETGID", "CAP_SETPCAP", "CAP_SETUID", "CAP_SYS_CHROOT"}
			if tc.add {
				h.CapAdd = []string{"CAP_SYS_ADMIN"}
			}
			engine := &executionSocketEngine{executionEngine: &executionEngine{info: system.Info{CgroupVersion: "2", MemoryLimit: true, SwapLimit: true, CPUCfsQuota: true, PidsLimit: true, SecurityOptions: []string{"name=seccomp"}}, MockDockerClient: &MockDockerClient{ContainerDetails: map[string]container.InspectResponse{id: {ContainerJSONBase: &container.ContainerJSONBase{ID: id, HostConfig: h, State: &container.State{Running: true}}, Config: c}}}}, host: "unix://" + socket}
			rt := NewWithClient(engine)
			report, err := rt.inspectExecution(t.Context(), id, contract, false)
			if (err != nil) != (tc.wantOutcome != "") || (tc.wantOutcome != "" && report.Outcome != tc.wantOutcome) || report.Eligible {
				t.Fatalf("native capability admission: %+v %v", report, err)
			}
			if err != nil && (!strings.Contains(err.Error(), "capabilities") || strings.Contains(err.Error(), "private-engine-detail")) {
				t.Fatalf("missing safe field diagnostic: %v", err)
			}
			if tc.wantOutcome == "" {
				if report, err := rt.CheckExecution(t.Context(), id, contract); err == nil || report.Eligible || report.Outcome != "unknown" {
					t.Fatalf("native capability evidence replaced kernel observation: %+v %v", report, err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := rt.inspectExecution(ctx, id, contract, false); err == nil {
				t.Fatal("canceled native observation admitted")
			}
		})
	}
}
