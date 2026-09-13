package docker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/runtime"
)

type executionEngine struct {
	*MockDockerClient
	info system.Info
	err  error
}

func (e *executionEngine) Info(context.Context) (system.Info, error) { return e.info, e.err }
func (e *executionEngine) DaemonHost() string                        { return "unix:///fixture.sock" }

func executionTestContract(t *testing.T) *execution.ExecutionContract {
	t.Helper()
	uid, gid := uint32(1000), uint32(1000)
	contract, err := execution.ResolveExecution(execution.Server{Transport: "stdio", Execution: &execution.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid}})
	if err != nil {
		t.Fatal(err)
	}
	return contract
}

func TestExecution_PreflightRefusesBeforeCreate(t *testing.T) {
	for _, info := range []system.Info{{}, {CgroupVersion: "1", SecurityOptions: []string{"name=seccomp,profile=builtin"}}, {CgroupVersion: "2", MemoryLimit: true, CPUCfsQuota: true, PidsLimit: true}} {
		engine := &executionEngine{MockDockerClient: &MockDockerClient{}, info: info}
		rt := NewWithClient(engine)
		if err := rt.PreflightExecution(context.Background(), executionTestContract(t)); err == nil {
			t.Fatal("unsupported daemon accepted")
		}
		if _, err := rt.Start(context.Background(), runtime.WorkloadConfig{Execution: executionTestContract(t), Type: runtime.WorkloadTypeMCPServer}); err == nil {
			t.Fatal("unsupported launch accepted")
		}
		if len(engine.Calls) != 0 {
			t.Fatalf("create/launch happened before preflight: %v", engine.Calls)
		}
	}
	rt := NewWithClient(&MockDockerClient{})
	if err := rt.PreflightExecution(context.Background(), executionTestContract(t)); err == nil {
		t.Fatal("missing capability API accepted")
	}
	if report, err := rt.CheckExecution(context.Background(), "", nil); report != nil || err != nil {
		t.Fatal("nil contract changed behavior")
	}
	if err := rt.CheckExecutionBeforeStart(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestExecution_InspectRejectsWeakerControls(t *testing.T) {
	for _, mutate := range []func(*container.Config, *container.HostConfig){
		func(c *container.Config, _ *container.HostConfig) { c.User = "root" },
		func(_ *container.Config, h *container.HostConfig) { h.Privileged = true },
		func(_ *container.Config, h *container.HostConfig) { h.PidMode = "host" },
		func(_ *container.Config, h *container.HostConfig) { h.ReadonlyRootfs = false },
		func(_ *container.Config, h *container.HostConfig) { h.CapAdd = []string{"SYS_ADMIN"} },
		func(_ *container.Config, h *container.HostConfig) { h.SecurityOpt = []string{"seccomp=unconfined"} },
		func(_ *container.Config, h *container.HostConfig) { h.MemorySwap = -1 },
		func(_ *container.Config, h *container.HostConfig) { h.CPUQuota = -1 },
		func(_ *container.Config, h *container.HostConfig) { h.PidsLimit = nil },
		func(_ *container.Config, h *container.HostConfig) { h.NetworkMode = "host" },
		func(_ *container.Config, h *container.HostConfig) { h.ExtraHosts = []string{"secret-value"} },
		func(c *container.Config, _ *container.HostConfig) { c.Volumes = map[string]struct{}{"/app": {}} },
	} {
		contract := executionTestContract(t)
		c, h := &container.Config{}, &container.HostConfig{}
		applyExecution(contract, c, h)
		mutate(c, h)
		engine := &executionEngine{info: system.Info{CgroupVersion: "2", MemoryLimit: true, SwapLimit: true, CPUCfsQuota: true, PidsLimit: true, SecurityOptions: []string{"name=seccomp,profile=builtin"}}, MockDockerClient: &MockDockerClient{ContainerDetails: map[string]container.InspectResponse{"fixture": {ContainerJSONBase: &container.ContainerJSONBase{HostConfig: h, State: &container.State{Running: true}}, Config: c}}}}
		rt := NewWithClient(engine)
		if err := rt.CheckExecutionBeforeStart(context.Background(), "fixture", contract); err == nil {
			t.Fatal("weaker settings admitted before start")
		}
		report, err := rt.CheckExecution(context.Background(), "fixture", contract)
		if err == nil || report.Eligible || report.Outcome != "mismatch" {
			t.Fatalf("weaker settings routed: %+v %v", report, err)
		}
		if strings.Contains(err.Error(), "secret-value") {
			t.Fatal("engine value disclosed")
		}
	}
}

func TestExecution_CapabilityErrorsAreRedacted(t *testing.T) {
	rt := NewWithClient(&executionEngine{MockDockerClient: &MockDockerClient{}, err: errors.New("credential-bearing engine error")})
	if err := rt.PreflightExecution(context.Background(), executionTestContract(t)); err == nil || strings.Contains(err.Error(), "credential-bearing") {
		t.Fatalf("unsafe preflight error: %v", err)
	}
}

func TestExecution_InspectTmpfsOptions(t *testing.T) {
	for _, tc := range []struct {
		name, options string
		wantError     bool
	}{
		{"original", "rw,nosuid,nodev,noexec,size=67108864,mode=1777", false},
		{"reordered", "mode=1777,size=67108864,noexec,nodev,nosuid,rw", false},
		{"binary units and private propagation", "rw,rprivate,nosuid,nodev,noexec,size=64m,mode=01777", false},
		{"Podman copy-up", "rw,rprivate,nosuid,nodev,noexec,size=64m,mode=1777,tmpcopyup", false},
		{"copy-up cannot replace noexec", "rw,nosuid,nodev,size=64m,mode=1777,tmpcopyup", true},
		{"copy-up value", "rw,nosuid,nodev,noexec,size=64m,mode=1777,tmpcopyup=true", true},
		{"missing noexec", "rw,nosuid,nodev,size=64m,mode=1777", true},
		{"conflicting exec", "rw,nosuid,nodev,noexec,exec,size=64m,mode=1777", true},
		{"conflicting ro", "rw,ro,nosuid,nodev,noexec,size=64m,mode=1777", true},
		{"unbounded", "rw,nosuid,nodev,noexec,mode=1777", true},
		{"larger", "rw,nosuid,nodev,noexec,size=128m,mode=1777", true},
		{"duplicate size", "rw,nosuid,nodev,noexec,size=64m,size=128m,mode=1777", true},
		{"wrong mode", "rw,nosuid,nodev,noexec,size=64m,mode=0777", true},
		{"unknown option", "rw,nosuid,nodev,noexec,size=64m,mode=1777,private-engine-detail", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract := executionTestContract(t)
			c, h := &container.Config{}, &container.HostConfig{}
			applyExecution(contract, c, h)
			h.Tmpfs[contract.Tmpfs[0].Target] = tc.options
			engine := &executionEngine{info: system.Info{CgroupVersion: "2", MemoryLimit: true, SwapLimit: true, CPUCfsQuota: true, PidsLimit: true, SecurityOptions: []string{"name=seccomp"}}, MockDockerClient: &MockDockerClient{ContainerDetails: map[string]container.InspectResponse{"fixture": {ContainerJSONBase: &container.ContainerJSONBase{HostConfig: h, State: &container.State{}}, Config: c}}}}
			report, err := NewWithClient(engine).inspectExecution(t.Context(), "fixture", contract, false)
			if (err != nil) != tc.wantError || report.Eligible {
				t.Fatalf("tmpfs admission: %+v %v", report, err)
			}
			if err != nil && (!strings.Contains(err.Error(), "data_mounts") || strings.Contains(err.Error(), "private-engine-detail")) {
				t.Fatalf("missing safe field diagnostic: %v", err)
			}
		})
	}
}

func TestExecution_MountInventoryDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		mutate      func(*container.InspectResponse)
	}{
		{"automatic scratch", "scratch_inventory", func(i *container.InspectResponse) {
			i.HostConfig.Tmpfs["/private-detail"] = "rw,nosuid,nodev,tmpcopyup"
		}},
		{"undeclared actual scratch", "scratch_inventory", func(i *container.InspectResponse) {
			i.Mounts = []container.MountPoint{{Type: "tmpfs", Destination: "/private-detail", RW: true}}
		}},
		{"duplicate actual scratch", "scratch_inventory", func(i *container.InspectResponse) {
			i.Mounts = []container.MountPoint{{Type: "tmpfs", Destination: "/tmp", RW: true}, {Type: "tmpfs", Destination: "/tmp", RW: true}}
		}},
		{"image volume", "image_inventory", func(i *container.InspectResponse) { i.Config.Volumes = map[string]struct{}{"/private-detail": {}} }},
		{"weak scratch", "scratch_options", func(i *container.InspectResponse) { i.HostConfig.Tmpfs["/tmp"] = "rw,size=64m,mode=1777,tmpcopyup" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract := executionTestContract(t)
			c, h := &container.Config{}, &container.HostConfig{}
			applyExecution(contract, c, h)
			i := container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{HostConfig: h, State: &container.State{}}, Config: c}
			tc.mutate(&i)
			engine := &executionEngine{info: system.Info{CgroupVersion: "2", MemoryLimit: true, SwapLimit: true, CPUCfsQuota: true, PidsLimit: true, SecurityOptions: []string{"name=seccomp"}}, MockDockerClient: &MockDockerClient{ContainerDetails: map[string]container.InspectResponse{"fixture": i}}}
			report, err := NewWithClient(engine).inspectExecution(t.Context(), "fixture", contract, false)
			if err == nil || report.Eligible || !strings.Contains(err.Error(), "data_mounts."+tc.field) || strings.Contains(err.Error(), "private-detail") {
				t.Fatalf("mount boundary/diagnostic: %+v %v", report, err)
			}
		})
	}
}

func TestExecution_CanceledRequestDoesNotStopWorkload(t *testing.T) {
	engine := &executionEngine{MockDockerClient: &MockDockerClient{}}
	rt := NewWithClient(engine)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := rt.CheckExecution(ctx, "fixture", executionTestContract(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if len(engine.Calls) != 0 {
		t.Fatal("canceled caller triggered runtime mutation")
	}
}

type executionVolumesEngine struct {
	*executionEngine
	volume    volume.Volume
	volumeErr error
}

func (e *executionVolumesEngine) VolumeInspect(context.Context, string) (volume.Volume, error) {
	return e.volume, e.volumeErr
}

func TestExecution_VolumeAuthorityBoundary(t *testing.T) {
	contract := executionTestContract(t)
	contract.Mounts = []execution.ExecutionMount{{Source: "fixture-data", Target: "/data"}}
	for _, tc := range []struct {
		volume                  volume.Volume
		err                     error
		allowMissing, wantError bool
	}{
		{volume: volume.Volume{Driver: "local"}},
		{err: errdefs.ErrNotFound, allowMissing: true},
		{err: errdefs.ErrNotFound, wantError: true},
		{volume: volume.Volume{Driver: "plugin"}, wantError: true},
		{volume: volume.Volume{Driver: "local", Options: map[string]string{"device": "/sensitive-host-source"}}, wantError: true},
	} {
		rt := NewWithClient(&executionVolumesEngine{executionEngine: &executionEngine{MockDockerClient: &MockDockerClient{}}, volume: tc.volume, volumeErr: tc.err})
		err := rt.checkExecutionVolumes(t.Context(), contract, tc.allowMissing)
		if (err != nil) != tc.wantError {
			t.Fatalf("volume boundary: %v", err)
		}
		if err != nil && strings.Contains(err.Error(), "sensitive-host-source") {
			t.Fatal("volume diagnostic disclosed host source")
		}
	}
}

func TestExecution_FailedStartCleanup(t *testing.T) {
	for _, cleanupErr := range []error{nil, errors.New("private engine detail")} {
		engine := &MockDockerClient{ContainerRemoveError: cleanupErr}
		rt := NewWithClient(engine)
		rt.executions = map[string]*execution.ExecutionContract{"fixture": executionTestContract(t)}
		err := rt.failedExecutionStart(t.Context(), "fixture")
		if err == nil || strings.Contains(err.Error(), "private engine detail") {
			t.Fatal("failed start diagnostic is missing or unsafe")
		}
		if cleanupErr == nil && rt.executions["fixture"] != nil {
			t.Fatal("removed instance retained execution state")
		}
		if cleanupErr != nil && !strings.Contains(err.Error(), "cleanup") {
			t.Fatal("cleanup failure was swallowed")
		}
	}
}
