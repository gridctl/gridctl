package docker

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/go-connections/nat"
	"github.com/gridctl/gridctl/pkg/execution"
)

// CheckExecution refreshes instance-bound evidence and stops a rejected workload.
// Callers must refuse routing on any error, including cleanup failure.
func (d *DockerRuntime) CheckExecution(ctx context.Context, id string, e *execution.ExecutionContract) (*execution.Report, error) {
	if e == nil {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	report, err := d.inspectExecution(ctx, id, e, true)
	if err != nil {
		if requestCtx.Err() != nil && report.Outcome == "unknown" {
			return nil, requestCtx.Err()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cleanupCancel()
		if stopErr := StopContainer(cleanupCtx, d.cli, id, 5); stopErr != nil {
			err = errors.Join(err, fmt.Errorf("execution.cleanup: stop noncompliant instance failed"))
		}
		return report, err
	}
	d.executionMu.Lock()
	if d.executions == nil {
		d.executions = map[string]*execution.ExecutionContract{}
	}
	d.executions[id] = e
	d.executionMu.Unlock()
	return report, nil
}

type executionInfoClient interface {
	Info(context.Context) (system.Info, error)
	DaemonHost() string
}

var errExecutionUnknown = errors.New("enforcement evidence unavailable")

func (d *DockerRuntime) failedExecutionStart(ctx context.Context, id string) error {
	return errors.Join(fmt.Errorf("execution.start: engine could not start the requested workload"), d.removeExecutionInstance(ctx, id))
}

func (d *DockerRuntime) removeExecutionInstance(ctx context.Context, id string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := RemoveContainer(cleanupCtx, d.cli, id, true); err != nil {
		return fmt.Errorf("execution.cleanup: startup instance removal failed")
	}
	d.executionMu.Lock()
	delete(d.executions, id)
	d.executionMu.Unlock()
	return nil
}

// CheckExecutionBeforeStart rejects weaker engine settings before recovery.
func (d *DockerRuntime) CheckExecutionBeforeStart(ctx context.Context, id string, e *execution.ExecutionContract) error {
	if e == nil {
		return nil
	}
	_, err := d.inspectExecution(ctx, id, e, false)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if stopErr := StopContainer(cleanupCtx, d.cli, id, 5); stopErr != nil {
			return errors.Join(err, fmt.Errorf("execution.cleanup: noncompliant recovery stop failed"))
		}
	}
	return err
}

type executionVolumeClient interface {
	VolumeInspect(context.Context, string) (volume.Volume, error)
}

func (d *DockerRuntime) checkExecutionVolumes(ctx context.Context, e *execution.ExecutionContract, allowMissing bool) error {
	for _, mount := range e.Mounts {
		cli, ok := d.cli.(executionVolumeClient)
		if !ok {
			return fmt.Errorf("execution.mounts: volume evidence unavailable")
		}
		v, err := cli.VolumeInspect(ctx, mount.Source)
		if allowMissing && errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("execution.mounts: volume inventory unavailable")
		}
		if v.Driver != "local" || len(v.Options) != 0 {
			return fmt.Errorf("execution.mounts: only plain engine-local data volumes supported")
		}
	}
	return nil
}

// PreflightExecution checks daemon capabilities before accepting a transition.
func (d *DockerRuntime) PreflightExecution(ctx context.Context, e *execution.ExecutionContract) error {
	if e == nil {
		return nil
	}
	_, err := d.executionPreflight(ctx, e)
	return err
}

func applyExecution(e *execution.ExecutionContract, c *container.Config, h *container.HostConfig) {
	c.User = fmt.Sprintf("%d:%d", e.UID, e.GID)
	h.Privileged = false
	h.PidMode = ""
	h.ReadonlyRootfs = e.ReadOnly
	h.CapDrop = slices.Clone(e.DropCapabilities)
	if e.NoNewPrivileges {
		h.SecurityOpt = []string{"no-new-privileges:true"}
	}
	h.Memory, h.MemorySwap = e.MemoryBytes, e.MemoryBytes
	h.CPUPeriod, h.CPUQuota = 100000, e.CPUMillis*100
	pids := e.PIDs
	h.PidsLimit = &pids
	h.Tmpfs = map[string]string{}
	for _, scratch := range e.Tmpfs {
		h.Tmpfs[scratch.Target] = fmt.Sprintf("rw,nosuid,nodev,noexec,size=%d,mode=1777", scratch.SizeBytes)
	}
	for _, mount := range e.Mounts {
		mode := "ro"
		if mount.ReadOnly != nil && !*mount.ReadOnly {
			mode = "rw"
		}
		h.Binds = append(h.Binds, mount.Source+":"+mount.Target+":"+mode)
	}
	if e.Network == "none" {
		h.NetworkMode, h.PortBindings, h.ExtraHosts = "none", nil, nil
		c.ExposedPorts = nil
	}
}

func (d *DockerRuntime) executionPreflight(ctx context.Context, e *execution.ExecutionContract) (system.Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if goruntime.GOOS != "linux" {
		return system.Info{}, fmt.Errorf("execution.runtime: trusted daemon-host observation unsupported on this platform")
	}
	client, ok := d.cli.(executionInfoClient)
	if !ok {
		return system.Info{}, fmt.Errorf("execution.runtime: daemon capability evidence unavailable")
	}
	endpoint, err := url.Parse(client.DaemonHost())
	if err != nil || endpoint.Scheme != "unix" {
		return system.Info{}, fmt.Errorf("execution.runtime: daemon-host observation requires a local Unix endpoint")
	}
	info, err := client.Info(ctx)
	if err != nil {
		return system.Info{}, errors.Join(fmt.Errorf("execution.runtime: %w", errExecutionUnknown), ctx.Err())
	}
	seccomp := false
	for _, option := range info.SecurityOptions {
		if strings.Contains(option, "name=seccomp") {
			seccomp = true
		}
	}
	if !seccomp {
		return info, fmt.Errorf("execution.seccomp: engine-default enforcement unsupported")
	}
	if info.CgroupVersion != "2" {
		return info, fmt.Errorf("execution.resources: required cgroup v2 controllers unavailable")
	}
	if !info.MemoryLimit || !info.SwapLimit || !info.CPUCfsQuota || !info.PidsLimit {
		if err := executionPodmanResources(ctx, endpoint.Path); err != nil {
			return info, err
		}
	}
	if err := d.checkExecutionVolumes(ctx, e, true); err != nil {
		return info, err
	}
	return info, nil
}

func (d *DockerRuntime) inspectExecution(ctx context.Context, id string, e *execution.ExecutionContract, started bool) (*execution.Report, error) {
	report := &execution.Report{Mode: e.Mode, Revision: e.Revision, Instance: id, Outcome: "pending", ObservedAt: time.Now().UTC(), Runtime: "docker-compatible", DaemonRootless: "unknown", UserNamespace: "unknown"}
	info, err := d.executionPreflight(ctx, e)
	if err != nil {
		report.Outcome = "unsupported"
		if errors.Is(err, errExecutionUnknown) {
			report.Outcome = "unknown"
		}
		return report, err
	}
	report.DaemonRootless = "false"
	if info.ID != "" {
		report.RuntimeContext = fmt.Sprintf("%x", sha256.Sum256([]byte(info.ID)))
	}
	for _, option := range info.SecurityOptions {
		if strings.Contains(option, "rootless") {
			report.DaemonRootless = "true"
		}
	}
	i, err := d.cli.ContainerInspect(ctx, id)
	if err != nil || i.ContainerJSONBase == nil || i.HostConfig == nil || i.Config == nil || i.State == nil {
		report.Outcome = "unknown"
		return report, fmt.Errorf("execution.inspect: evidence unavailable")
	}
	h := i.HostConfig
	check := func(field, requested, observed string, matches bool) {
		outcome := "observed"
		if !matches {
			outcome = "mismatch"
			report.Outcome = "mismatch"
		}
		report.Controls = append(report.Controls, execution.Control{Field: field, Requested: requested, Observed: observed, Outcome: outcome, Source: "engine-inspect"})
	}
	user := fmt.Sprintf("%d:%d", e.UID, e.GID)
	// Observed fields are deliberately reduced to safe outcomes when their
	// raw values can carry operator-controlled text.
	check("uid_gid", user, "numeric identity comparison", i.Config.User == user)
	check("privileged", "false", strconv.FormatBool(h.Privileged), !h.Privileged)
	check("devices", "no host device grants", "device inventory comparison", len(h.Devices) == 0 && len(h.DeviceRequests) == 0 && len(h.DeviceCgroupRules) == 0)
	check("pid_namespace", "private", "namespace comparison", h.PidMode == "private" || h.PidMode == "")
	check("read_only", strconv.FormatBool(e.ReadOnly), strconv.FormatBool(h.ReadonlyRootfs), h.ReadonlyRootfs == e.ReadOnly)
	noNewPrivileges, unconfined := false, false
	for _, option := range h.SecurityOpt {
		if option == "no-new-privileges" || option == "no-new-privileges:true" {
			noNewPrivileges = true
			continue
		}
		// The profile has no custom seccomp/LSM override. An unrecognized
		// override cannot establish preservation of engine-default protections.
		unconfined = true
	}
	check("no_new_privileges", strconv.FormatBool(e.NoNewPrivileges), strconv.FormatBool(noNewPrivileges), !e.NoNewPrivileges || noNewPrivileges)
	check("seccomp", "engine-default", "profile identity unknown", !unconfined)
	dropped := slices.Clone(h.CapDrop)
	for index, cap := range dropped {
		dropped[index] = strings.TrimPrefix(strings.ToUpper(cap), "CAP_")
	}
	slices.Sort(dropped)
	check("capabilities", "declared drops; no additions", "capability set comparison", slices.Equal(dropped, e.DropCapabilities) && len(h.CapAdd) == 0)
	check("memory_bytes", strconv.FormatInt(e.MemoryBytes, 10), strconv.FormatInt(h.Memory, 10), h.Memory == e.MemoryBytes && h.MemorySwap == e.MemoryBytes)
	check("cpu_millis", strconv.FormatInt(e.CPUMillis, 10), "quota comparison", h.CPUPeriod >= 1000 && h.CPUPeriod <= 1000000 && h.CPUQuota > 0 && h.CPUQuota <= 1000000000 && h.CPUQuota*1000 == e.CPUMillis*h.CPUPeriod)
	check("pids", strconv.FormatInt(e.PIDs, 10), "limit comparison", h.PidsLimit != nil && *h.PidsLimit == e.PIDs)
	if e.Network == "none" {
		noBindings := true
		if i.NetworkSettings != nil {
			for _, bindings := range i.NetworkSettings.Ports {
				if len(bindings) != 0 {
					noBindings = false
				}
			}
		}
		check("network", "none", "endpoint inventory comparison", h.NetworkMode == "none" && !h.PublishAllPorts && noBindings && len(h.PortBindings) == 0 && len(h.ExtraHosts) == 0 && (i.NetworkSettings == nil || len(i.NetworkSettings.Networks) == 0 || (len(i.NetworkSettings.Networks) == 1 && i.NetworkSettings.Networks["none"] != nil)))
	} else {
		mode := string(h.NetworkMode)
		connected := mode != "" && mode != "host" && mode != "none" && !strings.HasPrefix(mode, "container:") && !h.PublishAllPorts
		if e.NetworkName != "" {
			connected = connected && mode == e.NetworkName
		}
		if started {
			connected = connected && i.NetworkSettings != nil && len(i.NetworkSettings.Networks) == 1 && i.NetworkSettings.Networks[mode] != nil
		}
		if e.Transport != "stdio" {
			port := nat.Port(fmt.Sprintf("%d/tcp", e.Port))
			bindings := h.PortBindings[port]
			connected = connected && e.Port > 0 && len(h.PortBindings) == 1 && len(bindings) == 1 && bindings[0].HostIP == "127.0.0.1"
			if started {
				if i.NetworkSettings == nil {
					connected = false
				} else {
					actual := i.NetworkSettings.Ports[port]
					if len(actual) != 1 || actual[0].HostIP != "127.0.0.1" {
						connected = false
					} else {
						report.EndpointPort, err = strconv.Atoi(actual[0].HostPort)
						connected = connected && err == nil && report.EndpointPort > 0 && report.EndpointPort <= 65535
					}
				}
			}
		} else {
			connected = connected && len(h.PortBindings) == 0
		}
		check("network", "connected exception; no destination filtering", "private network and loopback publication comparison", connected)
	}
	// Engine-created image volumes must not silently add writable state.
	mountsMatch := len(h.Binds) == len(e.Mounts) && len(h.Mounts) == 0 && len(h.Tmpfs) == len(e.Tmpfs)
	declared := map[string]execution.ExecutionMount{}
	for _, mount := range e.Mounts {
		declared[mount.Target] = mount
	}
	for target := range i.Config.Volumes {
		if _, ok := declared[target]; ok {
			continue
		}
		declaredScratch := false
		for _, scratch := range e.Tmpfs {
			if scratch.Target == target {
				declaredScratch = true
			}
		}
		if !declaredScratch {
			mountsMatch = false
		}
	}
	seen := map[string]bool{}
	for _, mount := range i.Mounts {
		if mount.Type == "tmpfs" {
			continue
		}
		want, ok := declared[mount.Destination]
		if !ok || mount.Type != "volume" || mount.Name != want.Source || mount.RW != (want.ReadOnly != nil && !*want.ReadOnly) || seen[mount.Destination] {
			mountsMatch = false
		}
		seen[mount.Destination] = true
	}
	mountsMatch = mountsMatch && len(seen) == len(declared)
	if err := d.checkExecutionVolumes(ctx, e, false); err != nil {
		mountsMatch = false
	}
	for _, scratch := range e.Tmpfs {
		mountsMatch = mountsMatch && h.Tmpfs[scratch.Target] == fmt.Sprintf("rw,nosuid,nodev,noexec,size=%d,mode=1777", scratch.SizeBytes)
	}
	check("data_mounts", "declared bounded tmpfs and unbounded data volumes", "mount inventory comparison", mountsMatch)
	if report.Outcome == "mismatch" {
		return report, fmt.Errorf("execution.inspect: required control mismatch")
	}
	if !started {
		return report, nil
	}
	if !i.State.Running {
		report.Outcome = "unknown"
		return report, fmt.Errorf("execution.runtime: no running instance")
	}
	controls, err := observeExecution(ctx, id, i.State.Pid, e)
	report.Controls = append(report.Controls, controls...)
	if err != nil {
		report.Outcome = "unknown"
		for _, control := range controls {
			if control.Outcome == "mismatch" {
				report.Outcome = "mismatch"
				break
			}
		}
		return report, err
	}
	report.Outcome, report.Eligible = "observed", true
	return report, nil
}
