package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/internal/importer"
	"github.com/gridctl/gridctl/internal/stackedit"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Exit codes match the pins/optimize/validate contract: 0 success, 1 via a
// returned error (cancelled, or nothing imported from a non-empty selection),
// and 2 for infrastructure failures below.
const importExitInfrastructure = 2

// importJSONSchemaVersion identifies the shape of the import JSON document.
const importJSONSchemaVersion = 1

var (
	importAll     bool
	importDryRun  bool
	importYes     bool
	importName    string
	importFile    string
	importNoVault bool
	importFormat  string
	importAsJSON  *bool
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

Without arguments, all detected clients are scanned and servers are picked
interactively. Run 'gridctl link --help' for the supported client list.

Exit codes:
  0  imported (or nothing to import)
  1  cancelled, or every selected server failed
  2  infrastructure error (no stack file, parse or write failure,
     post-import validation failure)`,
	Example: `  gridctl import                    Scan all clients, pick interactively
  gridctl import cursor             Import from Cursor only
  gridctl import --all --dry-run    Preview everything without writing
  gridctl import --all --yes        Import everything, defaults applied`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(importFormat, cmd.Flags().Changed("format"), *importAsJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(importExitInfrastructure)
		}
		client := ""
		if len(args) == 1 {
			client = args[0]
		}
		return runImport(client, format)
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
	importAsJSON = addJSONAlias(importCmd)
}

// --- JSON document ---

type importSecretDoc struct {
	Key    string `json:"key"`
	Action string `json:"action"` // "vaulted" or "kept_literal"
	Var    string `json:"var,omitempty"`
}

type importServerDoc struct {
	Name       string            `json:"name"`
	Imported   bool              `json:"imported"`
	FoundIn    []string          `json:"found_in,omitempty"`
	Source     string            `json:"source,omitempty"`
	SkipReason string            `json:"skip_reason,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
	Secrets    []importSecretDoc `json:"secrets,omitempty"`
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
		label := fmt.Sprintf("%-24s from %s", c.Name, strings.Join(c.FoundIn, ", "))
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

func runImport(client, format string) error {
	// In JSON mode stdout carries exactly one document; narration moves to
	// stderr so pipelines can parse the output.
	printer := output.New()
	if strings.EqualFold(format, "json") {
		printer = output.NewWithWriter(os.Stderr)
	}
	registry := provisioner.NewRegistry()

	scope, err := importScope(registry, client)
	if err != nil {
		return err
	}
	if len(scope) == 0 {
		printer.Info("No supported LLM clients detected")
		printer.Print("Run 'gridctl link --help' for the supported client list.\n")
		return nil
	}

	stackPath, source, err := resolveImportStackFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(importExitInfrastructure)
	}
	existingNames, err := stackServerNames(source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parsing %s: %v\n", stackPath, err)
		os.Exit(importExitInfrastructure)
	}

	candidates, skipped := scanForCandidates(printer, scope)
	doc := importDoc{
		SchemaVersion: importJSONSchemaVersion,
		StackFile:     stackPath,
		DryRun:        importDryRun,
	}
	for _, s := range skipped {
		doc.Servers = append(doc.Servers, importServerDoc{
			Name: s.Name, FoundIn: s.FoundIn, Source: s.Source,
			SkipReason: s.SkipReason, Warnings: s.Warnings,
		})
	}
	doc.Summary.Found = len(candidates) + len(skipped)
	doc.Summary.Skipped = len(skipped)

	if len(candidates) == 0 {
		printer.Info("No importable servers found")
		return finishImport(printer, doc, format, nil)
	}

	selected, err := selectCandidates(candidates)
	if err != nil {
		return err
	}
	if len(selected) == 0 {
		printer.Info("No servers selected")
		return finishImport(printer, doc, format, nil)
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
			continue
		}
		action, newName, err := importCollisionResolver(selected[i].Name, taken)
		if err != nil {
			return err
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
			continue
		}
		seenNames[selected[i].Name] = strings.Join(selected[i].FoundIn, ", ")
	}

	// Secret handling. References were never classified as secrets; what is
	// left is literal values under secret-suggestive keys.
	secretDocs := make(map[string][]importSecretDoc)
	if !importDryRun {
		if err := vaultSelectedSecrets(printer, selected, secretDocs); err != nil {
			return err
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
			doc.Servers = append(doc.Servers, importServerDoc{
				Name: c.Name, FoundIn: c.FoundIn, Source: c.Source,
				SkipReason: c.SkipReason, Warnings: c.Warnings,
			})
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
	if len(importable) == 0 {
		// A non-empty selection that produced zero imports is the documented
		// exit-1 case: the user asked for servers and got none.
		return finishImport(printer, doc, format,
			errors.New("nothing imported: every selected server was skipped (see skip reasons above)"))
	}

	if importDryRun {
		for _, c := range importable {
			doc.Servers = append(doc.Servers, importServerDoc{
				Name: c.Name, Imported: false, FoundIn: c.FoundIn, Source: c.Source,
				Warnings: c.Warnings, Secrets: secretDocs[c.Name],
			})
		}
		printer.Print("\nNo changes made (dry run).\n")
		return finishImport(printer, doc, format, nil)
	}

	if err := warnRunningStack(printer, stackPath); err != nil {
		return err
	}
	if interactive {
		ok, err := importWriteConfirm(fmt.Sprintf("Append %d server(s) to %s?", len(importable), stackPath))
		if err != nil {
			return err
		}
		if !ok {
			printer.Info("Import cancelled")
			return finishImport(printer, doc, format, nil)
		}
	}

	backupPath, err := writeImportedServers(stackPath, importable, overwrites)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(importExitInfrastructure)
	}
	doc.BackupPath = backupPath

	for _, c := range importable {
		doc.Servers = append(doc.Servers, importServerDoc{
			Name: c.Name, Imported: true, FoundIn: c.FoundIn, Source: c.Source,
			Warnings: c.Warnings, Secrets: secretDocs[c.Name],
		})
		doc.Summary.Imported++
		printer.Info(fmt.Sprintf("Imported %s (from %s)", c.Name, strings.Join(c.FoundIn, ", ")))
	}
	if backupPath != "" {
		printer.Print("  Backup: %s\n", backupPath)
	}
	printer.Print("  Run 'gridctl apply %s' to deploy the imported servers.\n", stackPath)
	return finishImport(printer, doc, format, nil)
}

// importScope resolves which detected clients to scan.
func importScope(registry *provisioner.Registry, client string) ([]provisioner.DetectedClient, error) {
	if client == "" {
		return registry.DetectAll(), nil
	}
	prov, ok := registry.FindBySlug(client)
	if !ok {
		return nil, unknownClientError(registry, client)
	}
	configPath, found := prov.Detect()
	if !found {
		return nil, provisioner.ErrClientNotFound
	}
	return []provisioner.DetectedClient{{Provisioner: prov, ConfigPath: configPath}}, nil
}

// scanForCandidates lists, filters, maps, and dedupes servers across the
// scanned clients. Unparseable configs warn and are skipped; the scan never
// aborts because one client's file is broken.
func scanForCandidates(printer *output.Printer, scope []provisioner.DetectedClient) (importable, skipped []importer.Candidate) {
	var all []importer.Candidate
	seenSkip := make(map[string]bool)
	for _, dc := range scope {
		slug := dc.Provisioner.Slug()
		entries, err := dc.Provisioner.ListServers(dc.ConfigPath)
		if err != nil {
			printer.Warn(fmt.Sprintf("Skipping %s: cannot parse %s (%v)", dc.Provisioner.Name(), dc.ConfigPath, err))
			continue
		}
		for _, entry := range entries {
			if importer.IsGatewaySelfEntry(entry.Name, importName, entry.Raw) {
				key := entry.Name + "|self"
				if !seenSkip[key] {
					seenSkip[key] = true
					skipped = append(skipped, importer.Candidate{
						Name: entry.Name, FoundIn: []string{slug}, Source: slug,
						SkipReason: importer.SkipGatewaySelfEntry,
					})
				}
				continue
			}
			server, warnings, err := importer.MapEntry(slug, entry)
			if err != nil {
				key := entry.Name + "|unsupported"
				if !seenSkip[key] {
					seenSkip[key] = true
					skipped = append(skipped, importer.Candidate{
						Name: entry.Name, FoundIn: []string{slug}, Source: slug,
						SkipReason: importer.SkipUnsupported,
						Warnings:   []string{err.Error()},
					})
				}
				continue
			}
			all = append(all, importer.Candidate{
				Name: server.Name, Server: server,
				FoundIn: []string{slug}, Source: slug,
				Warnings:   warnings,
				SecretKeys: importer.ClassifySecretKeys(server.Env),
			})
		}
	}
	return importer.Dedupe(all), skipped
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
		printer.Print("  %s mcp-server %q (%s, from %s)\n", symbol, c.Name, label, strings.Join(c.FoundIn, ", "))
		for _, w := range c.Warnings {
			printer.Print("      warning: %s\n", w)
		}
	}
	printer.Print("\n")
}

// warnRunningStack confirms before writing to a stack a running daemon may
// hot-apply. Skipped under --yes; refuses in non-interactive runs without it.
func warnRunningStack(printer *output.Printer, stackPath string) error {
	if importYes {
		return nil
	}
	abs, err := filepath.Abs(stackPath)
	if err != nil {
		return nil
	}
	states, err := state.List()
	if err != nil {
		return nil
	}
	for _, s := range states {
		if !state.IsRunning(&s) || s.StackFile != abs {
			continue
		}
		printer.Warn(fmt.Sprintf("Stack %q is running; a watched daemon applies imported servers as soon as the file is saved", s.StackName))
		if !output.IsTerminal(os.Stdin) {
			return fmt.Errorf("refusing to modify a running stack non-interactively; pass --yes to proceed")
		}
		return nil
	}
	return nil
}

// writeImportedServers performs the locked read-verify-write cycle: backup,
// optional overwrite removals, append, validate the post-append stack, and
// atomically replace the file. Nothing is written when validation fails.
func writeImportedServers(stackPath string, importable []importer.Candidate, overwrites []string) (string, error) {
	mu := stackedit.PathLock(stackPath)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(stackPath)
	if err != nil {
		return "", fmt.Errorf("read stack file: %w", err)
	}
	originalHash := sha256.Sum256(original)

	updated := original
	for _, name := range overwrites {
		if updated, err = stackedit.RemoveResourceByName(updated, "mcp-servers", name); err != nil {
			return "", err
		}
	}

	snippets := make([][]byte, 0, len(importable))
	for _, c := range importable {
		snippet, err := yaml.Marshal(c.Server)
		if err != nil {
			return "", fmt.Errorf("marshal server %s: %w", c.Name, err)
		}
		snippets = append(snippets, snippet)
	}
	if updated, err = stackedit.AppendResources(updated, "mcp-servers", snippets...); err != nil {
		return "", err
	}

	// Article IX gate: the post-append stack must validate before a byte
	// lands on disk.
	var stack config.Stack
	if err := yaml.Unmarshal(updated, &stack); err != nil {
		return "", fmt.Errorf("post-import stack does not parse: %w", err)
	}
	config.ExpandStackVarsWithEnv(&stack)
	stack.SetDefaults()
	if result := config.ValidateWithIssues(&stack); !result.Valid {
		var lines []string
		for _, issue := range result.Issues {
			lines = append(lines, fmt.Sprintf("%s: %s", issue.Field, issue.Message))
		}
		return "", fmt.Errorf("post-import stack fails validation; nothing written:\n  %s", strings.Join(lines, "\n  "))
	}

	current, err := os.ReadFile(stackPath)
	if err != nil {
		return "", fmt.Errorf("re-read stack file: %w", err)
	}
	if sha256.Sum256(current) != originalHash {
		return "", fmt.Errorf("stack file changed on disk during the import; re-run to work from the current contents")
	}

	backupPath, err := provisioner.CreateBackup(stackPath)
	if err != nil {
		return "", fmt.Errorf("backing up stack file: %w", err)
	}
	if err := stackedit.AtomicWrite(stackPath, updated); err != nil {
		return "", err
	}
	return backupPath, nil
}

// resolveImportStackFile locates the target stack file: --file, then the
// single running stack's recorded file, then ./stack.yaml.
func resolveImportStackFile() (string, []byte, error) {
	tryRead := func(path string) (string, []byte, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("stack file %s: %w", path, err)
		}
		return path, data, nil
	}
	if importFile != "" {
		return tryRead(importFile)
	}
	if states, err := state.List(); err == nil {
		var running []state.DaemonState
		for _, s := range states {
			if state.IsRunning(&s) && s.StackFile != "" {
				running = append(running, s)
			}
		}
		if len(running) == 1 {
			return tryRead(running[0].StackFile)
		}
		if len(running) > 1 {
			return "", nil, fmt.Errorf("multiple running stacks; pass --file to pick the stack file to import into")
		}
	}
	if _, err := os.Stat("stack.yaml"); err == nil {
		return tryRead("stack.yaml")
	}
	return "", nil, fmt.Errorf("no stack file found: pass --file, run inside a directory with stack.yaml, or deploy a stack first")
}

// stackServerNames extracts existing server names without full config
// loading, so collision checks work even when the stack references vars
// that are unset in this shell.
func stackServerNames(source []byte) (map[string]bool, error) {
	var doc struct {
		Servers []struct {
			Name string `yaml:"name"`
		} `yaml:"mcp-servers"`
	}
	if err := yaml.Unmarshal(source, &doc); err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(doc.Servers))
	for _, s := range doc.Servers {
		if s.Name != "" {
			names[s.Name] = true
		}
	}
	return names, nil
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
