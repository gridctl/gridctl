package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/catalog"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

const addExitInfrastructure = 2

// addJSONSchemaVersion identifies the shape of the add JSON document.
const addJSONSchemaVersion = 1

var (
	addYes     bool
	addDryRun  bool
	addNoVault bool
	addFile    string
	addName    string
	addFormat  string
	addAsJSON  *bool
)

var addCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add an MCP server from the catalog to the stack",
	Long: `Resolves a catalog entry by name and appends the matching server block to
your stack.yaml: curated entries first ('gridctl add github'), then the
official MCP Registry by full reverse-DNS name
('gridctl add io.github.user/weather').

Required inputs are prompted for; secret values are masked and stored in
the variable store, so the stack file only ever carries ${var:KEY}
references. The stack file is backed up first and the post-add stack must
validate before a byte lands on disk. Registry entries are community
publications, not vetted by gridctl; review them before deploying.

Exit codes:
  0  added
  1  cancelled, unknown name, or the server was skipped (e.g. collision)
  2  infrastructure error (no stack file, parse or write failure,
     post-add validation failure)`,
	Example: `  gridctl add github                Add the curated GitHub server
  gridctl add postgres --dry-run    Preview without writing
  gridctl add fetch --yes           Non-interactive, defaults applied
  gridctl add io.github.user/tool   Add a registry server by full name`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		format, err := resolveFormat(addFormat, cmd.Flags().Changed("format"), *addAsJSON)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(addExitInfrastructure)
		}
		return runAdd(cmd.Context(), args[0], format)
	},
}

func init() {
	addCmd.Flags().BoolVarP(&addYes, "yes", "y", false, "Skip prompts: accept defaults, vault secrets, confirm the write")
	addCmd.Flags().BoolVar(&addDryRun, "dry-run", false, "Show what would be added without writing anything")
	addCmd.Flags().StringVarP(&addFile, "file", "f", "", "Stack file to append to (default: running stack's file, else ./stack.yaml)")
	addCmd.Flags().StringVarP(&addName, "name", "n", "", "Server name to use in the stack (default: the catalog name)")
	addCmd.Flags().BoolVar(&addNoVault, "no-vault", false, "Write secret inputs as literals instead of vault references (a warning is printed per secret)")
	addCmd.Flags().StringVar(&addFormat, "format", "", "Output format: 'json' for machine-readable output (default: text)")
	addAsJSON = addJSONAlias(addCmd)
}

// --- JSON document ---

type addServerDoc struct {
	Name        string            `json:"name"`
	CatalogName string            `json:"catalog_name"`
	Tier        string            `json:"tier"`
	Transport   string            `json:"transport,omitempty"`
	Added       bool              `json:"added"`
	Warnings    []string          `json:"warnings,omitempty"`
	Secrets     []importSecretDoc `json:"secrets,omitempty"`
	UnsetVars   []string          `json:"unset_vars,omitempty"`
}

type addDoc struct {
	SchemaVersion int          `json:"schema_version"`
	StackFile     string       `json:"stack_file"`
	BackupPath    string       `json:"backup_path,omitempty"`
	DryRun        bool         `json:"dry_run"`
	Server        addServerDoc `json:"server"`
}

// --- interactive seams ---

// addInputPrompter collects the entry's input values interactively. Secret
// values come back as literals; runAdd routes them to the vault.
var addInputPrompter = huhPromptEntryInputs

// addRegistryLookup resolves a name against the MCP Registry: the resolved
// entry (nil when not found) plus the search results the suggestion pass
// can draw names from.
var addRegistryLookup = defaultRegistryLookup

// ambiguousNameError reports a bare name matching several registry servers;
// unlike an outage it carries actionable guidance and surfaces directly.
type ambiguousNameError struct {
	arg   string
	names []string
}

func (e *ambiguousNameError) Error() string {
	return fmt.Sprintf("%q is ambiguous in the registry; use a full name: %s", e.arg, strings.Join(e.names, ", "))
}

func huhPromptEntryInputs(entry catalog.Entry) (map[string]string, error) {
	values := make(map[string]string, len(entry.Inputs))
	if len(entry.Inputs) == 0 {
		return values, nil
	}
	if !output.IsTerminal(os.Stdin) {
		return nil, fmt.Errorf("%q needs input values and stdin is not a terminal\nPass --yes to accept defaults (unset required values become ${var:KEY} references)", entry.Name)
	}

	holders := make([]*string, len(entry.Inputs))
	fields := make([]huh.Field, 0, len(entry.Inputs))
	for i, in := range entry.Inputs {
		v := in.Default
		holders[i] = &v
		title := in.Name
		if in.Required {
			title += " (required)"
		}
		if len(in.Choices) > 0 {
			options := make([]huh.Option[string], len(in.Choices))
			for j, c := range in.Choices {
				options[j] = huh.NewOption(c, c)
			}
			fields = append(fields, huh.NewSelect[string]().
				Title(title).Description(in.Description).Options(options...).Value(holders[i]))
			continue
		}
		input := huh.NewInput().Title(title).Description(in.Description).Value(holders[i])
		if in.Placeholder != "" {
			input = input.Placeholder(in.Placeholder)
		}
		if in.Secret {
			input = input.EchoMode(huh.EchoModePassword)
		}
		fields = append(fields, input)
	}

	form := huh.NewForm(huh.NewGroup(fields...)).WithAccessible(os.Getenv("ACCESSIBLE") != "")
	if !output.ColorEnabled(os.Stdout) {
		form = form.WithTheme(huh.ThemeBase())
	}
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, errPromptCancelled
		}
		return nil, err
	}
	for i, in := range entry.Inputs {
		values[in.Name] = strings.TrimSpace(*holders[i])
	}
	return values, nil
}

// --- command flow ---

func runAdd(ctx context.Context, arg, format string) error {
	// In JSON mode stdout carries exactly one document; narration moves to
	// stderr so pipelines can parse the output.
	printer := output.New()
	jsonMode := strings.EqualFold(format, "json")
	if jsonMode {
		printer = output.NewWithWriter(os.Stderr)
	}

	stackPath, source, err := resolveStackFileTarget(addFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(addExitInfrastructure)
	}
	existingNames, err := stackServerNames(source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parsing %s: %v\n", stackPath, err)
		os.Exit(addExitInfrastructure)
	}

	entry, err := resolveCatalogEntry(ctx, printer, arg)
	if err != nil {
		return err
	}
	if entry.Unsupported != "" {
		return &catalog.UnsupportedInstallError{Kind: entry.Unsupported}
	}

	interactive := !addYes && output.IsTerminal(os.Stdin)
	if entry.Status == catalog.StatusDeprecated {
		printer.Warn(fmt.Sprintf("%q is marked deprecated in the registry", entry.Name))
		if !addYes {
			if !interactive {
				return fmt.Errorf("refusing to add a deprecated server non-interactively; pass --yes to proceed")
			}
			ok, err := runConfirm(fmt.Sprintf("%q is marked deprecated in the registry. Add it anyway?", entry.Name))
			if err != nil {
				return err
			}
			if !ok {
				printer.Info("Add cancelled")
				return errPromptCancelled
			}
		}
	}

	target := addName
	if target == "" {
		target = entry.ServerName()
	}
	if target == "" {
		return fmt.Errorf("cannot derive a server name from %q; pass --name", entry.Name)
	}

	// Name collision against the stack: resolve interactively, refuse
	// otherwise (the documented exit-1 case).
	var overwrites []string
	var warnings []string
	if existingNames[target] {
		if !interactive {
			return fmt.Errorf("server %q already exists in %s; pass --name to add it under a different name", target, stackPath)
		}
		taken := func(name string) bool { return existingNames[name] }
		action, newName, err := importCollisionResolver(target, taken)
		if err != nil {
			return err
		}
		switch action {
		case "rename":
			warnings = append(warnings, fmt.Sprintf("added as %q (name collision)", newName))
			target = newName
		case "overwrite":
			overwrites = append(overwrites, target)
			warnings = append(warnings, "replaced the existing stack entry")
		default:
			printer.Info(fmt.Sprintf("Skipped %s (name collision)", target))
			return fmt.Errorf("nothing added: %q already exists in the stack", target)
		}
	}

	// Collect input values, then route them: secrets to the vault, unset
	// required values to ${var:KEY} placeholders.
	values := map[string]string{}
	if interactive {
		values, err = addInputPrompter(entry)
		if err != nil {
			return err
		}
	} else {
		for _, in := range entry.Inputs {
			values[in.Name] = in.Default
		}
	}
	resolved, secretDocs, unsetVars, err := routeInputValues(printer, entry, target, values)
	if err != nil {
		return err
	}

	server, mapWarnings, err := entry.Server(target, resolved)
	if err != nil {
		return err
	}
	warnings = append(warnings, mapWarnings...)

	doc := addDoc{
		SchemaVersion: addJSONSchemaVersion,
		StackFile:     stackPath,
		DryRun:        addDryRun,
		Server: addServerDoc{
			Name: target, CatalogName: entry.Name, Tier: entry.Tier,
			Transport: entry.Install.Transport, Warnings: warnings,
			Secrets: secretDocs, UnsetVars: unsetVars,
		},
	}

	// Plan block, in import's vocabulary.
	symbol, label := "+", "add"
	if len(overwrites) > 0 {
		symbol, label = "~", "replace"
	}
	printer.Print("\nPlan: 1 server to %s\n\n", label)
	printer.Print("  %s mcp-server %q (%s, %s)\n", symbol, target, entry.Tier, entry.InstallLabel())
	for _, w := range warnings {
		printer.Print("      warning: %s\n", w)
	}
	printer.Print("\n")

	if addDryRun {
		printer.Print("No changes made (dry run).\n")
		return finishAdd(doc, format, nil)
	}

	if err := warnRunningStack(printer, stackPath, addYes); err != nil {
		return err
	}
	if interactive {
		ok, err := runConfirm(fmt.Sprintf("Append %q to %s?", target, stackPath))
		if err != nil {
			return err
		}
		if !ok {
			printer.Info("Add cancelled")
			return finishAdd(doc, format, errPromptCancelled)
		}
	}

	backupPath, err := writeServersToStack(stackPath, []config.MCPServer{server}, overwrites)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(addExitInfrastructure)
	}
	doc.BackupPath = backupPath
	doc.Server.Added = true

	printer.Info(fmt.Sprintf("Added %s to %s", target, stackPath))
	if backupPath != "" {
		printer.Print("  Backup: %s\n", backupPath)
	}
	for _, key := range unsetVars {
		printer.Warn(fmt.Sprintf("%s: set %s before deploying: run 'gridctl var set %s'", target, key, key))
	}
	if entry.Install.Type == catalog.InstallURL && entry.Install.AuthType == "" {
		printer.Print("  If the server requires authorization, run 'gridctl auth login %s' after deploy.\n", target)
	}
	printer.Print("  First deploy pins the server's tool schemas; review with 'gridctl pins'.\n")
	printer.Print("  Run 'gridctl apply %s' to deploy.\n", stackPath)
	return finishAdd(doc, format, nil)
}

// resolveCatalogEntry resolves the argument: curated catalog first, then
// the MCP Registry, then a did-you-mean suggestion.
func resolveCatalogEntry(ctx context.Context, printer *output.Printer, arg string) (catalog.Entry, error) {
	if entry, ok := catalog.FindCurated(arg); ok {
		return entry, nil
	}

	entry, candidates, err := addRegistryLookup(ctx, arg)
	var ambiguous *ambiguousNameError
	if errors.As(err, &ambiguous) {
		return catalog.Entry{}, err
	}
	if err != nil && !errors.Is(err, catalog.ErrNotFound) {
		printer.Warn(fmt.Sprintf("MCP Registry unavailable (%v)", err))
	}
	if entry != nil {
		if !strings.EqualFold(entry.Name, arg) {
			printer.Info(fmt.Sprintf("Resolved %q to registry server %q", arg, entry.Name))
		}
		return *entry, nil
	}

	names := curatedNames()
	for _, c := range candidates {
		names = append(names, c.Name)
	}
	if suggestion := output.Suggest(arg, names); suggestion != "" {
		return catalog.Entry{}, fmt.Errorf("unknown server %q (did you mean %q?)", arg, suggestion)
	}
	return catalog.Entry{}, fmt.Errorf("unknown server %q; try 'gridctl search %s'", arg, arg)
}

// defaultRegistryLookup resolves a name against the MCP Registry. Names
// with a slash are full registry names and fetched directly; bare names go
// through search and must match exactly one result's short name.
func defaultRegistryLookup(ctx context.Context, arg string) (*catalog.Entry, []catalog.Entry, error) {
	client := catalog.NewClient()
	if strings.Contains(arg, "/") {
		entry, err := client.Get(ctx, arg)
		if err != nil {
			return nil, nil, err
		}
		return &entry, nil, nil
	}

	results, _, err := client.Search(ctx, arg)
	if err != nil {
		return nil, nil, err
	}
	var matches []catalog.Entry
	for _, e := range results {
		if strings.EqualFold(e.ServerName(), arg) || strings.EqualFold(e.Name, arg) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return nil, results, nil
	case 1:
		return &matches[0], results, nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return nil, results, &ambiguousNameError{arg: arg, names: names}
	}
}

// routeInputValues turns raw input values into the final strings the server
// block carries: secrets move to the vault as ${var:KEY} references, and
// unset required values become ${var:KEY} placeholders the user fills with
// 'gridctl var set'. A locked or unavailable store downgrades to literals
// with a warning, mirroring import.
func routeInputValues(printer *output.Printer, entry catalog.Entry, target string, values map[string]string) (map[string]string, []importSecretDoc, []string, error) {
	resolved := make(map[string]string, len(entry.Inputs))
	var secretDocs []importSecretDoc
	var unsetVars []string
	var store *vault.Store
	vaultDisabled := addNoVault

	for _, in := range entry.Inputs {
		v := strings.TrimSpace(values[in.Name])
		if v == "" {
			v = in.Default
		}
		// The env key stays the raw input name; ${var:KEY} references and
		// vault keys must fit the reference grammar (no dashes).
		varKey := catalog.VarKey(in.Name)
		switch {
		case v == "" && (in.Required || in.Auth):
			resolved[in.Name] = "${var:" + varKey + "}"
			unsetVars = append(unsetVars, varKey)
		case v == "":
			// Optional and unset: omitted from the server block.
		case !in.Secret:
			resolved[in.Name] = v
		default:
			if addDryRun {
				// Dry runs never touch the vault; report the intended action.
				resolved[in.Name] = "${var:" + varKey + "}"
				secretDocs = append(secretDocs, importSecretDoc{Key: in.Name, Action: "vaulted"})
				continue
			}
			if !vaultDisabled && store == nil {
				s, err := loadVault()
				if err == nil {
					err = ensureUnlocked(s)
				}
				if err != nil {
					printer.Warn(fmt.Sprintf("Variable store unavailable (%v); secrets will be written as literals", err))
					vaultDisabled = true
				} else {
					store = s
				}
			}
			if vaultDisabled {
				printer.Warn(fmt.Sprintf("%s: %s written as a literal secret; consider 'gridctl var set %s' and a ${var:%s} reference", target, in.Name, varKey, varKey))
				resolved[in.Name] = v
				secretDocs = append(secretDocs, importSecretDoc{Key: in.Name, Action: "kept_literal"})
				continue
			}
			storedKey, err := storeSecret(store, varKey, v)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("storing %s: %w", in.Name, err)
			}
			resolved[in.Name] = "${var:" + storedKey + "}"
			printer.Info(fmt.Sprintf("%s: %s stored in the vault as ${var:%s}", target, in.Name, storedKey))
			secretDocs = append(secretDocs, importSecretDoc{Key: in.Name, Action: "vaulted", Var: storedKey})
		}
	}
	return resolved, secretDocs, unsetVars, nil
}

// curatedNames lists curated entry names for suggestions.
func curatedNames() []string {
	entries, err := catalog.Curated()
	if err != nil {
		return nil
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

// finishAdd emits the JSON document when requested. Text mode has already
// printed its output incrementally.
func finishAdd(doc addDoc, format string, err error) error {
	if strings.EqualFold(format, "json") {
		if encodeErr := output.EncodeJSON(os.Stdout, doc); encodeErr != nil {
			fmt.Fprintln(os.Stderr, encodeErr)
			os.Exit(addExitInfrastructure)
		}
	}
	return err
}
