package output

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// WorkloadSummary contains data for the summary table.
type WorkloadSummary struct {
	Name      string
	Type      string // mcp-server, agent, resource
	Transport string // http, stdio, sse, external, local, ssh
	State     string // running, failed, pending
}

// GatewaySummary contains data for the gateway status table.
type GatewaySummary struct {
	Name     string
	Port     int
	PID      int
	Status   string // running, stopped
	Started  string // human-readable duration
	CodeMode string // "on" or empty
}

// MCPServerRollup is one row of the rolled-up MCP-servers table shown by
// `gridctl status`. A server with a single replica uses "—" in the Replicas
// column to match the UX spec.
type MCPServerRollup struct {
	Name      string
	Type      string // transport label: local-process, container, ssh, external, openapi
	Replicas  string // "N/M" for sets with replicas > 1, "—" for single-replica servers
	State     string // "healthy", "degraded (replica-N restarting, next in 4s)", "unhealthy"
	Autoscale string // "min/current/max (target=N)" for autoscaled servers, empty for static
}

// ReplicaDetail is one row of the expanded `gridctl status --replicas` view.
type ReplicaDetail struct {
	Server    string
	Replica   int
	Handle    string // PID for local-process, container id prefix for container-backed
	State     string
	Uptime    string
	InFlight  int64
	Autoscale string // "min/current/max (target=N)" — repeated per replica row for server-level info, empty for static servers
}

// ContainerSummary contains data for the container status table.
type ContainerSummary struct {
	ID        string
	Name      string
	Type      string // mcp-server, agent, resource
	Image     string
	State     string // running, exited, etc.
	Message   string // status message
	PinStatus string // pinned, drift, approved, unpinned, or empty to omit column
}

// Summary prints the final status table with amber styling.
func (p *Printer) Summary(workloads []WorkloadSummary) {
	if len(workloads) == 0 {
		return
	}

	p.Println()

	t := table.NewWriter()
	t.SetOutputMirror(p.out)
	t.SetStyle(p.tableStyle())

	t.AppendHeader(table.Row{"Name", "Type", "Transport", "State"})

	for _, w := range workloads {
		state := w.State
		if p.isTTY {
			state = colorState(w.State)
		}
		t.AppendRow(table.Row{w.Name, w.Type, w.Transport, state})
	}

	t.Render()
	p.Println()
}

// colorState applies color to state based on status.
func colorState(state string) string {
	var style lipgloss.Style
	switch state {
	case "running", "ready":
		style = lipgloss.NewStyle().Foreground(ColorGreen)
	case "failed", "error", "exited":
		style = lipgloss.NewStyle().Foreground(ColorRed)
	case "pending", "creating":
		style = lipgloss.NewStyle().Foreground(ColorAmber)
	case "stopped":
		style = lipgloss.NewStyle().Foreground(ColorMuted)
	default:
		style = lipgloss.NewStyle().Foreground(ColorGray)
	}
	return style.Render(state)
}

// Gateways prints the gateway status table with amber styling.
func (p *Printer) Gateways(gateways []GatewaySummary) {
	if len(gateways) == 0 {
		return
	}

	p.Section("GATEWAYS")

	t := table.NewWriter()
	t.SetOutputMirror(p.out)
	t.SetStyle(p.tableStyle())

	// Check if any gateway has code mode enabled
	hasCodeMode := false
	for _, g := range gateways {
		if g.CodeMode != "" {
			hasCodeMode = true
			break
		}
	}

	if hasCodeMode {
		t.AppendHeader(table.Row{"Name", "Port", "PID", "Status", "Code Mode", "Started"})
	} else {
		t.AppendHeader(table.Row{"Name", "Port", "PID", "Status", "Started"})
	}

	for _, g := range gateways {
		status := g.Status
		if p.isTTY {
			status = colorState(g.Status)
		}
		if hasCodeMode {
			t.AppendRow(table.Row{g.Name, g.Port, g.PID, status, g.CodeMode, g.Started})
		} else {
			t.AppendRow(table.Row{g.Name, g.Port, g.PID, status, g.Started})
		}
	}

	t.Render()
	p.Println()
}

// Containers prints the container status table with amber styling.
func (p *Printer) Containers(containers []ContainerSummary) {
	if len(containers) == 0 {
		return
	}

	p.Section("CONTAINERS")

	t := table.NewWriter()
	t.SetOutputMirror(p.out)
	t.SetStyle(p.tableStyle())

	// Show PIN STATUS column only when at least one container has pin data.
	hasPins := false
	for _, c := range containers {
		if c.PinStatus != "" {
			hasPins = true
			break
		}
	}

	if hasPins {
		t.AppendHeader(table.Row{"ID", "Name", "Type", "Image", "State", "Pin Status", "Status"})
	} else {
		t.AppendHeader(table.Row{"ID", "Name", "Type", "Image", "State", "Status"})
	}

	for _, c := range containers {
		state := c.State
		if p.isTTY {
			state = colorState(c.State)
		}
		if hasPins {
			pinStatus := c.PinStatus
			if p.isTTY {
				pinStatus = colorPinStatus(c.PinStatus)
			}
			t.AppendRow(table.Row{c.ID, c.Name, c.Type, c.Image, state, pinStatus, c.Message})
		} else {
			t.AppendRow(table.Row{c.ID, c.Name, c.Type, c.Image, state, c.Message})
		}
	}

	t.Render()
	p.Println()
}

// MCPServers prints the rolled-up MCP-server status table. The AUTOSCALE column
// is shown only when at least one row has autoscale configured, so static-only
// stacks see an unchanged table.
func (p *Printer) MCPServers(rows []MCPServerRollup) {
	if len(rows) == 0 {
		return
	}

	p.Section("MCP SERVERS")

	t := table.NewWriter()
	t.SetOutputMirror(p.out)
	t.SetStyle(p.tableStyle())

	hasAutoscale := false
	for _, r := range rows {
		if r.Autoscale != "" {
			hasAutoscale = true
			break
		}
	}

	if hasAutoscale {
		t.AppendHeader(table.Row{"Name", "Type", "Replicas", "Autoscale", "State"})
	} else {
		t.AppendHeader(table.Row{"Name", "Type", "Replicas", "State"})
	}
	for _, r := range rows {
		state := r.State
		if p.isTTY {
			state = colorReplicaState(r.State)
		}
		if hasAutoscale {
			t.AppendRow(table.Row{r.Name, r.Type, r.Replicas, r.Autoscale, state})
		} else {
			t.AppendRow(table.Row{r.Name, r.Type, r.Replicas, state})
		}
	}
	t.Render()
	p.Println()
}

// Replicas prints the per-replica detail table used by `gridctl status --replicas`.
// The AUTOSCALE column only appears when at least one row populates it.
func (p *Printer) Replicas(rows []ReplicaDetail) {
	if len(rows) == 0 {
		return
	}

	p.Section("REPLICAS")

	t := table.NewWriter()
	t.SetOutputMirror(p.out)
	t.SetStyle(p.tableStyle())

	hasAutoscale := false
	for _, r := range rows {
		if r.Autoscale != "" {
			hasAutoscale = true
			break
		}
	}

	if hasAutoscale {
		t.AppendHeader(table.Row{"Server", "Replica", "PID/Container", "State", "Uptime", "In-Flight", "Autoscale"})
	} else {
		t.AppendHeader(table.Row{"Server", "Replica", "PID/Container", "State", "Uptime", "In-Flight"})
	}
	for _, r := range rows {
		state := r.State
		if p.isTTY {
			state = colorReplicaState(r.State)
		}
		if hasAutoscale {
			t.AppendRow(table.Row{r.Server, fmt.Sprintf("%d", r.Replica), r.Handle, state, r.Uptime, r.InFlight, r.Autoscale})
		} else {
			t.AppendRow(table.Row{r.Server, fmt.Sprintf("%d", r.Replica), r.Handle, state, r.Uptime, r.InFlight})
		}
	}
	t.Render()
	p.Println()
}

// colorReplicaState colours rollup/expanded replica states. "degraded" starts
// with the token "degraded" so the prefix match catches the full annotation
// ("degraded (replica-1 restarting, next in 4s)").
func colorReplicaState(state string) string {
	switch {
	case state == "healthy":
		return lipgloss.NewStyle().Foreground(ColorGreen).Render(state)
	case state == "restarting", state == "unhealthy":
		return lipgloss.NewStyle().Foreground(ColorRed).Render(state)
	case len(state) >= 8 && state[:8] == "degraded":
		return lipgloss.NewStyle().Foreground(ColorAmber).Render(state)
	default:
		return lipgloss.NewStyle().Foreground(ColorGray).Render(state)
	}
}

// colorPinStatus applies color to a pin status label for TTY output.
func colorPinStatus(status string) string {
	switch status {
	case "✓ pinned":
		return lipgloss.NewStyle().Foreground(ColorGreen).Render(status)
	case "⚠ drift":
		return lipgloss.NewStyle().Foreground(ColorRed).Render(status)
	case "~ approved":
		return lipgloss.NewStyle().Foreground(ColorAmber).Render(status)
	case "— unpinned":
		return lipgloss.NewStyle().Foreground(ColorMuted).Render(status)
	default:
		return lipgloss.NewStyle().Foreground(ColorGray).Render(status)
	}
}

// tableStyle returns the standard amber-themed table style.
func (p *Printer) tableStyle() table.Style {
	style := table.StyleRounded
	if p.isTTY {
		style.Color.Header = text.Colors{text.FgHiYellow, text.Bold}
		style.Color.Border = text.Colors{text.FgHiBlack}
	}
	style.Options.SeparateRows = false
	return style
}

// Section prints a section header.
func (p *Printer) Section(title string) {
	if p.isTTY {
		style := lipgloss.NewStyle().Foreground(ColorAmber).Bold(true)
		p.Println(style.Render(title))
	} else {
		p.Println(title)
	}
}
