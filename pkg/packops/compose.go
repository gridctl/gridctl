package packops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/skills"
)

const composedDirName = "composed"

// ComposedStackPath is ~/.gridctl/packs/<base>/composed/stack.yaml.
func ComposedStackPath(home, packName string) string {
	return filepath.Join(PackCheckoutRoot(home, packName), composedDirName, "stack.yaml")
}

// attachedBoosters returns imported boosters whose fragment extends base,
// sorted by pack name. The booster's own record is included.
func attachedBoosters(lf *skills.LockFile, base string) []packSource {
	if lf == nil || base == "" {
		return nil
	}
	var out []packSource
	for _, ps := range packSources(lf) {
		if ps.Pack.Fragment != nil && ps.Pack.Fragment.Base == base {
			out = append(out, ps)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pack.Name < out[j].Pack.Name })
	return out
}

// composeTarget is the path a base daemon should run from. With no boosters
// it is the pinned checkout, exactly as a plain pack apply uses.
func composeTarget(home, base string, st *skills.LockedStack, boosters []packSource) string {
	if len(boosters) == 0 {
		return pinnedStackPath(st)
	}
	return ComposedStackPath(home, base)
}

func (m *Managers) applyPackStack(ctx context.Context, name string, ps packSource, opts ApplyOptions) (Row, stackOutcome) {
	if ps.Pack.Fragment != nil {
		return m.applyBooster(ctx, name, ps, opts)
	}
	lf, err := skills.ReadLockFile(m.lockPath())
	if err != nil {
		return Row{Kind: "stack", Name: stackRowName(ps.Pack.Stack), Action: "error", Detail: err.Error()}, stackOutcome{action: "error"}
	}
	boosters := attachedBoosters(lf, name)
	if len(boosters) == 0 {
		return m.applyStack(ctx, name, ps.Source.Repo, ps.Pack, opts)
	}
	return m.composeAndDeploy(ctx, ps, boosters, opts, false)
}

func (m *Managers) applyBooster(ctx context.Context, name string, ps packSource, opts ApplyOptions) (Row, stackOutcome) {
	rowName := ps.Pack.Fragment.Path
	if m.Launcher == nil {
		return Row{
			Kind: "stack", Name: rowName, Action: "skipped-unavailable",
			Detail:      "stack deploy is not available over this interface",
			Remediation: fmt.Sprintf("run 'gridctl pack apply %s' from the CLI", name),
		}, stackOutcome{action: "skipped-unavailable"}
	}
	lf, err := skills.ReadLockFile(m.lockPath())
	if err != nil {
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: err.Error()}, stackOutcome{action: "error"}
	}
	base, err := findPack(lf, ps.Pack.Fragment.Base)
	if err != nil {
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: err.Error()}, stackOutcome{action: "error"}
	}
	if base.Pack.Stack == nil {
		detail := fmt.Sprintf("base pack %q carries no stack", ps.Pack.Fragment.Base)
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: detail}, stackOutcome{action: "error"}
	}
	boosters := attachedBoosters(lf, ps.Pack.Fragment.Base)
	row, outcome := m.composeAndDeploy(ctx, base, boosters, opts, true)
	if outcome.action == "error" || strings.HasPrefix(outcome.action, "skipped") {
		row.Name = rowName
		return row, outcome
	}
	inner := outcome.action
	row.Kind = "stack"
	row.Name = rowName
	row.Action = "composed"
	row.Detail = fmt.Sprintf("composed into %s (%s)", base.Pack.Name, inner)
	if outcome.detail != "" && opts.PortSet {
		row.Detail = row.Detail + "; " + outcome.detail
	}
	outcome.action = "composed"
	return row, outcome
}

// composeAndDeploy loads the base and attached fragments, writes the composed
// file when needed, and starts, replaces, or reloads the base daemon.
// LaunchOptions are built from the base pack only.
func (m *Managers) composeAndDeploy(ctx context.Context, base packSource, boosters []packSource, opts ApplyOptions, fromBooster bool) (Row, stackOutcome) {
	st := base.Pack.Stack
	rowName := stackRowName(st)
	out := stackOutcome{checkout: st.CheckoutDir}
	if m.Launcher == nil {
		out.action = "skipped-unavailable"
		return Row{
			Kind: "stack", Name: rowName, Action: out.action,
			Detail:      "stack deploy is not available over this interface",
			Remediation: fmt.Sprintf("run 'gridctl pack apply %s' from the CLI", base.Pack.Name),
		}, out
	}
	home, err := m.homeDir()
	if err != nil {
		out.action = "error"
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: err.Error()}, out
	}
	if missing, absent := stackFileMissing(st); absent {
		out.action = "error"
		return Row{
			Kind: "stack", Name: rowName, Action: "error",
			Detail:      fmt.Sprintf("checkout or stack file is missing: %s", missing),
			Remediation: fmt.Sprintf("re-run 'gridctl pack add %s'", base.Source.Repo),
		}, out
	}
	stored, available := m.storedKeys()
	decls := composeDeclarations(base.Pack, boosters)
	if keys, ok := unmetRequired(&skills.LockedPack{Variables: decls}, stored, available); ok {
		out.action = "skipped-variables"
		return skippedVariablesRow(rowName, keys), out
	}
	target := composeTarget(home, base.Pack.Name, st, boosters)
	rendered, layers, renderErr := renderComposition(ctx, base, boosters)
	if renderErr != nil {
		out.action = "error"
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: renderErr.Error()}, out
	}
	run, err := classifyRunningStack(home, base.Pack.Name, st.Name, target)
	if err != nil {
		out.action = "error"
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: err.Error()}, out
	}
	if fromBooster && run.running && opts.PortSet {
		out.detail = fmt.Sprintf("base %s is running on %d; --port ignored", base.Pack.Name, run.port)
	}
	same := run.running && samePath(run.stackFile, target)
	changed := true
	if same && len(rendered) > 0 {
		existing, readErr := os.ReadFile(target)
		changed = readErr != nil || !bytes.Equal(existing, rendered)
	}
	if same && !changed {
		out.action = "unchanged"
		out.port = run.port
		out.portKnown = true
		row := Row{Kind: "stack", Name: rowName, Action: "unchanged"}
		if out.detail != "" {
			row.Detail = out.detail
		}
		return row, out
	}
	replace := false
	switch {
	case run.running && (run.owned || same):
		replace = !same
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
	if same && changed {
		action = "reloaded"
	} else if replace {
		action = "replaced"
	}
	if opts.DryRun {
		switch action {
		case "reloaded":
			action = "would-reload"
		case "replaced":
			action = "would-replace"
			out.port = run.port
			out.portKnown = run.running
		default:
			action = "would-start"
		}
		out.action = action
		return Row{Kind: "stack", Name: rowName, Action: action, Detail: out.detail}, out
	}
	var previous []byte
	if same {
		previous, _ = os.ReadFile(target)
	}
	if err := writeComposed(target, rendered); err != nil {
		out.action = "error"
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: err.Error()}, out
	}
	_ = layers
	port := opts.Port
	if run.running {
		port = run.port
	}
	if action == "reloaded" {
		if reloadErr := m.Launcher.Reload(ctx, st.Name); reloadErr != nil {
			if len(previous) > 0 {
				if restoreErr := writeComposed(target, previous); restoreErr != nil {
					reloadErr = fmt.Errorf("%w (restoring composed file: %v)", reloadErr, restoreErr)
				}
			}
			out.action = "error"
			return Row{Kind: "stack", Name: rowName, Action: "error", Detail: reloadErr.Error()}, out
		}
		out.action = "reloaded"
		out.port = run.port
		out.portKnown = true
		return Row{Kind: "stack", Name: rowName, Action: "reloaded", Detail: out.detail}, out
	}
	override := ""
	if home != "" && base.Pack.Name != "" {
		override = OverridePath(home, base.Pack.Name)
	}
	res, launchErr := m.Launcher.Launch(ctx, LaunchOptions{
		StackPath: target, Port: port, Replace: replace, OverridePath: override,
	})
	if launchErr != nil {
		if keys, ok := missingVariablesFromError(launchErr); ok {
			out.action = "skipped-variables"
			return skippedVariablesRow(rowName, keys), out
		}
		out.action = "error"
		return Row{Kind: "stack", Name: rowName, Action: "error", Detail: launchErr.Error()}, out
	}
	if replace {
		if perr := pruneOtherCheckouts(home, base.Pack.Name, st.CheckoutDir); perr != nil {
			out.action = action
			out.port = res.Port
			out.portKnown = res.Port != 0
			return Row{Kind: "stack", Name: rowName, Action: action, Detail: perr.Error()}, out
		}
	}
	out.action = action
	out.port = res.Port
	out.portKnown = res.Port != 0
	return Row{Kind: "stack", Name: rowName, Action: action, Detail: out.detail}, out
}

func stackRowName(st *skills.LockedStack) string {
	if st == nil {
		return "stack"
	}
	if st.Name != "" {
		return st.Name
	}
	return st.Path
}

func composeDeclarations(base *skills.LockedPack, boosters []packSource) map[string]skills.LockedVariableDeclaration {
	out := map[string]skills.LockedVariableDeclaration{}
	if base != nil {
		for key, decl := range base.Variables {
			out[key] = decl
		}
	}
	for _, b := range boosters {
		if b.Pack == nil {
			continue
		}
		for key, decl := range b.Pack.Variables {
			out[key] = decl
		}
	}
	return out
}

func renderComposition(ctx context.Context, base packSource, boosters []packSource) ([]byte, []config.Layer, error) {
	baseStack, err := loadPinnedStack(ctx, base.Pack.Stack)
	if err != nil {
		return nil, nil, err
	}
	baseLayer := config.Layer{
		Pack: base.Pack.Name, Commit: base.Source.CommitSHA,
		Path: base.Pack.Stack.Path, Dir: base.Pack.Stack.CheckoutDir, Stack: baseStack,
	}
	layers := []config.Layer{baseLayer}
	var fragments []config.Layer
	for _, booster := range boosters {
		frag, err := loadPinnedFragment(ctx, booster.Pack.Fragment)
		if err != nil {
			return nil, nil, err
		}
		layer := config.Layer{
			Pack: booster.Pack.Name, Commit: booster.Source.CommitSHA,
			Path: booster.Pack.Fragment.Path, Dir: booster.Pack.Fragment.CheckoutDir, Stack: frag,
		}
		fragments = append(fragments, layer)
		layers = append(layers, layer)
	}
	composed, err := config.Compose(baseLayer, fragments)
	if err != nil {
		return nil, nil, err
	}
	composed.SetDefaults()
	result := config.ValidateWithIssues(composed)
	if result != nil && result.ErrorCount > 0 {
		for _, issue := range result.Issues {
			if issue.Severity == config.SeverityError {
				return nil, nil, fmt.Errorf("%s: %s", issue.Field, issue.Message)
			}
		}
	}
	rendered, err := config.RenderComposed(composed, layers)
	if err != nil {
		return nil, nil, err
	}
	return rendered, layers, nil
}

func (m *Managers) removeBooster(ctx context.Context, name string, locked *skills.LockedPack, lf *skills.LockFile, opts RemoveOptions) (*Row, bool, error) {
	rowName := locked.Fragment.Path
	home, err := m.homeDir()
	if err != nil {
		return nil, false, err
	}
	base, err := findPack(lf, locked.Fragment.Base)
	if err != nil && !errors.Is(err, ErrNotImported) {
		return nil, false, err
	}
	baseMissing := err != nil || base.Pack == nil || base.Pack.Stack == nil
	var run stackRun
	if !baseMissing {
		run, err = classifyRunningStack(home, locked.Fragment.Base, base.Pack.Stack.Name, ComposedStackPath(home, locked.Fragment.Base))
		if err != nil {
			return nil, false, err
		}
	}
	remaining := attachedBoosters(lf, locked.Fragment.Base)
	var others []packSource
	for _, b := range remaining {
		if b.Pack.Name != name {
			others = append(others, b)
		}
	}
	if opts.DryRun {
		detail := "would remove the checkout"
		if run.running && samePath(run.stackFile, ComposedStackPath(home, locked.Fragment.Base)) {
			if len(others) == 0 {
				detail = "would replace the base daemon onto its pinned stack and remove the checkout"
			} else {
				detail = "would reload the base daemon and remove the checkout"
			}
		}
		return &Row{Kind: "stack", Name: rowName, Action: "would-remove", Detail: detail}, false, nil
	}
	if run.running && samePath(run.stackFile, ComposedStackPath(home, locked.Fragment.Base)) {
		if m.Launcher == nil {
			return &Row{
				Kind: "stack", Name: rowName, Action: "skipped-unavailable",
				Detail:      "stack deploy is not available over this interface",
				Remediation: fmt.Sprintf("run 'gridctl pack remove %s' from the CLI", name),
			}, true, nil
		}
		if len(others) == 0 {
			pinned := pinnedStackPath(base.Pack.Stack)
			res, launchErr := m.Launcher.Launch(ctx, LaunchOptions{
				StackPath: pinned, Port: run.port, Replace: true,
				OverridePath: OverridePath(home, locked.Fragment.Base),
			})
			if launchErr != nil {
				return &Row{Kind: "stack", Name: rowName, Action: "error", Detail: launchErr.Error()}, true, nil
			}
			_ = res
			_ = os.RemoveAll(filepath.Dir(ComposedStackPath(home, locked.Fragment.Base)))
		} else {
			rendered, _, renderErr := renderComposition(ctx, base, others)
			if renderErr != nil {
				return &Row{Kind: "stack", Name: rowName, Action: "error", Detail: renderErr.Error()}, true, nil
			}
			target := ComposedStackPath(home, locked.Fragment.Base)
			previous, _ := os.ReadFile(target)
			if writeErr := writeComposed(target, rendered); writeErr != nil {
				return &Row{Kind: "stack", Name: rowName, Action: "error", Detail: writeErr.Error()}, true, nil
			}
			if reloadErr := m.Launcher.Reload(ctx, base.Pack.Stack.Name); reloadErr != nil {
				if len(previous) > 0 {
					_ = writeComposed(target, previous)
				}
				return &Row{Kind: "stack", Name: rowName, Action: "error", Detail: reloadErr.Error()}, true, nil
			}
		}
	} else if len(others) == 0 {
		_ = os.RemoveAll(filepath.Dir(ComposedStackPath(home, locked.Fragment.Base)))
	}
	if rmErr := os.RemoveAll(PackCheckoutRoot(home, name)); rmErr != nil {
		return &Row{Kind: "stack", Name: rowName, Action: "error", Detail: rmErr.Error()}, true, nil
	}
	inner := "removed"
	if run.running && samePath(run.stackFile, ComposedStackPath(home, locked.Fragment.Base)) {
		if len(others) == 0 {
			inner = "replaced"
		} else {
			inner = "reloaded"
		}
	}
	detail := "removed the checkout"
	if inner == "reloaded" || inner == "replaced" {
		detail = fmt.Sprintf("removed from %s (%s)", locked.Fragment.Base, inner)
	}
	return &Row{Kind: "stack", Name: rowName, Action: "removed", Detail: detail}, false, nil
}

func writeComposed(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}


