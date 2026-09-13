//go:build linux

package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/runtime"
)

type executionSocketEngine struct {
	*executionEngine
	host string
}

func (e *executionSocketEngine) DaemonHost() string { return e.host }

func TestExecution_PodmanCreatePreservesInventory(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(map[bool]string{false: "none", true: "connected"}[connected], func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "api.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			id := strings.Repeat("a", 64)
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v4.0.0/libpod/containers/create" {
					t.Errorf("wrong create endpoint: %s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if body["read_write_tmpfs"] != false || body["read_only_filesystem"] != true || body["systemd"] != "false" || body["no_new_privileges"] != true || body["user"] != "1000:1000" || body["image"] != "fixture-image" || body["name"] != "fixture" {
					t.Errorf("native security/create fields lost")
				}
				if body["env"].(map[string]any)["FIXTURE"] != "explicit-value" || body["labels"].(map[string]any)["gridctl.execution-revision"] == nil {
					t.Error("environment or revision lost")
				}
				mounts := body["mounts"].([]any)
				if len(mounts) != 1 || mounts[0].(map[string]any)["destination"] != "/tmp" {
					t.Error("scratch inventory changed")
				}
				options := mounts[0].(map[string]any)["options"].([]any)
				var stringsOptions []string
				for _, option := range options {
					stringsOptions = append(stringsOptions, option.(string))
				}
				if !executionTmpfsMatches(strings.Join(stringsOptions, ","), 64<<20) {
					t.Error("scratch restrictions lost")
				}
				limits := body["resource_limits"].(map[string]any)
				memory := limits["memory"].(map[string]any)
				cpu := limits["cpu"].(map[string]any)
				if memory["limit"] != float64(256<<20) || memory["swap"] != memory["limit"] || cpu["quota"] != float64(100000) || cpu["period"] != float64(100000) || limits["pids"].(map[string]any)["limit"] != float64(128) {
					t.Error("resource limits lost")
				}
				netns := body["netns"].(map[string]any)["nsmode"]
				if connected {
					if netns != "bridge" || body["networks"] == nil {
						t.Error("connected network lost")
					}
					ports := body["portmappings"].([]any)
					if len(ports) != 1 || ports[0].(map[string]any)["host_ip"] != "127.0.0.1" || ports[0].(map[string]any)["container_port"] != float64(9000) {
						t.Error("loopback binding lost")
					}
				} else if netns != "none" || body["networks"] != nil || body["portmappings"] != nil || body["hostadd"] != nil {
					t.Error("network-none acquired endpoints or aliases")
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"Id":"` + id + `","Warnings":[]}`))
			})}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			engine := &executionSocketEngine{executionEngine: &executionEngine{MockDockerClient: &MockDockerClient{}}, host: "unix://" + socket}
			contract := executionTestContract(t)
			if connected {
				contract.Network, contract.Transport, contract.Port = "connected", "http", 9000
			}
			got, err := CreateContainer(t.Context(), engine, ContainerConfig{Name: "fixture", Image: "fixture-image", Env: map[string]string{"FIXTURE": "explicit-value"}, Execution: contract, NetworkName: "fixture-network", Port: 9000, Transport: contract.Transport, RuntimeInfo: &runtime.RuntimeInfo{Type: runtime.RuntimePodman}})
			if err != nil || got != id {
				t.Fatalf("native create: %s %v", got, err)
			}
			if len(engine.Calls) != 0 {
				t.Fatalf("compatibility create used: %v", engine.Calls)
			}
		})
	}
}

func TestExecution_PodmanCreateFailureDoesNotFallback(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, body string
		status     int
		cleanup    bool
	}{
		{"rejected", "private-engine-detail", http.StatusInternalServerError, false},
		{"redirect", "private-engine-detail", http.StatusTemporaryRedirect, false},
		{"malformed", `{`, http.StatusCreated, false},
		{"missing identity", `{}`, http.StatusCreated, false},
		{"invalid identity", `{"Id":"` + strings.Repeat("z", 64) + `"}`, http.StatusCreated, false},
		{"oversized", strings.Repeat("x", (1<<20)+1), http.StatusCreated, false},
		{"warning", `{"Id":"` + id + `","Warnings":["private-engine-detail"]}`, http.StatusCreated, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "api.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if body["mounts"] != nil || body["read_only_filesystem"] != false || body["no_new_privileges"] != false {
					t.Error("empty/false intent lost")
				}
				volumes := body["volumes"].([]any)
				if len(volumes) != 1 || volumes[0].(map[string]any)["name"] != "fixture-volume" || volumes[0].(map[string]any)["dest"] != "/data" || volumes[0].(map[string]any)["options"].([]any)[0] != "rw" {
					t.Error("data-volume intent lost")
				}
				w.Header().Set("Location", "http://invalid.example/")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			engine := &executionSocketEngine{executionEngine: &executionEngine{MockDockerClient: &MockDockerClient{}}, host: "unix://" + socket}
			contract := executionTestContract(t)
			readOnly := false
			contract.ReadOnly, contract.NoNewPrivileges, contract.Tmpfs = false, false, nil
			contract.Mounts = []execution.ExecutionMount{{Source: "fixture-volume", Target: "/data", ReadOnly: &readOnly}}
			cfg := ContainerConfig{Name: "fixture", Execution: contract, RuntimeInfo: &runtime.RuntimeInfo{Type: runtime.RuntimePodman}}
			if _, err := CreateContainer(t.Context(), engine, cfg); err == nil || strings.Contains(err.Error(), "private-engine-detail") {
				t.Fatalf("unsafe create failure: %v", err)
			}
			for _, call := range engine.Calls {
				if strings.Contains(call, "ContainerCreate") {
					t.Fatal("failed native create fell back")
				}
			}
			if tc.cleanup && len(engine.Calls) == 0 {
				t.Fatal("warned create not cleaned up")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := CreateContainer(ctx, engine, cfg); err == nil {
				t.Fatal("canceled creation succeeded")
			}
		})
	}
}

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
		{name: "nil bounding slice", body: strings.Replace(supported, `"BoundingCaps":[]`, `"BoundingCaps":null`, 1)},
		{name: "nil effective slice", body: strings.Replace(supported, `"EffectiveCaps":[]`, `"EffectiveCaps":null`, 1)},
		{name: "nil actual sets", body: strings.ReplaceAll(supported, `[]`, `null`)},
		{name: "missing bounding set", body: `{"Id":"` + id + `","EffectiveCaps":null}`, wantOutcome: "unknown"},
		{name: "missing effective set", body: `{"Id":"` + id + `","BoundingCaps":null}`, wantOutcome: "unknown"},
		{name: "nil effective with retained bounding", body: `{"Id":"` + id + `","EffectiveCaps":null,"BoundingCaps":["CAP_CHOWN"]}`, wantOutcome: "mismatch"},
		{name: "nil bounding with retained effective", body: `{"Id":"` + id + `","EffectiveCaps":["CAP_CHOWN"],"BoundingCaps":null}`, wantOutcome: "mismatch"},
		{name: "invalid bounding type", body: strings.Replace(supported, `"BoundingCaps":[]`, `"BoundingCaps":{}`, 1), wantOutcome: "unknown"},
		{name: "invalid effective type", body: strings.Replace(supported, `"EffectiveCaps":[]`, `"EffectiveCaps":""`, 1), wantOutcome: "unknown"},
		{name: "invalid set member", body: strings.Replace(supported, `"BoundingCaps":[]`, `"BoundingCaps":[123]`, 1), wantOutcome: "unknown"},
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
