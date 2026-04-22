package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker" // Register DockerRuntime factory
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
)

var (
	statusStack          string
	statusShowReplicas   bool
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of gateways and containers",
	Long: `Displays the current status of gridctl-managed gateways and containers.

Shows running gateways with their ports, and container states.
Use --stack to filter by a specific stack.
Use --replicas to expand multi-replica servers to one row per replica.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus(statusStack, statusShowReplicas)
	},
}

func init() {
	statusCmd.Flags().StringVarP(&statusStack, "stack", "s", "", "Only show containers from this stack")
	statusCmd.Flags().BoolVar(&statusShowReplicas, "replicas", false, "Expand to one row per replica instead of rolled-up per-server state")
}

func runStatus(stack string, showReplicas bool) error {
	printer := output.New()

	// Show gateway status from state files
	states, err := state.List()
	if err != nil && !os.IsNotExist(err) {
		printer.Warn("could not read state files", "error", err)
	}

	// Filter by stack if specified
	var filteredStates []state.DaemonState
	for _, s := range states {
		if stack == "" || s.StackName == stack {
			filteredStates = append(filteredStates, s)
		}
	}

	// Build gateway summaries
	var gateways []output.GatewaySummary
	for _, s := range filteredStates {
		status := "stopped"
		if state.IsRunning(&s) {
			status = "running"
		}
		gw := output.GatewaySummary{
			Name:    s.StackName,
			Port:    s.Port,
			PID:     s.PID,
			Status:  status,
			Started: formatDuration(time.Since(s.StartedAt)),
		}
		// Query the running gateway for code mode status
		if status == "running" {
			gw.CodeMode = queryCodeMode(s.Port)
		}
		gateways = append(gateways, gw)
	}

	// Load pin status for all filtered stacks (best-effort; errors are non-fatal).
	pinLabels := loadPinLabels(filteredStates)

	// Show container status (graceful degradation when Docker unavailable)
	var containers []output.ContainerSummary
	rt, err := runtime.New()
	if err != nil {
		printer.Warn("could not initialize runtime — container status unavailable", "error", err)
	} else {
		defer rt.Close()
		ctx := context.Background()
		workloadStatuses, statusErr := rt.Status(ctx, stack)
		if statusErr != nil {
			printer.Warn("container runtime unavailable — container status not shown", "error", statusErr)
		} else {
			for _, s := range workloadStatuses {
				// Get workload name from labels
				var workloadName string
				if s.Labels != nil {
					if name, ok := s.Labels[runtime.LabelMCPServer]; ok {
						workloadName = name
					} else if name, ok := s.Labels[runtime.LabelResource]; ok {
						workloadName = name
					} else if name, ok := s.Labels[runtime.LabelAgent]; ok {
						workloadName = name
					}
				}
				// Truncate ID for display
				id := string(s.ID)
				if len(id) > 12 {
					id = id[:12]
				}
				containers = append(containers, output.ContainerSummary{
					ID:        id,
					Name:      workloadName,
					Type:      string(s.Type),
					Image:     s.Image,
					State:     string(s.State),
					Message:   s.Message,
					PinStatus: pinLabels[workloadName],
				})
			}
		}
	}

	if len(containers) == 0 && len(gateways) == 0 {
		printer.Info("No managed gateways or containers found")
		return nil
	}

	// Print tables
	printer.Gateways(gateways)
	printer.Containers(containers)

	// MCP server tables: query each running gateway for replica info. If
	// --replicas is set, render one row per replica instead of a rollup.
	for _, s := range filteredStates {
		if !state.IsRunning(&s) {
			continue
		}
		servers := queryMCPServers(s.Port)
		if len(servers) == 0 {
			continue
		}
		if showReplicas {
			printer.Replicas(buildReplicaDetails(servers))
		} else {
			printer.MCPServers(buildMCPRollup(servers))
		}
	}

	// Show trace activity summary for each running gateway.
	for _, s := range filteredStates {
		if state.IsRunning(&s) {
			if count := queryTraceCount(s.Port); count >= 0 {
				printer.Info("traces recorded (last 24h)", "stack", s.StackName, "count", count)
			}
		}
	}

	return nil
}

// queryTraceCount queries a running gateway for the number of recorded traces.
// Returns -1 if the gateway is unreachable or tracing is unavailable.
func queryTraceCount(port int) int {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/traces", port))
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	var traces []struct{}
	if json.NewDecoder(resp.Body).Decode(&traces) == nil {
		return len(traces)
	}
	return -1
}

// mcpServerAPI mirrors the subset of internal/api.MCPServerStatus the CLI needs
// to render rolled-up and per-replica views. Defined locally so the CLI does
// not pull in the internal/api package.
type mcpServerAPI struct {
	Name         string            `json:"name"`
	Transport    string            `json:"transport"`
	External     bool              `json:"external"`
	LocalProcess bool              `json:"localProcess"`
	SSH          bool              `json:"ssh"`
	OpenAPI      bool              `json:"openapi"`
	Replicas     []mcpReplicaAPI   `json:"replicas,omitempty"`
	Autoscale    *autoscaleAPI     `json:"autoscale,omitempty"`
}

// autoscaleAPI mirrors the subset of mcp.AutoscaleStatus the CLI renders in
// the AUTOSCALE column.
type autoscaleAPI struct {
	Min            int `json:"min"`
	Max            int `json:"max"`
	Current        int `json:"current"`
	Target         int `json:"target"`
	TargetInFlight int `json:"targetInFlight"`
}

// mcpReplicaAPI is the per-replica slice of mcpServerAPI.
type mcpReplicaAPI struct {
	ReplicaID       int        `json:"replicaId"`
	State           string     `json:"state"`
	Healthy         bool       `json:"healthy"`
	InFlight        int64      `json:"inFlight"`
	StartedAt       time.Time  `json:"startedAt,omitempty"`
	RestartAttempts uint32     `json:"restartAttempts,omitempty"`
	NextRetryAt     *time.Time `json:"nextRetryAt,omitempty"`
	PID             int        `json:"pid,omitempty"`
	ContainerID     string     `json:"containerId,omitempty"`
}

// queryMCPServers fetches the /api/mcp-servers payload from a running
// gateway. Returns nil if the gateway is unreachable or the response is
// malformed — the CLI renders whatever it can.
func queryMCPServers(port int) []mcpServerAPI {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/mcp-servers", port))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var servers []mcpServerAPI
	if err := json.NewDecoder(resp.Body).Decode(&servers); err != nil {
		return nil
	}
	return servers
}

// buildMCPRollup converts API statuses into the rolled-up table rows shown
// by `gridctl status`.
func buildMCPRollup(servers []mcpServerAPI) []output.MCPServerRollup {
	rows := make([]output.MCPServerRollup, 0, len(servers))
	now := time.Now()
	for _, srv := range servers {
		row := output.MCPServerRollup{
			Name:      srv.Name,
			Type:      mcpServerType(srv),
			Replicas:  "—",
			State:     "healthy",
			Autoscale: formatAutoscaleCell(srv.Autoscale),
		}
		n := len(srv.Replicas)
		if n == 0 {
			// Autoscaled servers can legitimately report 0 replicas (scale-
			// to-zero): show the autoscale stats but still label unknown.
			if srv.Autoscale != nil {
				row.State = "idle"
				rows = append(rows, row)
				continue
			}
			// No replica info (e.g. pre-phase-3 daemon, or external transport).
			row.State = "unknown"
			rows = append(rows, row)
			continue
		}
		healthy := 0
		var firstRestarting *mcpReplicaAPI
		for i := range srv.Replicas {
			r := &srv.Replicas[i]
			if r.Healthy {
				healthy++
			} else if firstRestarting == nil && r.State == "restarting" {
				firstRestarting = r
			}
		}
		if n > 1 {
			row.Replicas = fmt.Sprintf("%d/%d", healthy, n)
		}
		switch healthy {
		case n:
			row.State = "healthy"
		case 0:
			row.State = "unhealthy"
		default:
			row.State = formatDegradedState(firstRestarting, now)
		}
		rows = append(rows, row)
	}
	return rows
}

// formatAutoscaleCell produces the "min/current/max (target=N)" render used
// in the AUTOSCALE column. Returns "" when the server is not autoscaled so
// the column renderer can suppress it for static-only stacks.
func formatAutoscaleCell(a *autoscaleAPI) string {
	if a == nil {
		return ""
	}
	return fmt.Sprintf("%d/%d/%d (target=%d)", a.Min, a.Current, a.Max, a.TargetInFlight)
}

// buildReplicaDetails converts API statuses into the per-replica rows for
// `gridctl status --replicas`. Autoscaled servers with zero replicas still
// produce one synthetic row so scale-to-zero state is visible.
func buildReplicaDetails(servers []mcpServerAPI) []output.ReplicaDetail {
	var rows []output.ReplicaDetail
	now := time.Now()
	for _, srv := range servers {
		autoCell := formatAutoscaleCell(srv.Autoscale)
		if len(srv.Replicas) == 0 && srv.Autoscale != nil {
			rows = append(rows, output.ReplicaDetail{
				Server:    srv.Name,
				Replica:   0,
				Handle:    "—",
				State:     "idle",
				Uptime:    "—",
				InFlight:  0,
				Autoscale: autoCell,
			})
			continue
		}
		for _, r := range srv.Replicas {
			row := output.ReplicaDetail{
				Server:    srv.Name,
				Replica:   r.ReplicaID,
				Handle:    replicaHandle(r),
				State:     r.State,
				InFlight:  r.InFlight,
				Autoscale: autoCell,
			}
			if r.Healthy && !r.StartedAt.IsZero() {
				row.Uptime = formatUptime(now.Sub(r.StartedAt))
			} else {
				row.Uptime = "—"
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// mcpServerType returns the transport label shown in the rollup view.
func mcpServerType(srv mcpServerAPI) string {
	switch {
	case srv.External:
		return "external"
	case srv.OpenAPI:
		return "openapi"
	case srv.LocalProcess:
		return "local-process"
	case srv.SSH:
		return "ssh"
	case srv.Transport != "":
		return srv.Transport
	default:
		return "container"
	}
}

// replicaHandle returns a short handle for the replica row's PID/container
// column. Prefers PID for local-process replicas, a truncated container id
// otherwise, or "—" when neither is known.
func replicaHandle(r mcpReplicaAPI) string {
	if r.PID > 0 {
		return fmt.Sprintf("%d", r.PID)
	}
	if r.ContainerID != "" {
		if len(r.ContainerID) > 12 {
			return r.ContainerID[:12]
		}
		return r.ContainerID
	}
	return "—"
}

// formatDegradedState composes the "degraded" annotation including next-retry
// info for the first restarting replica, matching the UX spec.
func formatDegradedState(r *mcpReplicaAPI, now time.Time) string {
	if r == nil {
		return "degraded"
	}
	if r.NextRetryAt != nil && !r.NextRetryAt.IsZero() {
		d := r.NextRetryAt.Sub(now)
		if d > 0 {
			return fmt.Sprintf("degraded (replica-%d restarting, next in %s)", r.ReplicaID, formatRetry(d))
		}
	}
	return fmt.Sprintf("degraded (replica-%d restarting)", r.ReplicaID)
}

// formatRetry formats a retry delay compactly ("4s" / "1m20s").
func formatRetry(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return d.String()
}

// formatUptime formats a duration as human-readable uptime ("12m", "3h", "2d").
func formatUptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// queryCodeMode queries a running gateway's API for code mode status.
// Returns "on" if active, empty string otherwise.
func queryCodeMode(port int) string {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/api/status", port))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var status struct {
		CodeMode string `json:"code_mode"`
	}
	if json.NewDecoder(resp.Body).Decode(&status) == nil {
		return status.CodeMode
	}
	return ""
}

// loadPinLabels loads pin status for all provided stacks and returns a map
// from server name to display label. Errors are logged and silently ignored
// so pin status is always best-effort and never blocks the status command.
func loadPinLabels(states []state.DaemonState) map[string]string {
	labels := make(map[string]string)
	for _, s := range states {
		ps := pins.New(s.StackName)
		if err := ps.Load(); err != nil {
			slog.Debug("status: could not load pins", "stack", s.StackName, "error", err)
			continue
		}
		for name, sp := range ps.GetAll() {
			labels[name] = pinStatusLabel(sp.Status)
		}
	}
	return labels
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}
