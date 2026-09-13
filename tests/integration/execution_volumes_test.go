//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/runtime"
	dockerruntime "github.com/gridctl/gridctl/pkg/runtime/docker"
)

func TestExecution_RealDataVolume(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
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
	if err := rt.EnsureImage(ctx, "python:3.13-alpine"); err != nil {
		t.Fatal(err)
	}
	stack := fmt.Sprintf("execution-volume-%d", time.Now().UnixNano())
	volume := stack + "-data"
	networkName := stack + "-net"
	volumeClient, ok := rt.Client().(interface {
		VolumeRemove(context.Context, string, bool) error
	})
	if !ok {
		t.Fatal("required volume cleanup primitive unavailable")
	}
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		for _, name := range []string{"init", "fixture"} {
			exists, id, err := rt.Exists(cleanupCtx, dockerruntime.ContainerName(stack, name))
			if err != nil {
				t.Error(err)
				continue
			}
			if exists {
				if err := rt.Remove(cleanupCtx, id); err != nil {
					t.Error(err)
				}
			}
		}
		if err := volumeClient.VolumeRemove(cleanupCtx, volume, true); err != nil {
			t.Error(err)
		}
		if err := rt.RemoveNetwork(cleanupCtx, networkName); err != nil {
			t.Error(err)
		}
	}()
	if err := rt.EnsureNetwork(ctx, networkName, runtime.NetworkOptions{Driver: "bridge", Stack: stack}); err != nil {
		t.Fatal(err)
	}
	// Fixture provisioning is a separate resource operation on a newly named
	// volume. Product hardening never changes ownership or relaxes a profile.
	init, err := rt.Start(ctx, runtime.WorkloadConfig{Name: "init", Stack: stack, Type: runtime.WorkloadTypeResource, Image: "python:3.13-alpine", NetworkName: networkName, Transport: "stdio", Command: []string{"python", "-c", "import os; os.chown('/data', 65534, 65534); os.chmod('/data', 0o700)"}, Volumes: []string{volume + ":/data"}, Labels: map[string]string{runtime.LabelManaged: "true", runtime.LabelStack: stack, runtime.LabelResource: "init"}})
	if err != nil {
		t.Fatal(err)
	}
	for {
		state, err := rt.Client().ContainerInspect(ctx, string(init.ID))
		if err != nil {
			t.Fatal(err)
		}
		if !state.State.Running {
			if state.State.ExitCode != 0 {
				t.Fatal("fixture volume provisioning failed")
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if init.Execution == nil || init.Execution.Mode != "not-covered" {
		t.Fatal("resource fixture not identified as outside hardening scope")
	}
	uid, gid, writable := uint32(65534), uint32(65534), false
	mounts := []config.ExecutionMount{{Source: volume, Target: "/data", ReadOnly: &writable}}
	server := config.MCPServer{Image: "python:3.13-alpine", Transport: "stdio", Execution: &config.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid, Mounts: &mounts}}
	contract, err := config.ResolveExecution(server)
	if err != nil {
		t.Fatal(err)
	}
	status, err := rt.Start(ctx, runtime.WorkloadConfig{Name: "fixture", Stack: stack, Type: runtime.WorkloadTypeMCPServer, Image: server.Image, Transport: "stdio", Command: []string{"sleep", "120"}, Execution: contract})
	if err != nil {
		t.Fatal(err)
	}
	runExecutionProbe(t, ctx, rt, string(status.ID), "import os\nwith open('/data/probe', 'w') as f: f.write('fixture')\nassert open('/data/probe').read() == 'fixture'\nos.unlink('/data/probe')\n")
	if status.Execution == nil || !status.Execution.Eligible {
		t.Fatal("declared data volume lacks admission evidence")
	}
}
