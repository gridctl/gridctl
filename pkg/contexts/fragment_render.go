package contexts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/project"
)

// Per-client fragment renders for multi-file passthrough targets. The
// contract mirrors pkg/agentsync: pure, deterministic, and honest — keys a
// dialect cannot express are dropped and NAMED, never half-converted.
//
// Adoptability follows render kind: only the identity render (claude-code)
// can flow a hand edit back into the canonical fragment; lossy renders
// refuse adopt so dialect output never overwrites canon.

// fragmentRendered is one fragment rendered for one client.
type fragmentRendered struct {
	data []byte
	// dropped names frontmatter keys the client's dialect cannot express.
	dropped []string
}

// fragmentFilePrefix marks projected fragment files as gridctl's in shared
// rules directories that also hold user files. Ownership is enforced by the
// lockfile; the prefix is legibility (and keeps names clear of the legacy
// single-file gridctl.md).
const fragmentFilePrefix = "gridctl-"

// multiFileCapable reports whether a target takes per-fragment files in
// fragments mode. Only the four clients with verified global rules
// directories (2026-08-01 market check): Claude Code, VS Code Copilot,
// Cline, Roo. Continue and every other dedicated-file target stay
// compiled; Cursor remains unsupported.
func multiFileCapable(t Target) bool {
	switch t.Slug {
	case "claude-code", "vscode", "cline", "roo":
		return true
	default:
		return false
	}
}

// usesMultiFile reports whether a target actually receives per-fragment
// files on this machine: statically capable AND its rules directory's
// parent already exists. Detection can fire on a different tree (Cline
// detects on ~/.agents too), and sync must never create a client tree
// wholesale; such targets fall back to compiled.
func (m *Manager) usesMultiFile(t Target) bool {
	if !multiFileCapable(t) {
		return false
	}
	parent := filepath.Dir(fragmentTargetDir(t, m.home))
	info, err := os.Stat(parent)
	return err == nil && info.IsDir()
}

// fragmentTargetDir is the client rules directory fragments land in.
// Paths are explicit (not derived from the single-file gridctl.md path)
// so Cline's multi-file dir (~/Documents/Cline/Rules) can differ from its
// single-file block target (~/.agents/AGENTS.md).
func fragmentTargetDir(t Target, home string) string {
	switch t.Slug {
	case "claude-code":
		return expandHome(home, "~/.claude/rules")
	case "vscode":
		return expandHome(home, "~/.copilot/instructions")
	case "roo":
		return expandHome(home, "~/.roo/rules")
	case "cline":
		// Verified 2026-08-01: Cline global rules live under Documents.
		return expandHome(home, "~/Documents/Cline/Rules")
	default:
		return filepath.Dir(t.targetPath(home))
	}
}

// fragmentFileName is the projected file name for one fragment.
func fragmentFileName(t Target, name string) string {
	if t.Slug == "vscode" {
		// Copilot only loads *.instructions.md from its instructions dir.
		return fragmentFilePrefix + name + ".instructions.md"
	}
	return fragmentFilePrefix + name + ".md"
}

// fragmentRenderIdentity reports whether a target's render is the identity
// (adoptable) form. Claude Code's rules dialect is gridctl's fragment
// format; every other multi-file dialect is lossy.
func fragmentRenderIdentity(t Target) bool {
	return t.Slug == "claude-code"
}

// renderFragmentFor renders one fragment in a client's native dialect.
func renderFragmentFor(t Target, f *Fragment) fragmentRendered {
	switch t.Slug {
	case "claude-code":
		// Identity: Claude Code's rules dialect IS gridctl's fragment
		// format (paths: frontmatter included — its own format, though its
		// user-scope path scoping has known open bugs; see docs).
		return fragmentRendered{data: append([]byte(nil), f.Raw...)}
	case "vscode":
		return renderFragmentCopilot(f)
	default:
		return renderFragmentPlain(f)
	}
}

// renderFragmentCopilot emits Copilot's .instructions.md dialect: applyTo
// from the paths globs ("**" when unscoped), description passed through,
// every other frontmatter key dropped and named.
func renderFragmentCopilot(f *Fragment) fragmentRendered {
	var out fragmentRendered
	applyTo := "**"
	if len(f.Paths) > 0 {
		applyTo = strings.Join(f.Paths, ", ")
	}
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "applyTo: %q\n", applyTo)
	if f.Description != "" {
		fmt.Fprintf(&b, "description: %q\n", f.Description)
	}
	b.WriteString("---\n")
	if body := strings.TrimSpace(f.Body); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	out.data = []byte(b.String())
	for _, extra := range f.Extra {
		out.dropped = append(out.dropped, extra.Key)
	}
	sort.Strings(out.dropped)
	return out
}

// renderFragmentPlain emits body-only markdown for clients whose rules
// files carry no frontmatter (roo, continue): all metadata is dropped and
// named, paths first since that is the scoping loss that matters.
func renderFragmentPlain(f *Fragment) fragmentRendered {
	var out fragmentRendered
	body := strings.TrimSpace(f.Body)
	if body != "" {
		out.data = []byte(body + "\n")
	} else {
		out.data = []byte{}
	}
	if len(f.Paths) > 0 {
		out.dropped = append(out.dropped, "paths")
	}
	if f.Description != "" {
		out.dropped = append(out.dropped, "description")
	}
	for _, extra := range f.Extra {
		out.dropped = append(out.dropped, extra.Key)
	}
	sort.Strings(out.dropped)
	return out
}

// fragmentDropDetail phrases a lossy render honestly, distinguishing
// "this client does not support path scoping" from generic key loss.
func fragmentDropDetail(t Target, dropped []string) string {
	if len(dropped) == 0 {
		return ""
	}
	hasPaths := false
	rest := make([]string, 0, len(dropped))
	for _, k := range dropped {
		if k == "paths" {
			hasPaths = true
			continue
		}
		rest = append(rest, k)
	}
	parts := make([]string, 0, 2)
	if hasPaths {
		parts = append(parts, fmt.Sprintf("%s does not support path scoping; written unscoped", t.Name))
	}
	if len(rest) > 0 {
		parts = append(parts, "dropped frontmatter keys "+strings.Join(rest, ", "))
	}
	return strings.Join(parts, "; ")
}

// backupFragmentFile copies a fragment (or projected fragment file) to
// <home>/.gridctl/project-backups/context/<scope>/<name>/<ts>-<base>.
// Out of tree on purpose: a sibling backup inside a client-scanned rules
// directory would surface as a live phantom rule, and one inside the
// fragments store would compose into every compiled target.
func backupFragmentFile(home, scope, name, srcPath string) (string, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("reading %s for backup: %w", srcPath, err)
	}
	dir := filepath.Join(home, ".gridctl", "project-backups", "context", scope, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating backup directory: %w", err)
	}
	dest := filepath.Join(dir, time.Now().UTC().Format(backupTimeFormat)+"-"+filepath.Base(srcPath))
	if err := project.AtomicWriteFile(dest, data); err != nil {
		return "", err
	}
	if backups, err := filepath.Glob(filepath.Join(dir, "*")); err == nil {
		sort.Strings(backups)
		for _, stale := range project.StaleBackups(backups, project.MaxBackups) {
			_ = os.Remove(stale)
		}
	}
	return dest, nil
}
