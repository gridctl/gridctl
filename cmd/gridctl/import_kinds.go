package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/jedib0t/go-pretty/v6/table"
)

// Multi-kind exit contract (--kind includes skills, agents, or context):
//
//	0  requested kinds completed, or a policy skip alongside another kind
//	1  cancelled selection, non-interactive without --all or --yes, every
//	   selected server skipped, a policy skip when that kind was the only
//	   one requested, unknown client, or no requested kind applies
//	2  infrastructure error in one kind; later kinds are not run
//
// The servers-only path (flag omitted, or exactly servers) keeps the exit
// contract in import.go and does not use this file's renderer.

const unifiedJSONSchemaVersion = 1

var importKindOrder = []string{"servers", "skills", "agents", "context"}

type importSummaryRow struct {
	Kind   string
	Name   string
	Source string
	Action string
}

type unifiedImportConfig struct {
	Format     string
	DryRun     bool
	All        bool
	Yes        bool
	Trust      bool
	NoActivate bool
	Plain      bool
	Fragment   string
}

type importClientSupport struct {
	servers bool
	skills  bool
	agents  bool
	context bool
}

type unknownUnifiedClientError struct {
	slug string
	list string
}

func (e *unknownUnifiedClientError) Error() string {
	return fmt.Sprintf("unknown client %q\nSupported clients: %s", e.slug, e.list)
}

type skippedKindDoc struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type contextImportDoc struct {
	Client        string `json:"client"`
	Path          string `json:"path"`
	Action        string `json:"action"`
	Reason        string `json:"reason"`
	Bytes         int    `json:"bytes"`
	CanonicalPath string `json:"canonical_path"`
	Fragment      string `json:"fragment"`
	FragmentPath  string `json:"fragment_path"`
	Migrated      bool   `json:"migrated"`
	BackupPath    string `json:"backup_path"`
}

type unifiedSummaryDoc struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	NotRun   int `json:"not_run"`
}

type unifiedKindsDoc struct {
	Servers *importDoc        `json:"servers,omitempty"`
	Skills  *skillImportDoc   `json:"skills,omitempty"`
	Agents  *skillImportDoc   `json:"agents,omitempty"`
	Context *contextImportDoc `json:"context,omitempty"`
}

type unifiedImportDoc struct {
	SchemaVersion int               `json:"schema_version"`
	Client        string            `json:"client"`
	DryRun        bool              `json:"dry_run"`
	Kinds         unifiedKindsDoc   `json:"kinds"`
	SkippedKinds  []skippedKindDoc  `json:"skipped_kinds"`
	NotRun        []string          `json:"not_run"`
	StoppedAt     string            `json:"stopped_at"`
	ExitCode      int               `json:"exit_code"`
	Summary       unifiedSummaryDoc `json:"summary"`
}

func resolvedImportKinds(changed bool, raw string) ([]string, error) {
	if !changed {
		return []string{"servers"}, nil
	}
	return parseImportKinds(raw)
}

func parseImportKinds(raw string) ([]string, error) {
	aliases := map[string]string{
		"server": "servers", "servers": "servers",
		"skill": "skills", "skills": "skills",
		"agent": "agents", "agents": "agents",
		"context": "context",
		"all":     "all",
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		canon, ok := aliases[part]
		if !ok {
			return nil, fmt.Errorf("unknown kind %q (supported: servers, skills, agents, context, all)", part)
		}
		if canon == "all" {
			return append([]string{}, importKindOrder...), nil
		}
		seen[canon] = true
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("unknown kind %q (supported: servers, skills, agents, context, all)", raw)
	}
	var ordered []string
	for _, kind := range importKindOrder {
		if seen[kind] {
			ordered = append(ordered, kind)
		}
	}
	return ordered, nil
}

func serversOnlyKinds(kinds []string) bool {
	return len(kinds) == 1 && kinds[0] == "servers"
}

func containsKind(kinds []string, kind string) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func validateUnifiedFlags(kinds []string) error {
	hasContext := containsKind(kinds, "context")
	hasSkillish := containsKind(kinds, "skills") || containsKind(kinds, "agents")
	if importContextFragment != "" && !hasContext {
		return fmt.Errorf("--context-fragment requires --kind context")
	}
	if importTrust && !hasSkillish {
		return fmt.Errorf("--trust requires --kind skills or agents")
	}
	if importNoActivate && !hasSkillish {
		return fmt.Errorf("--no-activate requires --kind skills or agents")
	}
	if importContextFragment != "" {
		if err := contexts.ValidateFragmentName(importContextFragment); err != nil {
			return err
		}
	}
	return nil
}

func unifiedClientSlugs() []string {
	set := map[string]struct{}{}
	for _, slug := range provisioner.NewRegistry().AllSlugs() {
		set[slug] = struct{}{}
	}
	for _, slug := range skills.SupportedImportClients() {
		if _, ok := skills.ClientLocations(slug); ok {
			set[slug] = struct{}{}
		}
	}
	for _, slug := range contexts.SupportedSlugs() {
		set[slug] = struct{}{}
	}
	for _, u := range contexts.Unsupported() {
		set[u.Slug] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for slug := range set {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out
}

func classifyImportClient(slug string) (importClientSupport, error) {
	var sup importClientSupport
	known := false
	for _, s := range provisioner.NewRegistry().AllSlugs() {
		if s == slug {
			sup.servers = true
			known = true
		}
	}
	if rows, ok := skills.ClientLocations(slug); ok {
		known = true
		for _, row := range rows {
			if row.Kind == skills.ResourceKindSkill {
				sup.skills = true
			}
			if row.Kind == skills.ResourceKindAgent {
				sup.agents = true
			}
		}
	}
	if _, ok := contexts.FindTarget(slug); ok {
		known = true
	}
	var unsupportedReason string
	for _, u := range contexts.Unsupported() {
		if u.Slug == slug {
			known = true
			unsupportedReason = u.Reason
		}
	}
	if !known {
		return sup, &unknownUnifiedClientError{slug: slug, list: strings.Join(unifiedClientSlugs(), ", ")}
	}
	if unsupportedReason != "" {
		return sup, nil
	}
	mgr, err := contexts.NewManager()
	if err != nil {
		return sup, err
	}
	if _, ok := contexts.FindTarget(slug); ok {
		for _, e := range mgr.Scan() {
			if e.Slug == slug {
				sup.context = true
				break
			}
		}
	}
	return sup, nil
}

func (s importClientSupport) kindSupported(kind string) bool {
	switch kind {
	case "servers":
		return s.servers
	case "skills":
		return s.skills
	case "agents":
		return s.agents
	case "context":
		return s.context
	default:
		return false
	}
}

func noApplicableKindMessage(client string, sup importClientSupport) string {
	yn := func(ok bool) string {
		if ok {
			return "yes"
		}
		return "no"
	}
	return fmt.Sprintf("no requested kind applies to %s (servers: %s; skills: %s; agents: %s; context: %s)",
		client, yn(sup.servers), yn(sup.skills), yn(sup.agents), yn(sup.context))
}

func anyRequestedSupported(sup importClientSupport, kinds []string) bool {
	for _, kind := range kinds {
		if sup.kindSupported(kind) {
			return true
		}
	}
	return false
}

func policySkipCode(kinds []string) int {
	if len(kinds) == 1 {
		return 1
	}
	return 0
}

func runUnifiedImport(ctx context.Context, stdout, stderr io.Writer, client string, kinds []string, cfg unifiedImportConfig) int {
	sup, err := classifyImportClient(client)
	if err != nil {
		var unknown *unknownUnifiedClientError
		if errors.As(err, &unknown) {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stderr, err)
		return importExitInfrastructure
	}
	if !anyRequestedSupported(sup, kinds) {
		fmt.Fprintln(stderr, noApplicableKindMessage(client, sup))
		return 1
	}

	jsonMode := strings.EqualFold(cfg.Format, "json")
	narrate := stdout
	if jsonMode {
		narrate = stderr
	}
	doc := unifiedImportDoc{
		SchemaVersion: unifiedJSONSchemaVersion,
		Client:        client,
		DryRun:        cfg.DryRun,
		SkippedKinds:  []skippedKindDoc{},
		NotRun:        []string{},
	}
	requested := map[string]bool{}
	for _, kind := range kinds {
		requested[kind] = true
	}

	var (
		rows           []importSummaryRow
		exitCode       int
		stopped        string
		cancelled      bool
		serversWritten bool
		serverStack    string
		skillsWritten  bool
		skillRoots     []string
		contextWritten bool
		skillReported  bool
	)

	for _, kind := range importKindOrder {
		if !requested[kind] {
			continue
		}
		if exitCode != 0 {
			rows = append(rows, importSummaryRow{Kind: kind, Name: "-", Source: "-", Action: "not run"})
			doc.NotRun = append(doc.NotRun, kind)
			continue
		}
		if !sup.kindSupported(kind) {
			reason := "not supported for " + client
			rows = append(rows, importSummaryRow{Kind: kind, Name: "-", Source: "-", Action: "skipped: " + reason})
			doc.SkippedKinds = append(doc.SkippedKinds, skippedKindDoc{Kind: kind, Reason: reason})
			continue
		}
		switch kind {
		case "servers":
			fmt.Fprintf(narrate, "== servers ==\n")
			res := runServerImport(ctx, stdout, stderr, client, cfg.Format)
			if res.doc != nil {
				doc.Kinds.Servers = res.doc
				serverStack = res.doc.StackFile
				serversWritten = !cfg.DryRun && res.doc.Summary.Imported > 0
			}
			rows = append(rows, res.rows...)
			if res.code != 0 {
				if res.err != nil {
					printCLIError(stderr, nil, res.err)
					if errors.Is(res.err, errPromptCancelled) {
						cancelled = true
					}
				}
				exitCode = res.code
				stopped = "servers"
			}
		case "skills", "agents":
			if skillReported {
				continue
			}
			skillReported = true
			header := "skills"
			if kind == "agents" {
				header = "agents"
			}
			fmt.Fprintf(narrate, "== %s ==\n", header)
			skillCfg := skillImportConfig{
				Kind:           skillKindFilter(requested, sup),
				All:            cfg.All || cfg.Yes,
				DryRun:         cfg.DryRun,
				Trust:          cfg.Trust,
				NoActivate:     cfg.NoActivate,
				Format:         "text",
				Plain:          cfg.Plain,
				NoTerminalHint: "pass --all or --yes",
			}
			result, skillDoc, roots, _, code, selErr := collectSkillImport(ctx, stderr, client, skillCfg)
			if code != 0 {
				if errors.Is(selErr, errPromptCancelled) {
					cancelled = true
				}
				exitCode = code
				stopped = kind
				continue
			}
			skillRoots = roots
			if result == nil {
				if !jsonMode {
					home, herr := state.Home()
					checked := ""
					if herr == nil {
						checked = strings.Join(skills.CheckedLocations(client, home), ", ")
					}
					fmt.Fprintf(stdout, "no skills or agents found in %s locations (checked: %s)\n", client, checked)
				}
			} else if !jsonMode {
				renderSkillImport(stdout, stderr, client, skillCfg, result)
			} else {
				for _, w := range result.Warnings {
					fmt.Fprintln(stderr, w)
				}
			}
			if requested["skills"] && sup.skills {
				filtered := filterSkillDoc(skillDoc, skills.ResourceKindSkill)
				doc.Kinds.Skills = &filtered
				rows = append(rows, skillSummaryRows("skills", filtered)...)
			}
			if requested["agents"] && sup.agents {
				filtered := filterSkillDoc(skillDoc, skills.ResourceKindAgent)
				doc.Kinds.Agents = &filtered
				rows = append(rows, skillSummaryRows("agents", filtered)...)
			}
			if !cfg.DryRun && skillImported(result) {
				skillsWritten = true
			}
		case "context":
			fmt.Fprintf(narrate, "== context ==\n")
			cres := runContextImport(ctx, narrate, stderr, client, cfg, kinds)
			if cres.doc != nil {
				doc.Kinds.Context = cres.doc
			}
			if cres.row.Kind != "" {
				rows = append(rows, cres.row)
			}
			contextWritten = cres.written
			if cres.code != 0 {
				exitCode = cres.code
				stopped = "context"
			}
		}
	}

	doc.StoppedAt = stopped
	doc.ExitCode = exitCode
	doc.Summary = summarizeRows(rows)
	if jsonMode {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return importExitInfrastructure
		}
		printUnifiedNextSteps(ctx, stderr, serverStack, serversWritten, skillRoots, skillsWritten, contextWritten)
	} else {
		renderUnifiedSummary(stdout, rows, cfg.Plain)
		printUnifiedNextSteps(ctx, stdout, serverStack, serversWritten, skillRoots, skillsWritten, contextWritten)
	}
	if cancelled {
		fmt.Fprintln(stderr, "Earlier kinds were written and are not rolled back.")
	}
	return exitCode
}

func skillKindFilter(requested map[string]bool, sup importClientSupport) string {
	var parts []string
	if requested["skills"] && sup.skills {
		parts = append(parts, skills.ResourceKindSkill)
	}
	if requested["agents"] && sup.agents {
		parts = append(parts, skills.ResourceKindAgent)
	}
	return strings.Join(parts, ",")
}

func filterSkillDoc(doc skillImportDoc, kind string) skillImportDoc {
	out := doc
	out.Entries = []skillImportEntryDoc{}
	for _, e := range doc.Entries {
		if e.Kind == kind {
			out.Entries = append(out.Entries, e)
		}
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out
}

func skillSummaryRows(kindName string, doc skillImportDoc) []importSummaryRow {
	rows := make([]importSummaryRow, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		action := e.Action
		if e.Action == "skipped" && e.Reason != "" {
			action = "skipped: " + e.Reason
		}
		source := e.Location
		if source == "" {
			source = "-"
		}
		rows = append(rows, importSummaryRow{Kind: kindName, Name: e.Name, Source: source, Action: action})
	}
	return rows
}

func skillImported(result *skills.ClientImportResult) bool {
	if result == nil {
		return false
	}
	for _, e := range result.Entries {
		if e.Action == "imported" {
			return true
		}
	}
	return false
}

type contextKindResult struct {
	doc     *contextImportDoc
	row     importSummaryRow
	code    int
	written bool
}

func runContextImport(ctx context.Context, narrate, stderr io.Writer, client string, cfg unifiedImportConfig, requested []string) contextKindResult {
	mgr, err := contexts.NewManager()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return contextKindResult{code: importExitInfrastructure}
	}
	content, path, err := mgr.ReadClientContext(ctx, client)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return contextSkip(mgr, client, path, cfg, "no file at "+path, 0, policySkipCode(requested))
		}
		if errors.Is(err, contexts.ErrNothingToImport) {
			return contextSkip(mgr, client, path, cfg, "empty after removing gridctl-managed content", 0, policySkipCode(requested))
		}
		fmt.Fprintln(stderr, err)
		return contextKindResult{code: importExitInfrastructure}
	}
	bytes := len(content)
	if cfg.Fragment != "" {
		exists, ferr := fragmentNamed(mgr, cfg.Fragment)
		if ferr != nil {
			fmt.Fprintln(stderr, ferr)
			return contextKindResult{code: importExitInfrastructure}
		}
		if exists {
			return contextSkip(mgr, client, path, cfg, "fragment "+cfg.Fragment+" exists", bytes, policySkipCode(requested))
		}
		if verr := contexts.ValidateCanonicalContent(content); verr != nil {
			return contextSkip(mgr, client, path, cfg, verr.Error(), bytes, policySkipCode(requested))
		}
		text, action := contextImportAction(cfg.DryRun, cfg.Fragment)
		cdoc := contextDoc(mgr, client, path, action, "", bytes, cfg.Fragment)
		if cfg.DryRun {
			if !mgr.FragmentsActive() {
				fmt.Fprintln(stderr, "fragments mode would be activated")
			}
			return contextKindResult{doc: cdoc, row: contextRow(path, text), code: 0}
		}
		res, aerr := mgr.AddFragment(cfg.Fragment, content)
		if aerr != nil {
			if errors.Is(aerr, contexts.ErrFragmentExists) {
				return contextSkip(mgr, client, path, cfg, "fragment "+cfg.Fragment+" exists", bytes, policySkipCode(requested))
			}
			fmt.Fprintln(stderr, aerr)
			return contextKindResult{code: importExitInfrastructure}
		}
		if res.Migrated {
			fmt.Fprintf(narrate, "Activated fragments mode: migrated %s → fragments/00-default.md\n", mgr.CanonicalPath())
			if res.MigratedBackup != "" {
				fmt.Fprintf(narrate, "Backup: %s\n", res.MigratedBackup)
			}
		}
		cdoc.Migrated = res.Migrated
		cdoc.BackupPath = res.MigratedBackup
		if res.CreatedPath != "" {
			cdoc.FragmentPath = res.CreatedPath
		}
		return contextKindResult{doc: cdoc, row: contextRow(path, text), code: 0, written: true}
	}
	if mgr.FragmentsActive() {
		fmt.Fprintf(stderr, "gridctl import %s --kind context --context-fragment <name>\n", client)
		return contextSkip(mgr, client, path, cfg, "fragments mode active", bytes, policySkipCode(requested))
	}
	if mgr.HasCanonical() {
		fmt.Fprintf(stderr, "gridctl ctx init --import %s --force\n", client)
		fmt.Fprintf(stderr, "gridctl import %s --kind context --context-fragment <name>\n", client)
		return contextSkip(mgr, client, path, cfg, "canonical context exists", bytes, policySkipCode(requested))
	}
	if verr := contexts.ValidateCanonicalContent(content); verr != nil {
		return contextSkip(mgr, client, path, cfg, verr.Error(), bytes, policySkipCode(requested))
	}
	text, action := contextImportAction(cfg.DryRun, "")
	cdoc := contextDoc(mgr, client, path, action, "", bytes, "")
	if cfg.DryRun {
		return contextKindResult{doc: cdoc, row: contextRow(path, text), code: 0}
	}
	if err := mgr.InitFromClient(client, false); err != nil {
		fmt.Fprintln(stderr, err)
		return contextKindResult{code: importExitInfrastructure}
	}
	return contextKindResult{doc: cdoc, row: contextRow(path, text), code: 0, written: true}
}

func contextSkip(mgr *contexts.Manager, client, path string, cfg unifiedImportConfig, reason string, bytes, code int) contextKindResult {
	text, action := "skipped: "+reason, "skipped"
	if cfg.DryRun {
		text, action = "would skip: "+reason, "would skip"
	}
	return contextKindResult{
		doc:  contextDoc(mgr, client, path, action, reason, bytes, cfg.Fragment),
		row:  contextRow(path, text),
		code: code,
	}
}

func contextImportAction(dryRun bool, fragment string) (text, action string) {
	if fragment != "" {
		if dryRun {
			return "would import as fragment " + fragment, "would import as fragment"
		}
		return "imported as fragment " + fragment, "imported as fragment"
	}
	if dryRun {
		return "would import", "would import"
	}
	return "imported", "imported"
}

func contextDoc(mgr *contexts.Manager, client, path, action, reason string, bytes int, fragment string) *contextImportDoc {
	doc := &contextImportDoc{
		Client:        client,
		Path:          path,
		Action:        action,
		Reason:        reason,
		Bytes:         bytes,
		CanonicalPath: mgr.CanonicalPath(),
		Fragment:      fragment,
	}
	if fragment != "" {
		doc.FragmentPath = filepath.Join(mgr.FragmentsDir(), fragment+".md")
	}
	return doc
}

func contextRow(path, action string) importSummaryRow {
	name := filepath.Base(path)
	source := path
	if path == "" {
		name, source = "-", "-"
	}
	return importSummaryRow{Kind: "context", Name: name, Source: source, Action: action}
}

func fragmentNamed(mgr *contexts.Manager, name string) (bool, error) {
	if !mgr.FragmentsActive() {
		return false, nil
	}
	frags, err := mgr.ListFragments()
	if err != nil {
		return false, err
	}
	for _, f := range frags {
		if f.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func renderUnifiedSummary(w io.Writer, rows []importSummaryRow, plain bool) {
	t := output.NewTableWriter(w, plain)
	t.AppendHeader(table.Row{"KIND", "NAME", "SOURCE", "ACTION"})
	for _, r := range rows {
		t.AppendRow(table.Row{r.Kind, r.Name, r.Source, r.Action})
	}
	t.Render()
}

func summarizeRows(rows []importSummaryRow) unifiedSummaryDoc {
	var sum unifiedSummaryDoc
	for _, r := range rows {
		switch {
		case r.Action == "not run":
			sum.NotRun++
		case strings.HasPrefix(r.Action, "skipped") || strings.HasPrefix(r.Action, "would skip"):
			sum.Skipped++
		case strings.HasPrefix(r.Action, "imported") || strings.HasPrefix(r.Action, "would import"):
			sum.Imported++
		}
	}
	return sum
}

func printUnifiedNextSteps(ctx context.Context, w io.Writer, stack string, serversWritten bool, roots []string, skillsWritten, contextWritten bool) {
	if serversWritten && stack != "" {
		fmt.Fprintf(w, "gridctl apply %s\n", stack)
	}
	if skillsWritten {
		home, err := state.Home()
		if err != nil {
			fmt.Fprintln(w, err)
			return
		}
		if err := writeProjectionHints(ctx, w, home, roots); err != nil {
			fmt.Fprintln(w, err)
		}
	}
	if contextWritten {
		fmt.Fprintln(w, "gridctl ctx sync --dry-run")
	}
}

func unifiedImportHelp() string {
	return `With --kind, one invocation can import servers, skills, agents, and context from a single client. Accepted values are comma-separated and case-insensitive: servers, skills, agents, context, and all. Singular aliases server, skill, and agent are accepted. all expands to all four. Omitting --kind, or passing exactly servers, runs the servers-only path above. Stdout, stderr, and the exit code stay the same.

A run that includes skills, agents, or context requires one client. Kinds run in the order servers, skills, agents, context. A kind the client does not support is a skip row, not an error. If none of the requested kinds apply, the command exits 1. Skills and agents share one enumeration. --all or --yes selects every skill and agent candidate. --yes also skips server name collisions and confirms the stack write, as it does on the servers-only path.

--context-fragment NAME is valid only with --kind context. The context kind never overwrites an existing canonical file and never adopts into a home that is already in fragments mode. Adding a fragment is explicit because the first fragment migrates the single-file store. --trust and --no-activate are valid only when skills or agents are requested. --plain affects the skill table and the combined summary table only.

Text output ends with one KIND NAME SOURCE ACTION table and a next-steps block. --format json writes one document to stdout and sends narration to stderr. The command stops at the first nonzero kind code. Later kinds are reported as not run and are not rolled back.

Exit codes for a multi-kind run:
  0  requested kinds completed, or a policy skip alongside another kind
  1  cancelled selection, non-interactive run without --all or --yes, every selected server skipped, a policy skip when that kind was the only one requested, unknown client, or no requested kind applies
  2  infrastructure error in one kind

Per-kind client coverage is derived from the provisioner, skill location, and context tables:
` + importCoverageLines()
}

func applyHomeFlagForHelp() {
	if homeFlag == "" {
		return
	}
	abs, err := filepath.Abs(homeFlag)
	if err != nil {
		return
	}
	_ = os.Setenv(state.HomeEnv, abs)
}

func importCoverageLines() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  servers: %s\n", strings.Join(provisioner.NewRegistry().AllSlugs(), ", "))
	var skillSlugs, agentSlugs []string
	seenSkill, seenAgent := map[string]bool{}, map[string]bool{}
	for _, slug := range skills.SupportedImportClients() {
		rows, ok := skills.ClientLocations(slug)
		if !ok {
			continue
		}
		for _, row := range rows {
			if row.Kind == skills.ResourceKindSkill && !seenSkill[slug] {
				seenSkill[slug] = true
				skillSlugs = append(skillSlugs, slug)
			}
			if row.Kind == skills.ResourceKindAgent && !seenAgent[slug] {
				seenAgent[slug] = true
				agentSlugs = append(agentSlugs, slug)
			}
		}
	}
	fmt.Fprintf(&b, "  skills: %s\n", strings.Join(skillSlugs, ", "))
	fmt.Fprintf(&b, "  agents: %s\n", strings.Join(agentSlugs, ", "))
	home, err := state.Home()
	if err != nil {
		fmt.Fprintf(&b, "  context: (home unavailable: %s)\n", err)
	} else {
		mgr := contexts.NewManagerWithHome(home)
		scanned := map[string]bool{}
		for _, e := range mgr.Scan() {
			scanned[e.Slug] = true
		}
		var slugs []string
		for _, slug := range contexts.SupportedSlugs() {
			if _, ok := contexts.FindTarget(slug); ok && scanned[slug] {
				slugs = append(slugs, slug)
			}
		}
		if len(slugs) == 0 {
			b.WriteString("  context: (none on this platform)\n")
		} else {
			fmt.Fprintf(&b, "  context: %s\n", strings.Join(slugs, ", "))
		}
	}
	for _, u := range contexts.Unsupported() {
		fmt.Fprintf(&b, "  context %s: not supported (%s)\n", u.Slug, u.Reason)
	}
	return b.String()
}
