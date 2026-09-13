//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
	dockerruntime "github.com/gridctl/gridctl/pkg/runtime/docker"
)

func TestExecution_RealRuntimeAdmission(t *testing.T) {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := dockerruntime.NewWithInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := rt.EnsureImage(ctx, "alpine:3.22"); err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(65534), uint32(65534)
	server := config.MCPServer{Image: "alpine:3.22", Transport: "stdio", Execution: &config.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid}}
	contract, err := config.ResolveExecution(server)
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtime.WorkloadConfig{Name: "fixture", Stack: fmt.Sprintf("execution-%d", time.Now().UnixNano()), Type: runtime.WorkloadTypeMCPServer, Image: server.Image, Transport: "stdio", Command: []string{"sleep", "120"}, Execution: contract}
	// Discover by fixture-owned name even when failed admission returns no ID.
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		exists, id, err := rt.Exists(cleanupCtx, dockerruntime.ContainerName(cfg.Stack, cfg.Name))
		if err != nil {
			t.Error(err)
			return
		}
		if exists {
			if err := rt.Remove(cleanupCtx, id); err != nil {
				t.Error(err)
			}
		}
	})
	status, err := rt.Start(ctx, cfg)
	if err != nil {
		t.Fatalf("required positive runtime admission: %v", err)
	}
	if status.Execution == nil || !status.Execution.Eligible || status.Execution.Instance != string(status.ID) || status.Execution.Revision != contract.Revision {
		t.Fatalf("missing instance-bound eligibility: %+v", status.Execution)
	}
	reused, err := rt.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reused.ID != status.ID || reused.Execution == nil || !reused.Execution.Eligible || !reused.Execution.ObservedAt.After(status.Execution.ObservedAt) {
		t.Fatalf("running reuse did not refresh instance-bound evidence: same_instance=%v evidence=%+v", reused.ID == status.ID, reused.Execution)
	}
	qualified := cfg
	qualified.Image = "docker.io/library/alpine:3.22"
	canonical, err := rt.Start(ctx, qualified)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.ID != status.ID || canonical.Execution == nil || !canonical.Execution.Eligible || !canonical.Execution.ObservedAt.After(reused.Execution.ObservedAt) {
		t.Fatal("equivalent qualified image did not reuse with fresh evidence")
	}
	if err := rt.Stop(ctx, status.ID); err != nil {
		t.Fatal(err)
	}
	restarted, err := rt.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.ID != status.ID || restarted.Execution == nil || !restarted.Execution.Eligible || !restarted.Execution.ObservedAt.After(canonical.Execution.ObservedAt) {
		t.Fatal("restart reused stale evidence")
	}
	// An explicit empty inventory must not acquire Podman's automatic /tmp,
	// /var/tmp, or /run scratch mounts. Kernel admission checks the full inventory.
	emptyScratch := []execution.ExecutionTmpfs{}
	server.Execution.Tmpfs = &emptyScratch
	cfg.Execution, err = config.ResolveExecution(server)
	if err != nil {
		t.Fatal(err)
	}
	withoutScratch, err := rt.Start(ctx, cfg)
	if err != nil {
		t.Fatalf("required positive empty-scratch admission: %v", err)
	}
	if withoutScratch.ID == restarted.ID || withoutScratch.Execution == nil || !withoutScratch.Execution.Eligible || withoutScratch.Execution.Revision != cfg.Execution.Revision {
		t.Fatal("empty scratch did not recreate with current kernel evidence")
	}
}

func TestExecution_RepresentativeMCPAndMismatch(t *testing.T) {
	for _, fixture := range []struct {
		name, image string
		command     []string
	}{
		{"python", "python:3.13-alpine", []string{"python", "-u", "-c", pythonFixtureModule + "\nmain()\n"}},
		{"node", "node:22-alpine", []string{"node", "-e", `require('readline').createInterface({input:process.stdin}).on('line',line=>{const r=JSON.parse(line);if(r.id===undefined)return;let result={};if(r.method==='initialize')result={protocolVersion:'2025-06-18',capabilities:{tools:{}},serverInfo:{name:'fixture',version:'1'}};else if(r.method==='tools/list')result={tools:[{name:'echo',description:'Echo',inputSchema:{type:'object',properties:{message:{type:'string'}}}}]};else if(r.method==='tools/call')result={content:[{type:'text',text:r.params.arguments.message}]};console.log(JSON.stringify({jsonrpc:'2.0',id:r.id,result}));});`}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			info, err := runtime.DetectRuntime(runtime.DetectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			rt, err := dockerruntime.NewWithInfo(info)
			if err != nil {
				t.Fatal(err)
			}
			defer rt.Close()
			if err := rt.EnsureImage(ctx, fixture.image); err != nil {
				t.Fatal(err)
			}
			uid, gid := uint32(65534), uint32(65534)
			server := config.MCPServer{Image: fixture.image, Transport: "stdio", Execution: &config.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid}}
			contract, err := config.ResolveExecution(server)
			if err != nil {
				t.Fatal(err)
			}
			cfg := runtime.WorkloadConfig{Name: "fixture", Stack: fmt.Sprintf("execution-mcp-%d", time.Now().UnixNano()), Type: runtime.WorkloadTypeMCPServer, Image: fixture.image, Command: fixture.command, Transport: "stdio", Execution: contract}
			defer func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				exists, id, err := rt.Exists(cleanupCtx, dockerruntime.ContainerName(cfg.Stack, cfg.Name))
				if err != nil {
					t.Error(err)
					return
				}
				if exists {
					if err := rt.Remove(cleanupCtx, id); err != nil {
						t.Error(err)
					}
				}
			}()
			status, err := rt.Start(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			gateway := mcp.NewGateway()
			gateway.SetDockerClient(rt.Client())
			client, err := gateway.BuildAgentClient(ctx, mcp.MCPServerConfig{Name: "fixture", Transport: mcp.TransportStdio, ContainerID: string(status.ID), ProtocolGeneration: "handshake", Execution: server.Execution,
				ExecutionCheck: func(ctx context.Context) (*execution.Report, error) {
					return rt.CheckExecution(ctx, string(status.ID), contract)
				},
				ExecutionBeforeStart: func(ctx context.Context) error { return rt.CheckExecutionBeforeStart(ctx, string(status.ID), contract) },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer client.(io.Closer).Close()
			message := strings.Repeat("representative payload ", 1024)
			for range 20 {
				result, err := client.CallTool(ctx, "echo", map[string]any{"message": message})
				if err != nil || result == nil || result.IsError {
					t.Fatalf("bounded MCP workload failed: %v", err)
				}
			}
			t.Logf("%s completed handshake and 20 bounded JSON round trips with memory=%d cpu_millis=%d pids=%d tmpfs=%d", fixture.name, contract.MemoryBytes, contract.CPUMillis, contract.PIDs, contract.Tmpfs[0].SizeBytes)
			if fixture.name == "python" {
				runExecutionProbe(t, ctx, rt, string(status.ID), `import os, socket, subprocess, errno
assert os.getuid() == 65534 and os.getgid() == 65534
assert [name for _, name in socket.if_nameindex()] == ['lo']
with open('/tmp/probe', 'w') as f: f.write('#!/bin/sh\nexit 0\n')
os.chmod('/tmp/probe', 0o700)
try:
 subprocess.run(['/tmp/probe'], check=True)
 raise AssertionError('scratch executable')
except PermissionError: pass
os.unlink('/tmp/probe')
try:
 with open('/tmp/bounded-scratch', 'wb', buffering=0) as f:
  for _ in range(80): f.write(b'x' * (1024 * 1024))
 raise AssertionError('scratch size was not bounded')
except OSError as error: assert error.errno == errno.ENOSPC
finally: os.unlink('/tmp/bounded-scratch')
try:
 open('/forbidden-root-write', 'w').close()
 raise AssertionError('root filesystem writable')
except OSError as error: assert error.errno in (errno.EROFS, errno.EACCES)
try:
 socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_ICMP)
 raise AssertionError('raw socket allowed')
except PermissionError: pass
try:
 os.setuid(0)
 raise AssertionError('root transition allowed')
except PermissionError: pass
children=[]
try:
 for _ in range(129):
  try: children.append(subprocess.Popen(['/bin/sleep', '30']))
  except OSError as error:
   assert error.errno == errno.EAGAIN
   break
 else: raise AssertionError('PID limit not enforced')
 assert children
finally:
 for child in children: child.terminate()
 for child in children: child.wait(timeout=5)
`)
			}
			evidence, err := rt.CheckExecution(ctx, string(status.ID), contract)
			if err != nil {
				t.Fatal(err)
			}
			for _, control := range evidence.Controls {
				if control.Field == "memory_peak_bytes" {
					t.Logf("%s memory peak: %s bytes", fixture.name, control.Observed)
				}
			}
			if info.Type == runtime.RuntimePodman {
				// Podman 4.9 has no Docker-compatible update endpoint. Target
				// the same daemon and fixture ID through its supported native CLI.
				cmd := exec.CommandContext(ctx, "podman", "--remote", "--url", info.DockerHost(), "update", "--cpu-period", "100000", "--cpu-quota", "200000", string(status.ID))
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("controlled Podman CPU update: %v: %s", err, output)
				}
			} else {
				updater, ok := rt.Client().(interface {
					ContainerUpdate(context.Context, string, container.UpdateConfig) (container.UpdateResponse, error)
				})
				if !ok {
					t.Fatal("runtime update test primitive unavailable")
				}
				if _, err := updater.ContainerUpdate(ctx, string(status.ID), container.UpdateConfig{Resources: container.Resources{CPUPeriod: 100000, CPUQuota: 200000}}); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := rt.Client().ContainerInspect(ctx, string(status.ID))
			if err != nil || changed.HostConfig == nil || changed.HostConfig.CPUPeriod != 100000 || changed.HostConfig.CPUQuota != 200000 {
				t.Fatalf("controlled update did not change the fixture CPU ceiling: %v", err)
			}
			if _, err := client.CallTool(ctx, "echo", map[string]any{"message": "must not route"}); err == nil {
				t.Fatal("required CPU mismatch routed")
			}
			observed, err := rt.Client().ContainerInspect(ctx, string(status.ID))
			if err != nil {
				t.Fatal(err)
			}
			if observed.State.Running {
				t.Fatal("noncompliant instance was not stopped")
			}
			replacement, err := rt.Start(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if replacement.ID == status.ID || replacement.Execution == nil || !replacement.Execution.Eligible {
				t.Fatal("retry retained the weaker instance")
			}
		})
	}
}

func runExecutionProbe(t *testing.T, ctx context.Context, rt *dockerruntime.DockerRuntime, id, script string) {
	t.Helper()
	client, ok := rt.Client().(interface {
		ContainerExecCreate(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error)
		ContainerExecStart(context.Context, string, container.ExecStartOptions) error
		ContainerExecInspect(context.Context, string) (container.ExecInspect, error)
	})
	if !ok {
		t.Fatal("required real runtime exec test primitive unavailable")
	}
	created, err := client.ContainerExecCreate(ctx, id, container.ExecOptions{Cmd: []string{"python", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ContainerExecStart(ctx, created.ID, container.ExecStartOptions{Detach: true}); err != nil {
		t.Fatal(err)
	}
	for {
		state, err := client.ContainerExecInspect(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !state.Running {
			if state.ExitCode != 0 {
				t.Fatalf("controlled behavior probe exited %d", state.ExitCode)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
