package controller

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestApplyCallGates_PolicyBeforeRateLimits(t *testing.T) {
	gw := mcp.NewGateway()
	stack := &config.Stack{
		Policy: &config.PolicyConfig{Rules: []config.PolicyRule{{Match: "alpha__*", Action: "block"}}},
		Limits: &config.LimitsConfig{RateLimits: []config.RateLimit{{Server: "alpha", CallsPerMinute: 10}}},
	}
	limitsPol, toolPol := ApplyCallGates(gw, stack, slog.New(slog.DiscardHandler), nil)
	if toolPol == nil || limitsPol == nil {
		t.Fatal("both policies must compile")
	}
	// The installer is the only production SetCallGates caller. A later
	// limits-only rebuild must keep the policy gate in front.
	limitsPol, toolPol = ApplyCallGates(gw, stack, slog.New(slog.DiscardHandler), limitsPol)
	if toolPol == nil || limitsPol == nil {
		t.Fatal("rebuild dropped a policy")
	}
}

func TestLintPolicyRules_SkipsHiddenAndWarnsOnUnknown(t *testing.T) {
	c := &lintProbeClient{}
	c.SetTools([]mcp.Tool{{Name: "list_paths"}, {Name: "read_secret"}})
	c.SetToolWhitelist([]string{"list_paths"})
	c.SetInitialized(mcp.ServerInfo{Name: "alpha"})
	r := mcp.NewRouter()
	r.AddClient(c)
	r.RefreshTools()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	lintPolicyRules(&config.Stack{Policy: &config.PolicyConfig{
		Rules: []config.PolicyRule{
			{Match: "alpha__read_secret", Action: "block"},
			{Match: "alpha__*", Action: "block"},
			{Match: "alpha__missing", Action: "block"},
		},
	}}, r, logger)
	text := logs.String()
	if strings.Contains(text, "alpha__read_secret") {
		t.Fatalf("hidden tool warned: %s", text)
	}
	if strings.Contains(text, "alpha__*") {
		t.Fatalf("glob warned: %s", text)
	}
	if !strings.Contains(text, "policy rule matches no known tool") || !strings.Contains(text, "alpha__missing") {
		t.Fatalf("missing literal did not warn: %s", text)
	}
}

// lintProbeClient embeds ClientBase so AllTools reports the pre-whitelist set.
type lintProbeClient struct {
	mcp.ClientBase
}

func (c *lintProbeClient) Name() string                       { return "alpha" }
func (c *lintProbeClient) Initialize(context.Context) error   { return nil }
func (c *lintProbeClient) RefreshTools(context.Context) error { return nil }
func (c *lintProbeClient) CallTool(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
	return &mcp.ToolCallResult{}, nil
}
