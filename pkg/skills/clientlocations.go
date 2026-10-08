package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gridctl/gridctl/pkg/project"
)

// ResourceKindSkill and ResourceKindAgent restrict an import to one kind.
const (
	ResourceKindSkill = "skill"
	ResourceKindAgent = "agent"
)

// ClientLocation is one home-scoped skill or agent directory a client scans.
// Owner is the provenance slug. SourceName is the lockfile key. They are
// recorded separately and are equal for every v1 row.
type ClientLocation struct {
	Client     string
	Owner      string
	SourceName string
	Kind       string
	Path       string
	Dialect    string
	Docs       string
	SkipReason string
}

// ClientLocations returns the v1 home-scoped locations for client, in
// display order. The second result is false for an unknown client.
//
// Citations:
//   - Claude Code skills: https://code.claude.com/docs/en/skills
//   - Claude Code agents: https://code.claude.com/docs/en/sub-agents
//   - OpenCode skills: https://opencode.ai/docs/skills/
//   - OpenCode agents: https://opencode.ai/docs/agents/
//   - Agents interop dir: https://github.com/vercel-labs/skills
func ClientLocations(client string) ([]ClientLocation, bool) {
	rows := clientLocationTable()
	var out []ClientLocation
	for _, row := range rows {
		if row.Client == client {
			out = append(out, row)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// SupportedImportClients lists the v1 client slugs in the error-message order.
func SupportedImportClients() []string {
	return []string{"agents", "claude-code", "opencode"}
}

func clientLocationTable() []ClientLocation {
	// Owner and SourceName are separate fields. v1 rows set them equal so
	// provenance client and lock key name the same stable location identity.
	return []ClientLocation{
		// https://code.claude.com/docs/en/skills
		{Client: "claude-code", Owner: "claude-code", SourceName: "claude-code", Kind: ResourceKindSkill, Path: "~/.claude/skills", Docs: "https://code.claude.com/docs/en/skills"},
		// https://code.claude.com/docs/en/sub-agents
		{Client: "claude-code", Owner: "claude-code-agents", SourceName: "claude-code-agents", Kind: ResourceKindAgent, Path: "~/.claude/agents", Dialect: "claude", Docs: "https://code.claude.com/docs/en/sub-agents"},
		// https://opencode.ai/docs/skills/
		{Client: "opencode", Owner: "opencode", SourceName: "opencode", Kind: ResourceKindSkill, Path: "~/.config/opencode/skills", Docs: "https://opencode.ai/docs/skills/"},
		{Client: "opencode", Owner: "claude-code", SourceName: "claude-code", Kind: ResourceKindSkill, Path: "~/.claude/skills", Docs: "https://code.claude.com/docs/en/skills"},
		{Client: "opencode", Owner: "agents", SourceName: "agents", Kind: ResourceKindSkill, Path: "~/.agents/skills", Docs: "https://opencode.ai/docs/skills/"},
		// https://opencode.ai/docs/agents/
		{Client: "opencode", Owner: "opencode", SourceName: "opencode", Kind: ResourceKindAgent, Path: "~/.config/opencode/agents", Dialect: "opencode", Docs: "https://opencode.ai/docs/agents/", SkipReason: "OpenCode agent dialect is not imported in this release"},
		// https://github.com/vercel-labs/skills
		{Client: "agents", Owner: "agents", SourceName: "agents", Kind: ResourceKindSkill, Path: "~/.agents/skills", Docs: "https://github.com/vercel-labs/skills"},
	}
}

// KnownSkillRoots returns resolved skill-location directories from the table.
func KnownSkillRoots(home string) []resolvedLocation {
	return resolvedRows(home, ResourceKindSkill)
}

// KnownAgentRoots returns resolved agent-location directories from the table.
func KnownAgentRoots(home string) []resolvedLocation {
	return resolvedRows(home, ResourceKindAgent)
}

type resolvedLocation struct {
	Path       string
	Owner      string
	SourceName string
	Kind       string
}

func resolvedRows(home, kind string) []resolvedLocation {
	seen := map[string]bool{}
	var out []resolvedLocation
	for _, row := range clientLocationTable() {
		if row.Kind != kind {
			continue
		}
		expanded := expandHome(home, row.Path)
		resolved, err := filepath.EvalSymlinks(expanded)
		if err != nil {
			resolved = expanded
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			continue
		}
		if seen[resolved+"\x00"+row.SourceName] {
			continue
		}
		seen[resolved+"\x00"+row.SourceName] = true
		out = append(out, resolvedLocation{Path: resolved, Owner: row.Owner, SourceName: row.SourceName, Kind: row.Kind})
	}
	return out
}

// KnownSourceName returns the location table's source name when root is a
// known skill or agent directory. A direct child does not match.
func KnownSourceName(home, root string) string {
	for _, rows := range [][]resolvedLocation{KnownSkillRoots(home), KnownAgentRoots(home)} {
		if match, ok := matchKnownRoot(root, rows); ok {
			return match.SourceName
		}
	}
	return ""
}

func matchKnownRoot(path string, rows []resolvedLocation) (resolvedLocation, bool) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return resolvedLocation{}, false
	}
	for _, row := range rows {
		if row.Path == resolved {
			return row, true
		}
	}
	return resolvedLocation{}, false
}

// projectionTarget is a directory gridctl may write into. A local import of
// that directory, or of a direct child, gets the projection hint.
type projectionTarget struct {
	Client string
	Path   string
}

func projectionTargets() []projectionTarget {
	return []projectionTarget{
		{Client: "claude-code", Path: "~/.claude/skills"},
		{Client: "agents", Path: "~/.agents/skills"},
		{Client: "antigravity", Path: "~/.gemini/config/skills"},
		{Client: "claude-code", Path: "~/.claude/agents"},
		{Client: "opencode", Path: "~/.config/opencode/agents"},
		{Client: "copilot", Path: "~/.copilot/agents"},
		{Client: "gemini", Path: "~/.gemini/agents"},
	}
}

// ProjectionHintClients returns the client slugs whose projection target
// equals root, is the parent of root, or whose recorded project.Entry.Path
// equals root. A missing project lock is not an error. A newer project lock
// is returned as-is.
func ProjectionHintClients(ctx context.Context, home string, roots []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	recorded, err := recordedProjectionPaths(ctx, home)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var clients []string
	add := func(client string) {
		if client == "" || seen[client] {
			return
		}
		seen[client] = true
		clients = append(clients, client)
	}
	targets := projectionTargets()
	for _, root := range roots {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			resolved = root
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			continue
		}
		parent := filepath.Dir(resolved)
		for _, target := range targets {
			expanded := expandHome(home, target.Path)
			tResolved, err := filepath.EvalSymlinks(expanded)
			if err != nil {
				tResolved = expanded
			}
			tResolved, err = filepath.Abs(tResolved)
			if err != nil {
				continue
			}
			if resolved == tResolved || parent == tResolved {
				add(target.Client)
			}
		}
		if client, ok := recorded[resolved]; ok {
			add(client)
		}
	}
	sort.Strings(clients)
	return clients, nil
}

func recordedProjectionPaths(ctx context.Context, home string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := project.NewStore(home)
	lock, err := store.Load(ctx)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	out := map[string]string{}
	for _, kind := range []project.Kind{project.KindSkill, project.KindAgent} {
		for _, entry := range lock.Entries(kind) {
			if entry == nil || entry.Path == "" {
				continue
			}
			resolved, err := filepath.EvalSymlinks(entry.Path)
			if err != nil {
				resolved = entry.Path
			}
			resolved, err = filepath.Abs(resolved)
			if err != nil {
				continue
			}
			out[resolved] = entry.Client
			out[entry.Path] = entry.Client
		}
	}
	return out, nil
}

// ProjectionHint is the single post-import warning for a colliding client.
func ProjectionHint(client string) string {
	return fmt.Sprintf("hint: projecting these to %s would replace the originals; run 'gridctl skill project sync --client %s' only after reviewing 'gridctl skill project status'", client, client)
}
