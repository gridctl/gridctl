package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"

	"github.com/spf13/cobra"
)

var (
	initName    string
	initForce   bool
	initExample string
)

var initCmd = &cobra.Command{
	Use:   "init [dir]",
	Short: "Scaffold a starter stack.yaml",
	Long: `Writes a commented starter stack.yaml so a stack can be bootstrapped
entirely from the terminal (the web wizard needs a running gateway; init
is how you get one).

The generated file passes 'gridctl validate' as-is. No runtime is started.`,
	Example: `  gridctl init                       Scaffold stack.yaml in the current directory
  gridctl init ./my-stack --name demo  Scaffold into ./my-stack as stack "demo"
  gridctl init --example skills        Include an example SKILL.md alongside the stack`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "."
		if len(args) == 1 {
			dir = args[0]
		}
		return runInit(os.Stdout, dir, initName, initExample, initForce)
	},
}

func init() {
	initCmd.Flags().StringVar(&initName, "name", "", "Stack name (default: directory name)")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite an existing stack.yaml")
	initCmd.Flags().StringVar(&initExample, "example", "minimal", "Scaffold variant: minimal or skills")
}

// stackTemplate is the commented starter stack. A string literal (not a
// marshaled struct) so the comments survive; TestInitScaffoldValidates
// guards it against schema drift.
const stackTemplate = `# stack.yaml (gridctl stack definition)
#
# Declare your MCP servers here, then deploy with:
#   gridctl apply ./stack.yaml
#
# Reference: https://github.com/gridctl/gridctl/blob/main/docs/config-schema.md
version: "1"
name: %[1]s

network:
  name: %[1]s-net

mcp-servers:
  # A containerized MCP server. gridctl pulls the image, starts the
  # container, and routes tool calls to it through the gateway.
  - name: everything
    image: mcp/everything:latest
    port: 8080
    description: "Reference server with example tools"

  # An external server that already runs elsewhere (no container managed
  # by gridctl). Uncomment and point at your server:
  # - name: remote-tools
  #   url: http://localhost:9001/mcp
  #   transport: sse
`

// skillTemplate is the example SKILL.md written by --example skills.
const skillTemplate = `---
name: getting-started
description: Example skill scaffolded by gridctl init
tags:
  - example
state: draft
---

# Getting Started

Replace this body with reusable instructions for your LLM clients.
Active skills are surfaced to every connected client as MCP prompts.
`

// initNameSanitizer collapses characters that are awkward in stack and
// network names down to hyphens.
var initNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// defaultStackName derives a stack name from the target directory.
func defaultStackName(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "my-stack"
	}
	name := strings.Trim(initNameSanitizer.ReplaceAllString(filepath.Base(abs), "-"), "-")
	if name == "" {
		return "my-stack"
	}
	return strings.ToLower(name)
}

// runInit scaffolds stack.yaml (and, for the skills example, an example
// SKILL.md) into dir. It never prompts: an existing stack.yaml is an
// error unless force is set, so scripted runs stay deterministic.
func runInit(w io.Writer, dir, name, example string, force bool) error {
	if example != "minimal" && example != "skills" {
		return fmt.Errorf("unknown --example %q (allowed: minimal, skills)", example)
	}

	if name == "" {
		name = defaultStackName(dir)
	} else if initNameSanitizer.MatchString(name) {
		// An explicit --name goes into the template verbatim, so reject
		// anything that could break the YAML instead of rewriting it.
		return fmt.Errorf("invalid --name %q (allowed characters: letters, digits, '-', '_')", name)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	stackPath := filepath.Join(dir, "stack.yaml")
	if _, err := os.Stat(stackPath); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", stackPath)
	}

	if err := os.WriteFile(stackPath, []byte(fmt.Sprintf(stackTemplate, name)), 0644); err != nil {
		return fmt.Errorf("writing %s: %w", stackPath, err)
	}

	printer := output.NewWithWriter(w)
	printer.Info("Wrote " + stackPath)

	if example == "skills" {
		skillPath := filepath.Join(dir, "skills", "getting-started", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skillPath), 0755); err != nil {
			return fmt.Errorf("creating skills directory: %w", err)
		}
		if err := os.WriteFile(skillPath, []byte(skillTemplate), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", skillPath, err)
		}
		printer.Info("Wrote " + skillPath)
		printer.Print("\nActivate the example skill after deploying:\n")
		printer.Print("  cp -r %s ~/.gridctl/registry/skills/\n", filepath.Join(dir, "skills", "getting-started"))
	}

	printer.Print("\nNext steps:\n")
	printer.Print("  1. Edit %s to declare your MCP servers\n", stackPath)
	printer.Print("  2. gridctl apply %s\n", stackPath)
	printer.Print("  3. gridctl link\n")
	return nil
}
