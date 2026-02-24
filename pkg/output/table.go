package output

import (
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

// ContainerSummary contains data for the container status table.
type ContainerSummary struct {
	ID      string
	Name    string
	Type    string // mcp-server, agent, resource
	Image   string
	State   string // running, exited, etc.
	Message string // status message
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

	t.AppendHeader(table.Row{"ID", "Name", "Type", "Image", "State", "Status"})

	for _, c := range containers {
		state := c.State
		if p.isTTY {
			state = colorState(c.State)
		}
		t.AppendRow(table.Row{c.ID, c.Name, c.Type, c.Image, state, c.Message})
	}

	t.Render()
	p.Println()
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
