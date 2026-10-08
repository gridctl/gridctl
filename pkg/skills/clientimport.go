package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	skipGridctlProjection = "gridctl projection"
	skipDanglingSymlink   = "dangling symlink"
	skipClaudeSyncDir     = "claude.ai sync directory"
	skipAgentSymlink      = "agent symlink target is not under an agents/ directory"
	skipDuplicate         = "duplicate"
)

// ClientCandidate is one skill or agent visible in a client location.
type ClientCandidate struct {
	Kind       string
	Name       string
	Location   string
	Source     string
	Root       string
	RelPath    string
	Resolved   string
	Client     string
	ProvPath   string
	SkipReason string
	KnownName  string
}

// EnumerateClient lists home-scoped skill and agent candidates for client.
// Permission errors are returned. A missing location is skipped. A missing
// project lock is not an error; a newer project lock is.
func EnumerateClient(ctx context.Context, client, home string) ([]ClientCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, ok := ClientLocations(client)
	if !ok {
		return nil, fmt.Errorf("unknown client %q (supported: %s)", client, strings.Join(SupportedImportClients(), ", "))
	}
	recorded, err := recordedProjectionPaths(ctx, home)
	if err != nil {
		return nil, err
	}
	skillRoots := KnownSkillRoots(home)
	agentRoots := KnownAgentRoots(home)
	var candidates []ClientCandidate
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		found, err := enumerateLocation(ctx, row, home, client, recorded, skillRoots, agentRoots)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, found...)
	}
	markDuplicates(candidates)
	return candidates, nil
}

func enumerateLocation(ctx context.Context, row ClientLocation, home, scanned string, recorded map[string]string, skillRoots, agentRoots []resolvedLocation) ([]ClientCandidate, error) {
	visibleRoot := expandHome(home, row.Path)
	info, err := os.Lstat(visibleRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", visibleRoot, err)
	}
	if info.Mode()&os.ModeSymlink == 0 && !info.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(visibleRoot)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", visibleRoot, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(visibleRoot)
	if err != nil {
		resolvedRoot = visibleRoot
	}
	resolvedRoot, _ = filepath.Abs(resolvedRoot)
	var out []ClientCandidate
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate, ok, err := candidateFromEntry(row, scanned, visibleRoot, resolvedRoot, entry.Name(), recorded, skillRoots, agentRoots)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, candidate)
		}
	}
	return out, nil
}

func candidateFromEntry(row ClientLocation, scanned, visibleRoot, resolvedRoot, name string, recorded map[string]string, skillRoots, agentRoots []resolvedLocation) (ClientCandidate, bool, error) {
	visible := filepath.Join(visibleRoot, name)
	info, err := os.Lstat(visible)
	if err != nil {
		return ClientCandidate{}, false, fmt.Errorf("reading %s: %w", visible, err)
	}
	if row.Kind == ResourceKindSkill && info.Mode()&os.ModeSymlink == 0 && !info.IsDir() {
		return ClientCandidate{}, false, nil
	}
	if row.Kind == ResourceKindAgent {
		if info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
			return ClientCandidate{}, false, nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") && info.Mode()&os.ModeSymlink == 0 {
			return ClientCandidate{}, false, nil
		}
	}
	displayName := name
	if row.Kind == ResourceKindAgent {
		displayName = strings.TrimSuffix(name, filepath.Ext(name))
	}
	c := ClientCandidate{
		Kind:      row.Kind,
		Name:      displayName,
		Location:  visible,
		Client:    row.Owner,
		KnownName: row.SourceName,
	}
	if row.SkipReason != "" {
		c.SkipReason = row.SkipReason
		c.Source = row.SourceName
		c.Root = resolvedRoot
		c.RelPath = name
		c.Resolved = visible
		c.ProvPath = visible
		return c, true, nil
	}
	if row.Kind == ResourceKindSkill && row.Path == "~/.claude/skills" && (name == "synced" || name == "anthropic-skills") {
		c.SkipReason = skipClaudeSyncDir
		c.Source = row.SourceName
		c.Root = resolvedRoot
		c.Resolved = visible
		c.ProvPath = visible
		return c, true, nil
	}
	resolved, err := filepath.EvalSymlinks(visible)
	if err != nil {
		c.SkipReason = skipDanglingSymlink
		c.Source = row.SourceName
		c.Root = resolvedRoot
		c.Resolved = visible
		c.ProvPath = visible
		return c, true, nil
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return ClientCandidate{}, false, err
	}
	c.Resolved = resolved
	if skipped, reason := projectionSkip(resolved, visible, recorded); skipped {
		c.SkipReason = reason
		c.Source = row.SourceName
		c.Root = resolvedRoot
		c.ProvPath = visible
		return c, true, nil
	}
	if row.Kind == ResourceKindSkill {
		assignSkillRoot(&c, row, scanned, visible, resolved, skillRoots)
		if c.SkipReason == "" && HasOrigin(resolved) {
			c.SkipReason = skipGridctlProjection
		}
		return c, true, nil
	}
	assignAgentRoot(&c, row, scanned, visible, resolved, agentRoots)
	return c, true, nil
}

func projectionSkip(resolved, visible string, recorded map[string]string) (bool, string) {
	if inside, err := InsideGridctlHome(resolved); err == nil && inside {
		return true, skipGridctlProjection
	}
	if _, ok := recorded[resolved]; ok {
		return true, skipGridctlProjection
	}
	if _, ok := recorded[visible]; ok {
		return true, skipGridctlProjection
	}
	return false, ""
}

func assignSkillRoot(c *ClientCandidate, row ClientLocation, scanned, visible, resolved string, skillRoots []resolvedLocation) {
	parent := filepath.Dir(resolved)
	if known, ok := matchKnownRoot(parent, skillRoots); ok {
		c.Root = known.Path
		c.RelPath = filepath.Base(resolved)
		c.Name = filepath.Base(resolved)
		c.Source = known.SourceName
		c.KnownName = known.SourceName
		c.Client = known.Owner
		c.ProvPath = filepath.Join(known.Path, c.RelPath)
		return
	}
	c.Root = resolved
	c.RelPath = "."
	c.Name = filepath.Base(resolved)
	c.Source = ""
	c.KnownName = ""
	c.Client = scanned
	c.ProvPath = visible
}

func assignAgentRoot(c *ClientCandidate, row ClientLocation, scanned, visible, resolved string, agentRoots []resolvedLocation) {
	parent := filepath.Dir(resolved)
	if filepath.Base(parent) != "agents" {
		c.SkipReason = skipAgentSymlink
		c.Source = row.SourceName
		c.Root = parent
		c.ProvPath = visible
		return
	}
	if known, ok := matchKnownRoot(parent, agentRoots); ok {
		c.Root = known.Path
		c.RelPath = filepath.Base(resolved)
		c.Name = strings.TrimSuffix(filepath.Base(resolved), filepath.Ext(resolved))
		c.Source = known.SourceName
		c.KnownName = known.SourceName
		c.Client = known.Owner
		c.ProvPath = filepath.Join(known.Path, filepath.Base(resolved))
		return
	}
	c.Root = parent
	c.RelPath = filepath.Base(resolved)
	c.Name = strings.TrimSuffix(filepath.Base(resolved), filepath.Ext(resolved))
	c.Source = ""
	c.KnownName = ""
	c.Client = scanned
	c.ProvPath = visible
}

func markDuplicates(candidates []ClientCandidate) {
	seen := map[string]bool{}
	for i := range candidates {
		if candidates[i].SkipReason != "" {
			continue
		}
		key := candidates[i].Kind + "\x00" + candidates[i].Resolved
		if seen[key] {
			candidates[i].SkipReason = skipDuplicate
			continue
		}
		seen[key] = true
	}
}

// CheckedLocations lists the path templates a client scan visits.
func CheckedLocations(client, home string) []string {
	rows, ok := ClientLocations(client)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		path := expandHome(home, row.Path)
		if seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// ClientImportOptions controls one client-import apply.
type ClientImportOptions struct {
	Trust      bool
	Force      bool
	NoActivate bool
	DryRun     bool
}

// ClientImportEntry is one enumerated row after selection is applied.
type ClientImportEntry struct {
	Candidate ClientCandidate
	Action    string
	Reason    string
}

// ClientImportResult is the outcome of applying a selection.
type ClientImportResult struct {
	Entries  []ClientImportEntry
	Warnings []string
	Roots    []string
}

// ApplyClientImport imports selected candidates, batched by resolved root
// and kind. Dry-run does not write. Policy skips stay skips.
func ApplyClientImport(ctx context.Context, imp *Importer, opts ClientImportOptions, selected []ClientCandidate) (*ClientImportResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	res := &ClientImportResult{Warnings: []string{}}
	var importable []ClientCandidate
	for _, c := range selected {
		entry := ClientImportEntry{Candidate: c}
		if c.SkipReason != "" {
			entry.Action = "skipped"
			entry.Reason = c.SkipReason
			res.Entries = append(res.Entries, entry)
			continue
		}
		if opts.DryRun {
			entry.Action = "would import"
			res.Entries = append(res.Entries, entry)
			continue
		}
		importable = append(importable, c)
		res.Entries = append(res.Entries, entry)
	}
	if opts.DryRun || len(importable) == 0 {
		return res, nil
	}
	lf, err := ReadLockFile(imp.lockPath)
	if err != nil {
		return nil, err
	}
	batches, err := batchCandidates(lf, importable)
	if err != nil {
		return nil, err
	}
	sourceByRoot := map[string]string{}
	for _, batch := range batches {
		sourceByRoot[batch.root] = batch.sourceName
	}
	for i := range res.Entries {
		if res.Entries[i].Candidate.Source == "" {
			res.Entries[i].Candidate.Source = sourceByRoot[res.Entries[i].Candidate.Root]
		}
	}
	imported := map[string]bool{}
	skipped := map[string]string{}
	var roots []string
	rootSeen := map[string]bool{}
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prov := map[string]EntryProvenance{}
		var names []string
		for _, c := range batch.entries {
			names = append(names, c.Name)
			prov[c.RelPath] = EntryProvenance{Client: c.Client, Location: c.ProvPath}
		}
		sort.Strings(names)
		iopts := ImportOptions{
			Repo:       batch.root,
			Kind:       SourceKindLocal,
			SourceName: batch.sourceName,
			Trust:      opts.Trust,
			Force:      opts.Force,
			NoActivate: opts.NoActivate,
			Provenance: prov,
		}
		if batch.kind == ResourceKindAgent {
			iopts.ResourceKind = ResourceKindAgent
			iopts.SelectedAgents = names
		} else {
			iopts.ResourceKind = ResourceKindSkill
			iopts.Selected = names
		}
		result, err := imp.Import(ctx, iopts)
		if err != nil {
			return nil, err
		}
		res.Warnings = append(res.Warnings, result.Warnings...)
		for _, importedSkill := range result.Imported {
			imported[ResourceKindSkill+"\x00"+importedSkill.Name+"\x00"+batch.root] = true
		}
		for _, importedAgent := range result.ImportedAgents {
			imported[ResourceKindAgent+"\x00"+importedAgent.Name+"\x00"+batch.root] = true
		}
		for _, sk := range result.Skipped {
			skipped[ResourceKindSkill+"\x00"+sk.Name+"\x00"+batch.root] = sk.Reason
		}
		for _, ag := range result.SkippedAgents {
			skipped[ResourceKindAgent+"\x00"+ag.Name+"\x00"+batch.root] = ag.Reason
		}
		if !rootSeen[batch.root] && (len(result.Imported) > 0 || len(result.ImportedAgents) > 0) {
			rootSeen[batch.root] = true
			roots = append(roots, batch.root)
		}
	}
	for i := range res.Entries {
		c := res.Entries[i].Candidate
		if c.SkipReason != "" || opts.DryRun {
			continue
		}
		key := c.Kind + "\x00" + c.Name + "\x00" + c.Root
		switch {
		case imported[key]:
			res.Entries[i].Action = "imported"
		case skipped[key] != "":
			res.Entries[i].Action = "skipped"
			res.Entries[i].Reason = skipped[key]
		default:
			res.Entries[i].Action = "skipped"
			res.Entries[i].Reason = "not imported"
		}
	}
	res.Roots = roots
	return res, nil
}

type importBatch struct {
	root       string
	kind       string
	sourceName string
	entries    []ClientCandidate
}

func batchCandidates(lf *LockFile, candidates []ClientCandidate) ([]importBatch, error) {
	type key struct {
		root string
		kind string
	}
	order := []key{}
	groups := map[key][]ClientCandidate{}
	names := map[string]string{}
	for _, c := range candidates {
		if _, ok := names[c.Root]; !ok {
			name, err := ResolveSourceName(lf, SourceNameInput{
				Kind:         SourceKindLocal,
				Root:         c.Root,
				KnownName:    c.KnownName,
				ClientImport: true,
			})
			if err != nil {
				return nil, err
			}
			names[c.Root] = name
		}
		k := key{root: c.Root, kind: c.Kind}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], c)
	}
	batches := make([]importBatch, 0, len(order))
	for _, k := range order {
		entries := groups[k]
		for i := range entries {
			entries[i].Source = names[k.root]
		}
		batches = append(batches, importBatch{
			root:       k.root,
			kind:       k.kind,
			sourceName: names[k.root],
			entries:    entries,
		})
	}
	return batches, nil
}

// EntryProvenance is the client and location written onto one origin.
type EntryProvenance struct {
	Client   string
	Location string
}
