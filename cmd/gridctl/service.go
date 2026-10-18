package main

import (
	"context"
	"fmt"
	"io"
	"errors"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/service"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

const serviceUnsupportedMessage = "gridctl service is not supported on Windows; run 'gridctl apply' from a login session or use WSL2"

const (
	serviceExitOK         = 0
	serviceExitAttention  = 1
	serviceExitInfra      = 2
)

var (
	serviceFormat         string
	serviceJSON           bool
	serviceStack          string
	servicePack           string
	servicePort           int
	serviceBasePort       int
	serviceWatch          bool
	serviceDryRun         bool
	serviceNoEnable       bool
	serviceReplace        bool
	serviceForce          bool
	servicePassphraseFile string
	serviceBinary         string
	serviceLogsFollow     bool
	serviceLogsTail       int
	serviceFormatChanged  bool

	// serviceGOOS is the operating system the command refuses. Tests set
	// it to windows.
	serviceGOOS = runtime.GOOS
	// newServiceSupervisor builds the host supervisor. Tests inject a fake.
	newServiceSupervisor = service.NewSupervisor
)

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Install the gateway as a user service that survives logout and reboot",
	Long: `Installs a user-scoped systemd unit or launchd agent that runs the
gateway in the foreground. On Linux it comes back at the next
user-manager start (login, or boot when linger is enabled).

The unit is recorded in the project lockfile before it is written. It
runs 'gridctl apply <stack> --foreground' with a clean environment.
macOS launch agents start at login, not at boot.

On Windows every subcommand exits 1. Run 'gridctl apply' from a login
session, or use WSL2.

Exit codes:
  0  success
  1  attention (drift, a running stack, a locked store, enable failed)
  2  infrastructure (unresolvable home, newer lockfile, supervisor missing)`,
	SilenceUsage: true,
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a user service for a stack or pack",
	Example: `  gridctl service install --stack stack.yaml
  gridctl service install --pack team --passphrase-file ~/.gridctl-pass
  gridctl service install --stack stack.yaml --dry-run`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if code := refuseUnsupportedService(os.Stderr); code != 0 {
			os.Exit(code)
		}
		serviceFormatChanged = cmd.Flags().Changed("format")
		code := runServiceInstall(cmd.Context(), os.Stdout, os.Stderr)
		if code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Show installed service state",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if code := refuseUnsupportedService(os.Stderr); code != 0 {
			os.Exit(code)
		}
		serviceFormatChanged = cmd.Flags().Changed("format")
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		code := runServiceStatus(cmd.Context(), os.Stdout, os.Stderr, name)
		if code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Remove an installed service",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if code := refuseUnsupportedService(os.Stderr); code != 0 {
			os.Exit(code)
		}
		serviceFormatChanged = cmd.Flags().Changed("format")
		code := runServiceUninstall(cmd.Context(), os.Stdout, os.Stderr, args[0])
		if code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

var serviceLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Show service logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if code := refuseUnsupportedService(os.Stderr); code != 0 {
			os.Exit(code)
		}
		serviceFormatChanged = cmd.Flags().Changed("format")
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		code := runServiceLogs(ctx, os.Stdout, os.Stderr, args[0])
		if code != 0 {
			os.Exit(code)
		}
		return nil
	},
}

func init() {
	serviceCmd.AddCommand(serviceInstallCmd, serviceStatusCmd, serviceUninstallCmd, serviceLogsCmd)

	serviceInstallCmd.Flags().StringVar(&serviceStack, "stack", "", "Stack file to run")
	serviceInstallCmd.Flags().StringVar(&servicePack, "pack", "", "Installed pack whose pinned stack to run")
	serviceInstallCmd.Flags().IntVarP(&servicePort, "port", "p", 8180, "Gateway port")
	serviceInstallCmd.Flags().IntVar(&serviceBasePort, "base-port", 9000, "Base port for MCP server host ports")
	serviceInstallCmd.Flags().BoolVar(&serviceWatch, "watch", false, "Reload the stack when the file changes")
	serviceInstallCmd.Flags().BoolVar(&serviceDryRun, "dry-run", false, "Print the unit without writing it")
	serviceInstallCmd.Flags().BoolVar(&serviceNoEnable, "no-enable", false, "Write the unit without enabling it")
	serviceInstallCmd.Flags().BoolVar(&serviceReplace, "replace", false, "Stop a running daemon for this stack first")
	serviceInstallCmd.Flags().BoolVar(&serviceForce, "force", false, "Replace a hand-edited unit after backing it up")
	serviceInstallCmd.Flags().StringVar(&servicePassphraseFile, "passphrase-file", "", "User-owned passphrase file (mode 0600) the unit references")
	serviceInstallCmd.Flags().StringVar(&serviceBinary, "binary", "", "Absolute path to the gridctl binary the unit runs")
	addServiceFormatFlags(serviceInstallCmd)

	serviceStatusCmd.Flags().StringVar(&serviceFormat, "format", "table", "Output format: table or json")
	serviceStatusCmd.Flags().BoolVar(&serviceJSON, "json", false, "Shorthand for --format json")

	serviceUninstallCmd.Flags().BoolVar(&serviceForce, "force", false, "Remove a hand-edited unit")
	serviceUninstallCmd.Flags().BoolVar(&serviceDryRun, "dry-run", false, "Show what would be removed")
	addServiceFormatFlags(serviceUninstallCmd)

	serviceLogsCmd.Flags().BoolVarP(&serviceLogsFollow, "follow", "f", false, "Follow log output")
	serviceLogsCmd.Flags().IntVarP(&serviceLogsTail, "tail", "n", 100, "Number of lines to show")
	addServiceFormatFlags(serviceLogsCmd)
}

// addServiceFormatFlags registers --format and --json. Status registers
// them itself because install and status cannot share one flag variable
// binding twice; each command gets its own pair via the shared variables
// only when the commands are not parsed together. Cobra rejects a flag
// registered on two commands through the same variable only if the flag
// set is shared. Subcommands have separate flag sets, so the shared
// variables are safe.
func addServiceFormatFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&serviceFormat, "format", "table", "Output format: table or json")
	cmd.Flags().BoolVar(&serviceJSON, "json", false, "Shorthand for --format json")
}

func refuseUnsupportedService(w io.Writer) int {
	if serviceGOOS == "windows" {
		fmt.Fprintln(w, serviceUnsupportedMessage)
		return serviceExitAttention
	}
	return 0
}

func resolveServiceFormat(stderr io.Writer) (string, int) {
	format, err := resolveFormat(serviceFormat, serviceFormatChanged, serviceJSON)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return "", serviceExitInfra
	}
	switch strings.ToLower(format) {
	case "", "table", "json":
		return format, 0
	default:
		fmt.Fprintf(stderr, "invalid --format %q (want table or json)\n", format)
		return "", serviceExitInfra
	}
}

func runServiceInstall(ctx context.Context, stdout, stderr io.Writer) int {
	format, code := resolveServiceFormat(stderr)
	if code != 0 {
		return code
	}
	if (serviceStack == "") == (servicePack == "") {
		fmt.Fprintln(stderr, "install requires exactly one of --stack or --pack")
		return serviceExitAttention
	}
	home, err := state.Home()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitInfra
	}
	stackPath, stackName, packName, code := resolveServiceTarget(stderr)
	if code != 0 {
		return code
	}
	stack, err := config.LoadStack(stackPath)
	if err != nil {
		fmt.Fprintf(stderr, "failed to load stack: %v\n", err)
		return serviceExitAttention
	}
	if stack.Name != "" {
		stackName = stack.Name
	}
	if code := packVariableGate(stderr, packName); code != 0 {
		return code
	}
	notes, passFile, code := serviceVaultGate(stderr, stack)
	if code != 0 {
		return code
	}
	binary := serviceBinary
	if binary == "" {
		resolved, err := service.ResolveBinary("")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return serviceExitInfra
		}
		binary = resolved
	} else {
		resolved, err := service.ResolveBinary(binary)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return serviceExitAttention
		}
		binary = resolved
	}
	spec, buildNotes, err := service.BuildSpec(ctx, service.BuildInput{
		StackName:      stackName,
		StackFile:      stackPath,
		Port:           servicePort,
		BasePort:       serviceBasePort,
		Watch:          serviceWatch,
		RuntimeFlag:    runtimeFlag,
		Home:           home,
		HomeOverridden: state.HomeOverridden(),
		Environ:        os.Environ(),
		PassphraseFile: passFile,
		Binary:         binary,
		Pack:           packName,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitCode(err)
	}
	notes = append(notes, buildNotes...)
	if state.HomeOverridden() && spec.Platform == service.PlatformSystemd {
		notes = append(notes, "the unit path is under the overridden home; the user manager will not load a unit outside its search path")
	}
	mgr := service.NewManager(home, newServiceSupervisor())
	res, err := mgr.Install(ctx, service.InstallOptions{
		Spec:           spec,
		DryRun:         serviceDryRun,
		Force:          serviceForce,
		NoEnable:       serviceNoEnable,
		Replace:        serviceReplace,
		HomeOverridden: state.HomeOverridden(),
		Notes:          notes,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitCode(err)
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, installDocument(res, spec)); err != nil {
			fmt.Fprintln(stderr, err)
			return serviceExitInfra
		}
		return serviceExitOK
	}
	if serviceDryRun {
		fmt.Fprintln(stdout, res.Text)
		fmt.Fprintln(stdout, "No changes made (dry run).")
		for _, note := range res.Notes {
			fmt.Fprintf(stdout, "Note: %s\n", note)
		}
		return serviceExitOK
	}
	fmt.Fprintf(stdout, "Wrote %s\n", res.UnitPath)
	if res.Enabled {
		active := "inactive"
		if res.Running {
			active = "active"
		}
		fmt.Fprintf(stdout, "Enabled %s (%s, pid %d, port %d)\n", service.UnitFileName(spec.Name), active, res.PID, spec.Port)
	} else if serviceNoEnable {
		fmt.Fprintf(stdout, "Not enabled. Enable it with: %s\n", manualEnable(spec))
	}
	if res.Linger == "no" {
		fmt.Fprintf(stdout, "Hint: run 'loginctl enable-linger %s' so the service keeps running after logout\n", currentUserName())
	}
	if spec.Platform == service.PlatformLaunchd && res.Enabled {
		fmt.Fprintln(stdout, "Starts at login; a reboot restores it once you log in")
	}
	for _, note := range res.Notes {
		fmt.Fprintf(stdout, "Note: %s\n", note)
	}
	return serviceExitOK
}

func runServiceStatus(ctx context.Context, stdout, stderr io.Writer, name string) int {
	format, code := resolveServiceFormat(stderr)
	if code != 0 {
		return code
	}
	home, err := state.Home()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitInfra
	}
	rows, err := service.NewManager(home, newServiceSupervisor()).Status(ctx, name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitCode(err)
	}
	doc := struct {
		SchemaVersion int           `json:"schema_version"`
		Services      []service.Row `json:"services"`
	}{SchemaVersion: 1, Services: rows}
	if doc.Services == nil {
		doc.Services = []service.Row{}
	}
	attention := false
	for _, row := range rows {
		if row.State != "running" || row.Enabled != "yes" {
			attention = true
		}
	}
	if strings.EqualFold(format, "json") {
		if err := output.EncodeJSON(stdout, doc); err != nil {
			fmt.Fprintln(stderr, err)
			return serviceExitInfra
		}
	} else {
		renderServiceStatus(stdout, home, rows)
	}
	if attention {
		return serviceExitAttention
	}
	return serviceExitOK
}

func runServiceUninstall(ctx context.Context, stdout, stderr io.Writer, name string) int {
	format, code := resolveServiceFormat(stderr)
	if code != 0 {
		return code
	}
	home, err := state.Home()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitInfra
	}
	res, err := service.NewManager(home, newServiceSupervisor()).Uninstall(ctx, name, service.UninstallOptions{
		Force:  serviceForce,
		DryRun: serviceDryRun,
	})
	doc := struct {
		SchemaVersion int    `json:"schema_version"`
		Name          string `json:"name"`
		Action        string `json:"action"`
		UnitPath      string `json:"unit_path,omitempty"`
		Detail        string `json:"detail,omitempty"`
		DryRun        bool   `json:"dry_run"`
	}{SchemaVersion: 1, Name: res.Name, Action: res.Action, UnitPath: res.UnitPath, Detail: res.Detail, DryRun: res.DryRun}
	if strings.EqualFold(format, "json") {
		if encErr := output.EncodeJSON(stdout, doc); encErr != nil {
			fmt.Fprintln(stderr, encErr)
			return serviceExitInfra
		}
	} else if err == nil {
		fmt.Fprintf(stdout, "%s %s\n", res.Action, res.UnitPath)
		if res.Detail != "" {
			fmt.Fprintln(stdout, res.Detail)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitCode(err)
	}
	return serviceExitOK
}

func runServiceLogs(ctx context.Context, stdout, stderr io.Writer, name string) int {
	if _, code := resolveServiceFormat(stderr); code != 0 {
		return code
	}
	home, err := state.Home()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitInfra
	}
	entry, err := service.Lookup(ctx, home, name)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitCode(err)
	}
	if entry == nil {
		fmt.Fprintf(stderr, "no installed service %q\n", name)
		return serviceExitAttention
	}
	if entry.Client == string(service.PlatformLaunchd) {
		path, pathErr := state.LogPath(entry.Source)
		if pathErr != nil {
			fmt.Fprintln(stderr, pathErr)
			return serviceExitInfra
		}
		if err := tailDaemonLog(ctx, stdout, path, serviceLogsTail, serviceLogsFollow); err != nil {
			fmt.Fprintln(stderr, err)
			return serviceExitAttention
		}
		return serviceExitOK
	}
	spec := service.Spec{
		Platform: service.PlatformSystemd,
		Name:     serviceNameOf(entry),
		Label:    labelOf(entry),
		UnitPath: entry.Path,
	}
	sup := newServiceSupervisor()
	if err := sup.Logs(ctx, spec, service.LogOptions{Lines: serviceLogsTail, Follow: serviceLogsFollow, Stdout: stdout, Stderr: stderr}); err != nil {
		fmt.Fprintln(stderr, err)
		return serviceExitCode(err)
	}
	return serviceExitOK
}

func resolveServiceTarget(stderr io.Writer) (stackPath, stackName, packName string, code int) {
	if serviceStack != "" {
		abs, err := filepath.Abs(serviceStack)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return "", "", "", serviceExitAttention
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			fmt.Fprintf(stderr, "stack file %s is not readable\n", abs)
			return "", "", "", serviceExitAttention
		}
		return abs, "", "", 0
	}
	locked, err := packops.LoadLockedPack(servicePack)
	if err != nil {
		if errors.Is(err, skills.ErrNewerImportLockVersion) || errors.Is(err, project.ErrNewerLockVersion) {
			fmt.Fprintln(stderr, err)
			return "", "", "", serviceExitInfra
		}
		if !errors.Is(err, packops.ErrNotImported) && !errors.Is(err, packops.ErrNameCollision) {
			fmt.Fprintln(stderr, err)
			return "", "", "", serviceExitInfra
		}
		fmt.Fprintf(stderr, "pack %q has no stack\nRun 'gridctl pack add' to import a pack that carries a stack.\n", servicePack)
		return "", "", "", serviceExitAttention
	}
	if locked == nil || locked.Stack == nil {
		fmt.Fprintf(stderr, "pack %q has no stack\nRun 'gridctl pack add' to import a pack that carries a stack.\n", servicePack)
		return "", "", "", serviceExitAttention
	}
	if missing, absent := packStackMissing(locked.Stack); absent {
		fmt.Fprintf(stderr, "pack %q pinned stack is missing: %s\nRun 'gridctl pack add' to import it again.\n", servicePack, missing)
		return "", "", "", serviceExitAttention
	}
	return packops.PinnedStackPath(locked.Stack), locked.Stack.Name, servicePack, 0
}

func packStackMissing(st *skills.LockedStack) (string, bool) {
	if st == nil || st.CheckoutDir == "" {
		if st == nil {
			return "", true
		}
		return st.CheckoutDir, true
	}
	info, err := os.Stat(st.CheckoutDir)
	if err != nil || !info.IsDir() {
		return st.CheckoutDir, true
	}
	path := packops.PinnedStackPath(st)
	info, err = os.Stat(path)
	if err != nil || info.IsDir() {
		return path, true
	}
	return "", false
}

func packVariableGate(stderr io.Writer, packName string) int {
	if packName == "" {
		return 0
	}
	locked, err := packops.LoadLockedPack(packName)
	if err != nil || locked == nil {
		return 0
	}
	store, err := loadVault()
	if err != nil || store.IsLocked() {
		return 0
	}
	keys := map[string]bool{}
	for _, variable := range store.List() {
		keys[variable.Key] = true
	}
	unmet := packops.UnmetVariables(locked.Variables, keys, true)
	if len(unmet) == 0 {
		return 0
	}
	fmt.Fprintf(stderr, "pack %q is missing required variables\n", packName)
	for _, variable := range unmet {
		fmt.Fprintln(stderr, variable.Command)
	}
	return serviceExitAttention
}

func serviceVaultGate(stderr io.Writer, stack *config.Stack) (notes []string, passFile string, code int) {
	needs := config.VaultNeeds(stack)
	store, err := loadVault()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return nil, "", serviceExitInfra
	}
	if servicePassphraseFile != "" {
		abs, err := filepath.Abs(servicePassphraseFile)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return nil, "", serviceExitAttention
		}
		pass, err := vault.ReadPassphraseFile(abs)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return nil, "", serviceExitAttention
		}
		if store.IsLocked() {
			if err := store.Unlock(pass); err != nil {
				fmt.Fprintln(stderr, "passphrase file was rejected; the passphrase does not unlock the variable store")
				return nil, "", serviceExitAttention
			}
		}
		passFile = abs
	}
	if len(needs) > 0 && store.IsLocked() && passFile == "" {
		fmt.Fprintf(stderr, "stack %s references stored variables and the variable store is encrypted,\nso a service cannot start it without a passphrase. Choose one:\n", stack.Name)
		fmt.Fprintln(stderr, "  1. Create a passphrase file you own (mode 0600) and pass --passphrase-file <path>;")
		fmt.Fprintln(stderr, "     the unit references that path and gridctl never writes the file.")
		fmt.Fprintln(stderr, "  2. Switch the store to plaintext with 'gridctl var ...' (values readable on disk).")
		for _, reason := range needs {
			fmt.Fprintf(stderr, "  %s\n", reason)
		}
		return nil, "", serviceExitAttention
	}
	if len(needs) == 0 && passFile == "" && store.IsLocked() {
		notes = append(notes, "the variable store stays locked in the service")
	}
	return notes, passFile, 0
}

func installDocument(res service.InstallResult, spec service.Spec) map[string]any {
	doc := map[string]any{
		"schema_version": 1,
		"name":           res.Name,
		"platform":       res.Platform,
		"unit_path":      res.UnitPath,
		"label":          res.Label,
		"exec_start":     res.ExecStart,
		"environment":    res.Environment,
		"enabled":        res.Enabled,
		"running":        res.Running,
		"pid":            res.PID,
		"linger":         res.Linger,
		"notes":          res.Notes,
		"dry_run":        res.DryRun,
	}
	if res.DryRun {
		doc["text"] = res.Text
		doc["would_enable"] = res.WouldEnable
	}
	if doc["notes"] == nil {
		doc["notes"] = []string{}
	}
	if doc["linger"] == "" {
		doc["linger"] = "unknown"
	}
	_ = spec
	return doc
}

func renderServiceStatus(w io.Writer, home string, rows []service.Row) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "No installed services.")
		return
	}
	tw := output.NewTableWriter(w, true)
	tw.AppendHeader(table.Row{"NAME", "PLATFORM", "STATE", "ENABLED", "PID", "PORT", "STACK", "UNIT"})
	for _, row := range rows {
		pid := "-"
		if row.PID > 0 {
			pid = strconv.Itoa(row.PID)
		}
		port := "-"
		if row.Port > 0 {
			port = strconv.Itoa(row.Port)
		}
		tw.AppendRow(table.Row{row.Name, row.Platform, row.State, row.Enabled, pid, port, shortenHome(home, row.Stack), shortenHome(home, row.Unit)})
	}
	tw.Render()
	for _, row := range rows {
		if row.Remediation != "" {
			fmt.Fprintf(w, "%s: %s\n", row.Name, row.Remediation)
		}
	}
}

func shortenHome(home, path string) string {
	if home == "" || path == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~/" + rel
}

func serviceExitCode(err error) int {
	if err == nil {
		return serviceExitOK
	}
	var infra *service.InfraError
	if errors.As(err, &infra) {
		return serviceExitInfra
	}
	if errors.Is(err, project.ErrNewerLockVersion) || strings.Contains(err.Error(), "newer gridctl") {
		return serviceExitInfra
	}
	return serviceExitAttention
}

func currentUserName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	if name := os.Getenv("USER"); name != "" {
		return name
	}
	return strconv.Itoa(os.Getuid())
}

func manualEnable(spec service.Spec) string {
	if spec.Platform == service.PlatformLaunchd {
		uid := strconv.Itoa(os.Getuid())
		return fmt.Sprintf("launchctl bootstrap gui/%s %s && launchctl enable gui/%s/%s", uid, spec.UnitPath, uid, spec.Label)
	}
	return "systemctl --user daemon-reload && systemctl --user enable --now " + service.UnitFileName(spec.Name)
}

func serviceNameOf(entry *project.Entry) string {
	if entry == nil {
		return ""
	}
	if entry.Extra != nil {
		if name, ok := entry.Extra["service_name"].(string); ok && name != "" {
			return name
		}
	}
	return service.SanitizeName(entry.Source)
}

func labelOf(entry *project.Entry) string {
	if entry != nil && entry.Extra != nil {
		if label, ok := entry.Extra["label"].(string); ok && label != "" {
			return label
		}
	}
	return service.Label(serviceNameOf(entry))
}
