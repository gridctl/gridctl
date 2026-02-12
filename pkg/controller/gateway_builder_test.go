package controller

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func TestGatewayBuilder_BuildLogging_Fresh(t *testing.T) {
	cfg := Config{Verbose: true}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	logBuffer, handler := builder.buildLogging(true)
	if logBuffer == nil {
		t.Fatal("expected logBuffer to be non-nil")
	}
	if handler == nil {
		t.Fatal("expected handler to be non-nil")
	}
}

func TestGatewayBuilder_BuildLogging_Existing(t *testing.T) {
	cfg := Config{}
	stack := &config.Stack{Name: "test"}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})

	existingBuffer := logging.NewLogBuffer(100)
	existingHandler := logging.NewRedactingHandler(logging.NewBufferHandler(existingBuffer, nil))
	builder.SetExistingLogInfra(existingBuffer, existingHandler)

	logBuffer, handler := builder.buildLogging(false)
	if logBuffer != existingBuffer {
		t.Error("expected existing buffer to be returned")
	}
	if handler != existingHandler {
		t.Error("expected existing handler to be returned")
	}
}

func TestGatewayBuilder_BuildA2AGateway_NoA2A(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name:    "test",
		Agents:  []config.Agent{{Name: "agent1"}},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	gw := builder.buildA2AGateway(handler)
	if gw != nil {
		t.Error("expected nil A2A gateway when no A2A agents")
	}
}

func TestGatewayBuilder_BuildA2AGateway_WithLocalA2A(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		Agents: []config.Agent{
			{
				Name: "agent1",
				A2A: &config.A2AConfig{
					Enabled: true,
					Skills:  []config.A2ASkill{{ID: "s1", Name: "Skill One"}},
				},
			},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	gw := builder.buildA2AGateway(handler)
	if gw == nil {
		t.Error("expected A2A gateway when agents have A2A config")
	}
}

func TestGatewayBuilder_BuildA2AGateway_WithExternalA2A(t *testing.T) {
	cfg := Config{Port: 8180}
	stack := &config.Stack{
		Name: "test",
		A2AAgents: []config.A2AAgent{
			{Name: "remote-agent", URL: "https://example.com/agent"},
		},
	}
	builder := NewGatewayBuilder(cfg, stack, "/path/stack.yaml", nil, &runtime.UpResult{})
	handler := logging.NewRedactingHandler(logging.NewBufferHandler(logging.NewLogBuffer(100), nil))

	gw := builder.buildA2AGateway(handler)
	if gw == nil {
		t.Error("expected A2A gateway when external A2A agents exist")
	}
}

func TestGatewayBuilder_SetVersion(t *testing.T) {
	builder := NewGatewayBuilder(Config{}, &config.Stack{}, "", nil, &runtime.UpResult{})
	builder.SetVersion("v0.1.0")
	if builder.version != "v0.1.0" {
		t.Errorf("expected version 'v0.1.0', got '%s'", builder.version)
	}
}

func TestNewDaemonManager(t *testing.T) {
	cfg := Config{Port: 8180, BasePort: 9000}
	dm := NewDaemonManager(cfg)
	if dm == nil {
		t.Fatal("expected non-nil DaemonManager")
	}
	if dm.config.Port != 8180 {
		t.Errorf("expected port 8180, got %d", dm.config.Port)
	}
}
