package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// OverrideFileName is the override discovered beside a stack file when
// neither --override nor --no-override is set.
const OverrideFileName = "stack.override.yaml"

// overridableServerFields is the allowlist for an override server entry.
// Decoding, docs, and tests share this table. headers is included because
// the base field exists; maps merge by key, matching env.
var overridableServerFields = []string{
	"enabled",
	"env",
	"headers",
	"output_format",
	"ping_timeout",
	"ready_timeout",
	"replicas",
	"restart",
	"tools",
}

// OverridableServerFields returns a copy of the allowlist, sorted.
func OverridableServerFields() []string {
	out := append([]string(nil), overridableServerFields...)
	sort.Strings(out)
	return out
}

// Override is the decoded override document. Only mcp-servers is allowed
// at the top level, and only the allowlisted server fields decode.
type Override struct {
	MCPServers map[string]ServerOverride `yaml:"mcp-servers"`
}

// ServerOverride is one server's overridable fields. Pointers and nil maps
// distinguish omission from an explicit value. Decoding uses KnownFields
// so any other key fails.
type ServerOverride struct {
	Enabled      *bool             `yaml:"enabled"`
	Env          map[string]string `yaml:"env"`
	Tools        []string          `yaml:"tools"`
	Replicas     *int              `yaml:"replicas"`
	Restart      *string           `yaml:"restart"`
	PingTimeout  *string           `yaml:"ping_timeout"`
	ReadyTimeout *string           `yaml:"ready_timeout"`
	OutputFormat *string           `yaml:"output_format"`
	Headers      map[string]string `yaml:"headers"`

	// fields is the set of keys present in the source mapping, in a stable
	// order. It is not decoded from YAML.
	fields []string
}

// OverrideSelection chooses which override file a parser merges.
type OverrideSelection struct {
	// Path is an explicit override file. Empty means discover
	// stack.override.yaml beside the stack file.
	Path string
	// Suppress ignores every override, including a discovered sibling.
	Suppress bool
	// Required makes a missing explicit Path an error. A discovered
	// sibling that is absent is never an error.
	Required bool
}

// DiscoverOverride resolves the override path for stackPath.
// A missing discovered file is not an error. An explicit path that does
// not exist is an error only when Required is set; otherwise found is false
// and the path is still returned so a watcher can arm on it.
func DiscoverOverride(stackPath string, sel OverrideSelection) (path string, found bool, err error) {
	if sel.Suppress {
		return "", false, nil
	}
	if sel.Path != "" {
		info, statErr := os.Stat(sel.Path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				if sel.Required {
					return "", false, fmt.Errorf("override: %s: file does not exist", sel.Path)
				}
				return sel.Path, false, nil
			}
			return "", false, fmt.Errorf("override: %s: %w", sel.Path, statErr)
		}
		if info.IsDir() {
			return "", false, fmt.Errorf("override: %s: is a directory", sel.Path)
		}
		return sel.Path, true, nil
	}
	candidate := filepath.Join(filepath.Dir(stackPath), OverrideFileName)
	info, statErr := os.Stat(candidate)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("override: %s: %w", candidate, statErr)
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("override: %s: is a directory", candidate)
	}
	return candidate, true, nil
}

var overrideUnknownField = regexp.MustCompile(`field ([^ ]+) not found in type`)

// DecodeOverride parses an override document. Unknown keys fail with a
// path that names the field. The function does not read the stack and
// does not expand variables.
func DecodeOverride(data []byte, path string) (*Override, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &Override{}, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("override: %s: %w", path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return &Override{}, nil
	}
	root := doc.Content[0]
	if root.Kind == yaml.ScalarNode && (root.Tag == "!!null" || root.Value == "" || root.Value == "null") {
		return &Override{}, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("override: %s: top-level value must be a mapping", path)
	}
	ov := &Override{MCPServers: map[string]ServerOverride{}}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if key != "mcp-servers" {
			return nil, fmt.Errorf("override: %s: top-level key %q is not allowed; only mcp-servers is", path, key)
		}
		servers := root.Content[i+1]
		if servers.Kind == yaml.ScalarNode && servers.Tag == "!!null" {
			continue
		}
		if servers.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("override: %s: mcp-servers must be a mapping of server names", path)
		}
		for j := 0; j+1 < len(servers.Content); j += 2 {
			name := servers.Content[j].Value
			if name == "" {
				return nil, fmt.Errorf("override: %s: mcp-servers entry name is empty", path)
			}
			decoded, err := decodeServerOverride(servers.Content[j+1], path, name)
			if err != nil {
				return nil, err
			}
			ov.MCPServers[name] = decoded
		}
	}
	return ov, nil
}

func decodeServerOverride(node *yaml.Node, path, name string) (ServerOverride, error) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return ServerOverride{}, nil
	}
	if node.Kind != yaml.MappingNode {
		return ServerOverride{}, fmt.Errorf("override: %s: mcp-servers.%s must be a mapping", path, name)
	}
	fields := make([]string, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		fields = append(fields, node.Content[i].Value)
	}
	raw, err := yaml.Marshal(node)
	if err != nil {
		return ServerOverride{}, fmt.Errorf("override: %s: mcp-servers.%s: %w", path, name, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var srv ServerOverride
	if err := dec.Decode(&srv); err != nil {
		if field := overrideFieldName(err); field != "" {
			return ServerOverride{}, fmt.Errorf("override: %s: mcp-servers.%s.%s is not overridable", path, name, field)
		}
		return ServerOverride{}, fmt.Errorf("override: %s: mcp-servers.%s: %w", path, name, err)
	}
	sort.Strings(fields)
	srv.fields = fields
	return srv, nil
}

func overrideFieldName(err error) string {
	if err == nil {
		return ""
	}
	if match := overrideUnknownField.FindStringSubmatch(err.Error()); match != nil {
		return match[1]
	}
	return ""
}

// ApplyOverride merges ov onto stack. Unknown server names are errors.
// The function does not expand variables or read stores. Scalars and lists
// replace; env and headers merge by key with the override winning.
func ApplyOverride(stack *Stack, ov *Override, path string) error {
	_, err := applyOverride(stack, ov, path, false)
	return err
}

// ApplyOverrideWarnUnknown merges ov and reports unknown server names as
// warnings instead of errors. ValidateStackFile uses this when the file
// has an extends chain that this parser does not resolve.
func ApplyOverrideWarnUnknown(stack *Stack, ov *Override, path string) ([]string, error) {
	return applyOverride(stack, ov, path, true)
}

func applyOverride(stack *Stack, ov *Override, path string, warnUnknown bool) ([]string, error) {
	if stack == nil {
		return nil, fmt.Errorf("override: %s: stack is nil", path)
	}
	if ov == nil || len(ov.MCPServers) == 0 {
		if path != "" {
			stack.OverridePath = path
		}
		return nil, nil
	}
	names := make([]string, 0, len(ov.MCPServers))
	for name := range ov.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	var warnings []string
	if stack.Overrides == nil {
		stack.Overrides = map[string][]string{}
	}
	for _, name := range names {
		idx := indexMCPServer(stack, name)
		if idx < 0 {
			msg := fmt.Sprintf("override: %s: mcp-servers.%s does not exist in the stack", path, name)
			if warnUnknown {
				warnings = append(warnings, msg)
				continue
			}
			return nil, fmt.Errorf("%s", msg)
		}
		fields := applyServerOverride(&stack.MCPServers[idx], ov.MCPServers[name])
		if len(fields) > 0 {
			stack.Overrides[name] = fields
		}
	}
	stack.OverridePath = path
	return warnings, nil
}

func indexMCPServer(stack *Stack, name string) int {
	if stack == nil {
		return -1
	}
	for i := range stack.MCPServers {
		if stack.MCPServers[i].Name == name {
			return i
		}
	}
	return -1
}

func applyServerOverride(dst *MCPServer, ov ServerOverride) []string {
	present := map[string]bool{}
	for _, field := range ov.fields {
		present[field] = true
	}
	var applied []string
	if present["enabled"] {
		dst.Enabled = ov.Enabled
		applied = append(applied, "enabled")
	}
	if present["env"] {
		if dst.Env == nil {
			dst.Env = map[string]string{}
		}
		if ov.Env == nil {
			dst.Env = map[string]string{}
		} else {
			for k, v := range ov.Env {
				dst.Env[k] = v
			}
		}
		applied = append(applied, "env")
	}
	if present["headers"] {
		if dst.Headers == nil {
			dst.Headers = map[string]string{}
		}
		if ov.Headers == nil {
			dst.Headers = map[string]string{}
		} else {
			for k, v := range ov.Headers {
				dst.Headers[k] = v
			}
		}
		applied = append(applied, "headers")
	}
	if present["tools"] {
		if ov.Tools == nil {
			dst.Tools = nil
		} else {
			dst.Tools = append([]string(nil), ov.Tools...)
		}
		applied = append(applied, "tools")
	}
	if present["replicas"] {
		if ov.Replicas == nil {
			dst.Replicas = 0
		} else {
			dst.Replicas = *ov.Replicas
		}
		applied = append(applied, "replicas")
	}
	if present["restart"] {
		if ov.Restart == nil {
			dst.Restart = ""
		} else {
			dst.Restart = *ov.Restart
		}
		applied = append(applied, "restart")
	}
	if present["ping_timeout"] {
		if ov.PingTimeout == nil {
			dst.PingTimeout = ""
		} else {
			dst.PingTimeout = *ov.PingTimeout
		}
		applied = append(applied, "ping_timeout")
	}
	if present["ready_timeout"] {
		if ov.ReadyTimeout == nil {
			dst.ReadyTimeout = ""
		} else {
			dst.ReadyTimeout = *ov.ReadyTimeout
		}
		applied = append(applied, "ready_timeout")
	}
	if present["output_format"] {
		if ov.OutputFormat == nil {
			dst.OutputFormat = ""
		} else {
			dst.OutputFormat = *ov.OutputFormat
		}
		applied = append(applied, "output_format")
	}
	sort.Strings(applied)
	return applied
}

// mergeLoadOverride applies the loader's override selection after extends
// resolution and before variable expansion.
func mergeLoadOverride(stack *Stack, stackPath string, cfg *loadConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.noOverride && cfg.overridePath != "" {
		return fmt.Errorf("override: --override and --no-override are mutually exclusive")
	}
	if cfg.noOverride {
		return nil
	}
	path, found, err := DiscoverOverride(stackPath, OverrideSelection{
		Path:     cfg.overridePath,
		Required: cfg.overrideRequired,
	})
	if err != nil || !found {
		return err
	}
	return mergeOverrideFile(stack, path, false)
}

func mergeOverrideFile(stack *Stack, path string, warnUnknown bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("override: %s: %w", path, err)
	}
	ov, err := DecodeOverride(data, path)
	if err != nil {
		return err
	}
	if warnUnknown {
		warnings, err := ApplyOverrideWarnUnknown(stack, ov, path)
		if err != nil {
			return err
		}
		if len(warnings) > 0 && stack != nil {
			stack.overrideWarnings = append(stack.overrideWarnings, warnings...)
		}
		return nil
	}
	return ApplyOverride(stack, ov, path)
}

// AnnotatePlanOverride marks diff items whose changed details come from
// overridden fields and records the override path on the plan.
func AnnotatePlanOverride(diff *PlanDiff, proposed *Stack) {
	if diff == nil || proposed == nil || proposed.OverridePath == "" {
		return
	}
	diff.Override = proposed.OverridePath
	if len(proposed.Overrides) == 0 {
		return
	}
	for i := range diff.Items {
		item := &diff.Items[i]
		if item.Kind != "mcp-server" {
			continue
		}
		fields := proposed.Overrides[item.Name]
		if len(fields) == 0 || item.Action != DiffChange {
			continue
		}
		for _, detail := range item.Details {
			if detailFromOverride(detail, fields) {
				item.Override = true
				break
			}
		}
	}
}

func detailFromOverride(detail string, fields []string) bool {
	for _, field := range fields {
		if detail == field+" changed" || strings.HasPrefix(detail, field+":") || strings.HasPrefix(detail, field+" ") {
			return true
		}
	}
	return false
}
