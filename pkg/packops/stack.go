package packops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
)

// stackLinkWarning is the add-time notice when a carried stack declares
// its own link block. Pack wiring owns client connection.
const stackLinkWarning = "stack declares link:; pack wiring (wiring:/clients:) applies instead and link: is ignored under pack apply"

// StackLauncher deploys and stops a pack-carried stack. The CLI supplies
// it; the REST server leaves it nil.
type StackLauncher interface {
	// Launch deploys the stack at StackPath as a background daemon and
	// returns the port it listens on. Replace stops a running daemon
	// with the same stack name first.
	Launch(ctx context.Context, opts LaunchOptions) (LaunchResult, error)
	// Stop stops the daemon named by stackName and removes its
	// containers. It must only be called for a daemon whose state file
	// StackFile is under the pack checkout directory.
	Stop(ctx context.Context, stackName string) error
}

// LaunchOptions is one pack-stack deploy.
type LaunchOptions struct {
	StackPath string
	Port      int
	Replace   bool
}

// LaunchResult is the daemon a launch left running.
type LaunchResult struct {
	StackName string
	Port      int
}

// StackSummary is the carried stack as add and preview report it.
type StackSummary struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Servers int    `json:"servers"`
}

// PackCheckoutRoot is ~/.gridctl/packs/<pack> under home.
func PackCheckoutRoot(home, pack string) string {
	return filepath.Join(home, ".gridctl", "packs", pack)
}

// PackCheckoutDir is the pinned checkout for one commit.
func PackCheckoutDir(home, pack, sha string) string {
	return filepath.Join(PackCheckoutRoot(home, pack), sha)
}

// IsPackOwnedPath reports whether path lies under the pack's checkout
// root. This is a path heuristic: the daemon state file records StackFile
// and no pack owner, so a hand-started daemon whose stack file sits in
// that directory is treated as pack-owned.
func IsPackOwnedPath(home, pack, path string) bool {
	if home == "" || pack == "" || path == "" {
		return false
	}
	root, err := filepath.Abs(PackCheckoutRoot(home, pack))
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, filepath.Clean(abs))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// UnmetVariables lists required pack declarations absent from stored.
// available false (locked or unread vault) yields nothing, matching the
// add-time display: the apply gate then falls through to the loader.
func UnmetVariables(declarations map[string]skills.LockedVariableDeclaration, stored map[string]bool, available bool) []VariableRequirement {
	if !available || len(declarations) == 0 {
		return nil
	}
	keys := make([]string, 0, len(declarations))
	for key, declaration := range declarations {
		if declaration.Required != nil && *declaration.Required && !stored[key] {
			keys = append(keys, key)
		}
	}
	return variableRequirements(keys, declarations)
}

func variableRequirements(keys []string, declarations map[string]skills.LockedVariableDeclaration) []VariableRequirement {
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	out := make([]VariableRequirement, 0, len(keys))
	for _, key := range keys {
		declaration := declarations[key]
		typeName := declaration.Type
		if typeName == "" {
			typeName = "string"
		}
		secret := declaration.Secret == nil || *declaration.Secret
		out = append(out, VariableRequirement{
			Key: key, Type: typeName, Secret: secret,
			Description: declaration.Description, Docs: declaration.Docs,
			Command: "gridctl var set " + key,
		})
	}
	return out
}

func variableRemediation(keys []string) string {
	sort.Strings(keys)
	lines := make([]string, len(keys))
	for i, key := range keys {
		lines[i] = "gridctl var set " + key
	}
	return strings.Join(lines, "\n")
}

func (m *Managers) homeDir() (string, error) {
	if m != nil && m.Home != "" {
		return m.Home, nil
	}
	return state.Home()
}

func (m *Managers) storedKeys() (map[string]bool, bool) {
	if m == nil || m.StoredVariables == nil {
		return nil, false
	}
	return m.StoredVariables()
}

type stackRun struct {
	running   bool
	owned     bool
	pinned    bool
	port      int
	stackFile string
	commit    string
}

func classifyRunningStack(home, pack, stackName, pinned string) (stackRun, error) {
	st, err := state.Load(stackName)
	if err != nil {
		if os.IsNotExist(err) {
			return stackRun{}, nil
		}
		return stackRun{}, err
	}
	if st == nil || !state.IsRunning(st) {
		return stackRun{}, nil
	}
	run := stackRun{running: true, port: st.Port, stackFile: st.StackFile, owned: IsPackOwnedPath(home, pack, st.StackFile)}
	run.pinned = samePath(st.StackFile, pinned)
	run.commit = checkoutCommit(home, pack, st.StackFile)
	return run, nil
}

func checkoutCommit(home, pack, stackFile string) string {
	root, err := filepath.Abs(PackCheckoutRoot(home, pack))
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(stackFile)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		return ""
	}
	return parts[0]
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

func pinnedStackPath(st *skills.LockedStack) string {
	if st == nil {
		return ""
	}
	return filepath.Join(st.CheckoutDir, filepath.FromSlash(st.Path))
}

func stackFileMissing(st *skills.LockedStack) (string, bool) {
	if st == nil || st.CheckoutDir == "" {
		return st.CheckoutDir, true
	}
	if info, err := os.Stat(st.CheckoutDir); err != nil || !info.IsDir() {
		return st.CheckoutDir, true
	}
	path := pinnedStackPath(st)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return path, true
	}
	return "", false
}

// stackOutcome is the apply stack step's result, consumed by wiring.
type stackOutcome struct {
	action    string
	port      int
	portKnown bool
	checkout  string
}

func (m *Managers) applyStack(ctx context.Context, name, repo string, locked *skills.LockedPack, opts ApplyOptions) (Row, stackOutcome) {
	st := locked.Stack
	rowName := st.Name
	if rowName == "" {
		rowName = st.Path
	}
	out := stackOutcome{checkout: st.CheckoutDir}
	if m.Launcher == nil {
		out.action = "skipped-unavailable"
		return Row{
			Kind: "stack", Name: rowName, Action: out.action,
			Detail:      "stack deploy is not available over this interface",
			Remediation: fmt.Sprintf("run 'gridctl pack apply %s' from the CLI", name),
		}, out
	}
	if missing, absent := stackFileMissing(st); absent {
		out.action = "error"
		return Row{
			Kind: "stack", Name: rowName, Action: out.action,
			Detail:      fmt.Sprintf("checkout or stack file is missing: %s", missing),
			Remediation: fmt.Sprintf("re-run 'gridctl pack add %s'", repo),
		}, out
	}
	stored, available := m.storedKeys()
	if keys, ok := unmetRequired(locked, stored, available); ok {
		out.action = "skipped-variables"
		return skippedVariablesRow(rowName, keys), out
	}
	home, err := m.homeDir()
	if err != nil {
		out.action = "error"
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: err.Error()}, out
	}
	pinned := pinnedStackPath(st)
	run, err := classifyRunningStack(home, name, st.Name, pinned)
	if err != nil {
		out.action = "error"
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: err.Error()}, out
	}
	if run.running && run.pinned {
		out.action = "unchanged"
		out.port = run.port
		out.portKnown = true
		return Row{Kind: "stack", Name: rowName, Action: "unchanged"}, out
	}
	replace := false
	switch {
	case run.running && run.owned:
		replace = true
	case run.running:
		if !opts.Force {
			out.action = "skipped-running"
			return Row{
				Kind: "stack", Name: rowName, Action: out.action,
				Detail:      fmt.Sprintf("stack '%s' is already running from %s", st.Name, run.stackFile),
				Remediation: fmt.Sprintf("stop it with 'gridctl destroy %s' or re-run with --force", st.Name),
			}, out
		}
		replace = true
	}
	action := "started"
	if replace {
		action = "replaced"
	}
	if opts.DryRun {
		if replace {
			action = "would-replace"
			out.port = run.port
			out.portKnown = run.running
		} else {
			action = "would-start"
		}
		out.action = action
		return Row{Kind: "stack", Name: rowName, Action: action}, out
	}
	res, launchErr := m.Launcher.Launch(ctx, LaunchOptions{StackPath: pinned, Port: opts.Port, Replace: replace})
	if launchErr != nil {
		if keys, ok := missingVariablesFromError(launchErr); ok {
			out.action = "skipped-variables"
			return skippedVariablesRow(rowName, keys), out
		}
		out.action = "error"
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: launchErr.Error()}, out
	}
	if replace {
		if perr := pruneOtherCheckouts(home, name, st.CheckoutDir); perr != nil {
			out.action = "replaced"
			out.port = res.Port
			out.portKnown = res.Port != 0
			return Row{Kind: "stack", Name: rowName, Action: "replaced", Detail: perr.Error()}, out
		}
	}
	out.action = action
	out.port = res.Port
	out.portKnown = res.Port != 0
	return Row{Kind: "stack", Name: rowName, Action: action}, out
}

func unmetRequired(locked *skills.LockedPack, stored map[string]bool, available bool) ([]string, bool) {
	if locked == nil || !available {
		return nil, false
	}
	var keys []string
	for key, declaration := range locked.Variables {
		if declaration.Required != nil && *declaration.Required && !stored[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, len(keys) > 0
}

func skippedVariablesRow(name string, keys []string) Row {
	return Row{
		Kind: "stack", Name: name, Action: "skipped-variables",
		Detail:      "required variables are unset: " + strings.Join(keys, ", "),
		Remediation: variableRemediation(keys),
	}
}

func missingVariablesFromError(err error) ([]string, bool) {
	if err == nil {
		return nil, false
	}
	const prefix = "missing variable(s):"
	msg := err.Error()
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return nil, false
	}
	rest := strings.TrimSpace(msg[idx+len(prefix):])
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	var keys []string
	for _, key := range strings.Split(rest, ",") {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys, len(keys) > 0
}

func pruneOtherCheckouts(home, pack, keepDir string) error {
	root := PackCheckoutRoot(home, pack)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	states, err := state.List()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if samePath(dir, keepDir) || checkoutReferenced(states, dir) {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("pruning checkout %s: %w", dir, err)
		}
	}
	return nil
}

func checkoutReferenced(states []state.DaemonState, dir string) bool {
	for i := range states {
		st := &states[i]
		if !state.IsRunning(st) {
			continue
		}
		if samePath(st.StackFile, dir) || pathWithin(dir, st.StackFile) {
			return true
		}
	}
	return false
}

func pathWithin(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	absRoot, err1 := filepath.Abs(root)
	absPath, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// statusStackRow is the stack row for one pack, or nil when the pack
// carries no stack. attention is true for stale, drifted, and target-missing.
func statusStackRow(home, packName string, st *skills.LockedStack) (Row, bool) {
	if st == nil {
		return Row{}, false
	}
	name := st.Name
	if name == "" {
		name = st.Path
	}
	if missing, absent := stackFileMissing(st); absent {
		return Row{
			Kind: "stack", Name: name, State: "target-missing",
			Detail:      fmt.Sprintf("checkout or stack file is missing: %s", missing),
			Remediation: "re-run 'gridctl pack add'",
		}, true
	}
	run, err := classifyRunningStack(home, packName, st.Name, pinnedStackPath(st))
	if err != nil {
		return Row{Kind: "stack", Name: name, State: "target-missing", Detail: err.Error(), Remediation: "re-run 'gridctl pack add'"}, true
	}
	if !run.running {
		return Row{Kind: "stack", Name: name, State: "missing"}, false
	}
	if run.pinned {
		return Row{Kind: "stack", Name: name, State: "in-sync"}, false
	}
	if run.owned {
		detail := "daemon is running from an older pack checkout"
		if run.commit != "" {
			detail = fmt.Sprintf("daemon is running from checkout %s", run.commit)
		}
		return Row{
			Kind: "stack", Name: name, State: "stale",
			Detail:      detail,
			Remediation: fmt.Sprintf("re-run 'gridctl pack apply %s'", packName),
		}, true
	}
	return Row{
		Kind: "stack", Name: name, State: "drifted",
		Detail: fmt.Sprintf("daemon is running from %s", run.stackFile),
	}, true
}

// removeStack decides the stack row and whether the checkout may be deleted.
// A nil row means no stack row (a non-pack daemon is left running).
// block is true when removal must stop before dropping the pack record.
func (m *Managers) removeStack(ctx context.Context, name string, st *skills.LockedStack, dryRun bool) (row *Row, block bool, err error) {
	if st == nil {
		return nil, false, nil
	}
	home, err := m.homeDir()
	if err != nil {
		return nil, false, err
	}
	rowName := st.Name
	if rowName == "" {
		rowName = st.Path
	}
	run, err := classifyRunningStack(home, name, st.Name, pinnedStackPath(st))
	if err != nil {
		return nil, false, err
	}
	if run.running && !run.owned {
		if dryRun {
			return nil, false, nil
		}
		if rmErr := os.RemoveAll(PackCheckoutRoot(home, name)); rmErr != nil {
			return nil, true, rmErr
		}
		return nil, false, nil
	}
	if dryRun {
		detail := "would remove the checkout"
		if run.running {
			detail = "would stop the pack daemon and remove the checkout"
		}
		return &Row{Kind: "stack", Name: rowName, Action: "would-remove", Detail: detail}, false, nil
	}
	if run.running && run.owned && m.Launcher == nil {
		return &Row{
			Kind: "stack", Name: rowName, Action: "skipped-unavailable",
			Detail:      "stack deploy is not available over this interface",
			Remediation: fmt.Sprintf("gridctl destroy %s", st.Name),
		}, false, nil
	}
	if run.running && run.owned {
		if stopErr := m.Launcher.Stop(ctx, st.Name); stopErr != nil {
			return &Row{Kind: "stack", Name: rowName, Action: "error", Detail: stopErr.Error()}, true, nil
		}
	}
	checkout := PackCheckoutRoot(home, name)
	if rmErr := os.RemoveAll(checkout); rmErr != nil {
		return &Row{Kind: "stack", Name: rowName, Action: "error", Detail: rmErr.Error()}, true, nil
	}
	detail := "removed the checkout"
	if run.running {
		detail = "stopped the pack daemon and removed the checkout"
	}
	return &Row{Kind: "stack", Name: rowName, Action: "removed", Detail: detail}, false, nil
}
