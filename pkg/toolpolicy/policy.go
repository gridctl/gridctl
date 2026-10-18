// Package toolpolicy enforces the stack.yaml `policy:` block: ordered
// per-tool call rules, an optional destructive-hint default, and a
// permissive or blocking fallback. It implements the gateway's CallGate
// seam. Evaluation is in-memory and does no I/O.
package toolpolicy

import (
	"context"
	"fmt"
	"log/slog"
	"path"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
)

const (
	actionAllow           = "allow"
	actionBlock           = "block"
	actionRequireApproval = "require_approval"

	sourceRule             = "rule"
	sourceBlockDestructive = "block_destructive"
	sourceDefault          = "default"
)

// compiledRule is one match compiled at policy build. An unparseable
// pattern never matches: validation rejects it, and a stray one must not
// fail open.
type compiledRule struct {
	index   int
	pattern string
	action  string
	ok      bool
}

// Policy is the compiled, enforcement-ready form of a config.PolicyConfig.
// A nil *Policy means the key was omitted; every method is nil-safe and
// permissive. A non-nil policy, including an empty block, installs a gate.
type Policy struct {
	defaultBlock     bool
	blockDestructive bool
	authoredDefault  string
	rules            []compiledRule
}

// Verdict is one tool's policy decision. Action is the authored action
// (require_approval is not rewritten to block). Source is rule,
// block_destructive, or default. RuleIndex is set only for a rule match.
type Verdict struct {
	Action    string
	Source    string
	RuleIndex int
	Rule      string
	Matched   bool
}

// NewPolicy compiles the policy block. A nil config returns a nil policy
// (no block, byte-identical legacy dispatch). Any non-nil config, including
// an empty block, returns a non-nil policy.
func NewPolicy(cfg *config.PolicyConfig, logger *slog.Logger) *Policy {
	if cfg == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	p := &Policy{
		defaultBlock:     cfg.Default == actionBlock,
		blockDestructive: cfg.BlockDestructive,
		authoredDefault:  cfg.Default,
	}
	if p.authoredDefault == "" {
		p.authoredDefault = actionAllow
	}
	for i, rule := range cfg.Rules {
		cr := compiledRule{index: i, pattern: rule.Match, action: rule.Action, ok: true}
		if _, err := path.Match(rule.Match, ""); err != nil {
			cr.ok = false
			logger.Warn("policy: unparseable match never matches", "rule", i+1, "match", rule.Match)
		}
		p.rules = append(p.rules, cr)
	}
	return p
}

// Gates returns the policy's pre-call gate. A nil policy returns nil. A
// non-nil policy always returns one gate, including an empty block.
func (p *Policy) Gates() []mcp.CallGate {
	if p == nil {
		return nil
	}
	return []mcp.CallGate{&policyGate{p}}
}

// Evaluate applies last-match-wins rules, then block_destructive, then
// default. A nil policy allows. A tool with no annotations is unaffected
// by block_destructive. ann is the downstream server's own hints, not a
// group override.
func (p *Policy) Evaluate(prefixedName string, ann *mcp.ToolAnnotations) Verdict {
	if p == nil {
		return Verdict{Action: actionAllow, Source: sourceDefault}
	}
	var matched *compiledRule
	for i := range p.rules {
		rule := &p.rules[i]
		if !rule.ok || !rule.matches(prefixedName) {
			continue
		}
		matched = rule
	}
	if matched != nil {
		return Verdict{
			Action:    matched.action,
			Source:    sourceRule,
			RuleIndex: matched.index,
			Rule:      matched.pattern,
			Matched:   true,
		}
	}
	if p.blockDestructive && destructive(ann) {
		return Verdict{Action: actionBlock, Source: sourceBlockDestructive}
	}
	action := actionAllow
	if p.defaultBlock {
		action = actionBlock
	}
	return Verdict{Action: action, Source: sourceDefault}
}

func (r *compiledRule) matches(name string) bool {
	ok, err := path.Match(r.pattern, name)
	return err == nil && ok
}

func destructive(ann *mcp.ToolAnnotations) bool {
	return ann != nil && ann.DestructiveHint != nil && *ann.DestructiveHint
}

// policyGate implements mcp.CallGate. Name is "policy" so outcome.gate
// and the denial log identify this block, not the rate-limit gate.
type policyGate struct{ p *Policy }

func (g *policyGate) Name() string { return "policy" }

func (g *policyGate) CheckToolCall(_ context.Context, call mcp.GateCall) mcp.GateDecision {
	verdict := g.p.Evaluate(call.PrefixedTool, call.Annotations)
	if !denies(verdict.Action) {
		return mcp.GateAllow()
	}
	return mcp.GateDenyWithReason(denialMessage(verdict), runs.ReasonPolicyBlocked, ruleField(verdict))
}

// denies reports whether an authored action blocks the call.
// require_approval is enforced as block. An unknown action fails closed.
func denies(action string) bool {
	return action != actionAllow
}

func denialMessage(v Verdict) string {
	switch v.Source {
	case sourceRule:
		if v.Action == actionRequireApproval {
			return fmt.Sprintf("Error: tool blocked by policy (rule %d requires approval, which this gateway does not yet support); do not retry", v.RuleIndex+1)
		}
		return fmt.Sprintf("Error: tool blocked by policy (rule %d: %s); do not retry", v.RuleIndex+1, v.Rule)
	case sourceBlockDestructive:
		return "Error: tool blocked by policy (block_destructive: destructiveHint is true); do not retry"
	default:
		return "Error: tool blocked by policy (default: block); do not retry"
	}
}

func ruleField(v Verdict) string {
	switch v.Source {
	case sourceRule:
		return fmt.Sprintf("%d: %s", v.RuleIndex+1, v.Rule)
	case sourceBlockDestructive:
		return sourceBlockDestructive
	default:
		return "default: block"
	}
}
