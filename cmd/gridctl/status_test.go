package main

import (
	"strings"
	"testing"
	"time"
)

func TestBuildMCPRollup_HealthyServers(t *testing.T) {
	now := time.Now()
	servers := []mcpServerAPI{
		{
			Name:         "junos",
			LocalProcess: true,
			Replicas: []mcpReplicaAPI{
				{ReplicaID: 0, Healthy: true, State: "healthy", StartedAt: now.Add(-12 * time.Minute)},
				{ReplicaID: 1, Healthy: true, State: "healthy", StartedAt: now.Add(-12 * time.Minute)},
				{ReplicaID: 2, Healthy: true, State: "healthy", StartedAt: now.Add(-12 * time.Minute)},
			},
		},
		{
			Name:     "github",
			External: true,
			// external transport shows a single-replica set in phase 2;
			// rollup should render "—" in the replicas column.
			Replicas: []mcpReplicaAPI{
				{ReplicaID: 0, Healthy: true, State: "healthy", StartedAt: now.Add(-20 * time.Minute)},
			},
		},
	}

	rows := buildMCPRollup(servers)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Name != "junos" || rows[0].Replicas != "3/3" || rows[0].State != "healthy" {
		t.Errorf("junos row wrong: %+v", rows[0])
	}
	if rows[0].Type != "local-process" {
		t.Errorf("expected local-process type for junos, got %q", rows[0].Type)
	}
	if rows[1].Name != "github" || rows[1].Replicas != "—" || rows[1].State != "healthy" {
		t.Errorf("github row wrong: %+v", rows[1])
	}
	if rows[1].Type != "external" {
		t.Errorf("expected external type for github, got %q", rows[1].Type)
	}
}

func TestBuildMCPRollup_DegradedWithNextRetry(t *testing.T) {
	now := time.Now()
	next := now.Add(4 * time.Second)
	servers := []mcpServerAPI{
		{
			Name:      "docker",
			Transport: "stdio",
			Replicas: []mcpReplicaAPI{
				{ReplicaID: 0, Healthy: true, State: "healthy"},
				{ReplicaID: 1, Healthy: false, State: "restarting", NextRetryAt: &next, RestartAttempts: 1},
				{ReplicaID: 2, Healthy: true, State: "healthy"},
			},
		},
	}
	rows := buildMCPRollup(servers)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Replicas != "2/3" {
		t.Errorf("replicas column = %q, want 2/3", rows[0].Replicas)
	}
	if !strings.HasPrefix(rows[0].State, "degraded (replica-1 restarting") {
		t.Errorf("state should start with degraded; got %q", rows[0].State)
	}
	if !strings.Contains(rows[0].State, "next in ") {
		t.Errorf("state should include retry window; got %q", rows[0].State)
	}
}

func TestBuildMCPRollup_AllUnhealthy(t *testing.T) {
	servers := []mcpServerAPI{
		{
			Name: "broken",
			Replicas: []mcpReplicaAPI{
				{ReplicaID: 0, Healthy: false, State: "unhealthy"},
				{ReplicaID: 1, Healthy: false, State: "unhealthy"},
			},
		},
	}
	rows := buildMCPRollup(servers)
	if rows[0].State != "unhealthy" {
		t.Errorf("all-down server should render unhealthy; got %q", rows[0].State)
	}
	if rows[0].Replicas != "0/2" {
		t.Errorf("replicas column = %q, want 0/2", rows[0].Replicas)
	}
}

func TestBuildReplicaDetails(t *testing.T) {
	now := time.Now()
	servers := []mcpServerAPI{
		{
			Name: "junos",
			Replicas: []mcpReplicaAPI{
				{ReplicaID: 0, Healthy: true, State: "healthy", StartedAt: now.Add(-12 * time.Minute), PID: 82341, InFlight: 2},
				{ReplicaID: 1, Healthy: true, State: "healthy", StartedAt: now.Add(-12 * time.Minute), PID: 82342, InFlight: 0},
			},
		},
		{
			Name: "db",
			Replicas: []mcpReplicaAPI{
				{ReplicaID: 0, Healthy: true, State: "healthy", ContainerID: "abc123def456789", StartedAt: now.Add(-1 * time.Hour)},
			},
		},
	}
	rows := buildReplicaDetails(servers)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Handle != "82341" {
		t.Errorf("expected PID as handle, got %q", rows[0].Handle)
	}
	if rows[0].Uptime != "12m" {
		t.Errorf("uptime = %q, want 12m", rows[0].Uptime)
	}
	if rows[2].Handle != "abc123def456" {
		t.Errorf("container id should be truncated to 12 chars; got %q", rows[2].Handle)
	}
}

func TestBuildReplicaDetails_UnhealthyShowsDash(t *testing.T) {
	servers := []mcpServerAPI{
		{
			Name: "x",
			Replicas: []mcpReplicaAPI{
				{ReplicaID: 0, Healthy: false, State: "restarting"},
			},
		},
	}
	rows := buildReplicaDetails(servers)
	if rows[0].Uptime != "—" {
		t.Errorf("unhealthy replica uptime = %q, want —", rows[0].Uptime)
	}
}
