package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	exportOutputDir string
	exportFormat    string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export a stack spec from current running state",
	Long: `Generate a complete Stack Spec from the currently running deployment.
Reverse-engineers stack.yaml from gateway state files.

Secrets are never included — only ${vault:KEY} placeholders are used.
If remote skills are active, a skills.yaml is also generated.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runExport()
	},
}

func init() {
	exportCmd.Flags().StringVarP(&exportOutputDir, "output", "o", "", "Output directory (default: stdout)")
	exportCmd.Flags().StringVar(&exportFormat, "format", "yaml", "Output format: yaml or json")
	rootCmd.AddCommand(exportCmd)
}

func runExport() error {
	// Find running stacks
	states, err := state.List()
	if err != nil {
		return fmt.Errorf("listing running stacks: %w", err)
	}

	var running *state.DaemonState
	for i, s := range states {
		if state.IsRunning(&states[i]) {
			running = &s
			break
		}
	}

	if running == nil {
		return fmt.Errorf("no running stack found")
	}

	// Load the running stack's config
	stack, _, err := config.ValidateStackFile(running.StackFile)
	if err != nil {
		return fmt.Errorf("loading running stack config: %w", err)
	}

	// Sanitize secrets — replace raw env values that look like secrets with vault refs
	sanitizeSecrets(stack)

	if exportFormat == "json" {
		return outputJSON(stack)
	}
	return outputYAML(stack)
}

// sanitizeSecrets replaces raw secret-like env values with ${vault:KEY} placeholders.
func sanitizeSecrets(stack *config.Stack) {
	for i := range stack.MCPServers {
		sanitizeEnvMap(stack.MCPServers[i].Env, stack.MCPServers[i].Name)
	}
	for i := range stack.Resources {
		sanitizeEnvMap(stack.Resources[i].Env, stack.Resources[i].Name)
	}
}

// sanitizeEnvMap replaces values that are not already vault references
// and look like sensitive values with ${vault:KEY} placeholders.
func sanitizeEnvMap(env map[string]string, prefix string) {
	for key, val := range env {
		// Already a vault reference — leave as-is
		if len(val) > 8 && val[:7] == "${vault:" {
			continue
		}
		// Check if key looks sensitive
		if isSensitiveKey(key) {
			env[key] = fmt.Sprintf("${vault:%s_%s}", prefix, key)
		}
	}
}

// isSensitiveKey returns true if the env var key likely holds a secret.
func isSensitiveKey(key string) bool {
	sensitive := []string{
		"PASSWORD", "SECRET", "TOKEN", "API_KEY", "APIKEY",
		"PRIVATE_KEY", "ACCESS_KEY", "AUTH", "CREDENTIAL",
	}
	upper := strings.ToUpper(key)
	for _, s := range sensitive {
		if strings.Contains(upper, s) {
			return true
		}
	}
	return false
}

func outputJSON(stack *config.Stack) error {
	data, err := json.MarshalIndent(stack, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	if exportOutputDir != "" {
		return writeToDir(exportOutputDir, "stack.json", data)
	}

	fmt.Print(string(data))
	return nil
}

func outputYAML(stack *config.Stack) error {
	data, err := yaml.Marshal(stack)
	if err != nil {
		return fmt.Errorf("marshaling YAML: %w", err)
	}

	if exportOutputDir != "" {
		if err := writeToDir(exportOutputDir, "stack.yaml", data); err != nil {
			return err
		}

		// Generate skills.yaml if remote skills are active
		return exportSkillsConfig(exportOutputDir)
	}

	fmt.Print(string(data))
	return nil
}

func writeToDir(dir, filename string, data []byte) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	fmt.Printf("Wrote %s\n", path)
	return nil
}

// exportSkillsConfig generates a skills.yaml if any remote skills exist.
func exportSkillsConfig(dir string) error {
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil || len(lf.Sources) == 0 {
		return nil
	}

	type skillsYAML struct {
		Sources []skillSourceYAML `yaml:"sources"`
	}

	type sourceEntry struct {
		name string
		src  skills.LockedSource
	}

	var entries []sourceEntry
	for name, src := range lf.Sources {
		entries = append(entries, sourceEntry{name, src})
	}

	sy := skillsYAML{}
	for _, entry := range entries {
		sy.Sources = append(sy.Sources, skillSourceYAML{
			Name: entry.name,
			Repo: entry.src.Repo,
			Ref:  entry.src.Ref,
		})
	}

	data, err := yaml.Marshal(sy)
	if err != nil {
		return fmt.Errorf("marshaling skills.yaml: %w", err)
	}

	return writeToDir(dir, "skills.yaml", data)
}

type skillSourceYAML struct {
	Name string `yaml:"name"`
	Repo string `yaml:"repo"`
	Ref  string `yaml:"ref,omitempty"`
}
