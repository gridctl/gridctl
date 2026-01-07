package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/network"
)

func TestEnsureNetwork_Creates(t *testing.T) {
	mock := &MockDockerClient{
		Networks: []network.Summary{}, // Empty - network doesn't exist
	}

	ctx := context.Background()
	id, err := EnsureNetwork(ctx, mock, "test-net", "bridge", "my-topo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id != "mock-network-test-net" {
		t.Errorf("got ID %q, want %q", id, "mock-network-test-net")
	}

	if len(mock.CreatedNetworks) != 1 || mock.CreatedNetworks[0] != "test-net" {
		t.Errorf("expected network 'test-net' to be created, got %v", mock.CreatedNetworks)
	}
}

func TestEnsureNetwork_Exists(t *testing.T) {
	mock := &MockDockerClient{
		Networks: []network.Summary{
			{ID: "existing-id", Name: "test-net"},
		},
	}

	ctx := context.Background()
	id, err := EnsureNetwork(ctx, mock, "test-net", "bridge", "my-topo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id != "existing-id" {
		t.Errorf("got ID %q, want %q", id, "existing-id")
	}

	if len(mock.CreatedNetworks) != 0 {
		t.Errorf("expected no networks to be created, got %v", mock.CreatedNetworks)
	}
}

func TestEnsureNetwork_CreateError(t *testing.T) {
	mock := &MockDockerClient{
		Networks:           []network.Summary{},
		NetworkCreateError: errors.New("create failed"),
	}

	ctx := context.Background()
	_, err := EnsureNetwork(ctx, mock, "test-net", "bridge", "my-topo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, mock.NetworkCreateError) {
		t.Errorf("expected wrapped error containing %v, got %v", mock.NetworkCreateError, err)
	}
}

func TestEnsureNetwork_ListError(t *testing.T) {
	mock := &MockDockerClient{
		NetworkListError: errors.New("list failed"),
	}

	ctx := context.Background()
	_, err := EnsureNetwork(ctx, mock, "test-net", "bridge", "my-topo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListManagedNetworks_FilterByTopology(t *testing.T) {
	mock := &MockDockerClient{
		Networks: []network.Summary{
			{Name: "agentlab-net-1"},
			{Name: "agentlab-net-2"},
		},
	}

	ctx := context.Background()
	names, err := ListManagedNetworks(ctx, mock, "test-topo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(names) != 2 {
		t.Errorf("expected 2 networks, got %d", len(names))
	}
}

func TestListManagedNetworks_Error(t *testing.T) {
	mock := &MockDockerClient{
		NetworkListError: errors.New("list failed"),
	}

	ctx := context.Background()
	_, err := ListManagedNetworks(ctx, mock, "test-topo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRemoveNetwork_Success(t *testing.T) {
	mock := &MockDockerClient{}

	ctx := context.Background()
	err := RemoveNetwork(ctx, mock, "test-net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.RemovedNetworks) != 1 || mock.RemovedNetworks[0] != "test-net" {
		t.Errorf("expected 'test-net' to be removed, got %v", mock.RemovedNetworks)
	}
}

func TestRemoveNetwork_Error(t *testing.T) {
	mock := &MockDockerClient{
		NetworkRemoveError: errors.New("remove failed"),
	}

	ctx := context.Background()
	err := RemoveNetwork(ctx, mock, "test-net")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
