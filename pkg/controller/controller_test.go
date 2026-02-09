package controller

import (
	"io/fs"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestNew(t *testing.T) {
	cfg := Config{
		StackPath: "/path/to/stack.yaml",
		Port:      8180,
		BasePort:  9000,
	}
	ctrl := New(cfg)
	if ctrl == nil {
		t.Fatal("New returned nil")
	}
}

func TestStackController_SetVersion(t *testing.T) {
	ctrl := New(Config{})
	ctrl.SetVersion("v1.0.0")
	if ctrl.version != "v1.0.0" {
		t.Errorf("expected version 'v1.0.0', got '%s'", ctrl.version)
	}
}

func TestStackController_SetWebFS(t *testing.T) {
	ctrl := New(Config{})
	ctrl.SetWebFS(func() (fs.FS, error) {
		return nil, nil
	})
	if ctrl.webFS == nil {
		t.Error("expected webFS to be set")
	}
}

func TestBuildWorkloadSummaries_Empty(t *testing.T) {
	stack := &config.Stack{}
	result := &runtime.UpResult{}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries, got %d", len(summaries))
	}
}

func TestBuildWorkloadSummaries_MCPServers(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{
			{Name: "http-server", Transport: "http"},
			{Name: "stdio-server", Transport: "stdio"},
			{Name: "ext-server", URL: "https://example.com"},
			{Name: "local-server", Command: []string{"./server"}},
			{Name: "ssh-server", Command: []string{"/opt/server"}, SSH: &config.SSHConfig{Host: "10.0.0.1", User: "user"}},
			{Name: "api-server", OpenAPI: &config.OpenAPIConfig{Spec: "/spec.json"}},
		},
	}
	result := &runtime.UpResult{
		MCPServers: []runtime.MCPServerResult{
			{Name: "http-server"},
			{Name: "stdio-server"},
			{Name: "ext-server"},
			{Name: "local-server"},
			{Name: "ssh-server"},
			{Name: "api-server"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)

	if len(summaries) != 6 {
		t.Fatalf("expected 6 summaries, got %d", len(summaries))
	}

	expectedTransports := map[string]string{
		"http-server":  "http",
		"stdio-server": "stdio",
		"ext-server":   "external",
		"local-server": "local",
		"ssh-server":   "ssh",
		"api-server":   "openapi",
	}

	for _, s := range summaries {
		if s.Type != "mcp-server" {
			t.Errorf("expected type 'mcp-server', got '%s' for %s", s.Type, s.Name)
		}
		if s.State != "running" {
			t.Errorf("expected state 'running', got '%s' for %s", s.State, s.Name)
		}
		expected, ok := expectedTransports[s.Name]
		if !ok {
			t.Errorf("unexpected server: %s", s.Name)
			continue
		}
		if s.Transport != expected {
			t.Errorf("expected transport '%s' for %s, got '%s'", expected, s.Name, s.Transport)
		}
	}
}

func TestBuildWorkloadSummaries_DefaultTransport(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{
			{Name: "default-server", Transport: ""}, // Empty transport defaults to "http"
		},
	}
	result := &runtime.UpResult{
		MCPServers: []runtime.MCPServerResult{
			{Name: "default-server"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Transport != "http" {
		t.Errorf("expected transport 'http' for default, got '%s'", summaries[0].Transport)
	}
}

func TestBuildWorkloadSummaries_Agents(t *testing.T) {
	stack := &config.Stack{}
	result := &runtime.UpResult{
		Agents: []runtime.AgentResult{
			{Name: "agent-1"},
			{Name: "agent-2"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	for _, s := range summaries {
		if s.Type != "agent" {
			t.Errorf("expected type 'agent', got '%s'", s.Type)
		}
		if s.Transport != "container" {
			t.Errorf("expected transport 'container', got '%s'", s.Transport)
		}
	}
}

func TestBuildWorkloadSummaries_Resources(t *testing.T) {
	stack := &config.Stack{
		Resources: []config.Resource{
			{Name: "postgres"},
			{Name: "redis"},
		},
	}
	result := &runtime.UpResult{}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	for _, s := range summaries {
		if s.Type != "resource" {
			t.Errorf("expected type 'resource', got '%s'", s.Type)
		}
		if s.Transport != "container" {
			t.Errorf("expected transport 'container', got '%s'", s.Transport)
		}
	}
}

func TestBuildWorkloadSummaries_Mixed(t *testing.T) {
	stack := &config.Stack{
		MCPServers: []config.MCPServer{
			{Name: "server1", Transport: "http"},
		},
		Resources: []config.Resource{
			{Name: "db"},
		},
	}
	result := &runtime.UpResult{
		MCPServers: []runtime.MCPServerResult{
			{Name: "server1"},
		},
		Agents: []runtime.AgentResult{
			{Name: "agent1"},
		},
	}

	summaries := BuildWorkloadSummaries(stack, result)
	if len(summaries) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(summaries))
	}

	types := make(map[string]int)
	for _, s := range summaries {
		types[s.Type]++
	}
	if types["mcp-server"] != 1 {
		t.Errorf("expected 1 mcp-server, got %d", types["mcp-server"])
	}
	if types["agent"] != 1 {
		t.Errorf("expected 1 agent, got %d", types["agent"])
	}
	if types["resource"] != 1 {
		t.Errorf("expected 1 resource, got %d", types["resource"])
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{}
	if cfg.Port != 0 {
		t.Errorf("expected default port 0 (zero value), got %d", cfg.Port)
	}
	if cfg.Verbose {
		t.Error("expected Verbose to default to false")
	}
	if cfg.DaemonChild {
		t.Error("expected DaemonChild to default to false")
	}
}
