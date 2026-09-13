package docker

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// Podman 4.9's compatibility flags use cgroup-v1 resource filenames.
// Native info reports the controllers available to the daemon instead.
// This is preflight evidence only; instance-bound kernel limits still gate routing.
func executionPodmanResources(ctx context.Context, socket string) error {
	body, err := executionPodmanRead(ctx, socket, "/v4.0.0/libpod/info")
	if err != nil {
		return fmt.Errorf("execution.resources: %w", err)
	}
	var info struct {
		Host struct {
			CgroupVersion     string   `json:"cgroupVersion"`
			CgroupManager     string   `json:"cgroupManager"`
			CgroupControllers []string `json:"cgroupControllers"`
		} `json:"host"`
		Version struct {
			Version string `json:"Version"`
		} `json:"version"`
	}
	if json.Unmarshal(body, &info) != nil {
		return fmt.Errorf("execution.resources: %w", errExecutionUnknown)
	}
	if info.Version.Version == "" || info.Host.CgroupVersion != "v2" || (info.Host.CgroupManager != "systemd" && info.Host.CgroupManager != "cgroupfs") {
		return fmt.Errorf("execution.resources: required native cgroup v2 capabilities unavailable")
	}
	for _, controller := range []string{"cpu", "memory", "pids"} {
		if !slices.Contains(info.Host.CgroupControllers, controller) {
			return fmt.Errorf("execution.resources: required native cgroup v2 controllers unavailable")
		}
	}
	return nil
}

// Podman's compatibility CapDrop is a difference against daemon defaults, not
// the requested ALL sentinel. Verify native OCI-derived sets for the same ID;
// the started workload must still pass all five kernel capability-set checks.
func (d *DockerRuntime) executionPodmanNoCapabilities(ctx context.Context, id string) (bool, error) {
	client, ok := d.cli.(executionInfoClient)
	if !ok {
		return false, errExecutionUnknown
	}
	endpoint, err := url.Parse(client.DaemonHost())
	if err != nil || endpoint.Scheme != "unix" || len(id) != 64 {
		return false, errExecutionUnknown
	}
	if _, err := hex.DecodeString(id); err != nil {
		return false, errExecutionUnknown
	}
	body, err := executionPodmanRead(ctx, endpoint.Path, "/v4.0.0/libpod/containers/"+id+"/json")
	if err != nil {
		return false, errExecutionUnknown
	}
	var inspect struct {
		ID            string          `json:"Id"`
		EffectiveCaps json.RawMessage `json:"EffectiveCaps"`
		BoundingCaps  json.RawMessage `json:"BoundingCaps"`
	}
	if json.Unmarshal(body, &inspect) != nil || inspect.ID != id || inspect.EffectiveCaps == nil || inspect.BoundingCaps == nil {
		return false, errExecutionUnknown
	}
	// Native inspect's Go slices can encode empty sets as null or []. Keep
	// presence separate from length so an omitted field still refuses admission.
	empty := true
	for _, raw := range []json.RawMessage{inspect.EffectiveCaps, inspect.BoundingCaps} {
		var caps []string
		if json.Unmarshal(raw, &caps) != nil {
			return false, errExecutionUnknown
		}
		empty = empty && len(caps) == 0
	}
	return empty, nil
}

func executionPodmanRead(ctx context.Context, socket, path string) ([]byte, error) {
	return executionPodmanRequest(ctx, socket, path, http.MethodGet, nil, http.StatusOK)
}

func executionPodmanRequest(ctx context.Context, socket, path, method string, payload []byte, status int) ([]byte, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, bytes.NewReader(payload))
	if err != nil {
		return nil, errExecutionUnknown
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, errExecutionUnknown
	}
	defer res.Body.Close()
	if res.StatusCode != status {
		return nil, fmt.Errorf("native endpoint evidence unavailable")
	}
	const maxInfoBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxInfoBytes+1))
	if err != nil || len(body) > maxInfoBytes {
		return nil, errExecutionUnknown
	}
	return body, nil
}
