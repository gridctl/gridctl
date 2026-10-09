package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/internal/importer"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// Exit codes match the pins/optimize/validate contract: 0 success, 1 via a
// returned error (cancelled, or nothing imported from a non-empty selection),
// and 2 for infrastructure failures below.
const importExitInfrastructure = 2

// importJSONSchemaVersion identifies the shape of the import JSON document.
const importJSONSchemaVersion = 1

var (
	importAll          bool
	importDryRun       bool
	importYes          bool
	importName         string
	importFile         string
	importNoVault      bool
	importFormat       string
	importAsJSON       *bool
	importSourceConfig string
	importProjectDir   string
	importScopeFlag       string
	importKinds           string
	importContextFragment string
	importTrust           bool
	importNoActivate      bool
	importPlain           *bool
)

var importCmd = &cobra.Command{
	Use:   "import [client]",
	Short: "Import MCP servers from installed client configs",
	Long: `Scans installed LLM clients for existing MCP server definitions and adds
selected servers to your stack.yaml. The reverse of 'gridctl link'.

Client configs are read-only: the only file modified is the stack file
(backed up first as .gridctl-backup-<timestamp>). Identical servers found
in several clients are imported once, with their provenance shown. Entries
that connect a client to this gridctl gateway are filtered out, and name
collisions with existing stack servers are skipped unless resolved
interactively. Plaintext secret-looking env values are offered into the
variable store as ${var:KEY} references; genuine references such as
${env:VAR}, ${input:id}, or op:// URIs are preserved as-is.

Project files are discovered from the working directory, or --project-dir,
up to the nearest git root. The clients are Claude Code and VS Code
(.mcp.json), Cursor (.cursor/mcp.json), VS Code (.vscode/mcp.json), Roo
(.roo/mcp.json), Gemini (.gemini/settings.json), Zed (.zed/settings.json),
and OpenCode (opencode.jsonc, then opencode.json). A project file is read
whether or not that client is installed. --scope all (the default) also
reads Claude Code local scope from ~/.claude.json, then OpenCode's
OPENCODE_CONFIG file when that variable is set, then each detected client's
user file. A readable ~/.claude.json with no projects object produces no
local row. A project file that is also that client's user file is listed
once, as the user row. --scope user reads user files only. --scope project
reads project files only and does not open a home file. An empty project
scan says no project files were found.

On a same-name conflict gridctl keeps the earlier scope. The order is local,
then project, then custom, then user. That order is gridctl's choice. It
matches the documented rules for Claude Code, Roo, Gemini, and OpenCode.
Cursor does not document a replacement rule, and Zed gates project servers
on worktree trust, which gridctl does not consult.

Without arguments, all detected clients are scanned and servers are picked
interactively. Run 'gridctl link --help' for the supported client list.

Exit codes:
  0  imported, dry-run, or nothing enumerated (including an empty or
     all-skipped OpenCode source)
  1  cancelled, every selected server skipped after selection, unknown
     client, or --source-config used without a client argument
  2  infrastructure error (no stack file, parse or write failure,
     post-import validation failure, invalid --project-dir or --scope)`,
	Example: `  gridctl import                         Scan all clients, pick interactively
  gridctl import cursor                  Import from Cursor only
  gridctl import --scope project         Import checked-in project files only
  gridctl import cursor --source-config ./mcp.json
  gridctl import --all --dry-run         Preview everything without writing
  gridctl import --all --yes             Import everything, defaults applied`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(importFormat, cmd.Flags().Changed("format"), *importAsJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(importExitInfrastructure)
		}
		if err := resolvePlain(importPlain != nil && *importPlain, format); err != nil {
			return err
		}
		client := ""
		if len(args) == 1 {
			client = args[0]
		}
		kinds, err := resolvedImportKinds(cmd.Flags().Changed("kind"), importKinds)
		if err != nil {
			return err
		}
		if err := validateUnifiedFlags(kinds); err != nil {
			return err
		}
		if serversOnlyKinds(kinds) {
			return runImport(cmd.Context(), client, format)
		}
		if client == "" {
			return fmt.Errorf("--kind %s requires a client argument (skills, agents, and context are imported from one client at a time)", strings.TrimSpace(importKinds))
		}
		code := runUnifiedImport(cmd.Context(), os.Stdout, os.Stderr, client, kinds, unifiedImportConfig{
			Format:     format,
			DryRun:     importDryRun,
			All:        importAll,
			Yes:        importYes,
			Trust:      importTrust,
			NoActivate: importNoActivate,
			Plain:      importPlain != nil && *importPlain,
			Fragment:   importContextFragment,
		})
		if code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

func init() {
	importCmd.Flags().BoolVarP(&importAll, "all", "a", false, "Import from every detected client without a selection prompt")
	importCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Show what would be imported without writing anything")
	importCmd.Flags().BoolVarP(&importYes, "yes", "y", false, "Skip prompts: vault secrets, skip collisions, confirm the write")
	importCmd.Flags().StringVarP(&importName, "name", "n", "gridctl", "Gateway entry name to exclude from the scan (matches 'gridctl link --name')")
	importCmd.Flags().StringVarP(&importFile, "file", "f", "", "Stack file to append to (default: running stack's file, else ./stack.yaml)")
	importCmd.Flags().BoolVar(&importNoVault, "no-vault", false, "Import env values as-is instead of offering vault moves (a warning is printed per secret)")
	importCmd.Flags().StringVar(&importFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	importCmd.Flags().StringVar(&importSourceConfig, "source-config", "", "Config file to read exactly for the named client (no other sources; relative paths use the working directory)")
	importCmd.Flags().StringVar(&importProjectDir, "project-dir", "", "Directory to start project discovery from (default: working directory)")
	importCmd.Flags().StringVar(&importScopeFlag, "scope", "all", "Which scopes to scan: all, user, or project")
	importCmd.Flags().StringVar(&importKinds, "kind", "", "Kinds to import: servers, skills, agents, context, or all (default: servers)")
	importCmd.Flags().StringVar(&importContextFragment, "context-fragment", "", "Import context as this fragment instead of the canonical file (requires --kind context)")
	importCmd.Flags().BoolVar(&importTrust, "trust", false, "Import skills and agents despite security findings (requires --kind skills or agents)")
	importCmd.Flags().BoolVar(&importNoActivate, "no-activate", false, "Import skills as draft (requires --kind skills or agents)")
	importPlain = addPlainFlag(importCmd)
	importCmd.Long += "\n\n" + openCodeImportHelp
	importCmd.Long += "\n\n" + unifiedImportHelp()
	importCmd.Example += "\n  gridctl import opencode --kind all --dry-run\n  gridctl import claude-code --kind skills,agents --yes"
	importAsJSON = addJSONAlias(importCmd)
}

const openCodeImportHelp = `OpenCode user-scope import reads one file under the Gridctl home (GRIDCTL_HOME when
set, otherwise the OS home). It does not follow XDG_CONFIG_HOME. Existing
opencode.json is selected; opencode.jsonc is read only when opencode.json is
absent. If both exist, opencode.json is selected and opencode.jsonc is
reported but not merged.
` + provisioner.OpenCodeImportPolicy + `
config.json is not read automatically. OPENCODE_CONFIG_DIR and
OPENCODE_CONFIG_CONTENT are not read. OPENCODE_CONFIG is one custom-scope
file under --scope all, and is not consulted under --scope user, --scope
project, or --source-config. Project opencode.jsonc and opencode.json are
discovered with the other project files. --source-config PATH reads exactly
that file for the named client, with no fallback if the file is missing,
unreadable, or malformed.

Native local command arrays are imported as argv and are not shell-split.
A whole-value {env:NAME} reference becomes ${NAME} and expands when the
stack loads; an unset name becomes empty. {file} expressions, embedded
expressions, and invalid env names are skipped. Disabled servers and entries
with a working directory are skipped for every OpenCode shape, including
remote entries and string commands. A timeout is not transferred.

Text output includes per-entry skip reasons and a Sources block. JSON keeps
schema version 1. source and found_in remain client slugs. Optional
source_path, source_paths, origins, scopes, and sources fields carry
provenance. servers is null when nothing is enumerated.`

// --- JSON document ---

type importSecretDoc struct {
	Key    string `json:"key"`
	Action string `json:"action"` // "vaulted" or "kept_literal"
	Var    string `json:"var,omitempty"`
}

type importOriginDoc struct {
	Client string `json:"client"`
	Scope  string `json:"scope"`
	Path   string `json:"path"`
}

type importServerDoc struct {
	Name        string            `json:"name"`
	Imported    bool              `json:"imported"`
	FoundIn     []string          `json:"found_in,omitempty"`
	Origins     []importOriginDoc `json:"origins,omitempty"`
	Scopes      []string          `json:"scopes,omitempty"`
	Source      string            `json:"source,omitempty"`
	SourcePath  string            `json:"source_path,omitempty"`
	SourcePaths []string          `json:"source_paths,omitempty"`
	SkipReason  string            `json:"skip_reason,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
	Secrets     []importSecretDoc `json:"secrets,omitempty"`
}

// importSourceDoc is file-level provenance. It does not replace client slugs.
// count is text-only and is not part of the JSON document.
type importSourceDoc struct {
	Client        string   `json:"client"`
	Clients       []string `json:"clients,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	Path          string   `json:"path,omitempty"`
	Status        string   `json:"status"`
	Detail        string   `json:"detail,omitempty"`
	AlternatePath string   `json:"alternate_path,omitempty"`
	Notes         []string `json:"notes,omitempty"`
	count         int
}

type importSummaryDoc struct {
	Found    int `json:"found"`
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

type importDoc struct {
	SchemaVersion int               `json:"schema_version"`
	StackFile     string            `json:"stack_file"`
	BackupPath    string            `json:"backup_path,omitempty"`
	DryRun        bool              `json:"dry_run"`
	Sources       []importSourceDoc `json:"sources,omitempty"`
	Servers       []importServerDoc `json:"servers"`
	Summary       importSummaryDoc  `json:"summary"`
}

// --- interactive seams (swappable in tests, mirroring clientSelector) ---

// importSelector picks which candidates to import. Receives only importable
// candidates; returns indexes into that slice.
var importSelector = huhSelectCandidates

// importCollisionResolver decides what to do with a name collision:
// "skip", "rename" (with the new name), or "overwrite".
var importCollisionResolver = huhResolveCollision

// importVaultConfirm asks whether one server's secret keys move to the vault.
var importVaultConfirm = huhConfirmVault

// importWriteConfirm is the final gate before stack.yaml is written.
var importWriteConfirm = huhConfirmWrite

func huhSelectCandidates(candidates []importer.Candidate) ([]int, error) {
	if err := requireInteractiveStdin("import"); err != nil {
		return nil, err
	}
	options := make([]huh.Option[int], len(candidates))
	for i, c := range candidates {
		label := fmt.Sprintf("%-24s from %s", c.Name, formatProvenance(c))
		options[i] = huh.NewOption(label, i).Selected(true)
	}
	var picked []int
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[int]().
			Title("Select servers to import").
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

func huhResolveCollision(name string, taken func(string) bool) (string, string, error) {
	action := "skip"
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(fmt.Sprintf("%q already exists in the stack", name)).
			Options(
				huh.NewOption("Skip", "skip"),
				huh.NewOption("Import under a different name", "rename"),
				huh.NewOption("Overwrite the stack entry", "overwrite"),
			).
			Value(&action),
	)).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", "", errPromptCancelled
		}
		return "", "", err
	}
	if action != "rename" {
		return action, "", nil
	}
	newName := name + "-imported"
	rename := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("New server name").
			Value(&newName).
			Validate(func(s string) error {
				s = strings.TrimSpace(s)
				if s == "" {
					return errors.New("name cannot be empty")
				}
				if strings.ContainsAny(s, " \t") {
					return errors.New("name cannot contain whitespace")
				}
				if taken(s) {
					return fmt.Errorf("%q is also taken", s)
				}
				return nil
			}),
	)).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := rename.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", "", errPromptCancelled
		}
		return "", "", err
	}
	return "rename", strings.TrimSpace(newName), nil
}

func huhConfirmVault(server string, keys []string) (bool, error) {
	return runConfirm(fmt.Sprintf("%s: move %s to the vault as ${var:KEY}?", server, strings.Join(keys, ", ")))
}

func huhConfirmWrite(summary string) (bool, error) {
	return runConfirm(summary)
}

// runConfirm renders a default-yes confirm prompt with the shared
// accessibility and abort-mapping conventions.
func runConfirm(title string) (bool, error) {
	confirmed := true
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Value(&confirmed),
	)).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, errPromptCancelled
		}
		return false, err
	}
	return confirmed, nil
}

// --- command flow ---

func emptyImportNotice(scope, projectDir string) (message, hint string) {
	if scope == "project" {
		return fmt.Sprintf("No project MCP files found from %s up to the nearest git root", projectDir), ""
	}
	return "No supported LLM clients detected", "Run 'gridctl link --help' for the supported client list.\n"
}

// serverImportResult is the servers kind outcome. emit is set only on the
// paths that today call finishImport, so the default wrapper stays
// byte-identical: a returned error without emit still reaches Cobra, and
// infrastructure exits do not encode JSON.
type serverImportResult struct {
	doc  *importDoc
	rows []importSummaryRow
	code int
	err  error
	emit bool
}

func runImport(ctx context.Context, client, format string) error {
	res := runServerImport(ctx, os.Stdout, os.Stderr, client, format)
	if res.code == importExitInfrastructure {
		os.Exit(res.code)
	}
	if res.emit && res.doc != nil {
		return finishImport(nil, *res.doc, format, res.err)
	}
	return res.err
}

func runServerImport(ctx context.Context, stdout, stderr io.Writer, client, format string) serverImportResult {
	// In JSON mode stdout carries exactly one document; narration moves to
	// stderr so pipelines can parse the output. The wrapper, not this
	// function, encodes that document.
	printer := output.NewWithWriter(stdout)
	if strings.EqualFold(format, "json") {
		printer = output.NewWithWriter(stderr)
	}
	fail := func(err error) serverImportResult {
		return serverImportResult{code: 1, err: err}
	}
	infra := func() serverImportResult {
		return serverImportResult{code: importExitInfrastructure}
	}
	done := func(doc *importDoc, rows []importSummaryRow, err error) serverImportResult {
		code := 0
		if err != nil {
			code = 1
		}
		return serverImportResult{doc: doc, rows: rows, code: code, err: err, emit: true}
	}
	keep := func(doc *importDoc, rows []importSummaryRow, err error) serverImportResult {
		code := 0
		if err != nil {
			code = 1
		}
		return serverImportResult{doc: doc, rows: rows, code: code, err: err}
	}
	if err := rejectSourceConfig(client); err != nil {
		return fail(err)
	}
	if err := validateImportScope(importScopeFlag); err != nil {
		fmt.Fprintln(stderr, err)
		return infra()
	}
	projectDir, err := resolveProjectDir(importProjectDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return infra()
	}
	registry := provisioner.NewRegistry()

	collected, err := collectSources(ctx, registry, client, importCollectOpts{
		Scope:      importScopeFlag,
		ProjectDir: projectDir,
		Explicit:   importSourceConfig,
	})
	if err != nil {
		return fail(err)
	}
	sources := sourceDocs(collected)
	if client == "" && len(collected) == 0 {
		message, hint := emptyImportNotice(importScopeFlag, projectDir)
		printer.Info(message)
		if hint != "" {
			printer.Print("%s", hint)
		}
		return serverImportResult{}
	}

	stackPath, source, err := resolveStackFileTarget(importFile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return infra()
	}
	existingNames, err := stackServerNames(source)
	if err != nil {
		fmt.Fprintf(stderr, "parsing %s: %v\n", stackPath, err)
		return infra()
	}

	printImportSources(printer, sources)
	candidates, skipped := scanForCandidates(printer, collected)
	doc := &importDoc{
		SchemaVersion: importJSONSchemaVersion,
		StackFile:     stackPath,
		DryRun:        importDryRun,
		Sources:       sources,
	}
	for _, s := range skipped {
		renderImportSkip(printer, s)
		doc.Servers = append(doc.Servers, serverDoc(s, false, nil))
	}
	doc.Summary.Found = len(candidates) + len(skipped)
	doc.Summary.Skipped = len(skipped)

	scanRows := serverSummaryRows(skipped, nil, nil, false)
	if len(candidates) == 0 {
		printer.Info("No importable servers found")
		return done(doc, scanRows, nil)
	}

	selected, err := selectCandidates(candidates)
	if err != nil {
		return keep(doc, scanRows, err)
	}
	if len(selected) == 0 {
		printer.Info("No servers selected")
		return done(doc, scanRows, nil)
	}

	// Resolve name collisions against the stack and within the selection.
	taken := func(name string) bool {
		if existingNames[name] {
			return true
		}
		for i := range selected {
			if selected[i].Name == name && selected[i].SkipReason == "" {
				return true
			}
		}
		return false
	}
	overwriting := make(map[string]bool)
	interactive := !importYes && output.IsTerminal(os.Stdin)
	for i := range selected {
		if !existingNames[selected[i].Name] {
			continue
		}
		if !interactive {
			selected[i].SkipReason = importer.SkipNameCollision
			renderImportSkip(printer, selected[i])
			continue
		}
		action, newName, err := importCollisionResolver(selected[i].Name, taken)
		if err != nil {
			return keep(doc, scanRows, err)
		}
		switch action {
		case "rename":
			selected[i].Warnings = append(selected[i].Warnings, fmt.Sprintf("imported as %q (name collision)", newName))
			selected[i].Name = newName
			selected[i].Server.Name = newName
		case "overwrite":
			overwriting[selected[i].Name] = true
			selected[i].Warnings = append(selected[i].Warnings, "replaced the existing stack entry")
		default:
			selected[i].SkipReason = importer.SkipNameCollision
			renderImportSkip(printer, selected[i])
		}
	}

	// Intra-selection duplicates: two clients can define different servers
	// under one name (or names that sanitize to the same string). Dedupe
	// keeps them as separate candidates for review; only one can land in the
	// stack, so later occurrences are skipped with a pointer at the winner.
	// Without this, the post-append validation gate would reject the whole
	// batch and nothing would import.
	seenNames := make(map[string]string) // name -> source of the winner
	for i := range selected {
		if selected[i].SkipReason != "" {
			continue
		}
		if winner, dup := seenNames[selected[i].Name]; dup {
			selected[i].SkipReason = importer.SkipNameCollision
			selected[i].Warnings = append(selected[i].Warnings,
				fmt.Sprintf("another selected server named %q (from %s) was imported instead; rename one and re-run to import both", selected[i].Name, winner))
			renderImportSkip(printer, selected[i])
			continue
		}
		seenNames[selected[i].Name] = formatProvenance(selected[i])
	}

	// Secret handling. References were never classified as secrets; what is
	// left is literal values under secret-suggestive keys.
	secretDocs := make(map[string][]importSecretDoc)
	if !importDryRun {
		if err := vaultSelectedSecrets(printer, selected, secretDocs); err != nil {
			return keep(doc, scanRows, err)
		}
	} else {
		for _, c := range selected {
			for _, key := range c.SecretKeys {
				secretDocs[c.Name] = append(secretDocs[c.Name], importSecretDoc{Key: key, Action: "vaulted"})
			}
		}
	}

	importable := make([]importer.Candidate, 0, len(selected))
	for _, c := range selected {
		if c.SkipReason == "" {
			importable = append(importable, c)
		} else {
			doc.Servers = append(doc.Servers, serverDoc(c, false, nil))
			doc.Summary.Skipped++
		}
	}

	// Remove a stack entry only when its replacement actually survived to
	// the importable set, and remove it exactly once even if several
	// same-named candidates chose overwrite.
	var overwrites []string
	for _, c := range importable {
		if overwriting[c.Name] {
			overwrites = append(overwrites, c.Name)
			overwriting[c.Name] = false
		}
	}

	renderImportPlan(printer, importable, overwrites)
	laterSkipped := selectedSkips(selected)
	planned := serverSummaryRows(append(append([]importer.Candidate{}, skipped...), laterSkipped...), importable, overwrites, false)
	if len(importable) == 0 {
		// A non-empty selection that produced zero imports is the documented
		// exit-1 case: the user asked for servers and got none.
		return done(doc, planned,
			errors.New("nothing imported: every selected server was skipped (see skip reasons above)"))
	}

	if importDryRun {
		for _, c := range importable {
			doc.Servers = append(doc.Servers, serverDoc(c, false, secretDocs[c.Name]))
		}
		printer.Print("\nNo changes made (dry run).\n")
		return done(doc, serverSummaryRows(append(append([]importer.Candidate{}, skipped...), laterSkipped...), importable, overwrites, true), nil)
	}

	if err := warnRunningStack(printer, stackPath, importYes); err != nil {
		return keep(doc, planned, err)
	}
	if interactive {
		ok, err := importWriteConfirm(fmt.Sprintf("Append %d server(s) to %s?", len(importable), stackPath))
		if err != nil {
			return keep(doc, planned, err)
		}
		if !ok {
			printer.Info("Import cancelled")
			declined := declineCandidates(importable)
			return done(doc, serverSummaryRows(append(append([]importer.Candidate{}, skipped...), append(laterSkipped, declined...)...), nil, nil, false), nil)
		}
	}

	backupPath, err := writeImportedServers(stackPath, importable, overwrites)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serverImportResult{doc: doc, rows: planned, code: importExitInfrastructure}
	}
	doc.BackupPath = backupPath

	for _, c := range importable {
		doc.Servers = append(doc.Servers, serverDoc(c, true, secretDocs[c.Name]))
		doc.Summary.Imported++
		printer.Info(fmt.Sprintf("Imported %s (from %s)", c.Name, formatProvenance(c)))
	}
	if backupPath != "" {
		printer.Print("  Backup: %s\n", backupPath)
	}
	printer.Print("  Run 'gridctl apply %s' to deploy the imported servers.\n", stackPath)
	return done(doc, serverSummaryRows(append(append([]importer.Candidate{}, skipped...), laterSkipped...), importable, overwrites, false), nil)
}

func selectedSkips(selected []importer.Candidate) []importer.Candidate {
	var out []importer.Candidate
	for _, c := range selected {
		if c.SkipReason != "" {
			out = append(out, c)
		}
	}
	return out
}

func declineCandidates(importable []importer.Candidate) []importer.Candidate {
	out := make([]importer.Candidate, len(importable))
	for i, c := range importable {
		c.SkipReason = "import cancelled"
		out[i] = c
	}
	return out
}

func serverSummaryRows(skipped, importable []importer.Candidate, overwrites []string, dryRun bool) []importSummaryRow {
	replacing := make(map[string]bool, len(overwrites))
	for _, name := range overwrites {
		replacing[name] = true
	}
	rows := make([]importSummaryRow, 0, len(skipped)+len(importable))
	for _, c := range skipped {
		source := formatProvenance(c)
		if source == "" {
			source = "-"
		}
		rows = append(rows, importSummaryRow{Kind: "servers", Name: c.Name, Source: source, Action: "skipped: " + c.SkipReason})
	}
	for _, c := range importable {
		source := formatProvenance(c)
		if source == "" {
			source = "-"
		}
		action := "imported"
		if dryRun {
			action = "would import"
		}
		if replacing[c.Name] {
			action += " (replace)"
		}
		rows = append(rows, importSummaryRow{Kind: "servers", Name: c.Name, Source: source, Action: action})
	}
	return rows
}

func rejectSourceConfig(client string) error {
	if importSourceConfig == "" || client != "" {
		return nil
	}
	return fmt.Errorf("--source-config requires a client argument (got a multi-client scan)")
}

func resolveSourceConfig(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving --source-config: %w", err)
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}

// scanForCandidates lists, filters, maps, and dedupes servers across the
// ordered sources. A broken source warns and is skipped; the scan never
// aborts because one file is broken.
func scanForCandidates(printer *output.Printer, sources []importSource) (importable, skipped []importer.Candidate) {
	var all []importer.Candidate
	seenSkip := make(map[string]int)
	for _, src := range sources {
		if src.ListErr != nil {
			name := src.Doc.Client
			if name == "" && len(src.Slugs) > 0 {
				name = src.Slugs[0]
			}
			printer.Warn(fmt.Sprintf("Skipping %s: cannot parse %s (%v)", name, src.Path, src.ListErr))
			continue
		}
		if src.Doc.Status != "" && src.Doc.Status != provisioner.OpenCodeImportSelected {
			continue
		}
		slug := ""
		if len(src.Slugs) > 0 {
			slug = src.Slugs[0]
		}
		origins := originsFor(src)
		for _, entry := range src.Entries {
			if importer.IsGatewaySelfEntry(entry.Name, importName, entry.Raw) {
				key := entry.Name + "|self|" + src.Scope
				skipped = appendSkip(skipped, seenSkip, key, withOrigins(importer.Candidate{
					Name:       entry.Name,
					SkipReason: importer.SkipGatewaySelfEntry,
				}, origins))
				continue
			}
			server, warnings, err := importer.MapEntryWithOptions(slug, entry, importer.MapOptions{
				OwnerDir: src.OwnerDir,
				Scope:    src.Scope,
			})
			if err != nil {
				reason, detail := skipFromMapErr(err)
				key := entry.Name + "|" + reason + "|" + src.Scope
				skipped = appendSkip(skipped, seenSkip, key, withOrigins(importer.Candidate{
					Name:       entry.Name,
					SkipReason: reason,
					Warnings:   []string{detail},
				}, origins))
				continue
			}
			all = append(all, withOrigins(importer.Candidate{
				Name:       server.Name,
				Server:     server,
				Warnings:   warnings,
				SecretKeys: importer.ClassifySecretKeys(server.Env),
			}, origins))
		}
	}
	return importer.Dedupe(all), skipped
}

func appendSkip(skipped []importer.Candidate, seen map[string]int, key string, c importer.Candidate) []importer.Candidate {
	if i, ok := seen[key]; ok {
		mergeSkip(&skipped[i], c)
		return skipped
	}
	seen[key] = len(skipped)
	return append(skipped, c)
}

func appendSourcePath(c *importer.Candidate, path string) {
	if path == "" {
		return
	}
	if c.SourcePath == "" {
		c.SourcePath = path
	}
	for _, existing := range c.SourcePaths {
		if existing == path {
			return
		}
	}
	c.SourcePaths = append(c.SourcePaths, path)
}

func skipFromMapErr(err error) (string, string) {
	var me *importer.MapError
	if errors.As(err, &me) && me.Reason != "" {
		detail := me.Detail
		if detail == "" {
			detail = me.Reason
		}
		return me.Reason, detail
	}
	return importer.SkipUnsupported, err.Error()
}

func serverDoc(c importer.Candidate, imported bool, secrets []importSecretDoc) importServerDoc {
	var origins []importOriginDoc
	for _, o := range c.Origins {
		origins = append(origins, importOriginDoc{Client: o.Client, Scope: o.Scope, Path: o.Path})
	}
	scopes := c.Scopes()
	return importServerDoc{
		Name: c.Name, Imported: imported, FoundIn: c.FoundIn, Origins: origins, Scopes: scopes, Source: c.Source,
		SourcePath: c.SourcePath, SourcePaths: c.SourcePaths,
		SkipReason: c.SkipReason, Warnings: c.Warnings, Secrets: secrets,
	}
}

func printImportSources(printer *output.Printer, sources []importSourceDoc) {
	if len(sources) == 0 {
		return
	}
	printer.Print("Sources:\n")
	for _, s := range sources {
		clients := strings.Join(s.Clients, ", ")
		if clients == "" {
			clients = s.Client
		}
		scope := s.Scope
		if scope == "" {
			scope = "-"
		}
		path := s.Path
		if path == "" {
			path = "(none)"
		}
		printer.Print("  %-22s %-8s %s  %s\n", clients, scope, path, formatSourceStatus(s))
		detail := s.Detail
		if s.Scope == "local" && s.Status == provisioner.OpenCodeImportSelected {
			detail = ""
		}
		if detail != "" {
			printer.Print("    %s\n", detail)
		}
		if s.AlternatePath != "" {
			printer.Print("    unmerged sibling: %s\n", s.AlternatePath)
		}
		for _, note := range s.Notes {
			printer.Print("    %s\n", note)
		}
	}
}

func renderImportSkip(printer *output.Printer, c importer.Candidate) {
	loc := formatProvenance(c)
	if c.SourcePath != "" {
		if loc != "" {
			loc += " "
		}
		loc += c.SourcePath
	}
	detail := strings.Join(c.Warnings, "; ")
	if detail == "" {
		printer.Print("  skipped %s (%s) from %s\n", c.Name, c.SkipReason, loc)
		return
	}
	printer.Print("  skipped %s (%s) from %s: %s\n", c.Name, c.SkipReason, loc, detail)
}

// selectCandidates applies the selection mode: everything under --all or
// --yes, an interactive multi-select otherwise.
func selectCandidates(candidates []importer.Candidate) ([]importer.Candidate, error) {
	if importAll || importYes {
		return append([]importer.Candidate(nil), candidates...), nil
	}
	picked, err := importSelector(candidates)
	if err != nil {
		return nil, err
	}
	selected := make([]importer.Candidate, 0, len(picked))
	for _, i := range picked {
		selected = append(selected, candidates[i])
	}
	return selected, nil
}

// vaultSelectedSecrets moves confirmed secret env values into the variable
// store and rewrites the candidate env to ${var:KEY} references. Every
// movement is printed; values never are. A locked or unavailable store
// downgrades to keeping literals with a warning for the whole run.
func vaultSelectedSecrets(printer *output.Printer, selected []importer.Candidate, secretDocs map[string][]importSecretDoc) error {
	var store *vault.Store
	vaultDisabled := importNoVault
	for i := range selected {
		c := &selected[i]
		if c.SkipReason != "" || len(c.SecretKeys) == 0 {
			continue
		}
		if !vaultDisabled && store == nil {
			s, err := loadVault()
			if err == nil {
				err = ensureUnlocked(s)
			}
			if err != nil {
				printer.Warn(fmt.Sprintf("Variable store unavailable (%v); secrets will be imported as literals", err))
				vaultDisabled = true
			} else {
				store = s
			}
		}
		if vaultDisabled {
			for _, key := range c.SecretKeys {
				printer.Warn(fmt.Sprintf("%s: env %s imported as a literal secret; consider 'gridctl var set %s' and a ${var:%s} reference", c.Name, key, key, key))
				secretDocs[c.Name] = append(secretDocs[c.Name], importSecretDoc{Key: key, Action: "kept_literal"})
			}
			continue
		}
		vaultIt := true
		if !importYes && output.IsTerminal(os.Stdin) {
			ok, err := importVaultConfirm(c.Name, c.SecretKeys)
			if err != nil {
				return err
			}
			vaultIt = ok
		}
		if !vaultIt {
			for _, key := range c.SecretKeys {
				printer.Warn(fmt.Sprintf("%s: env %s kept as a literal secret", c.Name, key))
				secretDocs[c.Name] = append(secretDocs[c.Name], importSecretDoc{Key: key, Action: "kept_literal"})
			}
			continue
		}
		for _, key := range c.SecretKeys {
			varKey, err := storeSecret(store, key, c.Server.Env[key])
			if err != nil {
				return fmt.Errorf("storing %s for %s: %w", key, c.Name, err)
			}
			c.Server.Env[key] = "${var:" + varKey + "}"
			printer.Info(fmt.Sprintf("%s: env %s moved to the vault as ${var:%s}", c.Name, key, varKey))
			secretDocs[c.Name] = append(secretDocs[c.Name], importSecretDoc{Key: key, Action: "vaulted", Var: varKey})
		}
	}
	return nil
}

// storeSecret writes value under key in the variable store, suffixing the
// key when it already holds a different value.
func storeSecret(store *vault.Store, key, value string) (string, error) {
	candidate := key
	for n := 2; ; n++ {
		if existing, ok := store.GetVariable(candidate); ok {
			if existing.Value == value {
				return candidate, nil // identical secret already stored
			}
			candidate = fmt.Sprintf("%s_%d", key, n)
			continue
		}
		return candidate, store.SetVariable(vault.Variable{Key: candidate, Value: value, IsSecret: true})
	}
}

// renderImportPlan prints the selection in the plan vocabulary.
func renderImportPlan(printer *output.Printer, importable []importer.Candidate, overwrites []string) {
	if len(importable) == 0 {
		printer.Info("Nothing to import")
		return
	}
	overwriting := make(map[string]bool, len(overwrites))
	for _, name := range overwrites {
		overwriting[name] = true
	}
	printer.Print("\nPlan: %d server(s) to import\n\n", len(importable))
	for _, c := range importable {
		symbol, label := "+", "add"
		if overwriting[c.Name] {
			symbol, label = "~", "replace"
		}
		printer.Print("  %s mcp-server %q (%s, from %s)\n", symbol, c.Name, label, formatProvenance(c))
		for _, w := range c.Warnings {
			printer.Print("      warning: %s\n", w)
		}
	}
	printer.Print("\n")
}

// writeImportedServers appends the importable candidates through the shared
// locked write cycle (see stackwrite.go).
func writeImportedServers(stackPath string, importable []importer.Candidate, overwrites []string) (string, error) {
	servers := make([]config.MCPServer, 0, len(importable))
	for _, c := range importable {
		servers = append(servers, c.Server)
	}
	return writeServersToStack(stackPath, servers, overwrites)
}

// finishImport emits the JSON document when requested. Text mode has already
// printed its output incrementally.
func finishImport(_ *output.Printer, doc importDoc, format string, err error) error {
	if strings.EqualFold(format, "json") {
		if encodeErr := output.EncodeJSON(os.Stdout, doc); encodeErr != nil {
			fmt.Fprintln(os.Stderr, encodeErr)
			os.Exit(importExitInfrastructure)
		}
	}
	return err
}
