package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"time"
)

// Podman 4.9's compatibility flags use cgroup-v1 resource filenames.
// Native info reports the controllers available to the daemon instead.
// This is preflight evidence only; instance-bound kernel limits still gate routing.
func executionPodmanResources(ctx context.Context, socket string) error {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/v4.0.0/libpod/info", nil)
	if err != nil {
		return fmt.Errorf("execution.resources: %w", errExecutionUnknown)
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execution.resources: %w", errExecutionUnknown)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("execution.resources: native cgroup capability evidence unavailable")
	}
	const maxInfoBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxInfoBytes+1))
	if err != nil || len(body) > maxInfoBytes {
		return fmt.Errorf("execution.resources: %w", errExecutionUnknown)
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
