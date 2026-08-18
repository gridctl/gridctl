package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/gridctl/gridctl/pkg/output"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/runtime"
	"github.com/gridctl/gridctl/pkg/runtime/docker"
	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
	"github.com/gridctl/gridctl/pkg/wiring"

	"github.com/spf13/cobra"
)

// Exit codes — matched against the conventions in cmd/gridctl/optimize.go
// and cmd/gridctl/activate.go so CI scripts can rely on a stable contract.
const (
	doctorExitOK     = 0
	doctorExitErrors = 1
	doctorExitFailed = 2
)

// Check statuses. Words, not just colors, so meaning survives NO_COLOR.
const (
	doctorStatusOK   = "ok"
	doctorStatusWarn = "warn"
	doctorStatusFail = "fail"
	doctorStatusInfo = "info"
)

const doctorProbeTimeout = 2 * time.Second

var (
	doctorJSON  bool
	doctorQuiet bool
)

// doctorCheck is one verdict line of the report.
type doctorCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// doctorReport is the machine-readable shape of `gridctl doctor --json`.
// The schema is backward compatible within 0.x: fields may be added,
// never removed or retyped without a clearly-labeled release.
type doctorReport struct {
	OK           bool          `json:"ok"`
	ErrorCount   int           `json:"error_count"`
	WarningCount int           `json:"warning_count"`
	Checks       []doctorCheck `json:"checks"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the environment and report actionable problems",
	Long: `Runs opinionated health checks against the local environment: container
runtime, socket reachability, gateway port, Node.js availability for client
bridges, state directory hygiene, vault status, projection lockfile
generation, wiring ownership, and per-server MCP protocol generation. Each
check renders a verdict with a remediation hint.

Where 'gridctl info' reports facts and always exits 0, doctor judges and
exits non-zero when something needs fixing.

Exit codes:
  0  no errors (warnings allowed)
  1  one or more errors
  2  doctor itself failed to run`,
	Example: `  gridctl doctor               Run all checks
  gridctl doctor --json        Machine-readable report
  gridctl doctor -q            Only print failures and warnings`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
		defer cancel()

		report := runDoctorChecks(ctx)
		exit := renderDoctorReport(os.Stdout, report, doctorJSON, doctorQuiet)
		if exit != doctorExitOK {
			os.Exit(exit)
		}
		return nil
	},
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Output the report as JSON")
	doctorCmd.Flags().BoolVarP(&doctorQuiet, "quiet", "q", false, "Only print failing and warning checks")
}

// runDoctorChecks executes every check and aggregates the report.
func runDoctorChecks(ctx context.Context) doctorReport {
	var checks []doctorCheck

	checkHome(&checks)
	info := checkRuntimeDetect(&checks)
	checkRuntimeSocket(ctx, &checks, info)
	checkRuntimeVersion(&checks, info)
	checkGatewayPort(ctx, &checks)
	checkNpx(&checks)
	checkStateDir(ctx, &checks)
	checkStaleState(ctx, &checks)
	checkProjectLockfile(ctx, &checks)
	checkClientWiring(ctx, &checks)
	checkProtocolGenerations(ctx, &checks)
	checkVault(ctx, &checks)

	return summarizeDoctor(checks)
}

// summarizeDoctor folds check statuses into the report counters.
func summarizeDoctor(checks []doctorCheck) doctorReport {
	report := doctorReport{Checks: checks}
	for _, c := range checks {
		switch c.Status {
		case doctorStatusFail:
			report.ErrorCount++
		case doctorStatusWarn:
			report.WarningCount++
		}
	}
	report.OK = report.ErrorCount == 0
	return report
}

func checkRuntimeDetect(checks *[]doctorCheck) *runtime.RuntimeInfo {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{Explicit: runtimeFlag})
	if err != nil {
		*checks = append(*checks, doctorCheck{
			ID:      "runtime.detect",
			Status:  doctorStatusFail,
			Message: "no container runtime detected; install Docker or Podman",
		})
		return nil
	}
	msg := info.DisplayName()
	if info.Version != "" {
		msg += " " + info.Version
	}
	*checks = append(*checks, doctorCheck{ID: "runtime.detect", Status: doctorStatusOK, Message: msg})
	return info
}

func checkRuntimeSocket(ctx context.Context, checks *[]doctorCheck, info *runtime.RuntimeInfo) {
	if info == nil {
		*checks = append(*checks, doctorCheck{ID: "runtime.socket", Status: doctorStatusInfo, Message: "skipped (no runtime detected)"})
		return
	}
	dr, err := docker.NewWithInfo(info)
	if err != nil {
		*checks = append(*checks, doctorCheck{
			ID:      "runtime.socket",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("could not create client for %s: %v", info.SocketPath, err),
		})
		return
	}
	defer func() { _ = dr.Client().Close() }()

	pingCtx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	if _, err := dr.Client().Ping(pingCtx); err != nil {
		*checks = append(*checks, doctorCheck{
			ID:      "runtime.socket",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("%s not responding (is the %s daemon running?)", info.SocketPath, info.DisplayName()),
		})
		return
	}
	*checks = append(*checks, doctorCheck{ID: "runtime.socket", Status: doctorStatusOK, Message: info.SocketPath})
}

func checkRuntimeVersion(checks *[]doctorCheck, info *runtime.RuntimeInfo) {
	if info == nil {
		*checks = append(*checks, doctorCheck{ID: "runtime.version", Status: doctorStatusInfo, Message: "skipped (no runtime detected)"})
		return
	}
	if !info.IsSupportedPodmanVersion() {
		*checks = append(*checks, doctorCheck{
			ID:      "runtime.version",
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("podman %s is below 4.0; multi-container networking needs netavark (podman 4+)", info.Version),
		})
		return
	}
	if info.IsRootless() && !info.HasNetavark {
		*checks = append(*checks, doctorCheck{
			ID:      "runtime.version",
			Status:  doctorStatusWarn,
			Message: "rootless podman without netavark; install netavark and aardvark-dns for inter-container networking",
		})
		return
	}
	msg := "meets the supported version floor"
	if info.Version != "" {
		msg = fmt.Sprintf("%s %s meets the supported version floor", info.DisplayName(), info.Version)
	}
	*checks = append(*checks, doctorCheck{ID: "runtime.version", Status: doctorStatusOK, Message: msg})
}

func checkGatewayPort(ctx context.Context, checks *[]doctorCheck) {
	const port = 8180
	dialCtx, cancel := context.WithTimeout(ctx, doctorProbeTimeout)
	defer cancel()
	var dialer net.Dialer
	conn, err := dialer.DialContext(dialCtx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "port.gateway", Status: doctorStatusOK, Message: fmt.Sprintf("port %d is free", port)})
		return
	}
	_ = conn.Close()

	// Something is listening; fine if it is one of our own daemons.
	states, _ := state.List()
	for _, s := range states {
		if s.Port == port && state.IsRunning(&s) {
			*checks = append(*checks, doctorCheck{
				ID:      "port.gateway",
				Status:  doctorStatusOK,
				Message: fmt.Sprintf("port %d is served by gridctl stack %q", port, s.StackName),
			})
			return
		}
	}
	*checks = append(*checks, doctorCheck{
		ID:      "port.gateway",
		Status:  doctorStatusWarn,
		Message: fmt.Sprintf("port %d is in use by another process; pass --port to 'gridctl apply' or 'gridctl serve'", port),
	})
}

func checkNpx(checks *[]doctorCheck) {
	if provisioner.NpxAvailable() {
		*checks = append(*checks, doctorCheck{ID: "npx", Status: doctorStatusOK, Message: "npx found (mcp-remote bridges available)"})
		return
	}
	*checks = append(*checks, doctorCheck{
		ID:      "npx",
		Status:  doctorStatusWarn,
		Message: "npx not found; some 'gridctl link' targets need the mcp-remote bridge (install Node.js: https://nodejs.org/)",
	})
}

// checkHome reports the resolved home and its source, so a GRIDCTL_HOME
// override is always visible where users troubleshoot. An unresolvable
// home is a hard failure: every other check derives paths from it.
func checkHome(checks *[]doctorCheck) {
	home, err := state.Home()
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "home", Status: doctorStatusFail, Message: err.Error()})
		return
	}
	msg := home
	if state.HomeOverridden() {
		msg += " (from " + state.HomeEnv + ")"
	}
	*checks = append(*checks, doctorCheck{ID: "home", Status: doctorStatusInfo, Message: msg})
}

func checkStateDir(ctx context.Context, checks *[]doctorCheck) {
	if err := ctx.Err(); err != nil {
		*checks = append(*checks, doctorCheck{ID: "state.dir", Status: doctorStatusInfo, Message: "skipped (cancelled)"})
		return
	}
	dir, err := state.StateDir()
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "state.dir", Status: doctorStatusFail, Message: fmt.Sprintf("cannot resolve state directory: %v", err)})
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		*checks = append(*checks, doctorCheck{ID: "state.dir", Status: doctorStatusFail, Message: fmt.Sprintf("%s not writable: %v", dir, err)})
		return
	}
	probe, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "state.dir", Status: doctorStatusFail, Message: fmt.Sprintf("%s not writable: %v", dir, err)})
		return
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	*checks = append(*checks, doctorCheck{ID: "state.dir", Status: doctorStatusOK, Message: dir + " is writable"})
}

func checkStaleState(ctx context.Context, checks *[]doctorCheck) {
	if err := ctx.Err(); err != nil {
		*checks = append(*checks, doctorCheck{ID: "state.stale", Status: doctorStatusInfo, Message: "skipped (cancelled)"})
		return
	}
	states, err := state.List()
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "state.stale", Status: doctorStatusWarn, Message: fmt.Sprintf("could not read state files: %v", err)})
		return
	}
	var stale []string
	for _, s := range states {
		if !state.IsRunning(&s) {
			stale = append(stale, s.StackName)
		}
	}
	if len(stale) > 0 {
		*checks = append(*checks, doctorCheck{
			ID:      "state.stale",
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("stale state for stopped stack(s): %s (clean up with 'gridctl destroy <name>')", strings.Join(stale, ", ")),
		})
		return
	}
	*checks = append(*checks, doctorCheck{ID: "state.stale", Status: doctorStatusOK, Message: "no stale state files"})
}

// checkProjectLockfile reports the projection lockfile state: the
// unified pkg/project lockfile, legacy pre-unification lockfiles that
// migrate on the next sync, or the corrupt case of a migration
// tombstone with no unified file behind it.
func checkProjectLockfile(ctx context.Context, checks *[]doctorCheck) {
	if err := ctx.Err(); err != nil {
		*checks = append(*checks, doctorCheck{ID: "project.lockfile", Status: doctorStatusInfo, Message: "skipped (cancelled)"})
		return
	}
	home, err := state.Home()
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "project.lockfile", Status: doctorStatusWarn, Message: fmt.Sprintf("home directory unavailable: %v", err)})
		return
	}
	st := project.InspectLockfiles(home)
	switch {
	case st.Unified:
		*checks = append(*checks, doctorCheck{ID: "project.lockfile", Status: doctorStatusOK, Message: "unified project lockfile in use (" + st.UnifiedPath + ")"})
	case len(st.Tombstones) > 0:
		*checks = append(*checks, doctorCheck{
			ID:     "project.lockfile",
			Status: doctorStatusFail,
			Message: fmt.Sprintf("migration tombstone at %s but %s is missing; restore it from %s, or start over with 'gridctl reset'",
				strings.Join(st.Tombstones, ", "), st.UnifiedPath, st.BackupRoot),
		})
	case st.LegacySkill || st.LegacyContext:
		*checks = append(*checks, doctorCheck{ID: "project.lockfile", Status: doctorStatusOK, Message: "legacy projection lockfiles in use (migrate to " + st.UnifiedPath + " on the next sync)"})
	default:
		*checks = append(*checks, doctorCheck{ID: "project.lockfile", Status: doctorStatusOK, Message: "no projection lockfile yet (nothing projected)"})
	}
}

// checkClientWiring reports per-client wiring ownership state: detected
// clients that are not linked, recorded entries that drifted or lost
// their target, and foreign gridctl-named entries. Advisory only —
// wiring problems warn, never fail (Article: doctor's hard failures are
// reserved for runtime and state integrity).
func checkClientWiring(ctx context.Context, checks *[]doctorCheck) {
	if err := ctx.Err(); err != nil {
		*checks = append(*checks, doctorCheck{ID: "wiring.clients", Status: doctorStatusInfo, Message: "skipped (cancelled)"})
		return
	}
	mgr, err := wiring.NewManager()
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "wiring.clients", Status: doctorStatusWarn, Message: fmt.Sprintf("home directory unavailable: %v", err)})
		return
	}
	rows, err := mgr.Statuses(ctx, wiring.StatusOptions{Port: resolveGatewayPort(0)})
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "wiring.clients", Status: doctorStatusWarn, Message: err.Error()})
		return
	}
	if len(rows) == 0 {
		*checks = append(*checks, doctorCheck{ID: "wiring.clients", Status: doctorStatusOK, Message: "no supported LLM clients detected"})
		return
	}
	for _, r := range rows {
		id := "wiring." + r.Client
		switch r.State {
		case wiring.StateInSync:
			*checks = append(*checks, doctorCheck{ID: id, Status: doctorStatusOK, Message: fmt.Sprintf("'%s' entry in sync (%s)", r.Name, r.Target)})
		case wiring.StateMissing:
			*checks = append(*checks, doctorCheck{ID: id, Status: doctorStatusWarn, Message: fmt.Sprintf("detected but not linked; link it with 'gridctl link %s'", r.Client)})
		default:
			msg := fmt.Sprintf("'%s' entry is %s", r.Name, r.State)
			if r.Detail != "" {
				msg += ": " + r.Detail
			}
			*checks = append(*checks, doctorCheck{ID: id, Status: doctorStatusWarn, Message: msg})
		}
	}
}

// checkProtocolGenerations reports each running stack's per-server
// negotiated MCP protocol generation and flags mixed-generation
// bridging. Purely informational when nothing is running.
func checkProtocolGenerations(ctx context.Context, checks *[]doctorCheck) {
	if err := ctx.Err(); err != nil {
		*checks = append(*checks, doctorCheck{ID: "mcp.generation", Status: doctorStatusInfo, Message: "skipped (cancelled)"})
		return
	}
	states, err := state.List()
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "mcp.generation", Status: doctorStatusWarn, Message: fmt.Sprintf("could not read state files: %v", err)})
		return
	}
	running := false
	for _, s := range states {
		if !state.IsRunning(&s) {
			continue
		}
		running = true
		servers := queryMCPServers(s.Port)
		if len(servers) == 0 {
			*checks = append(*checks, doctorCheck{
				ID:      "mcp.generation",
				Status:  doctorStatusInfo,
				Message: fmt.Sprintf("%s: no MCP servers reported (daemon unreachable or empty stack)", s.StackName),
			})
			continue
		}
		var parts []string
		generations := map[string]bool{}
		for _, srv := range servers {
			generation := srv.ProtocolGeneration
			if generation == "" {
				generation = "n/a"
			} else {
				generations[generation] = true
			}
			parts = append(parts, fmt.Sprintf("%s=%s", srv.Name, generation))
		}
		msg := fmt.Sprintf("%s: %s", s.StackName, strings.Join(parts, ", "))
		if len(generations) > 1 {
			msg += " (mixed generations; the gateway bridges between them)"
		}
		*checks = append(*checks, doctorCheck{ID: "mcp.generation", Status: doctorStatusOK, Message: msg})
	}
	if !running {
		*checks = append(*checks, doctorCheck{ID: "mcp.generation", Status: doctorStatusInfo, Message: "no running stacks to inspect"})
	}
}

func checkVault(ctx context.Context, checks *[]doctorCheck) {
	if err := ctx.Err(); err != nil {
		*checks = append(*checks, doctorCheck{ID: "vault", Status: doctorStatusInfo, Message: "skipped (cancelled)"})
		return
	}
	vaultDir, err := state.VaultDir()
	if err != nil {
		*checks = append(*checks, doctorCheck{ID: "vault", Status: doctorStatusWarn, Message: fmt.Sprintf("could not resolve vault dir: %v", err)})
		return
	}
	store := vault.NewStore(vaultDir)
	if err := store.Load(); err != nil {
		*checks = append(*checks, doctorCheck{ID: "vault", Status: doctorStatusWarn, Message: fmt.Sprintf("could not read vault: %v", err)})
		return
	}
	switch {
	case store.IsLocked():
		*checks = append(*checks, doctorCheck{ID: "vault", Status: doctorStatusInfo, Message: "vault is locked (encrypted at rest); unlock via the web UI or GRIDCTL_VAULT_PASSPHRASE"})
	case len(store.Keys()) > 0:
		*checks = append(*checks, doctorCheck{ID: "vault", Status: doctorStatusInfo, Message: fmt.Sprintf("vault is unlocked (%d variable(s), plaintext on disk)", len(store.Keys()))})
	default:
		*checks = append(*checks, doctorCheck{ID: "vault", Status: doctorStatusInfo, Message: "no vault configured"})
	}
}

// renderDoctorReport writes the report and returns the process exit code.
func renderDoctorReport(w io.Writer, report doctorReport, asJSON, quiet bool) int {
	if asJSON {
		if err := output.EncodeJSON(w, report); err != nil {
			fmt.Fprintln(os.Stderr, "doctor: encoding report:", err)
			return doctorExitFailed
		}
	} else {
		renderDoctorHuman(w, report, quiet)
	}
	if report.ErrorCount > 0 {
		return doctorExitErrors
	}
	return doctorExitOK
}

func renderDoctorHuman(w io.Writer, report doctorReport, quiet bool) {
	color := output.ColorEnabled(os.Stdout)
	fmt.Fprintln(w)
	for _, c := range report.Checks {
		if quiet && c.Status != doctorStatusFail && c.Status != doctorStatusWarn {
			continue
		}
		fmt.Fprintf(w, "  %s %-16s %s\n", doctorStatusLabel(c.Status, color), c.ID, c.Message)
	}
	fmt.Fprintf(w, "\nResult: %d error(s), %d warning(s)\n", report.ErrorCount, report.WarningCount)
}

// doctorStatusLabel pads the status word to a fixed width before styling so
// alignment survives the ANSI escapes.
func doctorStatusLabel(status string, color bool) string {
	padded := fmt.Sprintf("%-4s", status)
	if !color {
		return padded
	}
	var c lipgloss.Color
	switch status {
	case doctorStatusOK:
		c = output.ColorGreen
	case doctorStatusWarn:
		c = output.ColorAmber
	case doctorStatusFail:
		c = output.ColorRed
	default:
		c = output.ColorMuted
	}
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(padded)
}
