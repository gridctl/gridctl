package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/service"
	"github.com/gridctl/gridctl/pkg/state"
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
		plans = append(plans, buildRestartPlan(st, entry, override, unread, goos))
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
	var restarted []string
	for i, plan := range plans {
		if err := ctx.Err(); err != nil {
			return u.restartStopped(restarted, plans[i:], err)
		}
		fmt.Fprintf(u.stdout, "Restarting %s\n", plan.Name)
		if err := u.restartOne(ctx, exePath, plan); err != nil {
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
	return nil
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

func (u *upgrader) restartOne(ctx context.Context, exePath string, plan restartPlan) error {
	if plan.Spec != nil {
		return restartSupervisor().Restart(ctx, *plan.Spec)
	}
	for _, args := range plan.Steps {
		// #nosec G204 -- exePath is the binary just installed; argv is destroy, apply, or serve.
		cmd := exec.CommandContext(ctx, exePath, args...)
		cmd.Env = os.Environ()
		cmd.Stdout = u.stdout
		cmd.Stderr = u.stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}
