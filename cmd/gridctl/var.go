package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var varCmd = &cobra.Command{
	Use:   "var",
	Short: "Manage variables (secrets and configuration) in the gridctl store",
	Long: `Store and manage variables — both secrets and non-sensitive configuration —
in the gridctl store. Reference them from stack YAML using ${var:KEY}.

Secrets are encrypted at rest (when the vault is locked) and redacted in logs.
Plaintext variables are visible in logs and the web UI.`,
	// Subcommands handle their own errors; cobra's default of printing usage
	// after every RunE error would drown out validation messages.
	SilenceUsage: true,
}

// Flags for `var set`.
var (
	varSetValue     string
	varSetSetName   string
	varSetSecret    bool
	varSetPlaintext bool
	varSetType      string
)

// Flags shared with the other var subcommands.
var (
	varGetPlain    bool
	varDeleteForce bool
	varListFmt     string
	varImportFmt   string
	varExportFmt   string
	varExportPlain bool
)

var varSetCmd = &cobra.Command{
	Use:   "set <KEY>",
	Short: "Store a variable",
	Long: `Store a variable. Without --value, prompts for interactive input.

By default variables are stored as secrets (Article XII: secure defaults).
Use --plaintext for non-sensitive configuration that should appear unredacted
in logs (e.g. REGION, CLUSTER_ID).

Use --type to validate and tag the value's shape: string (default), json,
list, number, or bool.`,
	Example: `  gridctl var set GITHUB_TOKEN             Prompt for a secret value
  gridctl var set REGION --value us-east-1 --plaintext
  gridctl var set FEATURES --type list --value "a,b,c"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarSet(args[0])
	},
}

var varGetCmd = &cobra.Command{
	Use:   "get <KEY>",
	Short: "Retrieve a variable",
	Long: `Retrieve a variable from the store. Secrets are masked by default; use
--plain to show the actual value. Plaintext variables are always shown
unmasked.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarGet(args[0])
	},
}

var varListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all variables",
	Long:  "List all variables with type and visibility. Values are never shown.",
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if varListFmt, err = resolveFormat(varListFmt, cmd.Flags().Changed("format"), *varListJSON); err != nil {
			return err
		}
		return runVarList()
	},
}

var varListJSON *bool

var varDeleteCmd = &cobra.Command{
	Use:   "delete <KEY>",
	Short: "Delete a variable",
	Long:  "Remove a variable from the store.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarDelete(args[0])
	},
}

var varImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import variables from a file",
	Long: `Import variables from a .env or .json file into the store.

The .env flavor accepts leading comment markers immediately preceding a
KEY=VALUE line to tag the variable:

    # @public            — store as plaintext (default is secret)
    # @type=list         — record the variable's type
    TAGS=app,backend,prod

The .json flavor accepts either the legacy {"KEY": "value"} map (everything
imports as secret/string) or the v2 shape {"variables": [...]}.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarImport(args[0])
	},
}

var varExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export variables",
	Long: `Export all variables. Values are masked by default; use --plain to show
actual values. The .env flavor includes # @public / # @type markers when
needed; the .json flavor writes the v2 {"variables": [...]} shape.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarExport()
	},
}

var varSetsCmd = &cobra.Command{
	Use:   "sets",
	Short: "Manage variable sets",
	Long:  "List, create, or delete variable sets that group related variables.",
}

var varSetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List variable sets",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarSetsList()
	},
}

var varSetsCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a variable set",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarSetsCreate(args[0])
	},
}

var varSetsDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a variable set",
	Long:  "Delete a variable set. Variables in the set are unassigned but not deleted.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarSetsDelete(args[0])
	},
}

var varLockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Encrypt the variable store with a passphrase",
	Long:  "Encrypt all variables at rest using envelope encryption (XChaCha20-Poly1305 + Argon2id).",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarLock()
	},
}

var varUnlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Decrypt the variable store for this session",
	Long:  "Unlock an encrypted store by providing the passphrase. Variables are decrypted into memory.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarUnlock()
	},
}

var varChangePassphraseCmd = &cobra.Command{
	Use:   "change-passphrase",
	Short: "Change the store passphrase",
	Long:  "Change the encryption passphrase. Only re-encrypts the key envelope — variable data is unchanged.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVarChangePassphrase()
	},
}

func init() {
	varSetCmd.Flags().StringVar(&varSetValue, "value", "", "Variable value (non-interactive)")
	varSetCmd.Flags().StringVar(&varSetSetName, "set", "", "Assign variable to a variable set")
	varSetCmd.Flags().BoolVar(&varSetSecret, "secret", false, "Mark as secret (default; redacted in logs)")
	varSetCmd.Flags().BoolVar(&varSetPlaintext, "plaintext", false, "Mark as plaintext (visible in logs)")
	varSetCmd.Flags().StringVar(&varSetType, "type", "string", "Value type: string, json, list, number, bool")
	varGetCmd.Flags().BoolVar(&varGetPlain, "plain", false, "Show unmasked value")
	varDeleteCmd.Flags().BoolVar(&varDeleteForce, "force", false, "Skip confirmation")
	varListCmd.Flags().StringVar(&varListFmt, "format", "table", "Output format (table, json)")
	varListJSON = addJSONAlias(varListCmd)
	varImportCmd.Flags().StringVar(&varImportFmt, "format", "", "File format (env, json). Auto-detected if omitted")
	varExportCmd.Flags().StringVar(&varExportFmt, "format", "env", "Export format (env, json)")
	varExportCmd.Flags().BoolVar(&varExportPlain, "plain", false, "Show unmasked values")

	varSetsCmd.AddCommand(varSetsListCmd)
	varSetsCmd.AddCommand(varSetsCreateCmd)
	varSetsCmd.AddCommand(varSetsDeleteCmd)

	varCmd.AddCommand(varSetCmd)
	varCmd.AddCommand(varGetCmd)
	varCmd.AddCommand(varListCmd)
	varCmd.AddCommand(varDeleteCmd)
	varCmd.AddCommand(varImportCmd)
	varCmd.AddCommand(varExportCmd)
	varCmd.AddCommand(varSetsCmd)
	varCmd.AddCommand(varLockCmd)
	varCmd.AddCommand(varUnlockCmd)
	varCmd.AddCommand(varChangePassphraseCmd)
}

// validateAndNormalize checks that value satisfies the given type and returns
// the on-disk normalized form. Returns an error naming the type and the
// offending input.
//   - string:  no validation
//   - json:    must parse as JSON
//   - list:    comma-separated input → JSON-encoded []string
//   - number:  must parse as float
//   - bool:    must parse as bool
func validateAndNormalize(typeName, value string) (string, error) {
	switch vault.VariableType(typeName) {
	case "", vault.TypeString:
		return value, nil
	case vault.TypeJSON:
		var probe any
		if err := json.Unmarshal([]byte(value), &probe); err != nil {
			return "", fmt.Errorf("invalid value for type=json: %q (%v)", value, err)
		}
		return value, nil
	case vault.TypeList:
		parts := strings.Split(value, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		b, err := json.Marshal(out)
		if err != nil {
			return "", fmt.Errorf("encoding list: %w", err)
		}
		return string(b), nil
	case vault.TypeNumber:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return "", fmt.Errorf("invalid value for type=number: %q", value)
		}
		return value, nil
	case vault.TypeBool:
		if _, err := strconv.ParseBool(value); err != nil {
			return "", fmt.Errorf("invalid value for type=bool: %q", value)
		}
		return value, nil
	default:
		return "", fmt.Errorf("unsupported type: %q (allowed: string, json, list, number, bool)", typeName)
	}
}

func runVarSet(key string) error {
	if varSetSecret && varSetPlaintext {
		return fmt.Errorf("--secret and --plaintext are mutually exclusive")
	}

	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	value := varSetValue
	if value == "" {
		// Interactive: read from terminal or stdin
		if isatty.IsTerminal(os.Stdin.Fd()) {
			fmt.Printf("Enter value for %s: ", key)
			raw, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Println() // newline after hidden input
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
			value = string(raw)
		} else {
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				value = scanner.Text()
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
		}
	}

	normalized, err := validateAndNormalize(varSetType, value)
	if err != nil {
		return err
	}

	// Default to secret (Article XII); --plaintext flips, --secret is the
	// explicit secure-default and a no-op.
	isSecret := !varSetPlaintext

	v := vault.Variable{
		Key:      key,
		Value:    normalized,
		Set:      varSetSetName,
		Type:     vault.VariableType(varSetType),
		IsSecret: isSecret,
	}
	if v.Type == "" {
		v.Type = vault.TypeString
	}

	if err := store.SetVariable(v); err != nil {
		return err
	}

	printer := output.New()
	visibility := "secret"
	if !isSecret {
		visibility = "plaintext"
	}
	attrs := []any{"key", key, "type", string(v.Type), "visibility", visibility}
	if varSetSetName != "" {
		attrs = append(attrs, "set", varSetSetName)
	}
	printer.Info("Variable stored", attrs...)
	return nil
}

func runVarGet(key string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	v, ok := store.GetVariable(key)
	if !ok {
		return fmt.Errorf("variable %q not found", key)
	}

	visibility := "secret"
	if !v.IsSecret {
		visibility = "plaintext"
	}

	// --plain or plaintext variable: show raw value alone (legacy --plain
	// behaviour for tooling pipes the raw value out).
	if varGetPlain || !v.IsSecret {
		fmt.Println(v.Value)
		return nil
	}

	fmt.Printf("%s = %s  (type: %s, %s)\n", key, maskValue(v.Value), v.Type, visibility)
	return nil
}

func runVarList() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	vars := store.List()

	if varListFmt == "json" {
		type entry struct {
			Key        string `json:"key"`
			Type       string `json:"type"`
			Visibility string `json:"visibility"`
			Set        string `json:"set,omitempty"`
		}
		out := make([]entry, 0, len(vars))
		for _, v := range vars {
			vis := "secret"
			if !v.IsSecret {
				vis = "plaintext"
			}
			out = append(out, entry{Key: v.Key, Type: string(v.Type), Visibility: vis, Set: v.Set})
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if len(vars) == 0 {
		fmt.Println("No variables stored")
		return nil
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"Key", "Type", "Visibility", "Set"})
	for _, v := range vars {
		visibility := "secret"
		if !v.IsSecret {
			visibility = "plaintext"
		}
		t.AppendRow(table.Row{v.Key, v.Type, visibility, v.Set})
	}
	t.Render()
	return nil
}

func runVarDelete(key string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	if !store.Has(key) {
		return fmt.Errorf("variable %q not found", key)
	}

	if !varDeleteForce {
		fmt.Printf("Delete %s? [y/N] ", key)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Cancelled")
			return nil
		}
	}

	if err := store.Delete(key); err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Variable deleted", "key", key)
	return nil
}

func runVarImport(file string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	vars, err := parseVariablesFile(file, varImportFmt)
	if err != nil {
		return err
	}

	count, err := store.ImportVariables(vars)
	if err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Imported variables", "count", count, "file", file)
	return nil
}

func runVarExport() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	vars := store.List()
	if len(vars) == 0 {
		fmt.Println("No variables stored")
		return nil
	}

	switch varExportFmt {
	case "json":
		type entry struct {
			Key      string `json:"key"`
			Value    string `json:"value"`
			Type     string `json:"type"`
			IsSecret bool   `json:"is_secret"`
			Set      string `json:"set,omitempty"`
		}
		out := struct {
			Variables []entry `json:"variables"`
		}{Variables: make([]entry, 0, len(vars))}
		for _, v := range vars {
			val := v.Value
			if !varExportPlain && v.IsSecret {
				val = maskValue(val)
			}
			out.Variables = append(out.Variables, entry{
				Key:      v.Key,
				Value:    val,
				Type:     string(v.Type),
				IsSecret: v.IsSecret,
				Set:      v.Set,
			})
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	default: // env
		for _, v := range vars {
			// Markers above the KEY=VALUE line only when they differ from
			// the .env defaults (secret/string) so existing .env consumers
			// can keep using vanilla parsers.
			if !v.IsSecret {
				fmt.Println("# @public")
			}
			if v.Type != "" && v.Type != vault.TypeString {
				fmt.Printf("# @type=%s\n", v.Type)
			}
			val := v.Value
			if !varExportPlain && v.IsSecret {
				val = maskValue(val)
			}
			fmt.Printf("%s=%s\n", v.Key, val)
		}
	}
	return nil
}

func runVarSetsList() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	sets := store.ListSets()
	if len(sets) == 0 {
		fmt.Println("No variable sets defined")
		return nil
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"Set", "Variables"})
	for _, s := range sets {
		t.AppendRow(table.Row{s.Name, s.Count})
	}
	t.Render()
	return nil
}

func runVarSetsCreate(name string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	if err := store.CreateSet(name); err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Variable set created", "name", name)
	return nil
}

func runVarSetsDelete(name string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	if err := store.DeleteSet(name); err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Variable set deleted", "name", name)
	return nil
}

func runVarLock() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if store.IsLocked() {
		if err := ensureUnlocked(store); err != nil {
			return err
		}
	}

	pass, err := promptPassphraseConfirm()
	if err != nil {
		return err
	}

	if err := store.Lock(pass); err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Variable store locked", "encryption", "XChaCha20-Poly1305", "kdf", "Argon2id")
	return nil
}

func runVarUnlock() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if !store.IsLocked() {
		fmt.Println("Variable store is already unlocked")
		return nil
	}

	pass, err := promptPassphrase("Vault passphrase: ")
	if err != nil {
		return err
	}

	if err := store.Unlock(pass); err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Variable store unlocked", "variables", len(store.List()))
	return nil
}

func runVarChangePassphrase() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if !store.IsEncrypted() {
		return fmt.Errorf("variable store is not encrypted. Run 'gridctl var lock' first")
	}

	oldPass, err := promptPassphrase("Current passphrase: ")
	if err != nil {
		return err
	}

	newPass, err := promptPassphraseConfirm()
	if err != nil {
		return err
	}

	if err := store.ChangePassphrase(oldPass, newPass); err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Passphrase changed")
	return nil
}

// parseVariablesFile reads a .env or .json file and returns variables with
// metadata threaded through where the source supports it.
func parseVariablesFile(path, format string) ([]vault.Variable, error) {
	if format == "" {
		if strings.HasSuffix(path, ".json") {
			format = "json"
		} else {
			format = "env"
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	switch format {
	case "json":
		return parseVariablesJSON(data)
	case "env":
		return parseVariablesEnv(string(data))
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// parseVariablesJSON accepts either the legacy map shape (everything
// imports as secret/string) or the new {variables: [...]} shape.
func parseVariablesJSON(data []byte) ([]vault.Variable, error) {
	// Try the v2 shape first.
	var typed struct {
		Variables []vault.Variable `json:"variables"`
	}
	if err := json.Unmarshal(data, &typed); err == nil && len(typed.Variables) > 0 {
		for i := range typed.Variables {
			if typed.Variables[i].Type == "" {
				typed.Variables[i].Type = vault.TypeString
			}
			if !vault.IsValidType(typed.Variables[i].Type) {
				return nil, fmt.Errorf("invalid type %q for key %q",
					typed.Variables[i].Type, typed.Variables[i].Key)
			}
		}
		return typed.Variables, nil
	}

	// Fall back to legacy map.
	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	out := make([]vault.Variable, 0, len(legacy))
	for k, v := range legacy {
		out = append(out, vault.Variable{
			Key: k, Value: v, Type: vault.TypeString, IsSecret: true,
		})
	}
	return out, nil
}

// parseVariablesEnv parses KEY=VALUE lines with optional metadata markers.
// A run of `# @type=…` or `# @public` comments immediately preceding a
// KEY=VALUE line applies to that variable; markers are cleared at blank
// lines or after they are consumed by a value line.
func parseVariablesEnv(content string) ([]vault.Variable, error) {
	var out []vault.Variable
	pendingType := vault.TypeString
	pendingIsSecret := true
	resetMarkers := func() {
		pendingType = vault.TypeString
		pendingIsSecret = true
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)

		if line == "" {
			resetMarkers()
			continue
		}

		if strings.HasPrefix(line, "#") {
			// Look for our markers; ignore other comments without resetting
			// (so a "# section header" between markers doesn't break them).
			marker := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			switch {
			case marker == "@public":
				pendingIsSecret = false
			case strings.HasPrefix(marker, "@type="):
				typeVal := vault.VariableType(strings.TrimPrefix(marker, "@type="))
				if !vault.IsValidType(typeVal) {
					return nil, fmt.Errorf("invalid @type marker: %q", typeVal)
				}
				pendingType = typeVal
			}
			continue
		}

		// Strip optional "export " prefix
		line = strings.TrimPrefix(line, "export ")

		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		if key == "" {
			continue
		}

		out = append(out, vault.Variable{
			Key:      key,
			Value:    value,
			Type:     pendingType,
			IsSecret: pendingIsSecret,
		})
		resetMarkers()
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no valid KEY=VALUE pairs found")
	}
	return out, nil
}
