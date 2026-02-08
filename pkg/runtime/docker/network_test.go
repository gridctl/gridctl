package docker

import (
	"context"
	"fmt"
	"testing"

	"github.com/docker/docker/api/types/network"
)

func TestEnsureNetwork_Creates(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	netID, err := EnsureNetwork(ctx, mock, "test-net", "bridge", "my-stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if netID != "mock-network-test-net" {
		t.Errorf("expected network ID 'mock-network-test-net', got %q", netID)
	}

	if len(mock.CreatedNetworks) != 1 || mock.CreatedNetworks[0] != "test-net" {
		t.Errorf("expected network 'test-net' to be created, got %v", mock.CreatedNetworks)
	}
}

func TestEnsureNetwork_AlreadyExists(t *testing.T) {
	mock := &MockDockerClient{
		Networks: []network.Summary{
			{ID: "existing-net-id", Name: "test-net"},
		},
	}
	ctx := context.Background()

	netID, err := EnsureNetwork(ctx, mock, "test-net", "bridge", "my-stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if netID != "existing-net-id" {
		t.Errorf("expected existing network ID, got %q", netID)
	}

	// Should not create a new network
	if len(mock.CreatedNetworks) != 0 {
		t.Errorf("expected no networks created, got %v", mock.CreatedNetworks)
	}
}

func TestEnsureNetwork_PartialNameMatch(t *testing.T) {
	// Docker name filter does prefix matching, so we must check exact name
	mock := &MockDockerClient{
		Networks: []network.Summary{
			{ID: "net-1", Name: "test-net-extended"},
		},
	}
	ctx := context.Background()

	netID, err := EnsureNetwork(ctx, mock, "test-net", "bridge", "my-stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should create because "test-net-extended" != "test-net"
	if netID != "mock-network-test-net" {
		t.Errorf("expected new network to be created, got ID %q", netID)
	}
	if len(mock.CreatedNetworks) != 1 {
		t.Errorf("expected 1 network created, got %d", len(mock.CreatedNetworks))
	}
}

func TestEnsureNetwork_EmptyStack(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	_, err := EnsureNetwork(ctx, mock, "test-net", "bridge", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.CreatedNetworks) != 1 {
		t.Errorf("expected 1 network created, got %d", len(mock.CreatedNetworks))
	}
}

func TestEnsureNetwork_ListError(t *testing.T) {
	mock := &MockDockerClient{
		NetworkListError: fmt.Errorf("list failed"),
	}

	_, err := EnsureNetwork(context.Background(), mock, "test-net", "bridge", "my-stack")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEnsureNetwork_CreateError(t *testing.T) {
	mock := &MockDockerClient{
		NetworkCreateError: fmt.Errorf("create failed"),
	}

	_, err := EnsureNetwork(context.Background(), mock, "test-net", "bridge", "my-stack")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListManagedNetworks(t *testing.T) {
	mock := &MockDockerClient{
		Networks: []network.Summary{
			{ID: "net-1", Name: "gridctl-stack-net1"},
			{ID: "net-2", Name: "gridctl-stack-net2"},
		},
	}

	names, err := ListManagedNetworks(context.Background(), mock, "my-stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(names) != 2 {
		t.Errorf("expected 2 networks, got %d", len(names))
	}
}

func TestListManagedNetworks_Empty(t *testing.T) {
	mock := &MockDockerClient{}

	names, err := ListManagedNetworks(context.Background(), mock, "my-stack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(names) != 0 {
		t.Errorf("expected 0 networks, got %d", len(names))
	}
}

func TestListManagedNetworks_NoStack(t *testing.T) {
	mock := &MockDockerClient{
		Networks: []network.Summary{
			{ID: "net-1", Name: "some-net"},
		},
	}

	names, err := ListManagedNetworks(context.Background(), mock, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(names) != 1 {
		t.Errorf("expected 1 network, got %d", len(names))
	}
}

func TestListManagedNetworks_Error(t *testing.T) {
	mock := &MockDockerClient{
		NetworkListError: fmt.Errorf("list failed"),
	}

	_, err := ListManagedNetworks(context.Background(), mock, "my-stack")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRemoveNetwork_Success(t *testing.T) {
	mock := &MockDockerClient{}

	err := RemoveNetwork(context.Background(), mock, "test-net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.RemovedNetworks) != 1 || mock.RemovedNetworks[0] != "test-net" {
		t.Errorf("expected network 'test-net' to be removed, got %v", mock.RemovedNetworks)
	}
}

func TestRemoveNetwork_Error(t *testing.T) {
	mock := &MockDockerClient{
		NetworkRemoveError: fmt.Errorf("remove failed"),
	}

	err := RemoveNetwork(context.Background(), mock, "test-net")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
