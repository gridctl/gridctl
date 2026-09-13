package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type retirementSpawner struct {
	entered chan struct{}
	reaped  atomic.Bool
}

func (s *retirementSpawner) Spawn(ctx context.Context) (AgentClient, error) {
	close(s.entered)
	<-ctx.Done()
	// Model a provisioner completing just as retirement cancels its operation.
	return NewProcessClient("fixture", nil, "", nil), nil
}

func (s *retirementSpawner) Reap(ctx context.Context, r *Replica) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.reaped.Store(true)
	return r.Client().(*ProcessClient).Close()
}

func TestGateway_RetirementReapsLateSpawn(t *testing.T) {
	gateway := NewGateway()
	set := NewReplicaSet("fixture", ReplicaPolicyRoundRobin, nil)
	spawner := &retirementSpawner{entered: make(chan struct{})}
	scaler := NewAutoscaler("fixture", set, spawner, AutoscalePolicy{Max: 1, IdleToZero: true}, nil)
	gateway.Router().AddReplicaSet(set)
	gateway.autoscalers["fixture"] = scaler
	spawned := make(chan error, 1)
	go func() { spawned <- scaler.TriggerColdStart(t.Context()) }()
	<-spawner.entered
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := gateway.UnregisterMCPServerContext(ctx, "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := <-spawned; err == nil {
		t.Fatal("retired spawn was accepted")
	}
	if !spawner.reaped.Load() || len(set.Replicas()) != 0 || gateway.Router().GetReplicaSet("fixture") != nil {
		t.Fatal("late provisioner escaped retirement cleanup")
	}
	if err := scaler.TriggerColdStart(t.Context()); err == nil {
		t.Fatal("retired scaler started another operation")
	}
}
