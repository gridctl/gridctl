package packops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/skills"
)

func planSummary(plan *lockPlan) *LockSummary {
	if plan == nil {
		return nil
	}
	return plan.Summary
}

// refuseLockedExtends aborts a --locked add when the imported base commit
// does not match the lock, before source clones or writes.
func (m *Managers) refuseLockedExtends(manifest *pack.Manifest, plan *lockPlan, opts AddOptions) error {
	if manifest == nil || !manifest.IsBooster() || !opts.Locked || plan == nil {
		return nil
	}
	locked, ok := plan.Extends[manifest.Stack.Extends]
	if !ok {
		return nil
	}
	lf, err := skills.ReadLockFile(m.lockPath())
	if err != nil {
		return err
	}
	base, err := findPack(lf, manifest.Stack.Extends)
	if err != nil {
		if errors.Is(err, ErrNotImported) {
			return nil
		}
		return err
	}
	_, _, err = extendsLockNote(base, locked, true)
	return err
}

func (m *Managers) resolveFragment(ctx context.Context, clone *skills.CloneResult, manifest *pack.Manifest, resolved *resolvedSelection, dryRun bool) (*StackSummary, []string) {
	token := "stack:" + manifest.Stack.File
	var warnings []string
	fail := func(detail string) (*StackSummary, []string) {
		resolved.unresolved = append(resolved.unresolved, token)
		if resolved.unresolvedDetails == nil {
			resolved.unresolvedDetails = map[string]string{}
		}
		resolved.unresolvedDetails[token] = detail
		warnings = append(warnings, detail)
		return nil, warnings
	}
	lf, err := skills.ReadLockFile(m.lockPath())
	if err != nil {
		return fail(err.Error())
	}
	base, err := findPack(lf, manifest.Stack.Extends)
	if err != nil {
		if errors.Is(err, ErrNotImported) {
			return fail(fmt.Sprintf("base pack %q is not added", manifest.Stack.Extends))
		}
		return fail(err.Error())
	}
	if base.Pack.Fragment != nil {
		return fail(fmt.Sprintf("base pack %q is a booster; a booster cannot extend a booster", manifest.Stack.Extends))
	}
	if base.Pack.Stack == nil {
		return fail(fmt.Sprintf("base pack %q carries no stack", manifest.Stack.Extends))
	}
	if resolved.lockExtends != nil {
		if locked, ok := resolved.lockExtends[manifest.Stack.Extends]; ok {
			note, detail, noteErr := extendsLockNote(base, locked, false)
			if noteErr != nil {
				return fail(noteErr.Error())
			}
			if detail != "" {
				return fail(detail)
			}
			if note != "" && resolved.lock != nil {
				resolved.lock.Notes = append(resolved.lock.Notes, note)
			}
		}
	}
	rel := filepath.FromSlash(manifest.Stack.File)
	stackPath := filepath.Join(clone.RepoPath, rel)
	info, err := os.Stat(stackPath)
	if err != nil || info.IsDir() {
		return fail(fmt.Sprintf("stack file %q not found in the pack repository", manifest.Stack.File))
	}
	fragment, err := config.ValidateFragment(stackPath)
	if err != nil {
		return fail(rewriteFragmentPath(err.Error(), stackPath, manifest.Stack.File))
	}
	if err := m.trialCompose(ctx, manifest, base, fragment, stackPath); err != nil {
		return fail(rewriteFragmentPath(err.Error(), stackPath, manifest.Stack.File))
	}
	summary := &StackSummary{
		Path: manifest.Stack.File, Name: base.Pack.Stack.Name, Servers: len(fragment.MCPServers),
		Fragment: true, Extends: manifest.Stack.Extends,
	}
	if dryRun {
		return summary, warnings
	}
	home, err := m.homeDir()
	if err != nil {
		return fail(err.Error())
	}
	dest, err := filepath.Abs(PackCheckoutDir(home, manifest.Name, clone.CommitSHA))
	if err != nil {
		return fail(err.Error())
	}
	resolved.pendingStack = &pendingStack{
		repoPath: clone.RepoPath, dest: dest, rel: rel, stackPath: stackPath,
		name: base.Pack.Stack.Name, manifestStack: manifest.Stack.File,
		fragment: true, baseName: manifest.Stack.Extends,
	}
	return summary, warnings
}

func rewriteFragmentPath(msg, abs, rel string) string {
	if abs == "" || rel == "" {
		return msg
	}
	return strings.ReplaceAll(msg, abs, rel)
}

func (m *Managers) trialCompose(ctx context.Context, manifest *pack.Manifest, base packSource, fragment *config.Stack, fragmentPath string) error {
	baseStack, err := loadPinnedStack(ctx, base.Pack.Stack)
	if err != nil {
		return err
	}
	lf, err := skills.ReadLockFile(m.lockPath())
	if err != nil {
		return err
	}
	var layers []config.Layer
	for _, booster := range attachedBoosters(lf, manifest.Stack.Extends) {
		if booster.Pack.Name == manifest.Name {
			continue
		}
		frag, err := loadPinnedFragment(ctx, booster.Pack.Fragment)
		if err != nil {
			return err
		}
		layers = append(layers, config.Layer{
			Pack: booster.Pack.Name, Commit: booster.Source.CommitSHA,
			Path: booster.Pack.Fragment.Path, Dir: booster.Pack.Fragment.CheckoutDir, Stack: frag,
		})
	}
	layers = append(layers, config.Layer{
		Pack: manifest.Name, Path: manifest.Stack.File, Dir: filepath.Dir(fragmentPath), Stack: fragment,
	})
	composed, err := config.Compose(config.Layer{
		Pack: base.Pack.Name, Path: base.Pack.Stack.Path, Dir: base.Pack.Stack.CheckoutDir, Stack: baseStack,
	}, layers)
	if err != nil {
		return err
	}
	composed.SetDefaults()
	result := config.ValidateWithIssues(composed)
	if result != nil && result.ErrorCount > 0 {
		for _, issue := range result.Issues {
			if issue.Severity == config.SeverityError {
				if issue.Field != "" {
					return fmt.Errorf("%s: %s", issue.Field, issue.Message)
				}
				return fmt.Errorf("%s", issue.Message)
			}
		}
		return fmt.Errorf("composed stack has %d validation error(s)", result.ErrorCount)
	}
	return nil
}

func loadPinnedStack(ctx context.Context, st *skills.LockedStack) (*config.Stack, error) {
	if st == nil {
		return nil, fmt.Errorf("pinned stack is missing")
	}
	return config.ReadUnresolved(ctx, filepath.Join(st.CheckoutDir, filepath.FromSlash(st.Path)))
}

func loadPinnedFragment(ctx context.Context, frag *skills.LockedFragment) (*config.Stack, error) {
	if frag == nil {
		return nil, fmt.Errorf("pinned fragment is missing")
	}
	path := filepath.Join(frag.CheckoutDir, filepath.FromSlash(frag.Path))
	stack, err := config.ValidateFragment(path)
	if err != nil {
		return nil, err
	}
	return stack, nil
}
