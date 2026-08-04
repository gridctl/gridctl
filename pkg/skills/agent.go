package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gridctl/gridctl/pkg/project"
)

// AgentDefinition is a parsed Claude Code subagent definition (AGENT.md /
// agents/<name>.md). Name and Description are the only typed fields; every
// other frontmatter key (tools, model, hooks, mcpServers, permissionMode,
// vendor extensions) rides in Extra as raw YAML nodes in document order.
//
// AgentDefinition deliberately does not reuse AgentSkill/ParseSkillMD:
// that parser types skill-specific keys and would silently strand tools
// and model in an untyped map, and its render path normalizes frontmatter.
// Agents are stored and projected verbatim (identity render), so Raw is
// the source of truth; the parsed form exists for validation, scanning,
// and listing.
type AgentDefinition struct {
	Name        string
	Description string
	// Extra holds every frontmatter key other than name and description,
	// in document order, as raw YAML nodes.
	Extra []AgentExtraField
	// Body is the markdown after the frontmatter block.
	Body string
	// Raw is the file exactly as read; imports write it verbatim.
	Raw []byte
}

// AgentExtraField is one passthrough frontmatter key.
type AgentExtraField struct {
	Key   string
	Value *yaml.Node
}

// ExtraByKey returns the raw frontmatter node for one passthrough key.
// Renderers use it for key-level access without scanning Extra at every
// call site.
func (d *AgentDefinition) ExtraByKey(key string) (*yaml.Node, bool) {
	for _, f := range d.Extra {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

// agentNamePattern matches valid agent names: lowercase letters, digits,
// and hyphens. Colons are excluded (Claude Code v2.1.218+ refuses agent
// names containing ":").
var agentNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidateAgentName validates an agent name: non-empty, lowercase
// letters, digits, and hyphens only.
func ValidateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name is required")
	}
	if strings.Contains(name, ":") {
		return fmt.Errorf("agent name %q must not contain colons (Claude Code refuses them)", name)
	}
	if !agentNamePattern.MatchString(name) {
		return fmt.Errorf("agent name %q must be lowercase letters, digits, and hyphens (matching %s)", name, agentNamePattern.String())
	}
	return nil
}

// ParseAgentMD parses an agent definition file. Frontmatter with a
// non-empty description is required: a markdown file without it (a
// README dropped into an agents/ directory, say) is not an agent
// definition. The frontmatter mapping is walked by hand so unknown keys
// are preserved in order rather than silently dropped, and a duplicate
// key cannot discard the valid ones.
func ParseAgentMD(data []byte) (*AgentDefinition, error) {
	content := strings.ReplaceAll(string(data), "\r\n", "\n")

	frontmatter, body, ok := splitAgentFrontmatter(content)
	if !ok {
		return nil, fmt.Errorf("missing frontmatter (agent definitions require a name and description)")
	}

	def := &AgentDefinition{Body: body, Raw: data}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(frontmatter), &root); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}
	mapping := yamlDocumentMapping(&root)
	if mapping == nil {
		return nil, fmt.Errorf("frontmatter is not a mapping")
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode, valNode := mapping.Content[i], mapping.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		var err error
		switch keyNode.Value {
		case "name":
			err = valNode.Decode(&def.Name)
		case "description":
			err = valNode.Decode(&def.Description)
		default:
			def.Extra = append(def.Extra, AgentExtraField{Key: keyNode.Value, Value: valNode})
		}
		if err != nil {
			return nil, fmt.Errorf("parsing frontmatter key %q: %w", keyNode.Value, err)
		}
	}

	if strings.TrimSpace(def.Description) == "" {
		return nil, fmt.Errorf("frontmatter has no description (agent definitions require one)")
	}
	return def, nil
}

// splitAgentFrontmatter splits content into the YAML between the first
// two --- delimiter lines and the body after the closing delimiter. The
// scan mirrors ParseSkillMD's, but absence of frontmatter is a failure
// here rather than a whole-file body.
func splitAgentFrontmatter(content string) (frontmatter, body string, ok bool) {
	trimmed := strings.TrimLeft(content, " \t")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", false
	}
	lines := strings.SplitAfter(content, "\n")
	openIdx, closeIdx := -1, -1
	for i, line := range lines {
		if strings.TrimSpace(strings.TrimRight(line, "\n")) == "---" {
			if openIdx == -1 {
				openIdx = i
			} else {
				closeIdx = i
				break
			}
		}
	}
	if closeIdx == -1 {
		return "", "", false
	}
	var fm, b strings.Builder
	for i := openIdx + 1; i < closeIdx; i++ {
		fm.WriteString(lines[i])
	}
	for i := closeIdx + 1; i < len(lines); i++ {
		b.WriteString(lines[i])
	}
	return fm.String(), strings.TrimPrefix(b.String(), "\n"), true
}

// yamlDocumentMapping unwraps a parsed document to its top-level mapping
// node, or nil when the document is empty or not a mapping.
func yamlDocumentMapping(root *yaml.Node) *yaml.Node {
	n := root
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	return n
}

// frontmatterScanText renders the frontmatter values of a definition for the
// security scan: scalar values verbatim, everything else through the
// node's re-encoded form. The scan runs over prose an agent will act on;
// frontmatter carries hooks and command strings, so it is scanned too.
func (d *AgentDefinition) frontmatterScanText() string {
	var b strings.Builder
	for _, f := range d.Extra {
		var v any
		if err := f.Value.Decode(&v); err != nil {
			continue
		}
		fmt.Fprintf(&b, "%s: %v\n", f.Key, v)
	}
	return b.String()
}

// ScanAgent checks an agent definition for dangerous patterns in its
// body and frontmatter values. Like skill bodies (and unlike supporting
// files), any finding blocks: an agent definition is instructions the
// client executes with tool access.
func ScanAgent(def *AgentDefinition) *ScanResult {
	result := &ScanResult{SkillName: def.Name, Safe: true}
	if def.Body != "" {
		scanText("body", def.Body, result)
	}
	if fm := def.frontmatterScanText(); fm != "" {
		scanText("frontmatter", fm, result)
	}
	result.Safe = len(result.Findings) == 0
	return result
}

// ScanFragment checks a context rule fragment for dangerous patterns.
// Rule fragments are instructions clients inject into every session, so
// they get the same blocking scan agents do: the whole file (frontmatter
// and body) is scanned, and any finding blocks without --trust.
func ScanFragment(name string, content []byte) *ScanResult {
	result := &ScanResult{SkillName: name, Safe: true}
	if len(content) > 0 {
		scanText("fragment", string(content), result)
	}
	result.Safe = len(result.Findings) == 0
	return result
}

// AgentsRoot returns the canonical agent store directory under a
// registry directory.
func AgentsRoot(registryDir string) string {
	return filepath.Join(registryDir, "agents")
}

// AgentDir returns the canonical directory for one imported agent; the
// definition itself lives at AgentDir(...)/AGENT.md.
func AgentDir(registryDir, name string) string {
	return filepath.Join(AgentsRoot(registryDir), name)
}

// InstalledAgent is one agent present in the canonical store.
type InstalledAgent struct {
	Name       string
	Definition *AgentDefinition
	// Dir is the agent's canonical directory (holds AGENT.md and its
	// .origin.json sidecar).
	Dir string
}

// ListAgents returns the agents in the canonical store, sorted by name.
// A missing store directory is the normal no-agents state. Entries whose
// AGENT.md is missing or unparseable are skipped: the store lists what
// it can serve.
func ListAgents(registryDir string) ([]InstalledAgent, error) {
	root := AgentsRoot(registryDir)
	dirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading agent store: %w", err)
	}
	var agents []InstalledAgent
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		dir := filepath.Join(root, d.Name())
		data, rerr := os.ReadFile(filepath.Join(dir, "AGENT.md")) // #nosec G304 -- fixed name inside the managed store
		if rerr != nil {
			continue
		}
		def, perr := ParseAgentMD(data)
		if perr != nil {
			continue
		}
		agents = append(agents, InstalledAgent{Name: d.Name(), Definition: def, Dir: dir})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents, nil
}

// GetAgent returns one agent from the canonical store.
func GetAgent(registryDir, name string) (*InstalledAgent, error) {
	if err := ValidateAgentName(name); err != nil {
		return nil, err
	}
	dir := AgentDir(registryDir, name)
	data, err := os.ReadFile(filepath.Join(dir, "AGENT.md")) // #nosec G304 -- name validated above, fixed file name
	if err != nil {
		return nil, fmt.Errorf("agent %q not found: %w", name, err)
	}
	def, err := ParseAgentMD(data)
	if err != nil {
		return nil, fmt.Errorf("agent %q: %w", name, err)
	}
	return &InstalledAgent{Name: name, Definition: def, Dir: dir}, nil
}

// SaveAgent validates raw as an agent definition named name and writes
// it into the canonical store byte-for-byte. No normalization happens
// here on purpose: identity projections copy the canonical bytes
// verbatim, so any rewrite (key reordering, trailing-newline fixes)
// would surface as drift on every synced client after an edit. Returns
// the parsed definition so callers can inspect what was written.
func SaveAgent(registryDir, name string, raw []byte) (*AgentDefinition, error) {
	if err := ValidateAgentName(name); err != nil {
		return nil, err
	}
	def, err := ParseAgentMD(raw)
	if err != nil {
		return nil, err
	}
	if def.Name != "" && def.Name != name {
		return nil, fmt.Errorf("frontmatter names the agent %q, not %q; renames are not supported", def.Name, name)
	}
	dir := AgentDir(registryDir, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating agent directory: %w", err)
	}
	if err := project.AtomicWriteFile(filepath.Join(dir, "AGENT.md"), raw); err != nil {
		return nil, fmt.Errorf("writing AGENT.md: %w", err)
	}
	return def, nil
}

// DeleteAgent removes one agent from the canonical store.
func DeleteAgent(registryDir, name string) error {
	if err := ValidateAgentName(name); err != nil {
		return err
	}
	dir := AgentDir(registryDir, name)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("agent %q not found", name)
		}
		return err
	}
	return os.RemoveAll(dir)
}
