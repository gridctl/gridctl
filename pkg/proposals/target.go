package proposals

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/ctxpins"
	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/skills"
)

// Target is one governed document as the store sees it. Implementations
// keep the store kind-agnostic. Paths are slash-separated and relative to
// the target root (the skill directory, the fragment file's directory, or
// the hook directory).
type Target interface {
	Kind() string
	Name() string
	// Stack is the skill pin stack, or empty for stackless kinds.
	Stack() string
	// CurrentHash is the composite hash of the current content, in the
	// form the kind's pin store produces.
	CurrentHash(ctx context.Context) (string, error)
	// PinStatus is pinned, drift, or unpinned.
	PinStatus(ctx context.Context) (string, error)
	// ReadFiles validates each path against the kind's rules and returns
	// the current bytes of those that exist. A missing key is an absent
	// file. A refused path wraps ErrDiffUnsupported.
	ReadFiles(ctx context.Context, paths []string) (map[string][]byte, error)
	Provenance(ctx context.Context) (Provenance, error)
	// Scan returns advisory findings for the post-apply content.
	Scan(ctx context.Context, applied map[string][]byte, enabled bool, ignore []string) ([]pins.Finding, error)
	// PostApplyHash parses the post-apply content and returns the
	// composite hash the pin store will compute after Write, including
	// any render normalization the write performs. A parse or validation
	// failure wraps ErrApplyFailed.
	PostApplyHash(ctx context.Context, applied map[string][]byte) (string, error)
	// Write writes the applied files in order. On failure the returned
	// restore puts back every file it touched and reports which files
	// were restored and which are still changed.
	Write(ctx context.Context, applied map[string][]byte) (restore func() (restored, stillChanged []string), err error)
	// Repin re-pins the target and appends the approvals log entry
	// through the kind's pin store.
	Repin(ctx context.Context, req RepinRequest) error
}

// RepinRequest carries the approval written to the pin store on the apply
// path. ExpectedHash is the post-apply composite hash.
type RepinRequest struct {
	ExpectedHash     string
	Reason           string
	Evidence         []string
	ApprovedBy       string
	ApprovedBySource string
}

// Resolver builds the Target for a kind and name.
type Resolver interface {
	Resolve(ctx context.Context, kind, name string) (Target, error)
}

// fileWriteError names the file whose write failed.
type fileWriteError struct {
	File string
	Err  error
}

func (e *fileWriteError) Error() string { return fmt.Sprintf("writing %s: %v", e.File, e.Err) }
func (e *fileWriteError) Unwrap() error { return e.Err }

// Targets resolves targets from the stores the caller already built. The
// CLI and the API fill it; pkg/proposals never constructs a store that
// resolves the real home. Registry and SkillPins must be loaded. LockPath
// is skills.lock.yaml; empty skips lockfile provenance.
type Targets struct {
	Registry    *registry.Store
	SkillPins   *skillpins.Store
	Stack       string
	LockPath    string
	Context     *contexts.Manager
	ContextPins *ctxpins.Store
	HookPins    *hookpins.Store
}

var hookNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Resolve validates the name for its kind and checks that the target
// exists. A missing target wraps ErrTargetNotFound; an invalid name wraps
// ErrInvalidInput.
func (t *Targets) Resolve(ctx context.Context, kind, name string) (Target, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch kind {
	case KindSkill:
		return t.resolveSkill(name)
	case KindContext:
		return t.resolveContext(name)
	case KindHook:
		return t.resolveHook(name)
	case KindAgent:
		return nil, fmt.Errorf("proposals: %w: kind agent is reserved; agent pins do not exist yet", ErrKindUnsupported)
	default:
		return nil, fmt.Errorf("proposals: %w: kind must be skill, context, or hook", ErrInvalidInput)
	}
}

func (t *Targets) resolveSkill(name string) (Target, error) {
	if err := registry.ValidateSkillName(name); err != nil {
		return nil, fmt.Errorf("proposals: %w: skill name: %v", ErrInvalidInput, err)
	}
	if t.Registry == nil {
		return nil, fmt.Errorf("proposals: %w: skill registry", ErrStoreUnavailable)
	}
	sk, err := t.Registry.GetSkill(name)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, fmt.Errorf("proposals: skill %q: %w", name, ErrTargetNotFound)
		}
		return nil, fmt.Errorf("proposals: reading skill %q: %w", name, err)
	}
	return &skillTarget{
		name:       name,
		dir:        filepath.Join(t.Registry.Dir(), "skills", sk.Dir),
		reg:        t.Registry,
		pins:       t.SkillPins,
		stack:      t.Stack,
		lockPath:   t.LockPath,
		writeFile:  t.Registry.WriteFile,
		deleteFile: t.Registry.DeleteFile,
		saveSkill:  t.Registry.SaveSkill,
	}, nil
}

func (t *Targets) resolveContext(name string) (Target, error) {
	if t.Context == nil {
		return nil, fmt.Errorf("proposals: %w: context store", ErrStoreUnavailable)
	}
	ct := &contextTarget{name: name, mgr: t.Context, pins: t.ContextPins, lockPath: t.LockPath}
	if t.Context.FragmentsActive() {
		if name == ctxpins.CanonicalName {
			return nil, fmt.Errorf("proposals: context %q: fragments mode is on; target a fragment name: %w", name, ErrTargetNotFound)
		}
		if err := contexts.ValidateFragmentName(name); err != nil {
			return nil, fmt.Errorf("proposals: %w: %v", ErrInvalidInput, err)
		}
		ct.file = name + ".md"
		ct.path = filepath.Join(t.Context.FragmentsDir(), ct.file)
	} else {
		if name != ctxpins.CanonicalName {
			if err := contexts.ValidateFragmentName(name); err != nil {
				return nil, fmt.Errorf("proposals: %w: context target must be a fragment name or %s: %v", ErrInvalidInput, ctxpins.CanonicalName, err)
			}
			return nil, fmt.Errorf("proposals: context %q: fragments mode is off, so the only context target is %s: %w", name, ctxpins.CanonicalName, ErrTargetNotFound)
		}
		ct.canonical = true
		ct.file = ctxpins.CanonicalName
		ct.path = t.Context.CanonicalPath()
	}
	info, err := os.Lstat(ct.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("proposals: context %q: %w", name, ErrTargetNotFound)
		}
		return nil, fmt.Errorf("proposals: reading context %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("proposals: context %q is not a regular file: %w", name, ErrTargetNotFound)
	}
	return ct, nil
}

func (t *Targets) resolveHook(name string) (Target, error) {
	if !hookNamePattern.MatchString(name) {
		return nil, fmt.Errorf("proposals: %w: hook name %q must be lowercase letters, digits, and hyphens", ErrInvalidInput, name)
	}
	if t.Registry == nil {
		return nil, fmt.Errorf("proposals: %w: hook registry", ErrStoreUnavailable)
	}
	ht := &hookTarget{
		name:        name,
		registryDir: t.Registry.Dir(),
		dir:         filepath.Join(t.Registry.Dir(), "hooks", name),
		pins:        t.HookPins,
		lockPath:    t.LockPath,
	}
	if _, err := os.Stat(filepath.Join(ht.dir, "hook.json")); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("proposals: hook %q: %w", name, ErrTargetNotFound)
		}
		return nil, fmt.Errorf("proposals: reading hook %q: %w", name, err)
	}
	return ht, nil
}

// readExisting returns the bytes at full when it is a regular UTF-8 file,
// (nil, false, nil) when absent, and ErrDiffUnsupported for a symlink, a
// directory, an irregular file, or binary content.
func readExisting(full, rel string) ([]byte, bool, error) {
	info, err := os.Lstat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("proposals: reading %s: %w", rel, err)
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, false, fmt.Errorf("%w: %s is a symbolic link", ErrDiffUnsupported, rel)
	case info.IsDir():
		return nil, false, fmt.Errorf("%w: %s is a directory", ErrDiffUnsupported, rel)
	case !info.Mode().IsRegular():
		return nil, false, fmt.Errorf("%w: %s is not a regular file", ErrDiffUnsupported, rel)
	}
	data, err := os.ReadFile(full) // #nosec G304 -- full is a validated relative path joined to the target root
	if err != nil {
		return nil, false, fmt.Errorf("proposals: reading %s: %w", rel, err)
	}
	if !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return nil, false, fmt.Errorf("%w: %s is not UTF-8 text", ErrDiffUnsupported, rel)
	}
	return data, true, nil
}

// checkParents refuses a path whose parent components exist as anything
// other than a real directory under root.
func checkParents(root, rel string) error {
	parts := strings.Split(rel, "/")
	cur := root
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("proposals: reading %s: %w", rel, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s is under a symbolic link", ErrDiffUnsupported, rel)
		}
		if !info.IsDir() {
			return fmt.Errorf("%w: %s is under a file, not a directory", ErrDiffUnsupported, rel)
		}
	}
	return nil
}

// skillTarget is a registry skill. The write hooks are the registry
// methods; tests replace them to inject failures.
type skillTarget struct {
	name     string
	dir      string
	reg      *registry.Store
	pins     *skillpins.Store
	stack    string
	lockPath string

	writeFile  func(name, path string, data []byte) error
	deleteFile func(name, path string) error
	saveSkill  func(sk *registry.AgentSkill) error
}

func (t *skillTarget) Kind() string  { return KindSkill }
func (t *skillTarget) Name() string  { return t.name }
func (t *skillTarget) Stack() string { return t.stack }

func (t *skillTarget) CurrentHash(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sk, err := t.reg.GetSkill(t.name)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return "", fmt.Errorf("proposals: skill %q: %w", t.name, ErrTargetNotFound)
		}
		return "", err
	}
	skillHash, files, err := skillpins.ComputeDigests(t.reg, sk)
	if err != nil {
		return "", fmt.Errorf("proposals: hashing skill %q: %w", t.name, err)
	}
	return skillpins.CompositeHash(skillHash, files), nil
}

func (t *skillTarget) PinStatus(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.pins == nil {
		return "", fmt.Errorf("proposals: %w: skill pin store", ErrStoreUnavailable)
	}
	res, err := t.pins.Verify(t.name, t.reg)
	if err != nil {
		if errors.Is(err, skillpins.ErrNotPinned) {
			return PinStatusUnpinned, nil
		}
		return "", err
	}
	if res.Status == skillpins.StatusDrift {
		return PinStatusDrift, nil
	}
	return PinStatusPinned, nil
}

// nestedRoots lists subdirectories (with a trailing slash) that hold
// their own SKILL.md, matching skillpins.ComputeManifest.
func (t *skillTarget) nestedRoots() ([]string, error) {
	entries, err := t.reg.ListFiles(t.name)
	if err != nil {
		return nil, fmt.Errorf("proposals: listing files for skill %q: %w", t.name, err)
	}
	var roots []string
	for _, f := range entries {
		p := filepath.ToSlash(f.Path)
		if strings.HasSuffix(p, "/SKILL.md") {
			roots = append(roots, strings.TrimSuffix(p, "SKILL.md"))
		}
	}
	return roots, nil
}

func (t *skillTarget) ReadFiles(ctx context.Context, paths []string) (map[string][]byte, error) {
	roots, err := t.nestedRoots()
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(paths))
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := validateSkillPath(p, roots); err != nil {
			return nil, err
		}
		if err := checkParents(t.dir, p); err != nil {
			return nil, err
		}
		data, ok, err := readExisting(filepath.Join(t.dir, filepath.FromSlash(p)), p)
		if err != nil {
			return nil, err
		}
		if ok {
			out[p] = data
		}
	}
	return out, nil
}

// validateSkillPath applies the skill-only rules on top of cleanPatchPath:
// no SKILL.md below the root (a nested skill) and nothing under an
// existing nested skill. It also asserts agreement with
// skillpins.DigestedFile so every written file is hashed.
func validateSkillPath(p string, nestedRoots []string) error {
	if p != "SKILL.md" && (path.Base(p) == "SKILL.md") {
		return fmt.Errorf("%w: %s would create a nested skill", ErrDiffUnsupported, p)
	}
	for _, root := range nestedRoots {
		if strings.HasPrefix(p, root) {
			return fmt.Errorf("%w: %s is inside the nested skill %s", ErrDiffUnsupported, p, strings.TrimSuffix(root, "/"))
		}
	}
	if p != "SKILL.md" && !skillpins.DigestedFile(registry.SkillFile{Path: p}) {
		return fmt.Errorf("%w: %s is not part of the pinned file set", ErrDiffUnsupported, p)
	}
	return nil
}

func (t *skillTarget) Provenance(ctx context.Context) (Provenance, error) {
	if err := ctx.Err(); err != nil {
		return Provenance{}, err
	}
	var lf *skills.LockFile
	if t.lockPath != "" {
		read, err := skills.ReadLockFile(t.lockPath)
		if err != nil {
			return Provenance{}, fmt.Errorf("proposals: reading skills lockfile: %w", err)
		}
		lf = read
	}
	origin, err := skills.ReadOrigin(t.dir)
	if err != nil {
		origin = nil
		if !errors.Is(err, fs.ErrNotExist) {
			// A sidecar that exists but does not parse still marks an import.
			origin = &skills.Origin{}
		}
	}
	if pack := skillPack(lf, t.name); pack != "" {
		return Provenance{Kind: ProvenancePack, Pack: pack}, nil
	}
	if origin.IsLocal() {
		return Provenance{Kind: ProvenanceLocalImport, SourceDir: originSourceDir(origin.Repo, origin.Path)}, nil
	}
	owners := skills.SkillOwners(lf, t.name)
	for _, owner := range owners {
		src := lf.Sources[owner]
		if src.IsLocal() {
			return Provenance{Kind: ProvenanceLocalImport, SourceDir: originSourceDir(src.Repo, src.Skills[t.name].Path)}, nil
		}
	}
	if origin != nil || len(owners) > 0 {
		return Provenance{Kind: ProvenanceGit}, nil
	}
	return Provenance{Kind: ProvenanceLocal}, nil
}

// skillPack returns the pack that owns a skill: a pack record whose
// selection lists it, a pack record source whose Skills map lists it, or
// a pack member source. The first match in sorted source order wins.
func skillPack(lf *skills.LockFile, name string) string {
	if lf == nil {
		return ""
	}
	keys := make([]string, 0, len(lf.Sources))
	for k := range lf.Sources {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		src := lf.Sources[k]
		if src.Pack == nil {
			continue
		}
		for _, s := range src.Pack.Skills {
			if s == name {
				return src.Pack.Name
			}
		}
	}
	for _, owner := range skills.SkillOwners(lf, name) {
		src := lf.Sources[owner]
		if src.PackMember != "" {
			return src.PackMember
		}
		if src.Pack != nil && src.Pack.Name != "" {
			return src.Pack.Name
		}
	}
	return ""
}

func originSourceDir(repo, rel string) string {
	if rel == "" || rel == "." {
		return repo
	}
	return filepath.Join(repo, filepath.FromSlash(rel))
}

// postApplySkill parses the post-apply SKILL.md, or returns the current
// skill when SKILL.md is not touched. The name may not change.
func (t *skillTarget) postApplySkill(applied map[string][]byte) (*registry.AgentSkill, error) {
	cur, err := t.reg.GetSkill(t.name)
	if err != nil {
		return nil, err
	}
	data, ok := applied["SKILL.md"]
	if !ok {
		return cur, nil
	}
	parsed, err := registry.ParseSkillMD(data)
	if err != nil {
		return nil, fmt.Errorf("%w: SKILL.md does not parse: %v", ErrApplyFailed, err)
	}
	if parsed.Name != "" && parsed.Name != t.name {
		return nil, fmt.Errorf("%w: SKILL.md renames the skill to %q", ErrApplyFailed, parsed.Name)
	}
	parsed.Name = t.name
	parsed.Dir = cur.Dir
	if err := parsed.Validate(); err != nil {
		return nil, fmt.Errorf("%w: SKILL.md is not valid: %v", ErrApplyFailed, err)
	}
	return parsed, nil
}

func (t *skillTarget) Scan(ctx context.Context, applied map[string][]byte, enabled bool, ignore []string) ([]pins.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	sk, err := t.postApplySkill(applied)
	if err != nil {
		return nil, err
	}
	findings := pins.ScanSkill(sk.Name, sk.Description, sk.Body)
	for _, p := range sortedKeys(applied) {
		if p == "SKILL.md" {
			continue
		}
		findings = append(findings, pins.ScanSkill(t.name, "", string(applied[p]))...)
	}
	return pins.FilterFindings(findings, ignore), nil
}

// overlaySource is the registry with the post-apply skill and files laid
// over it, so the post-apply hash goes through skillpins.ComputeDigests.
type overlaySource struct {
	reg     *registry.Store
	name    string
	skill   *registry.AgentSkill
	applied map[string][]byte
}

func (o overlaySource) ListSkills() []*registry.AgentSkill { return o.reg.ListSkills() }

func (o overlaySource) GetSkill(name string) (*registry.AgentSkill, error) {
	if name == o.name {
		cp := *o.skill
		return &cp, nil
	}
	return o.reg.GetSkill(name)
}

func (o overlaySource) ListFiles(name string) ([]registry.SkillFile, error) {
	files, err := o.reg.ListFiles(name)
	if err != nil || name != o.name {
		return files, err
	}
	have := map[string]bool{}
	for _, f := range files {
		have[filepath.ToSlash(f.Path)] = true
	}
	for _, p := range sortedKeys(o.applied) {
		if p == "SKILL.md" || have[p] {
			continue
		}
		files = append(files, registry.SkillFile{Path: filepath.FromSlash(p), Size: int64(len(o.applied[p]))})
	}
	return files, nil
}

func (o overlaySource) ReadFile(name, p string) ([]byte, error) {
	if name == o.name {
		if data, ok := o.applied[filepath.ToSlash(p)]; ok {
			return data, nil
		}
	}
	return o.reg.ReadFile(name, p)
}

func (t *skillTarget) PostApplyHash(ctx context.Context, applied map[string][]byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sk, err := t.postApplySkill(applied)
	if err != nil {
		return "", err
	}
	// SaveSkill writes RenderSkillMD and caches the parse of what it wrote,
	// so hash that round trip rather than the applied bytes.
	rendered, err := registry.RenderSkillMD(sk)
	if err != nil {
		return "", fmt.Errorf("%w: rendering SKILL.md: %v", ErrApplyFailed, err)
	}
	canonical, err := registry.ParseSkillMD(rendered)
	if err != nil {
		return "", fmt.Errorf("%w: reparsing SKILL.md: %v", ErrApplyFailed, err)
	}
	canonical.Name = sk.Name
	canonical.Dir = sk.Dir
	src := overlaySource{reg: t.reg, name: t.name, skill: canonical, applied: applied}
	skillHash, files, err := skillpins.ComputeDigests(src, canonical)
	if err != nil {
		return "", fmt.Errorf("proposals: hashing post-apply skill %q: %w", t.name, err)
	}
	return skillpins.CompositeHash(skillHash, files), nil
}

// Write writes supporting files first through the registry and SKILL.md
// last through SaveSkill. Prior bytes are read before the first write.
// Writes are not atomic across files; the restore closure puts back every
// file written so far, including the one that failed.
func (t *skillTarget) Write(ctx context.Context, applied map[string][]byte) (func() ([]string, []string), error) {
	sk, err := t.postApplySkill(applied)
	if err != nil {
		return nil, err
	}
	order := make([]string, 0, len(applied))
	for _, p := range sortedKeys(applied) {
		if p != "SKILL.md" {
			order = append(order, p)
		}
	}
	if _, ok := applied["SKILL.md"]; ok {
		order = append(order, "SKILL.md")
	}
	type prior struct {
		data   []byte
		exists bool
	}
	priors := make(map[string]prior, len(order))
	for _, p := range order {
		data, ok, err := readExisting(filepath.Join(t.dir, filepath.FromSlash(p)), p)
		if err != nil {
			return nil, err
		}
		priors[p] = prior{data: data, exists: ok}
	}
	var touched []string
	restore := func() (restored, stillChanged []string) {
		for i := len(touched) - 1; i >= 0; i-- {
			p := touched[i]
			pr := priors[p]
			var rerr error
			if pr.exists {
				rerr = t.writeFile(t.name, p, pr.data)
			} else if derr := t.deleteFile(t.name, p); derr != nil && !errors.Is(derr, registry.ErrNotFound) {
				rerr = derr
			}
			if rerr != nil {
				stillChanged = append(stillChanged, p)
			} else {
				restored = append(restored, p)
			}
		}
		sort.Strings(restored)
		sort.Strings(stillChanged)
		return restored, stillChanged
	}
	for _, p := range order {
		touched = append(touched, p)
		if err := ctx.Err(); err != nil {
			return restore, &fileWriteError{File: p, Err: err}
		}
		if p == "SKILL.md" {
			cp := *sk
			if err := t.saveSkill(&cp); err != nil {
				return restore, &fileWriteError{File: p, Err: err}
			}
			continue
		}
		if err := t.writeFile(t.name, p, applied[p]); err != nil {
			return restore, &fileWriteError{File: p, Err: err}
		}
	}
	return nil, nil
}

func (t *skillTarget) Repin(ctx context.Context, req RepinRequest) error {
	if t.pins == nil {
		return fmt.Errorf("proposals: %w: skill pin store", ErrStoreUnavailable)
	}
	return t.pins.Approve(ctx, t.name, t.reg, skillpins.ApproveRequest{
		ExpectedHash:     req.ExpectedHash,
		Reason:           req.Reason,
		Evidence:         req.Evidence,
		ApprovedBy:       req.ApprovedBy,
		ApprovedBySource: req.ApprovedBySource,
	})
}

// contextTarget is one fragment, or AGENTS.md when fragments mode is off.
type contextTarget struct {
	name      string
	file      string
	path      string
	canonical bool
	mgr       *contexts.Manager
	pins      *ctxpins.Store
	lockPath  string
}

func (t *contextTarget) Kind() string  { return KindContext }
func (t *contextTarget) Name() string  { return t.name }
func (t *contextTarget) Stack() string { return "" }

func (t *contextTarget) raw() ([]byte, error) {
	data, ok, err := readExisting(t.path, t.file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("proposals: context %q: %w", t.name, ErrTargetNotFound)
	}
	return data, nil
}

func (t *contextTarget) CurrentHash(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := t.raw()
	if err != nil {
		return "", err
	}
	return ctxpins.CompositeHash(ctxpins.Hash(data)), nil
}

// source is the pin document source the CLI and API build: every pin
// document with pack provenance from the skills lockfile.
func (t *contextTarget) source(ctx context.Context) (ctxpins.Source, error) {
	docs, err := t.mgr.PinDocuments(ctx)
	if err != nil {
		return nil, fmt.Errorf("proposals: reading context documents: %w", err)
	}
	out := make([]ctxpins.Document, 0, len(docs))
	for _, d := range docs {
		out = append(out, ctxpins.Document{
			Name:        d.Name,
			Description: d.Description,
			Body:        d.Body,
			Raw:         d.Raw,
			Pack:        d.Pack,
			ReadErr:     d.ReadErr,
		})
	}
	return ctxpins.DocSource{Docs: ctxpins.AnnotatePack(out, t.lockPath)}, nil
}

func (t *contextTarget) PinStatus(ctx context.Context) (string, error) {
	if t.pins == nil {
		return "", fmt.Errorf("proposals: %w: context pin store", ErrStoreUnavailable)
	}
	src, err := t.source(ctx)
	if err != nil {
		return "", err
	}
	res, err := t.pins.Verify(ctx, t.name, src)
	if err != nil {
		if errors.Is(err, ctxpins.ErrNotPinned) {
			return PinStatusUnpinned, nil
		}
		return "", err
	}
	if res.Status == ctxpins.StatusDrift {
		return PinStatusDrift, nil
	}
	return PinStatusPinned, nil
}

func (t *contextTarget) ReadFiles(ctx context.Context, paths []string) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, p := range paths {
		if p != t.file {
			return nil, fmt.Errorf("%w: a context proposal may only change %s, not %s", ErrDiffUnsupported, t.file, p)
		}
		data, err := t.raw()
		if err != nil {
			return nil, err
		}
		out[p] = data
	}
	return out, nil
}

func (t *contextTarget) Provenance(ctx context.Context) (Provenance, error) {
	if err := ctx.Err(); err != nil {
		return Provenance{}, err
	}
	docs := ctxpins.AnnotatePack([]ctxpins.Document{{Name: t.name}}, t.lockPath)
	if len(docs) == 1 && docs[0].Pack != "" {
		return Provenance{Kind: ProvenancePack, Pack: docs[0].Pack}, nil
	}
	return Provenance{Kind: ProvenanceLocal}, nil
}

func (t *contextTarget) Scan(ctx context.Context, applied map[string][]byte, enabled bool, ignore []string) ([]pins.Finding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	data, ok := applied[t.file]
	if !ok {
		return nil, nil
	}
	body := string(data)
	if !t.canonical {
		f, err := contexts.ParseFragment(t.name, data)
		if err != nil {
			return nil, fmt.Errorf("%w: %s does not parse: %v", ErrApplyFailed, t.file, err)
		}
		body = f.Body
	}
	return pins.FilterFindings(pins.ScanSkill(t.name, "", body), ignore), nil
}

// rendered returns the bytes the write will put on disk: the fragment
// render for fragments mode, or the applied bytes for AGENTS.md.
func (t *contextTarget) rendered(applied map[string][]byte) (*contexts.Fragment, []byte, error) {
	data, ok := applied[t.file]
	if !ok {
		return nil, nil, fmt.Errorf("%w: diff does not change %s", ErrApplyFailed, t.file)
	}
	if t.canonical {
		if err := contexts.ValidateCanonicalContent(string(data)); err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrApplyFailed, err)
		}
		return nil, data, nil
	}
	f, err := contexts.ParseFragment(t.name, data)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s does not parse: %v", ErrApplyFailed, t.file, err)
	}
	out, err := contexts.RenderFragmentMD(f)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: rendering %s: %v", ErrApplyFailed, t.file, err)
	}
	return f, out, nil
}

func (t *contextTarget) PostApplyHash(ctx context.Context, applied map[string][]byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	_, out, err := t.rendered(applied)
	if err != nil {
		return "", err
	}
	// SaveCanonical normalizes trailing newlines; Hash trims them, so the
	// applied bytes hash the same as what lands on disk.
	return ctxpins.CompositeHash(ctxpins.Hash(out)), nil
}

// Write is one atomic file write, so a failure leaves the file unchanged
// and the restore closure has nothing to put back.
func (t *contextTarget) Write(ctx context.Context, applied map[string][]byte) (func() ([]string, []string), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, out, err := t.rendered(applied)
	if err != nil {
		return nil, err
	}
	noop := func() ([]string, []string) { return nil, nil }
	if t.canonical {
		if err := t.mgr.SaveCanonical(string(out)); err != nil {
			return noop, &fileWriteError{File: t.file, Err: err}
		}
		return nil, nil
	}
	if err := t.mgr.SaveFragment(f); err != nil {
		if errors.Is(err, contexts.ErrFragmentsInactive) {
			return nil, fmt.Errorf("proposals: context %q: fragments mode is off: %w", t.name, ErrTargetNotFound)
		}
		return noop, &fileWriteError{File: t.file, Err: err}
	}
	return nil, nil
}

func (t *contextTarget) Repin(ctx context.Context, req RepinRequest) error {
	if t.pins == nil {
		return fmt.Errorf("proposals: %w: context pin store", ErrStoreUnavailable)
	}
	src, err := t.source(ctx)
	if err != nil {
		return err
	}
	return t.pins.Approve(ctx, t.name, src, ctxpins.ApproveRequest{
		ExpectedHash:     req.ExpectedHash,
		Reason:           req.Reason,
		Evidence:         req.Evidence,
		ApprovedBy:       req.ApprovedBy,
		ApprovedBySource: req.ApprovedBySource,
	})
}

// hookTarget is an imported hook. Hooks are record-only: the store never
// writes or re-pins one, so Write, Repin, and PostApplyHash refuse.
type hookTarget struct {
	name        string
	registryDir string
	dir         string
	pins        *hookpins.Store
	lockPath    string
}

func (t *hookTarget) Kind() string  { return KindHook }
func (t *hookTarget) Name() string  { return t.name }
func (t *hookTarget) Stack() string { return "" }

func (t *hookTarget) store() *hookpins.Store {
	if t.pins != nil {
		return t.pins
	}
	return hookpins.New()
}

func (t *hookTarget) CurrentHash(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	h, err := t.store().CurrentCompositeHash(t.name, t.registryDir)
	if err != nil {
		return "", fmt.Errorf("proposals: hashing hook %q: %w", t.name, err)
	}
	return h, nil
}

func (t *hookTarget) PinStatus(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.pins == nil {
		return "", fmt.Errorf("proposals: %w: hook pin store", ErrStoreUnavailable)
	}
	res, err := t.pins.Verify(t.name, t.registryDir)
	if err != nil {
		if errors.Is(err, hookpins.ErrNotPinned) {
			return PinStatusUnpinned, nil
		}
		return "", err
	}
	if res.Status == hookpins.StatusDrift {
		return PinStatusDrift, nil
	}
	return PinStatusPinned, nil
}

func (t *hookTarget) ReadFiles(ctx context.Context, paths []string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(p, "hook.json.") {
			return nil, fmt.Errorf("%w: %s is a hook.json backup and is not pinned", ErrDiffUnsupported, p)
		}
		if err := checkParents(t.dir, p); err != nil {
			return nil, err
		}
		data, ok, err := readExisting(filepath.Join(t.dir, filepath.FromSlash(p)), p)
		if err != nil {
			return nil, err
		}
		if ok {
			out[p] = data
		}
	}
	return out, nil
}

// Provenance is pack when a pack record lists the hook, else the hook
// pin's recorded source. Every hook proposal is record-only regardless.
func (t *hookTarget) Provenance(ctx context.Context) (Provenance, error) {
	if err := ctx.Err(); err != nil {
		return Provenance{}, err
	}
	if t.lockPath != "" {
		lf, err := skills.ReadLockFile(t.lockPath)
		if err != nil {
			return Provenance{}, fmt.Errorf("proposals: reading skills lockfile: %w", err)
		}
		if pack := hookPack(lf, t.name); pack != "" {
			return Provenance{Kind: ProvenancePack, Pack: pack}, nil
		}
	}
	if t.pins != nil {
		if pin, ok := t.pins.Get(t.name); ok && pin.Source == hookpins.SourceLocal {
			return Provenance{Kind: ProvenanceLocalImport}, nil
		}
	}
	return Provenance{Kind: ProvenanceGit}, nil
}

func hookPack(lf *skills.LockFile, name string) string {
	if lf == nil {
		return ""
	}
	keys := make([]string, 0, len(lf.Sources))
	for k := range lf.Sources {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		src := lf.Sources[k]
		if src.Pack == nil {
			continue
		}
		for _, h := range src.Pack.Hooks {
			if h == name {
				return src.Pack.Name
			}
		}
	}
	return ""
}

func (t *hookTarget) Scan(ctx context.Context, _ map[string][]byte, _ bool, _ []string) ([]pins.Finding, error) {
	return nil, ctx.Err()
}

func (t *hookTarget) PostApplyHash(context.Context, map[string][]byte) (string, error) {
	return "", fmt.Errorf("%w: hook proposals are record-only", ErrDiffUnsupported)
}

func (t *hookTarget) Write(context.Context, map[string][]byte) (func() ([]string, []string), error) {
	return nil, fmt.Errorf("%w: hook proposals are record-only", ErrDiffUnsupported)
}

func (t *hookTarget) Repin(context.Context, RepinRequest) error {
	return fmt.Errorf("%w: hook proposals are record-only", ErrDiffUnsupported)
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
