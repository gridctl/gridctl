package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage remote skill dependencies",
	Long:  "Import, update, and manage skills from remote git repositories.",
}

// Flags
var (
	skillAddRef        string
	skillAddPath       string
	skillAddNoActivate bool
	skillAddTrust      bool
	skillAddForce      bool
	skillAddRename     string
	skillAddAuthToken  string
	skillAddVaultKey   string
	skillAddSSHKey     string
	skillListRemote    bool
	skillListFormat    string
	skillUpdateDryRun  bool
	skillUpdateForce   bool
	skillTryDuration   string
	skillTryAuthToken  string
	skillTryVaultKey   string
	skillTrySSHKey     string
)

var skillAddCmd = &cobra.Command{
	Use:   "add <repo-url>",
	Short: "Import skills from a git repository",
	Long:  "Clone a repository, discover SKILL.md files, and import them into the local registry.",
	Example: `  gridctl skill add https://github.com/acme/skills
  gridctl skill add git@github.com:acme/private-skills.git --vault-key GH_TOKEN`,
	Args:    cobra.ExactArgs(1),
	PreRunE: validateSkillAuthFlags(&skillAddAuthToken, &skillAddVaultKey),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillAdd(args[0])
	},
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all skills with origin info",
	Long:  "List all skills showing source origin (local/remote) and update status.",
	RunE: func(cmd *cobra.Command, args []string) error {
		var err error
		if skillListFormat, err = resolveFormat(skillListFormat, cmd.Flags().Changed("format"), *skillListJSON); err != nil {
			return err
		}
		return runSkillList()
	},
}

var skillListJSON *bool

var skillUpdateCmd = &cobra.Command{
	Use:     "update [name]",
	Aliases: []string{"sync"},
	Short:   "Update imported skills (alias: sync)",
	Long: `Fetch latest from source repositories and apply updates.

With no name, every imported skill is checked. By default, skills whose
on-disk SKILL.md has been locally modified since the last import are
refused; pass --force to overwrite them anyway.

This command is also available as 'gridctl skill sync' for parity with the
"Sync sources" affordance in the web UI Library.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		return runSkillUpdate(name)
	},
}

var skillRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an imported skill",
	Long:  "Remove a skill and clean up its origin file and lock entry.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillRemove(args[0])
	},
}

var skillPinCmd = &cobra.Command{
	Use:   "pin <name> <ref>",
	Short: "Pin a skill to a specific version",
	Long:  "Pin an imported skill to a specific git ref, disabling auto-update.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillPin(args[0], args[1])
	},
}

var skillInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show skill origin and update status",
	Long:  "Display detailed information about a skill's remote origin and update status.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillInfo(args[0])
	},
}

var skillValidateCmd = &cobra.Command{
	Use:   "validate <name>",
	Short: "Validate a skill definition",
	Long:  "Validate a skill's definition and display errors and warnings, including missing acceptance criteria.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillValidate(args[0])
	},
}

var skillTryCmd = &cobra.Command{
	Use:     "try <repo-url>",
	Short:   "Temporarily import a skill",
	Long:    "Import a skill temporarily for evaluation. Automatically removed after the specified duration.",
	Args:    cobra.ExactArgs(1),
	PreRunE: validateSkillAuthFlags(&skillTryAuthToken, &skillTryVaultKey),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSkillTry(args[0])
	},
}

func init() {
	skillAddCmd.Flags().StringVar(&skillAddRef, "ref", "", "Git ref (branch, tag, or commit)")
	skillAddCmd.Flags().StringVar(&skillAddPath, "path", "", "Subdirectory path within the repository")
	skillAddCmd.Flags().BoolVar(&skillAddNoActivate, "no-activate", false, "Import as draft instead of active")
	skillAddCmd.Flags().BoolVar(&skillAddTrust, "trust", false, "Skip security scan confirmation")
	skillAddCmd.Flags().BoolVar(&skillAddForce, "force", false, "Overwrite existing skills")
	skillAddCmd.Flags().StringVar(&skillAddRename, "rename", "", "Rename the skill on import (single skill only)")
	skillAddCmd.Flags().StringVar(&skillAddAuthToken, "auth-token", "", "Personal Access Token (HTTPS only; not persisted; intended for CI use)")
	skillAddCmd.Flags().StringVar(&skillAddVaultKey, "vault-key", "", "Resolve the PAT from this vault key (e.g. GIT_TOKEN)")
	skillAddCmd.Flags().StringVar(&skillAddSSHKey, "ssh-key", "", "Use an SSH private key at this path (SSH URLs only)")

	skillListCmd.Flags().BoolVar(&skillListRemote, "remote", false, "Show only remote (imported) skills")
	skillListCmd.Flags().StringVar(&skillListFormat, "format", "", "Output format (json)")
	skillListJSON = addJSONAlias(skillListCmd)

	skillUpdateCmd.Flags().BoolVar(&skillUpdateDryRun, "dry-run", false, "Show changes without applying")
	skillUpdateCmd.Flags().BoolVar(&skillUpdateForce, "force", false, "Force update even if no changes detected")

	skillTryCmd.Flags().StringVar(&skillTryDuration, "duration", "10m", "Duration before auto-cleanup")
	skillTryCmd.Flags().StringVar(&skillTryAuthToken, "auth-token", "", "Personal Access Token (HTTPS only; not persisted)")
	skillTryCmd.Flags().StringVar(&skillTryVaultKey, "vault-key", "", "Resolve the PAT from this vault key")
	skillTryCmd.Flags().StringVar(&skillTrySSHKey, "ssh-key", "", "Use an SSH private key at this path (SSH URLs only)")

	skillCmd.AddCommand(skillAddCmd)
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillUpdateCmd)
	skillCmd.AddCommand(skillRemoveCmd)
	skillCmd.AddCommand(skillPinCmd)
	skillCmd.AddCommand(skillInfoCmd)
	skillCmd.AddCommand(skillValidateCmd)
	skillCmd.AddCommand(skillTryCmd)
}

func loadRegistry() (*registry.Store, error) {
	registryDir := registryDir()
	store := registry.NewStore(registryDir)
	if err := store.Load(); err != nil {
		return nil, fmt.Errorf("loading registry: %w", err)
	}
	return store, nil
}

func registryDir() string {
	return filepath.Join(state.BaseDir(), "registry")
}

func skillDirPath(sk *registry.AgentSkill) string {
	dir := sk.Name
	if sk.Dir != "" {
		dir = sk.Dir
	}
	return filepath.Join(registryDir(), "skills", dir)
}

func newImporter(store *registry.Store) *skills.Importer {
	logger := slog.Default()
	imp := skills.NewImporter(store, registryDir(), skills.LockFilePath(), logger)
	imp.SetCredentialResolver(cliCredentialResolver)
	return imp
}

// validateSkillAuthFlags returns a PreRunE that rejects mutually exclusive
// auth flag combinations.
func validateSkillAuthFlags(token, vaultKey *string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, _ []string) error {
		if *token != "" && *vaultKey != "" {
			return errors.New("--auth-token and --vault-key are mutually exclusive")
		}
		return nil
	}
}

// buildAuthConfigFromFlags translates CLI auth flags into skills.AuthConfig.
// When --vault-key is set, the vault is unlocked (prompting if necessary)
// and the reference is resolved immediately so Import sees a ready Token.
func buildAuthConfigFromFlags(token, vaultKey, sshKey string) (skills.AuthConfig, error) {
	switch {
	case sshKey != "":
		return skills.AuthConfig{
			Method:        "ssh-key",
			SSHKeyPath:    sshKey,
			SSHPassphrase: os.Getenv("GRIDCTL_SSH_KEY_PASSPHRASE"),
		}, nil
	case token != "":
		return skills.AuthConfig{Method: "token", Token: token}, nil
	case vaultKey != "":
		ref := fmt.Sprintf("${var:%s}", vaultKey)
		resolved, err := cliCredentialResolver(ref)
		if err != nil {
			return skills.AuthConfig{}, err
		}
		return skills.AuthConfig{Method: "token", Token: resolved, CredentialRef: ref}, nil
	}
	return skills.AuthConfig{}, nil
}

// cliCredentialResolver resolves a "${var:KEY}" (or legacy "${vault:KEY}")
// reference via the local
// vault, unlocking it if necessary. Wired onto the CLI's Importer so
// `skill update` can re-resolve stored references automatically.
func cliCredentialResolver(ref string) (string, error) {
	store, err := loadVault()
	if err != nil {
		return "", err
	}
	if err := ensureUnlocked(store); err != nil {
		return "", err
	}
	resolver := config.VaultResolver(store)
	expanded, unresolved, _ := config.ExpandString(ref, resolver)
	if len(unresolved) > 0 {
		return "", fmt.Errorf("vault key %q not found", unresolved[0])
	}
	return expanded, nil
}

// printSkillAuthHint emits an actionable suggestion for a classified git
// auth error. Returns true when a hint was printed.
func printSkillAuthHint(err error) bool {
	switch {
	case errors.Is(err, gitpkg.ErrAuthRequired), errors.Is(err, gitpkg.ErrNotFound):
		fmt.Fprintln(os.Stderr, "hint: this repository may be private; add credentials with --auth-token or --vault-key")
		return true
	case errors.Is(err, gitpkg.ErrAuthFailed):
		fmt.Fprintln(os.Stderr, "hint: credentials were rejected; verify the token has repo-read access")
		return true
	case errors.Is(err, gitpkg.ErrSSHAgentMissing):
		fmt.Fprintln(os.Stderr, `hint: ssh-agent not detected; run eval "$(ssh-agent -s)" && ssh-add, or use --auth-token`)
		return true
	case errors.Is(err, gitpkg.ErrHostKeyMismatch):
		fmt.Fprintln(os.Stderr, "hint: the SSH host key does not match ~/.ssh/known_hosts — investigate before retrying")
		return true
	}
	return false
}

func runSkillAdd(repoURL string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}

	authCfg, err := buildAuthConfigFromFlags(skillAddAuthToken, skillAddVaultKey, skillAddSSHKey)
	if err != nil {
		return err
	}

	imp := newImporter(store)
	result, err := imp.Import(skills.ImportOptions{
		Repo:       repoURL,
		Ref:        skillAddRef,
		Path:       skillAddPath,
		Trust:      skillAddTrust,
		NoActivate: skillAddNoActivate,
		Force:      skillAddForce,
		Rename:     skillAddRename,
		Auth:       authCfg,
	})
	if err != nil {
		classified := gitpkg.ClassifyError(err)
		printSkillAuthHint(classified)
		return gitpkg.RedactError(classified)
	}

	printer := output.New()

	for _, imported := range result.Imported {
		printer.Info("Imported skill", "name", imported.Name)
		if len(imported.Findings) > 0 {
			fmt.Print(skills.FormatFindings(imported.Findings))
		}
	}

	for _, skipped := range result.Skipped {
		printer.Warn("Skipped skill", "name", skipped.Name, "reason", skipped.Reason)
	}

	for _, warning := range result.Warnings {
		printer.Warn(warning)
	}

	if len(result.Imported) == 0 {
		return fmt.Errorf("no skills were imported")
	}

	return nil
}

func runSkillList() error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}

	allSkills := store.ListSkills()
	if len(allSkills) == 0 {
		fmt.Println("No skills in registry")
		return nil
	}

	type skillEntry struct {
		Name   string `json:"name"`
		State  string `json:"state"`
		Source string `json:"source"`
		Repo   string `json:"repo,omitempty"`
		Ref    string `json:"ref,omitempty"`
	}

	var entries []skillEntry

	for _, sk := range allSkills {
		entry := skillEntry{
			Name:   sk.Name,
			State:  string(sk.State),
			Source: "local",
		}

		skillDir := skillDirPath(sk)

		if origin, err := skills.ReadOrigin(skillDir); err == nil {
			entry.Source = "remote"
			entry.Repo = origin.Repo
			entry.Ref = origin.Ref
		}

		if skillListRemote && entry.Source != "remote" {
			continue
		}

		entries = append(entries, entry)
	}

	if skillListFormat == "json" {
		data, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"Name", "State", "Source", "Repo"})
	for _, e := range entries {
		repo := e.Repo
		if repo != "" && e.Ref != "" {
			repo = fmt.Sprintf("%s@%s", repo, e.Ref)
		}
		t.AppendRow(table.Row{e.Name, e.State, e.Source, repo})
	}
	t.Render()

	// Show update notice if available
	if notice := skills.FormatUpdateNotice(); notice != "" {
		fmt.Print(notice)
	}

	return nil
}

// driftedSkills returns the names of skills with local edits to their on-disk
// SKILL.md. When skillName is non-empty, only that skill is inspected (so
// `gridctl skill update foo` doesn't pay to hash every installed skill).
func driftedSkills(store *registry.Store, skillName string) ([]string, error) {
	if skillName != "" {
		sk, err := store.GetSkill(skillName)
		if err != nil {
			return nil, nil // skill not found — let imp.Update produce the real error
		}
		dir := sk.Dir
		if dir == "" {
			dir = sk.Name
		}
		skillDir := filepath.Join(registryDir(), "skills", dir)
		origin, err := skills.ReadOrigin(skillDir)
		if err != nil || origin.InstalledHash == "" {
			return nil, nil
		}
		current, err := skills.ContentHashFile(filepath.Join(skillDir, "SKILL.md"))
		if err != nil {
			return nil, nil
		}
		if current != origin.InstalledHash {
			return []string{skillName}, nil
		}
		return nil, nil
	}
	return skills.DetectDrift(context.Background(), store, skills.LockFilePath(), "")
}

func runSkillUpdate(name string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}

	imp := newImporter(store)
	printer := output.New()

	// Drift check (unless --force or --dry-run). Refuse rather than silently
	// overwrite local edits to imported SKILL.md files.
	if !skillUpdateForce && !skillUpdateDryRun {
		drifted, err := driftedSkills(store, name)
		if err != nil {
			return fmt.Errorf("checking drift: %w", err)
		}
		if len(drifted) > 0 {
			fmt.Fprintln(os.Stderr, "The following skills have local edits that would be overwritten:")
			for _, d := range drifted {
				fmt.Fprintf(os.Stderr, "  - %s\n", d)
			}
			fmt.Fprintln(os.Stderr, "\nRe-run with --force to overwrite, or revert local changes first.")
			return fmt.Errorf("refusing to overwrite locally-edited skills")
		}
	}

	if name != "" {
		result, err := imp.Update(name, skillUpdateDryRun, skillUpdateForce)
		if err != nil {
			return err
		}
		for _, w := range result.Warnings {
			printer.Info(w)
		}
		for _, imported := range result.Imported {
			printer.Info("Updated skill", "name", imported.Name)
		}
		return nil
	}

	// Update all remote skills, grouped by source so we can honor pins and
	// print an aggregate summary at the end.
	allSkills := store.ListSkills()
	type skillRef struct {
		sk     *registry.AgentSkill
		ref    string
		source string
	}
	var remoteSkills []skillRef
	sourcesSeen := map[string]string{} // source name → ref (one is enough for pin check)
	for _, sk := range allSkills {
		skillDir := skillDirPath(sk)
		if !skills.HasOrigin(skillDir) {
			continue
		}
		origin, err := skills.ReadOrigin(skillDir)
		if err != nil {
			continue
		}
		sourceName := skills.RepoToName(origin.Repo)
		sourcesSeen[sourceName] = origin.Ref
		remoteSkills = append(remoteSkills, skillRef{sk: sk, ref: origin.Ref, source: sourceName})
	}

	var (
		updatedSkills int
		failedSources = map[string]bool{}
		syncedSources = map[string]bool{}
		pinnedSources = map[string]bool{}
	)

	for _, sr := range remoteSkills {
		if skills.IsPinnedRef(sr.ref) {
			pinnedSources[sr.source] = true
			continue
		}

		result, err := imp.Update(sr.sk.Name, skillUpdateDryRun, skillUpdateForce)
		if err != nil {
			printer.Warn("Failed to update", "skill", sr.sk.Name, "error", err)
			failedSources[sr.source] = true
			continue
		}
		for _, w := range result.Warnings {
			printer.Info(w)
		}
		for _, imported := range result.Imported {
			printer.Info("Updated skill", "name", imported.Name)
			updatedSkills++
		}
		if !failedSources[sr.source] {
			syncedSources[sr.source] = true
		}
	}

	// Sources can show up in both syncedSources and failedSources; the
	// failed-skill loop sets failedSources but doesn't unset syncedSources.
	// Reconcile so a source with any failure counts as failed.
	for src := range failedSources {
		delete(syncedSources, src)
	}

	if skillUpdateDryRun {
		return nil
	}

	if len(pinnedSources) > 0 {
		names := make([]string, 0, len(pinnedSources))
		for n := range pinnedSources {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Printf("Skipped pinned sources: %s (use 'gridctl skill update <name>' to force)\n", strings.Join(names, ", "))
	}

	fmt.Printf("Synced %d source(s), %d skill(s) updated, %d failed, %d pinned\n",
		len(syncedSources), updatedSkills, len(failedSources), len(pinnedSources))

	return nil
}

func runSkillRemove(name string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}

	imp := newImporter(store)
	if err := imp.Remove(name); err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Removed skill", "name", name)
	return nil
}

func runSkillPin(name, ref string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}

	imp := newImporter(store)
	if err := imp.Pin(name, ref); err != nil {
		return err
	}

	printer := output.New()
	printer.Info("Pinned skill", "name", name, "ref", ref)
	return nil
}

func runSkillInfo(name string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}

	imp := newImporter(store)
	info, err := imp.Info(name)
	if err != nil {
		return err
	}

	printer := output.New()

	if !info.IsRemote {
		printer.Info("Local skill", "name", info.Name)
	} else {
		printer.Info("Remote skill",
			"name", info.Name,
			"repo", info.Origin.Repo,
			"ref", info.Origin.Ref,
			"commit", info.Origin.CommitSHA[:8],
			"imported", info.Origin.ImportedAt.Format(time.RFC3339),
		)

		if !info.LastChecked.IsZero() {
			fmt.Printf("  Last checked: %s\n", info.LastChecked.Format(time.RFC3339))
		}
	}

	if sk, err := store.GetSkill(name); err == nil && len(sk.AcceptanceCriteria) > 0 {
		fmt.Println("\nAcceptance Criteria:")
		for i, c := range sk.AcceptanceCriteria {
			fmt.Printf("  %d. %s\n", i+1, c)
		}
	}

	return nil
}

func runSkillValidate(name string) error {
	store, err := loadRegistry()
	if err != nil {
		return err
	}

	sk, err := store.GetSkill(name)
	if err != nil {
		return err
	}

	result := registry.ValidateSkillFull(sk)

	if !result.Valid() {
		for _, e := range result.Errors {
			fmt.Printf("  ✗ %s: %s\n", name, e)
		}
	}

	for _, w := range result.Warnings {
		fmt.Printf("⚠  %s: %s\n", name, w)
	}

	if result.Valid() && len(result.Warnings) == 0 {
		fmt.Printf("✓ %s is valid\n", name)
	}

	return nil
}

func runSkillTry(repoURL string) error {
	duration, err := time.ParseDuration(skillTryDuration)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	store, err := loadRegistry()
	if err != nil {
		return err
	}

	authCfg, err := buildAuthConfigFromFlags(skillTryAuthToken, skillTryVaultKey, skillTrySSHKey)
	if err != nil {
		return err
	}

	imp := newImporter(store)
	result, err := imp.Import(skills.ImportOptions{
		Repo:  repoURL,
		Trust: true,
		Force: true,
		Auth:  authCfg,
	})
	if err != nil {
		classified := gitpkg.ClassifyError(err)
		printSkillAuthHint(classified)
		return gitpkg.RedactError(classified)
	}

	printer := output.New()
	var importedNames []string
	for _, imported := range result.Imported {
		printer.Info("Temporarily imported", "name", imported.Name, "duration", duration)
		importedNames = append(importedNames, imported.Name)
	}

	if len(importedNames) == 0 {
		return fmt.Errorf("no skills were imported")
	}

	fmt.Printf("\nSkill(s) will be automatically removed in %s\n", duration)
	fmt.Println("Press Ctrl+C to remove immediately and exit.")

	// Countdown with periodic updates
	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	cleanup := func() {
		cleanStore, err := loadRegistry()
		if err != nil {
			slog.Warn("failed to load registry for cleanup", "error", err)
			return
		}
		cleanImp := skills.NewImporter(cleanStore, registryDir(), skills.LockFilePath(), slog.Default())
		for _, name := range importedNames {
			if err := cleanImp.Remove(name); err != nil {
				slog.Warn("failed to clean up ephemeral skill", "name", name, "error", err)
			} else {
				printer.Info("Removed ephemeral skill", "name", name)
			}
		}
	}

	for {
		select {
		case <-sigCh:
			fmt.Println("\nCleaning up ephemeral skills...")
			cleanup()
			return nil
		case <-ticker.C:
			remaining := time.Until(deadline).Round(time.Second)
			if remaining > 0 {
				fmt.Printf("  ⏱ %s remaining before auto-cleanup\n", remaining)
			}
		case <-time.After(time.Until(deadline)):
			fmt.Println("\nDuration expired. Cleaning up ephemeral skills...")
			cleanup()
			return nil
		}
	}
}
