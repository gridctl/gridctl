package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

const skillImportExitInfrastructure = 2

var (
	skillImportKind       string
	skillImportSelect     []string
	skillImportAll        bool
	skillImportDryRun     bool
	skillImportTrust      bool
	skillImportForce      bool
	skillImportNoActivate bool
	skillImportFormat     string
	skillImportJSON       *bool
	skillImportPlain      *bool
)

var skillImportCmd = &cobra.Command{
	Use:   "import <client>",
	Short: "Import skills and agents from a client's home directories",
	Long: `Enumerate a client's home-scoped skill and agent locations and import
the selection into the registry.

Supported clients: agents, claude-code, opencode.

OpenCode agent files are listed and skipped. Skill directories named synced
or anthropic-skills under ~/.claude/skills are skipped. Copies gridctl
already projected are skipped.

Exit codes:
  0  imported, dry-run, or nothing enumerated (including an all-skipped scan)
  1  unknown client, cancelled selection, absent selected name, or every
     explicitly selected entry skipped
  2  infrastructure error (home resolution, lock read or write, newer lock)`,
	Example: `  gridctl skill import claude-code --dry-run
  gridctl skill import opencode --all
  gridctl skill import agents --select foo`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(skillImportFormat, cmd.Flags().Changed("format"), *skillImportJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(skillImportExitInfrastructure)
		}
		code := runSkillImport(cmd.Context(), os.Stdout, os.Stderr, args[0], skillImportConfig{
			Kind:       skillImportKind,
			Select:     skillImportSelect,
			All:        skillImportAll,
			DryRun:     skillImportDryRun,
			Trust:      skillImportTrust,
			Force:      skillImportForce,
			NoActivate: skillImportNoActivate,
			Format:     format,
			Plain:      skillImportPlain != nil && *skillImportPlain,
		})
		if code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

func init() {
	skillImportCmd.Flags().StringVar(&skillImportKind, "kind", "skill,agent", "Resource kinds to import: skill, agent, or both")
	skillImportCmd.Flags().StringArrayVar(&skillImportSelect, "select", nil, "Import this name (repeatable)")
	skillImportCmd.Flags().BoolVar(&skillImportAll, "all", false, "Import every candidate without prompting")
	skillImportCmd.Flags().BoolVar(&skillImportDryRun, "dry-run", false, "List candidates without writing")
	skillImportCmd.Flags().BoolVar(&skillImportTrust, "trust", false, "Import despite security findings")
	skillImportCmd.Flags().BoolVar(&skillImportForce, "force", false, "Overwrite skills and agents owned by another source")
	skillImportCmd.Flags().BoolVar(&skillImportNoActivate, "no-activate", false, "Import skills as draft")
	skillImportCmd.Flags().StringVar(&skillImportFormat, "format", "", "Output format: json")
	skillImportJSON = addJSONAlias(skillImportCmd)
	skillImportPlain = addPlainFlag(skillImportCmd)
	skillCmd.AddCommand(skillImportCmd)
}

type skillImportConfig struct {
	Kind       string
	Select     []string
	All        bool
	DryRun     bool
	Trust      bool
	Force      bool
	NoActivate bool
	Format     string
	Plain      bool
}

type skillImportDoc struct {
	SchemaVersion int                   `json:"schema_version"`
	Client        string                `json:"client"`
	DryRun        bool                  `json:"dry_run"`
	Entries       []skillImportEntryDoc `json:"entries"`
	Warnings      []string              `json:"warnings"`
}

type skillImportEntryDoc struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Source   string `json:"source"`
	Action   string `json:"action"`
	Reason   string `json:"reason,omitempty"`
}

var skillImportSelector = huhSelectSkillImports

func huhSelectSkillImports(candidates []skills.ClientCandidate) ([]int, error) {
	if !output.IsTerminal(os.Stdin) {
		return nil, fmt.Errorf("no selection and stdin is not a terminal; pass --all or --select <name>")
	}
	options := make([]huh.Option[int], 0, len(candidates))
	for i, c := range candidates {
		if c.SkipReason != "" {
			continue
		}
		label := fmt.Sprintf("%-8s %-24s %s", c.Kind, c.Name, c.Location)
		options = append(options, huh.NewOption(label, i).Selected(true))
	}
	if len(options) == 0 {
		return nil, nil
	}
	var picked []int
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[int]().
			Title("Select skills and agents to import").
			Options(options...).
			Value(&picked),
	)).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if !output.ColorEnabled(os.Stdout) {
		form = form.WithTheme(huh.ThemeBase())
	}
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, errPromptCancelled
		}
		return nil, err
	}
	return picked, nil
}

func runSkillImport(ctx context.Context, stdout, stderr io.Writer, client string, cfg skillImportConfig) int {
	if _, ok := skills.ClientLocations(client); !ok {
		fmt.Fprintf(stderr, "unknown client %q (supported: %s)\n", client, strings.Join(skills.SupportedImportClients(), ", "))
		return 1
	}
	kinds, err := parseSkillImportKinds(cfg.Kind)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	home, err := state.Home()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return skillImportExitInfrastructure
	}
	candidates, err := skills.EnumerateClient(ctx, client, home)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, project.ErrNewerLockVersion) || errors.Is(err, skills.ErrNewerImportLockVersion) {
			return skillImportExitInfrastructure
		}
		return skillImportExitInfrastructure
	}
	candidates = filterSkillImportKinds(candidates, kinds)
	if len(candidates) == 0 {
		if cfg.Format == "json" {
			writeSkillImportJSON(stdout, skillImportDoc{
				SchemaVersion: 1,
				Client:        client,
				DryRun:        cfg.DryRun,
				Entries:       []skillImportEntryDoc{},
				Warnings:      []string{},
			})
			return 0
		}
		fmt.Fprintf(stdout, "no skills or agents found in %s locations (checked: %s)\n", client, strings.Join(skills.CheckedLocations(client, home), ", "))
		return 0
	}
	selected, missing, code := selectSkillImport(stderr, candidates, cfg)
	if code != 0 {
		return code
	}
	store, err := loadRegistry()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return skillImportExitInfrastructure
	}
	imp := newImporter(store)
	result, err := skills.ApplyClientImport(ctx, imp, skills.ClientImportOptions{
		Trust:      cfg.Trust,
		Force:      cfg.Force,
		NoActivate: cfg.NoActivate,
		DryRun:     cfg.DryRun,
	}, selected)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, skills.ErrNewerImportLockVersion) || errors.Is(err, project.ErrNewerLockVersion) {
			return skillImportExitInfrastructure
		}
		if errors.Is(err, skills.ErrSourceConflict) {
			return 1
		}
		return skillImportExitInfrastructure
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "selected name not found: %s\n", strings.Join(missing, ", "))
	}
	renderSkillImport(stdout, stderr, client, cfg, result)
	if !cfg.DryRun && len(result.Roots) > 0 {
		if err := printProjectionHints(ctx, home, result.Roots); err != nil {
			fmt.Fprintln(stderr, err)
			return skillImportExitInfrastructure
		}
	}
	if len(missing) > 0 {
		return 1
	}
	if len(cfg.Select) > 0 && !cfg.All && !cfg.DryRun && everySelectedSkipped(result) {
		return 1
	}
	return 0
}

func parseSkillImportKinds(raw string) (map[string]bool, error) {
	kinds := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part != skills.ResourceKindSkill && part != skills.ResourceKindAgent {
			return nil, fmt.Errorf("unknown kind %q (supported: skill, agent)", part)
		}
		kinds[part] = true
	}
	if len(kinds) == 0 {
		return nil, fmt.Errorf("unknown kind %q (supported: skill, agent)", raw)
	}
	return kinds, nil
}

func filterSkillImportKinds(in []skills.ClientCandidate, kinds map[string]bool) []skills.ClientCandidate {
	var out []skills.ClientCandidate
	for _, c := range in {
		if kinds[c.Kind] {
			out = append(out, c)
		}
	}
	return out
}

func selectSkillImport(stderr io.Writer, candidates []skills.ClientCandidate, cfg skillImportConfig) ([]skills.ClientCandidate, []string, int) {
	if len(cfg.Select) > 0 {
		wanted := map[string]bool{}
		for _, name := range cfg.Select {
			wanted[name] = true
		}
		var selected []skills.ClientCandidate
		found := map[string]bool{}
		for _, c := range candidates {
			if wanted[c.Name] {
				selected = append(selected, c)
				found[c.Name] = true
			}
		}
		var missing []string
		for _, name := range cfg.Select {
			if !found[name] {
				missing = append(missing, name)
			}
		}
		if len(selected) == 0 {
			fmt.Fprintf(stderr, "selected name not found: %s\n", strings.Join(missing, ", "))
			return nil, missing, 1
		}
		return selected, missing, 0
	}
	if cfg.All || cfg.DryRun {
		return candidates, nil, 0
	}
	if !output.IsTerminal(os.Stdin) {
		fmt.Fprintln(stderr, "no selection and stdin is not a terminal; pass --all or --select <name>")
		return nil, nil, 1
	}
	picked, err := skillImportSelector(candidates)
	if err != nil {
		if errors.Is(err, errPromptCancelled) {
			fmt.Fprintln(stderr, "selection cancelled")
			return nil, nil, 1
		}
		fmt.Fprintln(stderr, err)
		return nil, nil, 1
	}
	var selected []skills.ClientCandidate
	for _, i := range picked {
		if i >= 0 && i < len(candidates) {
			selected = append(selected, candidates[i])
		}
	}
	if len(selected) == 0 {
		fmt.Fprintln(stderr, "selection cancelled")
		return nil, nil, 1
	}
	return selected, nil, 0
}

func everySelectedSkipped(result *skills.ClientImportResult) bool {
	if result == nil || len(result.Entries) == 0 {
		return true
	}
	for _, e := range result.Entries {
		if e.Action == "imported" {
			return false
		}
	}
	return true
}

func renderSkillImport(stdout, stderr io.Writer, client string, cfg skillImportConfig, result *skills.ClientImportResult) {
	doc := skillImportDoc{
		SchemaVersion: 1,
		Client:        client,
		DryRun:        cfg.DryRun,
		Entries:       []skillImportEntryDoc{},
		Warnings:      result.Warnings,
	}
	if doc.Warnings == nil {
		doc.Warnings = []string{}
	}
	importedSkills, importedAgents, skipped := 0, 0, 0
	for _, e := range result.Entries {
		action := e.Action
		if action == "" {
			action = "skipped"
		}
		reason := e.Reason
		if reason == "" {
			reason = e.Candidate.SkipReason
		}
		source := e.Candidate.Source
		if source == "" {
			source = e.Candidate.KnownName
		}
		doc.Entries = append(doc.Entries, skillImportEntryDoc{
			Kind:     e.Candidate.Kind,
			Name:     e.Candidate.Name,
			Location: e.Candidate.Location,
			Source:   source,
			Action:   action,
			Reason:   reason,
		})
		switch action {
		case "imported":
			if e.Candidate.Kind == skills.ResourceKindAgent {
				importedAgents++
			} else {
				importedSkills++
			}
		case "skipped":
			skipped++
		}
	}
	if cfg.Format == "json" {
		writeSkillImportJSON(stdout, doc)
		return
	}
	t := output.NewTableWriter(stdout, cfg.Plain)
	t.AppendHeader(table.Row{"KIND", "NAME", "LOCATION", "ACTION"})
	for _, e := range doc.Entries {
		action := e.Action
		if e.Action == "skipped" && e.Reason != "" {
			action = "skipped: " + e.Reason
		}
		t.AppendRow(table.Row{e.Kind, e.Name, e.Location, action})
	}
	t.Render()
	if !cfg.DryRun {
		fmt.Fprintf(stdout, "Imported %d skill(s), %d agent(s) from %d location(s); %d skipped\n", importedSkills, importedAgents, importedSkills+importedAgents, skipped)
	}
	for _, w := range result.Warnings {
		fmt.Fprintln(stderr, w)
	}
}

func writeSkillImportJSON(w io.Writer, doc skillImportDoc) {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintln(w, err)
		return
	}
	fmt.Fprintln(w, string(data))
}
