package docker

import (
	"context"
	"fmt"

	"agentlab/pkg/dockerclient"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// EnsureNetwork creates the network if it doesn't exist.
// The topology parameter is used for labeling (for cleanup).
func EnsureNetwork(ctx context.Context, cli dockerclient.DockerClient, name, driver, topology string) (string, error) {
	// Check if network exists
	networks, err := cli.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return "", fmt.Errorf("listing networks: %w", err)
	}

	for _, n := range networks {
		if n.Name == name {
			return n.ID, nil
		}
	}

	// Create network with topology label for cleanup
	labels := map[string]string{
		LabelManaged: "true",
	}
	if topology != "" {
		labels[LabelTopology] = topology
	}

	resp, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: driver,
		Labels: labels,
	})
	if err != nil {
		return "", fmt.Errorf("creating network %s: %w", name, err)
	}

	return resp.ID, nil
}

// ListManagedNetworks returns all networks managed by agentlab for a topology.
func ListManagedNetworks(ctx context.Context, cli dockerclient.DockerClient, topology string) ([]string, error) {
	filterArgs := filters.NewArgs(
		filters.Arg("label", LabelManaged+"=true"),
	)
	if topology != "" {
		filterArgs.Add("label", LabelTopology+"="+topology)
	}

	networks, err := cli.NetworkList(ctx, network.ListOptions{
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}

	var names []string
	for _, n := range networks {
		names = append(names, n.Name)
	}
	return names, nil
}

// RemoveNetwork removes a network by name.
func RemoveNetwork(ctx context.Context, cli dockerclient.DockerClient, name string) error {
	return cli.NetworkRemove(ctx, name)
}
