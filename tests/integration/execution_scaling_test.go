//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
	dockerruntime "github.com/gridctl/gridctl/pkg/runtime/docker"
)

func TestExecution_RealScaleGrowthAndWake(t *testing.T) {
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
	stack := fmt.Sprintf("execution-scale-%d", time.Now().UnixNano())
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		workloads, err := rt.List(cleanupCtx, runtime.WorkloadFilter{Stack: stack})
		if err != nil {
			t.Error(err)
			return
		}
		for _, workload := range workloads {
			if err := rt.Remove(cleanupCtx, workload.ID); err != nil {
				t.Error(err)
			}
		}
	}()
	uid, gid := uint32(65534), uint32(65534)
	server := config.MCPServer{Name: "fixture", Image: "python:3.13-alpine", Transport: "stdio", Command: []string{"python", "-u", "-c", pythonFixtureModule + "\nmain()\n"}, Execution: &config.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid}}
	contract, err := config.ResolveExecution(server)
	if err != nil {
		t.Fatal(err)
	}
	gateway := mcp.NewGateway()
	gateway.SetDockerClient(rt.Client())
	spawner := controller.NewContainerSpawner(controller.ContainerSpawnerOptions{Builder: gateway, Runtime: rt, Stack: stack, Server: server, Image: server.Image, Transport: "stdio"})
	set := mcp.NewReplicaSet(server.Name, "round-robin", nil)
	gateway.Router().AddReplicaSet(set)
	gateway.SetServerMeta(mcp.MCPServerConfig{Name: server.Name, Transport: mcp.TransportStdio, Execution: server.Execution, ExecutionRequested: execution.RequestedReport(contract)})
	defer gateway.UnregisterMCPServer(server.Name)
	policy := mcp.AutoscalePolicy{Min: 0, Max: 2, TargetInFlight: 1, IdleToZero: true, ScaleUpAfter: 10 * time.Second, ScaleDownAfter: time.Minute}
	scaler := mcp.NewAutoscaler(server.Name, set, spawner, policy, nil)
	if got := gateway.Status(); len(got) != 1 || len(got[0].Replicas) != 0 || got[0].Execution.Eligible {
		t.Fatal("idle state claimed evidence")
	}
	if err := scaler.TriggerColdStart(ctx); err != nil {
		t.Fatal(err)
	}
	first := gateway.ReplicaStatuses(server.Name)
	if len(first) != 1 || first[0].Execution == nil || !first[0].Execution.Eligible {
		t.Fatal("cold start lacks required evidence")
	}
	policy.Min, policy.IdleToZero = 2, false
	scaler.UpdatePolicy(policy)
	now := time.Now()
	if _, err := scaler.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	if got := gateway.ReplicaStatuses(server.Name); len(got) != 2 {
		t.Fatal("growth did not create two replicas")
	} else {
		for _, replica := range got {
			if replica.Execution == nil || !replica.Execution.Eligible || replica.Execution.Revision != contract.Revision {
				t.Fatal("growth lost contract or evidence")
			}
		}
	}
	policy.Min, policy.IdleToZero = 0, true
	scaler.UpdatePolicy(policy)
	for i := 0; i < 20 && set.HealthyCount() != 0; i++ {
		if _, err := scaler.Tick(ctx, now.Add(time.Duration(i+1)*10*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if got := gateway.Status(); len(got) != 1 || len(got[0].Replicas) != 0 || got[0].Execution.Eligible {
		t.Fatal("idle-to-zero retained active evidence")
	}
	if err := scaler.TriggerColdStart(ctx); err != nil {
		t.Fatal(err)
	}
	woken := gateway.ReplicaStatuses(server.Name)
	if len(woken) != 1 || woken[0].Execution == nil || !woken[0].Execution.Eligible || woken[0].Execution.Instance == first[0].Execution.Instance {
		t.Fatal("wake reused stale instance evidence")
	}
}
