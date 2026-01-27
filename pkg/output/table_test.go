package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrinter_Summary_Empty(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Summary(nil)

	if buf.Len() != 0 {
		t.Errorf("Summary(nil) should output nothing, got %q", buf.String())
	}
}

func TestPrinter_Summary_WithWorkloads(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	workloads := []WorkloadSummary{
		{Name: "server-a", Type: "mcp-server", Transport: "http", State: "running"},
		{Name: "agent-b", Type: "agent", Transport: "stdio", State: "pending"},
	}
	p.Summary(workloads)

	got := buf.String()
	// Check headers (go-pretty uppercases headers)
	if !strings.Contains(got, "NAME") {
		t.Error("Summary() should contain NAME header")
	}
	if !strings.Contains(got, "TYPE") {
		t.Error("Summary() should contain TYPE header")
	}
	if !strings.Contains(got, "TRANSPORT") {
		t.Error("Summary() should contain TRANSPORT header")
	}
	if !strings.Contains(got, "STATE") {
		t.Error("Summary() should contain STATE header")
	}
	// Check data
	if !strings.Contains(got, "server-a") {
		t.Error("Summary() should contain workload name")
	}
	if !strings.Contains(got, "mcp-server") {
		t.Error("Summary() should contain workload type")
	}
}

func TestPrinter_Gateways_Empty(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Gateways(nil)

	if buf.Len() != 0 {
		t.Errorf("Gateways(nil) should output nothing, got %q", buf.String())
	}
}

func TestPrinter_Gateways_WithData(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	gateways := []GatewaySummary{
		{Name: "my-topo", Port: 8180, PID: 12345, Status: "running", Started: "5 minutes ago"},
	}
	p.Gateways(gateways)

	got := buf.String()
	// Check section header
	if !strings.Contains(got, "GATEWAYS") {
		t.Error("Gateways() should contain section header")
	}
	// Check table headers (go-pretty uppercases headers)
	if !strings.Contains(got, "NAME") {
		t.Error("Gateways() should contain NAME header")
	}
	if !strings.Contains(got, "PORT") {
		t.Error("Gateways() should contain PORT header")
	}
	if !strings.Contains(got, "PID") {
		t.Error("Gateways() should contain PID header")
	}
	// Check data
	if !strings.Contains(got, "my-topo") {
		t.Error("Gateways() should contain gateway name")
	}
	if !strings.Contains(got, "8180") {
		t.Error("Gateways() should contain port")
	}
}

func TestPrinter_Containers_Empty(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	p.Containers(nil)

	if buf.Len() != 0 {
		t.Errorf("Containers(nil) should output nothing, got %q", buf.String())
	}
}

func TestPrinter_Containers_WithData(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)

	containers := []ContainerSummary{
		{ID: "abc123", Name: "mcp-server-1", Type: "mcp-server", Image: "my-image:latest", State: "running", Message: "Up 5 minutes"},
	}
	p.Containers(containers)

	got := buf.String()
	// Check section header
	if !strings.Contains(got, "CONTAINERS") {
		t.Error("Containers() should contain section header")
	}
	// Check table headers (go-pretty uppercases headers)
	if !strings.Contains(got, "ID") {
		t.Error("Containers() should contain ID header")
	}
	if !strings.Contains(got, "IMAGE") {
		t.Error("Containers() should contain IMAGE header")
	}
	// Check data
	if !strings.Contains(got, "abc123") {
		t.Error("Containers() should contain container ID")
	}
	if !strings.Contains(got, "mcp-server-1") {
		t.Error("Containers() should contain container name")
	}
}

func TestColorState(t *testing.T) {
	tests := []struct {
		state    string
		contains string // Non-TTY won't have colors, but function should not panic
	}{
		{"running", "running"},
		{"ready", "ready"},
		{"failed", "failed"},
		{"error", "error"},
		{"exited", "exited"},
		{"pending", "pending"},
		{"creating", "creating"},
		{"stopped", "stopped"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			result := colorState(tt.state)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("colorState(%q) = %q, should contain %q", tt.state, result, tt.contains)
			}
		})
	}
}
