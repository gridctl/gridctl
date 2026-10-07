package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/gridctl/gridctl/pkg/execution"
)

const (
	stdioContainerStopTimeout      = 10
	monitorContainerRestartTimeout = 30 * time.Second

	removedContainerReason = "container removed from runtime; re-apply to recreate"
	cleanExitRestartReason = "exited 0; restart: on-failure restarts failure exits only"
)

// containerIdentity is implemented by clients bound to one managed container.
// An empty ID means the client is not a container the monitor can restart.
type containerIdentity interface {
	ContainerID() string
}

// containerRestartError marks a ContainerRestart failure, as distinct from an
// admission failure. Unwrap preserves errdefs.IsNotFound.
type containerRestartError struct {
	err error
}

func (e *containerRestartError) Error() string {
	if e == nil || e.err == nil {
		return "restarting container"
	}
	return e.err.Error()
}

func (e *containerRestartError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

type parsedRestartPolicy struct {
	mode    string
	max     uint32
	display string
}

func parseRestartPolicy(raw string) parsedRestartPolicy {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "", "always":
		return parsedRestartPolicy{mode: "always", display: "always"}
	case "no":
		return parsedRestartPolicy{mode: "no", display: "no"}
	case "on-failure":
		return parsedRestartPolicy{mode: "on-failure", display: "on-failure"}
	}
	n, ok := strings.CutPrefix(raw, "on-failure:")
	if !ok {
		return parsedRestartPolicy{mode: "no", display: raw}
	}
	max, err := strconv.Atoi(n)
	if err != nil || max < 1 {
		return parsedRestartPolicy{mode: "no", display: raw}
	}
	return parsedRestartPolicy{mode: "on-failure", max: uint32(max), display: "on-failure:" + strconv.Itoa(max)}
}

func displayRestartPolicy(raw string) string {
	return parseRestartPolicy(raw).display
}

func restartPolicyNoReason(name string) string {
	return fmt.Sprintf("restart: no disables automatic container restarts; use POST /api/mcp-servers/%s/restart", name)
}

func restartBudgetReason(name, display string, max uint32) string {
	return fmt.Sprintf("restart budget exhausted after %d attempts (restart: %s); use POST /api/mcp-servers/%s/restart", max, display, name)
}

func containerIDOf(client AgentClient) string {
	id, ok := client.(containerIdentity)
	if !ok || id == nil {
		return ""
	}
	return id.ContainerID()
}

func isManagedStdio(cfg MCPServerConfig) bool {
	return cfg.Transport == TransportStdio && !cfg.External && !cfg.LocalProcess && !cfg.SSH && !cfg.OpenAPI && !cfg.A2A
}

// restartManagedContainer admits a hardened container when execution is set,
// then restarts it. A nil docker client or empty ID skips the runtime call
// after admission so the manual path can still refuse a missing admission
// hook. The runtime error is wrapped so errdefs.IsNotFound still matches.
func (g *Gateway) restartManagedContainer(ctx context.Context, name, containerID string, stopTimeout int, execCfg *execution.ExecutionConfig, beforeStart func(context.Context) error) error {
	if execCfg != nil {
		if beforeStart == nil {
			return fmt.Errorf("execution: recovery admission unavailable")
		}
		if err := beforeStart(ctx); err != nil {
			return err
		}
	}
	if g.dockerCli == nil || containerID == "" {
		return nil
	}
	timeout := stopTimeout
	if err := g.dockerCli.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return &containerRestartError{err: fmt.Errorf("restarting container for %s: %w", name, err)}
	}
	return nil
}

func (g *Gateway) serverMetaCopy(name string) MCPServerConfig {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.serverMeta[name]
}

func (g *Gateway) shouldRestartExitedContainer(name string, client AgentClient, exit *ContainerExit) bool {
	if exit == nil || g.dockerCli == nil || containerIDOf(client) == "" {
		return false
	}
	return g.GetAutoscaler(name) == nil
}

func restartRefusal(policy, name string, exit *ContainerExit, restarts uint32) (string, bool) {
	parsed := parseRestartPolicy(policy)
	switch parsed.mode {
	case "no":
		return restartPolicyNoReason(name), true
	case "on-failure":
		if exit != nil && exit.Code == 0 && !exit.OOMKilled {
			return cleanExitRestartReason, true
		}
		if parsed.max > 0 && restarts >= parsed.max {
			return restartBudgetReason(name, parsed.display, parsed.max), true
		}
	}
	return "", false
}

func (g *Gateway) markRestartTerminal(serverName string, replica *Replica, reason string) {
	replica.SetRestartExhausted(reason)
	g.rewriteReplicaError(serverName, replica.ID(), reason)
}

func (g *Gateway) rewriteReplicaError(serverName string, replicaID int, reason string) {
	if reason == "" {
		return
	}
	g.healthMu.Lock()
	defer g.healthMu.Unlock()
	status := g.replicaStatusLocked(serverName, replicaID)
	if status == nil {
		g.setReplicaStatusLocked(serverName, replicaID, &HealthStatus{
			Healthy:   false,
			LastCheck: time.Now(),
			Error:     reason,
		})
		return
	}
	copied := *status
	copied.Error = reason
	g.setReplicaStatusLocked(serverName, replicaID, &copied)
}

func (g *Gateway) noteContainerRestartFailure(logger *slog.Logger, name string, replica *Replica, exit *ContainerExit, now time.Time, err error) {
	delay := replica.Restart().Advance(now)
	if exit != nil {
		logger.Warn("container restart failed", "name", name, "error", err, "next_retry_in", delay, "exit_code", exit.Code, "oom_killed", exit.OOMKilled)
		return
	}
	logger.Warn("container restart failed", "name", name, "error", err, "next_retry_in", delay)
}

func (g *Gateway) recordContainerRestartSuccess(logger *slog.Logger, name string, replica *Replica, exit *ContainerExit, restarts uint32) {
	attempt := replica.Restart().Attempts()
	replica.Restart().Reset()
	replica.SetHealthy(true)
	replica.MarkStarted(time.Now())
	replica.ClearRestartExhausted()
	now := time.Now()
	g.healthMu.Lock()
	g.setReplicaStatusLocked(name, replica.ID(), &HealthStatus{
		Healthy:     true,
		LastCheck:   now,
		LastHealthy: now,
	})
	g.healthMu.Unlock()
	g.router.RefreshTools()
	exitCode := 0
	if exit != nil {
		exitCode = exit.Code
	}
	logger.Info("restarted exited container", "name", name, "attempt", int(attempt), "container_restarts", int(restarts), "exit_code", exitCode)
}

func (g *Gateway) restartPlainContainer(ctx context.Context, name, containerID string) error {
	restartCtx, cancel := context.WithTimeout(ctx, monitorContainerRestartTimeout)
	defer cancel()
	cfg := g.serverMetaCopy(name)
	return g.restartManagedContainer(restartCtx, name, containerID, stdioContainerStopTimeout, cfg.Execution, cfg.ExecutionBeforeStart)
}

func (g *Gateway) restartHardenedFromMonitor(ctx context.Context, logger *slog.Logger, name string, set *ReplicaSet, replica *Replica, client *executionClient, exit *ContainerExit, restarts uint32, now time.Time) {
	restartCtx, cancel := context.WithTimeout(ctx, monitorContainerRestartTimeout)
	defer cancel()
	if err := g.restartExecutionReplica(restartCtx, client); err != nil {
		g.noteContainerRestartFailure(logger, name, replica, exit, now, err)
		return
	}
	if err := g.finishReplicaRecovery(restartCtx, name, set, replica); err != nil {
		g.noteContainerRestartFailure(logger, name, replica, exit, now, err)
		return
	}
	g.recordContainerRestartSuccess(logger, name, replica, exit, restarts)
}

func missingContainer(err error) bool {
	return err != nil && errdefs.IsNotFound(err)
}

func isContainerRestartError(err error) bool {
	var restarted *containerRestartError
	return errors.As(err, &restarted)
}

func (g *Gateway) clearServerRestartState(name string) {
	set := g.router.GetReplicaSet(name)
	if set == nil {
		return
	}
	for _, replica := range set.Replicas() {
		replica.ClearMonitorRestartState()
	}
}
