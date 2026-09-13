package docker

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/gridctl/gridctl/pkg/dockerclient"
)

// Podman's compatibility create API forces read_write_tmpfs=true. Native
// creation disables those undeclared writable mounts without changing the
// contract. The usual engine and kernel admission still follow creation.
func executionPodmanCreate(ctx context.Context, cli dockerclient.DockerClient, cfg ContainerConfig, c *container.Config, h *container.HostConfig, aliases []string) (container.CreateResponse, error) {
	client, ok := cli.(executionInfoClient)
	if !ok {
		return container.CreateResponse{}, errExecutionUnknown
	}
	endpoint, err := url.Parse(client.DaemonHost())
	if err != nil || endpoint.Scheme != "unix" || endpoint.Path == "" {
		return container.CreateResponse{}, errExecutionUnknown
	}
	// This projection is limited to ContainerConfig's hardened create surface.
	// Keep image defaults for entrypoint, command, and working directory.
	spec := map[string]any{
		"name": cfg.Name, "image": c.Image, "command": c.Cmd,
		"env": cfg.Env, "labels": c.Labels, "stdin": c.OpenStdin,
		"user": c.User, "cap_drop": h.CapDrop, "no_new_privileges": cfg.Execution.NoNewPrivileges,
		"read_only_filesystem": h.ReadonlyRootfs, "read_write_tmpfs": false,
		"systemd": "false", "create_working_dir": true,
		"pidns": map[string]string{"nsmode": "private"},
		"resource_limits": map[string]any{
			"memory": map[string]int64{"limit": h.Memory, "swap": h.MemorySwap},
			"cpu":    map[string]int64{"period": h.CPUPeriod, "quota": h.CPUQuota},
			"pids":   map[string]int64{"limit": *h.PidsLimit},
		},
	}
	var mounts []map[string]any
	for _, scratch := range cfg.Execution.Tmpfs {
		mounts = append(mounts, map[string]any{
			"type": "tmpfs", "source": "tmpfs", "destination": scratch.Target,
			"options": strings.Split(h.Tmpfs[scratch.Target], ","),
		})
	}
	// Do not suppress image volumes: inspection must refuse any undeclared ones.
	spec["mounts"] = mounts
	var volumes []map[string]any
	for _, mount := range cfg.Execution.Mounts {
		mode := "ro"
		if mount.ReadOnly != nil && !*mount.ReadOnly {
			mode = "rw"
		}
		volumes = append(volumes, map[string]any{"name": mount.Source, "dest": mount.Target, "options": []string{mode}})
	}
	spec["volumes"] = volumes
	if cfg.Execution.Network == "none" {
		spec["netns"] = map[string]string{"nsmode": "none"}
	} else {
		spec["netns"] = map[string]string{"nsmode": "bridge"}
		spec["networks"] = map[string]any{cfg.NetworkName: map[string]any{"aliases": aliases}}
		spec["hostadd"] = h.ExtraHosts
		var ports []map[string]any
		for port, bindings := range h.PortBindings {
			for _, binding := range bindings {
				ports = append(ports, map[string]any{"container_port": port.Int(), "host_port": cfg.HostPort, "host_ip": binding.HostIP, "protocol": port.Proto()})
			}
		}
		spec["portmappings"] = ports
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		return container.CreateResponse{}, errExecutionUnknown
	}
	body, err := executionPodmanRequest(ctx, endpoint.Path, "/v4.0.0/libpod/containers/create", http.MethodPost, payload, http.StatusCreated)
	if err != nil {
		return container.CreateResponse{}, err
	}
	var response container.CreateResponse
	if json.Unmarshal(body, &response) != nil || len(response.ID) != 64 {
		return container.CreateResponse{}, errExecutionUnknown
	}
	if _, err := hex.DecodeString(response.ID); err != nil {
		return container.CreateResponse{}, errExecutionUnknown
	}
	return response, nil
}
