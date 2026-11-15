package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/service"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

const (
	// plainNotReproduced lists apply flags a restart cannot reproduce.
	// None of them is recorded in the daemon state file. A daemon started
	// with a non-default --base-port gets the default again.
	plainNotReproduced = "--watch, --base-port, --no-expand, --no-override, --override-optional, --bind, --bind-all, --insecure-allow-unauthenticated, --code-mode, --runtime, --log-file, --log-level"
	// stacklessNotReproduced lists serve flags a restart cannot reproduce.
	stacklessNotReproduced = "--bind, --bind-all, --insecure-allow-unauthenticated, --runtime, --log-file, --log-level"
)

// restartSupervisor builds the host supervisor. Tests replace it.
var restartSupervisor = newServiceSupervisor

// restartPlan is one daemon's restart command. Spec is set only for a
// service-owned daemon the supervisor should restart. Steps are the
// argv lists, without the binary, for a plain or stackless subprocess.
type restartPlan struct {
	Name  string
	Kind  string
	Line  string
	Notes []string
	Spec  *service.Spec
	Steps [][]string

	// daemon is the recorded state of a plain or stackless daemon, used to
	// read its variable store status before any step runs.
	daemon *state.DaemonState
}

// buildRestartPlan is pure. entry is the service lookup result (nil when
// none). override is the status override path. overrideUnread is true when
// the status endpoint could not be read; the plan then omits --override and
// says so. goos is "linux" or "darwin" for the service wording.
func buildRestartPlan(st state.DaemonState, entry *project.Entry, override string, overrideUnread bool, goos string) restartPlan {
	if strings.TrimSpace(st.StackFile) == "" {
		return stacklessPlan(st)
	}
	hit := serviceHitFrom(entry)
	if hit != nil {
		if hit.StackFile != "" && st.StackFile != "" && hit.StackFile != st.StackFile {
			plan := plainPlan(st, override, overrideUnread)
			plan.Notes = append([]string{fmt.Sprintf("service record stack file %s does not match daemon state %s; treating %s as a plain daemon", hit.StackFile, st.StackFile, st.StackName)}, plan.Notes...)
			return plan
		}
		return servicePlan(st, hit, goos, overrideUnread)
	}
	return plainPlan(st, override, overrideUnread)
}

type serviceHit struct {
	StackFile   string
	ServiceName string
	Label       string
	Binary      string
	UnitPath    string
	Platform    string
	Port        int
	BasePort    int
}

func serviceHitFrom(e *project.Entry) *serviceHit {
	if e == nil {
		return nil
	}
	name := extraString(e.Extra["service_name"])
	if name == "" {
		name = service.SanitizeName(e.Source)
	}
	label := extraString(e.Extra["label"])
	if label == "" {
		label = service.Label(name)
	}
	return &serviceHit{
		StackFile:   extraString(e.Extra["stack_file"]),
		ServiceName: name,
		Label:       label,
		Binary:      extraString(e.Extra["binary"]),
		UnitPath:    e.Path,
		Platform:    e.Client,
		Port:        extraInt(e.Extra["port"]),
		BasePort:    extraInt(e.Extra["base_port"]),
	}
}

func servicePlan(st state.DaemonState, hit *serviceHit, goos string, overrideUnread bool) restartPlan {
	line := serviceRestartLine(hit, goos)
	plan := restartPlan{
		Name: st.StackName,
		Kind: "service",
		Line: line,
		Spec: &service.Spec{
			Platform:  platformFor(hit.Platform, goos),
			Name:      hit.ServiceName,
			StackName: st.StackName,
			Label:     hit.Label,
			Binary:    hit.Binary,
			UnitPath:  hit.UnitPath,
			StackFile: firstNonEmpty(hit.StackFile, st.StackFile),
			Port:      firstPositive(hit.Port, st.Port),
			BasePort:  hit.BasePort,
		},
	}
	if overrideUnread {
		plan.Notes = append(plan.Notes, "override could not be read")
	}
	return plan
}

func serviceRestartLine(hit *serviceHit, goos string) string {
	if goos == "darwin" {
		return fmt.Sprintf("gridctl upgrade --restart (launchd label %s)", hit.Label)
	}
	return "systemctl --user restart " + service.UnitFileName(hit.ServiceName)
}

func plainPlan(st state.DaemonState, override string, overrideUnread bool) restartPlan {
	line := fmt.Sprintf("gridctl destroy %s && gridctl apply %s --port %d", st.StackName, st.StackFile, st.Port)
	args := []string{"apply", st.StackFile, "--port", fmt.Sprintf("%d", st.Port)}
	if !overrideUnread && override != "" {
		line += " --override " + override
		args = append(args, "--override", override)
	}
	plan := restartPlan{
		Name: st.StackName,
		Kind: "plain",
		Line: line,
		Notes: []string{
			"not reproduced: " + plainNotReproduced,
		},
		Steps: [][]string{
			{"destroy", st.StackName},
			args,
		},
	}
	if overrideUnread {
		plan.Notes = append(plan.Notes, "override could not be read")
	}
	return plan
}

func stacklessPlan(st state.DaemonState) restartPlan {
	name := st.StackName
	if name == "" {
		name = "gridctl"
	}
	return restartPlan{
		Name: name,
		Kind: "stackless",
		Line: fmt.Sprintf("gridctl destroy %s && gridctl serve --port %d", name, st.Port),
		Notes: []string{
			"not reproduced: " + stacklessNotReproduced,
		},
		Steps: [][]string{
			{"destroy", name},
			{"serve", "--port", fmt.Sprintf("%d", st.Port)},
		},
	}
}

func platformFor(client, goos string) service.Platform {
	switch service.Platform(client) {
	case service.PlatformSystemd, service.PlatformLaunchd:
		return service.Platform(client)
	}
	if goos == "darwin" {
		return service.PlatformLaunchd
	}
	return service.PlatformSystemd
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstPositive(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

func extraString(v any) string {
	s, _ := v.(string)
	return s
}

func extraInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func writeDaemonPlans(w io.Writer, plans []restartPlan, doRestart bool) {
	if len(plans) == 0 {
		return
	}
	fmt.Fprintln(w, "\nRunning daemons still use the previous binary:")
	for _, p := range plans {
		fmt.Fprintf(w, "  %-8s (%-9s)  %s\n", p.Name, p.Kind, p.Line)
		for _, note := range p.Notes {
			fmt.Fprintf(w, "                        %s\n", note)
		}
	}
	if !doRestart {
		fmt.Fprintln(w, "Run again with --restart to do this now.")
	}
}

func daemonVersionWarning(stack, daemonVer, cliVer, line string) string {
	return fmt.Sprintf("warning: %s runs gridctl %s; this CLI is %s. Restart it: %s", stack, daemonVer, cliVer, line)
}

// restartLineForStatus builds the restart command status prints when a
// running daemon's binary version differs from this CLI.
func restartLineForStatus(st state.DaemonState, override string, unread bool) string {
	var entry *project.Entry
	if home, err := state.Home(); err == nil {
		entry, _ = service.Lookup(context.Background(), home, st.StackName)
	}
	return buildRestartPlan(st, entry, override, unread, runtime.GOOS).Line
}

// reportDaemons prints the restart plan after a successful swap and, when
// --restart is set, performs it. A list failure after a swap is a warning
// unless --restart was asked for.
func (u *upgrader) reportDaemons(ctx context.Context, exePath string) error {
	plans, err := u.daemonPlans(ctx)
	if err != nil {
		fmt.Fprintf(u.stderr, "warning: could not list running daemons: %v\n", err)
		if u.restart {
			return fmt.Errorf("listing daemons for --restart: %w", err)
		}
		return nil
	}
	writeDaemonPlans(u.stdout, plans, u.restart)
	if !u.restart || len(plans) == 0 {
		return nil
	}
	return u.executeRestart(ctx, exePath, plans)
}

func (u *upgrader) daemonPlans(ctx context.Context) ([]restartPlan, error) {
	states, err := u.listDaemons()
	if err != nil {
		return nil, err
	}
	goos := u.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	var plans []restartPlan
	for _, st := range states {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !state.IsRunning(&st) {
			continue
		}
		var entry *project.Entry
		if home, herr := u.gridHome(); herr == nil {
			entry, herr = service.Lookup(ctx, home, st.StackName)
			if herr != nil {
				entry = nil
			}
		}
		override, unread := u.readOverride(ctx, st)
		plan := buildRestartPlan(st, entry, override, unread, goos)
		if plan.Spec == nil {
			daemon := st
			plan.daemon = &daemon
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func (u *upgrader) listDaemons() ([]state.DaemonState, error) {
	if u.home == "" {
		return state.List()
	}
	dir := u.stateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []state.DaemonState
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path) // #nosec G304 -- path is a state file under the upgrader home
		if err != nil {
			continue
		}
		var st state.DaemonState
		if err := json.Unmarshal(data, &st); err != nil {
			continue
		}
		out = append(out, st)
	}
	return out, nil
}

func (u *upgrader) stateDir() string {
	home, err := u.gridHome()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".gridctl", "state")
}

func (u *upgrader) gridHome() (string, error) {
	if u.home != "" {
		return u.home, nil
	}
	return state.Home()
}

func (u *upgrader) readOverride(ctx context.Context, st state.DaemonState) (string, bool) {
	if err := ctx.Err(); err != nil || st.Port <= 0 {
		return "", true
	}
	return readDaemonOverride(st)
}

// readDaemonOverride reports the override path and whether the status
// endpoint could not be read. An empty path with unread false means the
// daemon is up and merged no override.
func readDaemonOverride(st state.DaemonState) (string, bool) {
	api := newDaemonAPIFor(st, 2*time.Second)
	resp, err := api.Get(api.URL("/api/status"))
	if err != nil {
		return "", true
	}
	defer resp.Body.Close()
	if authErr := api.describeUnauthorized(resp); authErr != nil {
		return "", true
	}
	var body struct {
		Override string `json:"override"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return "", true
	}
	return body.Override, false
}

func (u *upgrader) executeRestart(ctx context.Context, exePath string, plans []restartPlan) error {
	handoff, err := u.prepareVaultHandoff(ctx, exePath, plans)
	if err != nil {
		return u.restartStopped(nil, plans, err)
	}
	var restarted, refused []string
	for i, plan := range plans {
		if err := ctx.Err(); err != nil {
			return u.restartStopped(restarted, plans[i:], err)
		}
		if reason, ok := handoff.refused[i]; ok {
			fmt.Fprintf(u.stderr, "Not restarted: %s (%s)\n", plan.Name, reason)
			refused = append(refused, plan.Name)
			continue
		}
		pass := ""
		if handoff.needs[i] {
			pass = handoff.passphrase
		}
		fmt.Fprintf(u.stdout, "Restarting %s\n", plan.Name)
		if err := u.restartOne(ctx, exePath, plan, pass); err != nil {
			fmt.Fprintf(u.stderr, "Not restarted: %s (%v)\n", plan.Name, err)
			for _, later := range plans[i+1:] {
				fmt.Fprintf(u.stderr, "Not restarted: %s (not attempted)\n", later.Name)
			}
			if len(restarted) > 0 {
				fmt.Fprintf(u.stdout, "Restarted: %s\n", strings.Join(restarted, ", "))
			}
			return fmt.Errorf("restarting %s: %w", plan.Name, err)
		}
		restarted = append(restarted, plan.Name)
		fmt.Fprintf(u.stdout, "Restarted %s\n", plan.Name)
	}
	if len(refused) > 0 {
		if len(restarted) > 0 {
			fmt.Fprintf(u.stdout, "Restarted: %s\n", strings.Join(restarted, ", "))
		}
		return fmt.Errorf("not restarted: %s", strings.Join(refused, ", "))
	}
	return nil
}

const (
	restartRefusedNoPassphrase    = "store is unlocked in the daemon and no passphrase is available here; set GRIDCTL_VAULT_PASSPHRASE_FILE or run in a terminal" // #nosec G101 -- message text, not a secret value
	restartRefusedNoCapability    = "target gridctl does not accept --passphrase-stdin; set GRIDCTL_VAULT_PASSPHRASE_FILE to restart it"                          // #nosec G101 -- message text, not a secret value
	restartRefusedStoreUnreadable = "local variable store could not be read; set GRIDCTL_VAULT_PASSPHRASE_FILE to restart it"                                     // #nosec G101 -- message text, not a secret value
)

// vaultHandoff is the outcome of the pre-restart variable store check.
// needs marks plans whose apply or serve step gets the passphrase on stdin;
// refused maps a plan index to the reason it is skipped.
type vaultHandoff struct {
	passphrase string
	needs      map[int]bool
	refused    map[int]string
}

// restartPrompt prompts for the store passphrase on the terminal and
// verifies it against store. Tests replace it.
var restartPrompt = func(ctx context.Context, store *vault.Store, out io.Writer) (string, error) {
	return controller.PromptPassphrase(ctx, store, os.Stdin, out)
}

// prepareVaultHandoff runs before any destroy. A plain or stackless daemon
// that holds an encrypted store unlocked would come back locked unless the
// new process gets the passphrase. The environment is inherited, so it
// needs nothing more. Otherwise the target binary must accept
// --passphrase-stdin, and on a terminal the passphrase is prompted for once
// and verified. With a target that predates the flag, or without a
// terminal, the daemon is refused and left running. A status read failure
// is a note and the plan runs as before.
func (u *upgrader) prepareVaultHandoff(ctx context.Context, exePath string, plans []restartPlan) (vaultHandoff, error) {
	h := vaultHandoff{needs: map[int]bool{}, refused: map[int]string{}}
	if os.Getenv("GRIDCTL_VAULT_PASSPHRASE") != "" || os.Getenv(vault.PassphraseFileEnv) != "" {
		return h, nil
	}
	for i, plan := range plans {
		if plan.Spec != nil || plan.daemon == nil {
			continue
		}
		encrypted, locked, err := readDaemonVaultStatus(ctx, *plan.daemon)
		if err != nil {
			fmt.Fprintf(u.stderr, "note: could not read the variable store status of %s (%v); restarting it as before\n", plan.Name, err)
			continue
		}
		if encrypted && !locked {
			h.needs[i] = true
		}
	}
	if len(h.needs) == 0 {
		return h, nil
	}
	refuseAll := func(reason string) (vaultHandoff, error) {
		for i := range h.needs {
			h.refused[i] = reason
		}
		h.needs = map[int]bool{}
		return h, nil
	}
	// The capability is checked first: a target that predates the flag
	// cannot restart the daemon even from a terminal, so its refusal names
	// the passphrase file, which works either way.
	if !targetAcceptsPassphraseStdin(ctx, exePath) {
		return refuseAll(restartRefusedNoCapability)
	}
	if u.isTTY == nil || !u.isTTY() {
		return refuseAll(restartRefusedNoPassphrase)
	}
	store, err := loadVault()
	if err != nil {
		// Only the daemons that need the passphrase are refused; the other
		// plans still run.
		fmt.Fprintf(u.stderr, "note: %v\n", err)
		return refuseAll(restartRefusedStoreUnreadable)
	}
	if !store.IsLocked() {
		// The local store is not encrypted, so there is nothing to verify
		// against; a daemon that reported encrypted is refused.
		return refuseAll(restartRefusedNoPassphrase)
	}
	promptCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	pass, err := restartPrompt(promptCtx, store, u.stderr)
	if err != nil {
		return h, err
	}
	h.passphrase = pass
	return h, nil
}

// readDaemonVaultStatus reads /api/var/status from a running daemon.
func readDaemonVaultStatus(ctx context.Context, st state.DaemonState) (encrypted, locked bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	status, err := readGatewayVarStatus(ctx, st)
	if err != nil {
		return false, false, err
	}
	return status.Encrypted, status.Locked, nil
}

// targetAcceptsPassphraseStdin probes the target binary once with
// "apply --help". A binary that predates --passphrase-stdin would reject
// the flag only after destroy had already stopped the daemon.
func targetAcceptsPassphraseStdin(ctx context.Context, exePath string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// #nosec G204 -- exePath is the binary just installed; argv is fixed.
	cmd := exec.CommandContext(probeCtx, exePath, "apply", "--help")
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "--passphrase-stdin")
}

func (u *upgrader) restartStopped(restarted []string, remaining []restartPlan, err error) error {
	for _, plan := range remaining {
		fmt.Fprintf(u.stderr, "Not restarted: %s (not attempted)\n", plan.Name)
	}
	if len(restarted) > 0 {
		fmt.Fprintf(u.stdout, "Restarted: %s\n", strings.Join(restarted, ", "))
	}
	return err
}

// restartOne runs one plan. A non-empty passphrase goes to each apply or
// serve step on a stdin pipe with --passphrase-stdin, never in argv or env.
func (u *upgrader) restartOne(ctx context.Context, exePath string, plan restartPlan, passphrase string) error {
	if plan.Spec != nil {
		return restartSupervisor().Restart(ctx, *plan.Spec)
	}
	for _, args := range plan.Steps {
		if passphrase != "" && len(args) > 0 && (args[0] == "apply" || args[0] == "serve") {
			args = append(append([]string{}, args...), "--passphrase-stdin")
		}
		// #nosec G204 -- exePath is the binary just installed; argv is destroy, apply, or serve.
		cmd := exec.CommandContext(ctx, exePath, args...)
		cmd.Env = os.Environ()
		cmd.Stdout = u.stdout
		cmd.Stderr = u.stderr
		if slices.Contains(args, "--passphrase-stdin") {
			cmd.Stdin = strings.NewReader(passphrase + "\n")
		}
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}
