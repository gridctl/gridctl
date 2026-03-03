package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var vaultCmd = &cobra.Command{
	Use:   "vault",
	Short: "Manage secrets in the gridctl vault",
	Long:  "Store and manage secrets that can be referenced in stack YAML files using ${vault:KEY} syntax.",
}

// Flags
var (
	vaultSetValue    string
	vaultSetSetName  string
	vaultGetPlain    bool
	vaultDeleteForce bool
	vaultImportFmt   string
	vaultExportFmt   string
	vaultExportPlain bool
)

var vaultSetCmd = &cobra.Command{
	Use:   "set <KEY>",
	Short: "Store a secret",
	Long:  "Store a secret in the vault. Without --value, prompts for interactive input.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultSet(args[0])
	},
}

var vaultGetCmd = &cobra.Command{
	Use:   "get <KEY>",
	Short: "Retrieve a secret",
	Long:  "Retrieve a secret from the vault. Values are masked by default; use --plain to show the actual value.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultGet(args[0])
	},
}

var vaultListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all secret keys",
	Long:  "List all keys stored in the vault. Values are never shown.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultList()
	},
}

var vaultDeleteCmd = &cobra.Command{
	Use:   "delete <KEY>",
	Short: "Delete a secret",
	Long:  "Remove a secret from the vault.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultDelete(args[0])
	},
}

var vaultImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import secrets from a file",
	Long:  "Import secrets from a .env or .json file into the vault.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultImport(args[0])
	},
}

var vaultExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export secrets",
	Long:  "Export all vault secrets. Values are masked by default; use --plain to show actual values.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultExport()
	},
}

var vaultSetsCmd = &cobra.Command{
	Use:   "sets",
	Short: "Manage variable sets",
	Long:  "List, create, or delete variable sets that group related secrets.",
}

var vaultSetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List variable sets",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultSetsList()
	},
}

var vaultSetsCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a variable set",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultSetsCreate(args[0])
	},
}

var vaultSetsDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a variable set",
	Long:  "Delete a variable set. Secrets in the set are unassigned but not deleted.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultSetsDelete(args[0])
	},
}

var vaultLockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Encrypt the vault with a passphrase",
	Long:  "Encrypt all vault secrets at rest using envelope encryption (XChaCha20-Poly1305 + Argon2id).",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultLock()
	},
}

var vaultUnlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Decrypt the vault for this session",
	Long:  "Unlock an encrypted vault by providing the passphrase. Secrets are decrypted into memory.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultUnlock()
	},
}

var vaultChangePassphraseCmd = &cobra.Command{
	Use:   "change-passphrase",
	Short: "Change the vault passphrase",
	Long:  "Change the vault encryption passphrase. Only re-encrypts the key envelope — secrets data is unchanged.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVaultChangePassphrase()
	},
}

func init() {
	vaultSetCmd.Flags().StringVar(&vaultSetValue, "value", "", "Secret value (non-interactive)")
	vaultSetCmd.Flags().StringVar(&vaultSetSetName, "set", "", "Assign secret to a variable set")
	vaultGetCmd.Flags().BoolVar(&vaultGetPlain, "plain", false, "Show unmasked value")
	vaultDeleteCmd.Flags().BoolVar(&vaultDeleteForce, "force", false, "Skip confirmation")
	vaultImportCmd.Flags().StringVar(&vaultImportFmt, "format", "", "File format (env, json). Auto-detected if omitted")
	vaultExportCmd.Flags().StringVar(&vaultExportFmt, "format", "env", "Export format (env, json)")
	vaultExportCmd.Flags().BoolVar(&vaultExportPlain, "plain", false, "Show unmasked values")

	vaultSetsCmd.AddCommand(vaultSetsListCmd)
	vaultSetsCmd.AddCommand(vaultSetsCreateCmd)
	vaultSetsCmd.AddCommand(vaultSetsDeleteCmd)

	vaultCmd.AddCommand(vaultSetCmd)
	vaultCmd.AddCommand(vaultGetCmd)
	vaultCmd.AddCommand(vaultListCmd)
	vaultCmd.AddCommand(vaultDeleteCmd)
	vaultCmd.AddCommand(vaultImportCmd)
	vaultCmd.AddCommand(vaultExportCmd)
	vaultCmd.AddCommand(vaultSetsCmd)
	vaultCmd.AddCommand(vaultLockCmd)
	vaultCmd.AddCommand(vaultUnlockCmd)
	vaultCmd.AddCommand(vaultChangePassphraseCmd)
}

func loadVault() (*vault.Store, error) {
	store := vault.NewStore(state.VaultDir())
	if err := store.Load(); err != nil {
		return nil, fmt.Errorf("loading vault: %w", err)
	}
	return store, nil
}

// ensureUnlocked prompts for the passphrase if the vault is locked.
func ensureUnlocked(store *vault.Store) error {
	if !store.IsLocked() {
		return nil
	}

	pass := os.Getenv("GRIDCTL_VAULT_PASSPHRASE")
	if pass == "" {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return fmt.Errorf("vault is locked. Set GRIDCTL_VAULT_PASSPHRASE or run 'gridctl vault unlock'")
		}
		fmt.Print("Vault passphrase: ")
		raw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("reading passphrase: %w", err)
		}
		pass = string(raw)
	}

	return store.Unlock(pass)
}

// promptPassphrase reads a hidden passphrase from the terminal.
func promptPassphrase(prompt string) (string, error) {
	pass := os.Getenv("GRIDCTL_VAULT_PASSPHRASE")
	if pass != "" {
		return pass, nil
	}

	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return "", fmt.Errorf("interactive input required. Set GRIDCTL_VAULT_PASSPHRASE for non-interactive use")
	}

	fmt.Print(prompt)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return string(raw), nil
}

// promptPassphraseConfirm reads a passphrase twice and confirms they match.
func promptPassphraseConfirm() (string, error) {
	pass1, err := promptPassphrase("New passphrase: ")
	if err != nil {
		return "", err
	}
	if pass1 == "" {
		return "", fmt.Errorf("passphrase cannot be empty")
	}

	// Skip confirmation if using env var
	if os.Getenv("GRIDCTL_VAULT_PASSPHRASE") != "" {
		return pass1, nil
	}

	pass2, err := promptPassphrase("Confirm passphrase: ")
	if err != nil {
		return "", err
	}

	if pass1 != pass2 {
		return "", fmt.Errorf("passphrases do not match")
	}
	return pass1, nil
}

func runVaultSet(key string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	value := vaultSetValue
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
			// Piped input
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				value = scanner.Text()
			}
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
		}
	}

	if vaultSetSetName != "" {
		if err := store.SetWithSet(key, value, vaultSetSetName); err != nil {
			return err
		}
	} else {
		if err := store.Set(key, value); err != nil {
			return err
		}
	}

	printer := output.New()
	if vaultSetSetName != "" {
		printer.Info("Secret stored", "key", key, "set", vaultSetSetName)
	} else {
		printer.Info("Secret stored", "key", key)
	}
	return nil
}

func runVaultGet(key string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	value, ok := store.Get(key)
	if !ok {
		return fmt.Errorf("secret %q not found", key)
	}

	if vaultGetPlain {
		fmt.Println(value)
	} else {
		masked := maskValue(value)
		fmt.Printf("%s = %s\n", key, masked)
	}
	return nil
}

func runVaultList() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	secrets := store.List()
	if len(secrets) == 0 {
		fmt.Println("No secrets stored in vault")
		return nil
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"Key", "Set"})
	for _, sec := range secrets {
		t.AppendRow(table.Row{sec.Key, sec.Set})
	}
	t.Render()
	return nil
}

func runVaultDelete(key string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	if !store.Has(key) {
		return fmt.Errorf("secret %q not found", key)
	}

	if !vaultDeleteForce {
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
	printer.Info("Secret deleted", "key", key)
	return nil
}

func runVaultImport(file string) error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	secrets, err := parseSecretsFile(file, vaultImportFmt)
	if err != nil {
		return err
	}

	count, err := store.Import(secrets)
	if err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Imported secrets", "count", count, "file", file)
	return nil
}

func runVaultExport() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if err := ensureUnlocked(store); err != nil {
		return err
	}

	secrets := store.Export()
	if len(secrets) == 0 {
		fmt.Println("No secrets stored in vault")
		return nil
	}

	switch vaultExportFmt {
	case "json":
		out := make(map[string]string, len(secrets))
		for k, v := range secrets {
			if vaultExportPlain {
				out[k] = v
			} else {
				out[k] = maskValue(v)
			}
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	default: // env
		keys := store.Keys()
		for _, k := range keys {
			v := secrets[k]
			if vaultExportPlain {
				fmt.Printf("%s=%s\n", k, v)
			} else {
				fmt.Printf("%s=%s\n", k, maskValue(v))
			}
		}
	}
	return nil
}

func runVaultSetsList() error {
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
	t.AppendHeader(table.Row{"Set", "Secrets"})
	for _, s := range sets {
		t.AppendRow(table.Row{s.Name, s.Count})
	}
	t.Render()
	return nil
}

func runVaultSetsCreate(name string) error {
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

func runVaultSetsDelete(name string) error {
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

func runVaultLock() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	// If already locked, unlock first so we can re-encrypt
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
	printer.Info("Vault locked", "encryption", "XChaCha20-Poly1305", "kdf", "Argon2id")
	return nil
}

func runVaultUnlock() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if !store.IsLocked() {
		fmt.Println("Vault is already unlocked")
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
	printer.Info("Vault unlocked", "secrets", len(store.List()))
	return nil
}

func runVaultChangePassphrase() error {
	store, err := loadVault()
	if err != nil {
		return err
	}

	if !store.IsEncrypted() {
		return fmt.Errorf("vault is not encrypted. Run 'gridctl vault lock' first")
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

// parseSecretsFile reads a .env or .json file and returns key-value pairs.
func parseSecretsFile(path, format string) (map[string]string, error) {
	if format == "" {
		// Auto-detect by extension
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
		var secrets map[string]string
		if err := json.Unmarshal(data, &secrets); err != nil {
			return nil, fmt.Errorf("parsing JSON: %w", err)
		}
		return secrets, nil
	case "env":
		return parseEnvFile(string(data))
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// parseEnvFile parses KEY=VALUE lines from .env format.
func parseEnvFile(content string) (map[string]string, error) {
	secrets := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix
		line = strings.TrimPrefix(line, "export ")

		// Split on first =
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Remove surrounding quotes
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		if key != "" {
			secrets[key] = value
		}
	}

	if len(secrets) == 0 {
		return nil, fmt.Errorf("no valid KEY=VALUE pairs found")
	}

	return secrets, nil
}

// maskValue returns a masked version of a secret value.
func maskValue(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return v[:2] + strings.Repeat("*", len(v)-4) + v[len(v)-2:]
}
