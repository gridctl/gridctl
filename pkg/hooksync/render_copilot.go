package hooksync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// copilotHookFileVersion is the version field Copilot CLI requires in a
// hook file.
const copilotHookFileVersion = 1

type copilotFile struct {
	Version int          `json:"version"`
	Hooks   copilotHooks `json:"hooks"`
}

// copilotHooks fixes the event key order: PreToolUse, then PostToolUse.
// The PascalCase keys select Copilot's Claude-shaped snake_case payload
// and Claude matcher semantics.
type copilotHooks struct {
	PreToolUse  []copilotHandler `json:"PreToolUse,omitempty"`
	PostToolUse []copilotHandler `json:"PostToolUse,omitempty"`
}

// copilotHandler is one exec-form command handler. exec with args runs
// the script directly, without a shell.
type copilotHandler struct {
	Type       string   `json:"type"`
	Exec       string   `json:"exec"`
	Args       []string `json:"args"`
	Matcher    *string  `json:"matcher,omitempty"`
	TimeoutSec *int64   `json:"timeoutSec,omitempty"`
}

// renderCopilot renders the Copilot CLI hook file for one hook. The
// matcher passes through as written, because Copilot applies Claude
// matcher semantics to PascalCase events itself; only PreToolUse and
// PostToolUse groups are expressible. The output is a pure function of the
// definition, the name, and the script directory.
func renderCopilot(spec *hookSpec, name, scriptDir string) (rendered, error) {
	out := rendered{client: "copilot"}
	var file copilotFile
	file.Version = copilotHookFileVersion
	for _, g := range spec.Groups {
		var list *[]copilotHandler
		switch g.Event {
		case "PreToolUse":
			list = &file.Hooks.PreToolUse
		case "PostToolUse":
			list = &file.Hooks.PostToolUse
		default:
			out.Unexpressible = append(out.Unexpressible, fmt.Sprintf("event %q has no copilot mapping", g.Event))
			out.DroppedGroups = appendUnique(out.DroppedGroups, g.Event)
			continue
		}
		var matcher *string
		if g.HasMatcher && g.Matcher != nil {
			m, ok := g.Matcher.(string)
			if !ok {
				pattern := fmt.Sprint(g.Matcher)
				out.Unexpressible = append(out.Unexpressible, fmt.Sprintf("matcher %q is not a string", pattern))
				out.DroppedGroups = appendUnique(out.DroppedGroups, fmt.Sprintf("matcher %q", pattern))
				continue
			}
			matcher = &m
		}
		for _, h := range g.Handlers {
			*list = append(*list, copilotHandler{
				Type:       "command",
				Exec:       h.scriptPath(scriptDir),
				Args:       h.args(),
				Matcher:    matcher,
				TimeoutSec: copilotTimeoutSec(h),
			})
			if cond, ok := h.Raw["if"].(string); ok {
				out.DroppedIf = appendUnique(out.DroppedIf, cond)
			}
			for _, key := range h.cosmeticKeys() {
				out.DroppedKeys = appendUnique(out.DroppedKeys, key)
			}
		}
	}
	sortStrings(out.DroppedKeys)
	if len(file.Hooks.PreToolUse) == 0 && len(file.Hooks.PostToolUse) == 0 {
		if len(out.Unexpressible) == 0 {
			out.Unexpressible = []string{"hook has no matcher groups"}
		}
		return out, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(file); err != nil {
		return rendered{}, fmt.Errorf("encoding copilot hook file for %s: %w", name, err)
	}
	out.Bytes = buf.Bytes()
	return out, nil
}

// copilotTimeoutSec converts the handler's timeout to whole seconds,
// rounding up so a fractional deadline is never shortened. It is nil when
// the definition set no timeout, and Copilot applies its own default.
func copilotTimeoutSec(h specHandler) *int64 {
	sec, ok := h.timeoutSeconds()
	if !ok {
		return nil
	}
	whole := math.Ceil(sec)
	if whole > math.MaxInt32 {
		whole = math.MaxInt32
	}
	v := int64(whole)
	return &v
}
