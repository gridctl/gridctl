package modelsync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxIncludeDepth bounds include recursion while scanning a LiteLLM
// config; LiteLLM itself has no deep nesting story and a cycle must
// not hang init.
const maxIncludeDepth = 5

// LiteLLMScan is what gridctl learns from reading a LiteLLM config:
// the declared model_name values (includes followed, relative to each
// file) and any auto_router entry already present.
type LiteLLMScan struct {
	// ModelNames are the model_list names in declaration order,
	// deduplicated, excluding auto_router entries.
	ModelNames []string
	// AutoRouterNames are model_list entries whose underlying model is
	// an auto_router/ route (already-routed setups).
	AutoRouterNames []string
}

// litellmConfig is the subset of LiteLLM's config gridctl reads. The
// decode is non-strict on purpose: real configs carry plenty of keys
// gridctl has no business interpreting.
type litellmConfig struct {
	Include   any `yaml:"include"`
	ModelList []struct {
		ModelName     string `yaml:"model_name"`
		LiteLLMParams struct {
			Model string `yaml:"model"`
		} `yaml:"litellm_params"`
	} `yaml:"model_list"`
}

// ParseLiteLLMConfig scans a LiteLLM config file, following its
// include entries (paths resolve relative to each file's directory,
// matching LiteLLM's own resolution).
func ParseLiteLLMConfig(path string) (*LiteLLMScan, error) {
	scan := &LiteLLMScan{}
	seen := map[string]bool{}
	names := map[string]bool{}
	if err := scanLiteLLMFile(path, 0, seen, names, scan); err != nil {
		return nil, err
	}
	return scan, nil
}

func scanLiteLLMFile(path string, depth int, seen, names map[string]bool, scan *LiteLLMScan) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("include depth exceeds %d at %s", maxIncludeDepth, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if seen[abs] {
		return nil // include cycle; LiteLLM would choke, gridctl just stops
	}
	seen[abs] = true

	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading LiteLLM config: %w", err)
	}
	var cfg litellmConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing %s: %w", abs, err)
	}
	for _, entry := range cfg.ModelList {
		if entry.ModelName == "" || names[entry.ModelName] {
			continue
		}
		names[entry.ModelName] = true
		if strings.HasPrefix(entry.LiteLLMParams.Model, "auto_router/") {
			scan.AutoRouterNames = append(scan.AutoRouterNames, entry.ModelName)
			continue
		}
		scan.ModelNames = append(scan.ModelNames, entry.ModelName)
	}
	for _, inc := range includePaths(cfg.Include) {
		if !filepath.IsAbs(inc) {
			inc = filepath.Join(filepath.Dir(abs), inc)
		}
		if err := scanLiteLLMFile(inc, depth+1, seen, names, scan); err != nil {
			return err
		}
	}
	return nil
}

// includePaths normalizes LiteLLM's include forms (scalar or list).
func includePaths(v any) []string {
	switch val := v.(type) {
	case string:
		if strings.TrimSpace(val) != "" {
			return []string{val}
		}
	case []any:
		var out []string
		for _, item := range val {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// InitFromLiteLLM scaffolds the policy from an existing LiteLLM config:
// its model_list names become backend references (never copied
// inventory) with a proposed tier mapping over the first backend, and
// the config becomes the sync target. The user edits tiers from there.
func (m *Manager) InitFromLiteLLM(path string, force bool) error {
	abs, err := m.expandPath(path)
	if err != nil {
		// Accept a plain relative CLI path by absolutizing against cwd;
		// the policy itself records the absolute form.
		if abs2, aerr := filepath.Abs(path); aerr == nil {
			abs = abs2
		} else {
			return err
		}
	}
	scan, err := ParseLiteLLMConfig(abs)
	if err != nil {
		return err
	}
	if len(scan.ModelNames) == 0 {
		return fmt.Errorf("%s declares no model_list entries to reference", abs)
	}

	entryModel := "smart-router"
	for _, taken := range scan.AutoRouterNames {
		if taken == entryModel {
			entryModel = "gridctl-router"
		}
	}
	first := scan.ModelNames[0]

	var b strings.Builder
	fmt.Fprintf(&b, "# gridctl models policy scaffolded from %s.\n", abs)
	b.WriteString("# Backends reference model_name values from that config; edit the tier\n")
	b.WriteString("# mapping below, then run 'gridctl models sync'.\n")
	b.WriteString("name: default\nkind: models\n")
	fmt.Fprintf(&b, "description: Routing policy scaffolded from %s\n\n", filepath.Base(abs))
	b.WriteString("router:\n")
	fmt.Fprintf(&b, "  entry_model: %s\n", entryModel)
	b.WriteString("  default_tier: MEDIUM\n\nbackends:\n")
	for _, name := range scan.ModelNames {
		fmt.Fprintf(&b, "  - %s\n", name)
	}
	b.WriteString("\n# Every tier starts on the first backend; spread them across your\n")
	b.WriteString("# fleet before syncing.\ntiers:\n")
	for _, tier := range tierOrder {
		fmt.Fprintf(&b, "  %s: %s\n", tier, first)
	}
	b.WriteString("\nweights:\n")
	b.WriteString("  tokenCount: 0.0\n  reasoningMarkers: 0.40\n  technicalTerms: 0.25\n")
	b.WriteString("  codePresence: 0.20\n  simpleIndicators: 0.10\n  multiStepPatterns: 0.05\n")
	b.WriteString("\nclients:\n  opencode:\n    provider_id: litellm\n")
	b.WriteString("    base_url: http://localhost:4000/v1\n    api_key_env: LITELLM_KEY\n")
	b.WriteString("    schema: detect\n")
	b.WriteString("\ntargets:\n  litellm:\n")
	fmt.Fprintf(&b, "    config_path: %s\n", abs)

	return m.initWith(b.String(), force)
}
