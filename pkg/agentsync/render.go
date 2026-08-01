package agentsync

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gridctl/gridctl/pkg/skills"
)

// This file holds the per-client renderers. Contract (shared with the
// projection plan's render pipeline): pure functions, deterministic
// output (frontmatter is emitted in a fixed hand-ordered key sequence,
// never by ranging a map), and explicit loss — every canonical
// frontmatter key the dialect cannot express lands in Rendered.Dropped.
//
// The `model` key is deliberately dropped on every rendered target:
// model vocabularies are client-specific (Claude aliases like "sonnet"
// mean nothing to OpenCode's provider/model IDs or Gemini's model
// names), and passing one through would break the agent harder than
// omitting it. The agent body is always carried verbatim.

// renderOpenCode emits the OpenCode agent dialect
// (~/.config/opencode/agents/<name>.md). OpenCode has no name key (the
// filename is the identity) and gates tools through a permission block
// whose semantics do not map from Claude's tools list; rather than
// invent a partial mapping that could over-grant, tools are dropped and
// OpenCode's defaults apply.
func renderOpenCode(def *skills.AgentDefinition) (Rendered, error) {
	var b strings.Builder
	b.WriteString("---\n")
	writeScalarField(&b, "description", def.Description)
	writeScalarField(&b, "mode", "subagent")
	b.WriteString("---\n\n")
	b.WriteString(def.Body)

	return Rendered{
		Bytes:   []byte(b.String()),
		Dropped: droppedKeys(def, nil), // name is structural (filename); every Extra key drops
	}, nil
}

// renderCopilot emits the Copilot custom-agent dialect
// (~/.copilot/agents/<name>.agent.md). Claude's comma-separated tools
// string becomes Copilot's YAML array.
func renderCopilot(def *skills.AgentDefinition) (Rendered, error) {
	return renderNameDescriptionTools(def)
}

// renderGemini emits the Gemini CLI subagent dialect
// (~/.gemini/agents/<name>.md), which is near-isomorphic to Claude's:
// name, description, and a tools array carry over.
func renderGemini(def *skills.AgentDefinition) (Rendered, error) {
	return renderNameDescriptionTools(def)
}

// renderNameDescriptionTools is the shared shape for dialects carrying
// name, description, and a tools array.
func renderNameDescriptionTools(def *skills.AgentDefinition) (Rendered, error) {
	var b strings.Builder
	b.WriteString("---\n")
	writeScalarField(&b, "name", def.Name)
	writeScalarField(&b, "description", def.Description)

	consumed := map[string]bool{}
	if tools, ok := claudeToolsList(def); ok {
		writeListField(&b, "tools", tools)
		consumed["tools"] = true
	}
	b.WriteString("---\n\n")
	b.WriteString(def.Body)

	return Rendered{Bytes: []byte(b.String()), Dropped: droppedKeys(def, consumed)}, nil
}

// claudeToolsList reads the canonical tools key: Claude Code writes a
// comma-separated string ("Read, Bash"); a YAML sequence is accepted
// too. Absent, empty, or unparseable values report not-ok (and the key
// then surfaces in Dropped rather than being half-converted).
func claudeToolsList(def *skills.AgentDefinition) ([]string, bool) {
	node, ok := def.ExtraByKey("tools")
	if !ok {
		return nil, false
	}
	switch node.Kind {
	case yaml.ScalarNode:
		var raw string
		if err := node.Decode(&raw); err != nil {
			return nil, false
		}
		var tools []string
		for _, part := range strings.Split(raw, ",") {
			if p := strings.TrimSpace(part); p != "" {
				tools = append(tools, p)
			}
		}
		return tools, len(tools) > 0
	case yaml.SequenceNode:
		var tools []string
		if err := node.Decode(&tools); err != nil {
			return nil, false
		}
		return tools, len(tools) > 0
	default:
		return nil, false
	}
}

// droppedKeys lists the canonical Extra keys the render did not carry,
// sorted for deterministic detail strings.
func droppedKeys(def *skills.AgentDefinition, consumed map[string]bool) []string {
	var dropped []string
	for _, f := range def.Extra {
		if !consumed[f.Key] {
			dropped = append(dropped, f.Key)
		}
	}
	sort.Strings(dropped)
	return dropped
}

// lossyDetail renders a Dropped list as the human detail line appended
// to sync results and status rows. Empty when nothing dropped.
func lossyDetail(target string, dropped []string) string {
	if len(dropped) == 0 {
		return ""
	}
	return fmt.Sprintf("lossy %s render: dropped frontmatter keys %s", target, strings.Join(dropped, ", "))
}

// writeScalarField emits one "key: value" line with YAML-safe scalar
// encoding (quoting and escaping via the yaml encoder, deterministic
// for scalars).
func writeScalarField(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(yamlScalar(value))
	b.WriteString("\n")
}

// writeListField emits one "key: [a, b]" flow-style line. Flow context
// has stricter quoting rules than block context (commas, brackets, and
// newlines change meaning), so items that are not plain-safe are
// JSON-quoted, which is always valid YAML flow.
func writeListField(b *strings.Builder, key string, values []string) {
	b.WriteString(key)
	b.WriteString(": [")
	for i, v := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		if plainFlowScalar.MatchString(v) {
			b.WriteString(v)
		} else {
			fmt.Fprintf(b, "%q", v)
		}
	}
	b.WriteString("]\n")
}

// plainFlowScalar matches values safe to emit unquoted inside a YAML
// flow sequence.
var plainFlowScalar = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// yamlScalar encodes one scalar value in YAML flow form, trimming the
// encoder's trailing newline. Plain strings stay unquoted; anything
// needing quotes or escapes gets them exactly as the yaml encoder
// decides, which is deterministic for a given input.
func yamlScalar(value string) string {
	out, err := yaml.Marshal(value)
	if err != nil {
		// A plain string cannot fail to marshal; quote defensively.
		return fmt.Sprintf("%q", value)
	}
	return strings.TrimRight(string(out), "\n")
}
