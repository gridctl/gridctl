package packops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/skills"
)

// LockSummary is the pack-lock state on an add or preview document.
type LockSummary struct {
	State  string   `json:"state"`
	Path   string   `json:"path,omitempty"`
	Digest string   `json:"digest,omitempty"`
	Reason string   `json:"reason,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

// LockSourceDoc is one source in a pack lock document.
type LockSourceDoc struct {
	Name   string `json:"name"`
	Repo   string `json:"repo"`
	Ref    string `json:"ref,omitempty"`
	Commit string `json:"commit,omitempty"`
	Error  string `json:"error,omitempty"`
}

// LockExtendsDoc is one base pack recorded in a pack lock document.
type LockExtendsDoc struct {
	Repo   string `json:"repo"`
	Commit string `json:"commit"`
}

// LockDoc is the machine-readable pack lock document.
type LockDoc struct {
	SchemaVersion int                       `json:"schema_version"`
	Pack          string                    `json:"pack"`
	Path          string                    `json:"path"`
	Status        string                    `json:"status"`
	Reason        string                    `json:"reason,omitempty"`
	Digest        string                    `json:"digest,omitempty"`
	Sources       []LockSourceDoc           `json:"sources,omitempty"`
	Extends       map[string]LockExtendsDoc `json:"extends,omitempty"`
	Warnings      []string                  `json:"warnings,omitempty"`
}

// LockOptions parameterizes gridctl pack lock.
type LockOptions struct {
	Dir        string
	Check      bool
	Auth       skills.AuthConfig
	SourceAuth map[string]skills.AuthConfig
	Resolver   skills.CredentialResolver
	// LockPath is the import lockfile used for stored auth and the extends
	// entry. Empty means the HOME-derived default.
	LockPath string
}

// lockPlan is the add-time decision for one cloned pack.
type lockPlan struct {
	Summary *LockSummary
	Commits map[string]string
	Extends map[string]pack.LockedExtends
	// report is true when the manifest declares sources or stack.extends.
	report bool
}

// Lock writes gridctl-pack.lock.yaml, or with Check compares it and writes
// nothing. Check does not clone. A source that fails to resolve leaves the
// file unwritten and sets status unresolved.
func Lock(ctx context.Context, opts LockOptions) (*LockDoc, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := opts.Dir
	if dir == "" {
		dir = "."
	}
	manifestPath := filepath.Join(dir, pack.ManifestFileName)
	manifest, err := pack.ParseFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no %s in %s", pack.ManifestFileName, dir)
		}
		return nil, err
	}
	lockPath := filepath.Join(dir, pack.LockFileName)
	digest := pack.ManifestDigest(manifest)
	doc := &LockDoc{
		SchemaVersion: SchemaVersion,
		Pack:          manifest.Name,
		Path:          lockPath,
		Digest:        digest,
	}
	if err := refuseNewerPackLock(lockPath); err != nil {
		return nil, err
	}
	if opts.Check {
		return checkLock(lockPath, manifest, digest, doc)
	}
	importLock := opts.LockPath
	if importLock == "" {
		importLock = skills.LockFilePath()
	}
	clones, warnings, err := cloneDeclaredSources(ctx, manifest, AddOptions{
		Auth: opts.Auth, SourceAuth: opts.SourceAuth, Resolver: opts.Resolver,
	}, importLock)
	if err != nil {
		return nil, err
	}
	doc.Warnings = append(doc.Warnings, warnings...)
	lf := &pack.LockFile{
		Version:        pack.LockVersion,
		Pack:           manifest.Name,
		ManifestDigest: digest,
	}
	if len(manifest.Sources) > 0 {
		lf.Sources = map[string]pack.LockedSource{}
	}
	unresolved := false
	for _, name := range sortedSourceNames(manifest) {
		rs := clones[name]
		item := LockSourceDoc{Name: name, Repo: redactRepo(manifest.Sources[name].Repo), Ref: manifest.Sources[name].Ref}
		if rs == nil || rs.err != nil {
			unresolved = true
			if rs != nil {
				item.Error = redactSourceErr(rs.err)
			} else {
				item.Error = "source did not resolve"
			}
			doc.Sources = append(doc.Sources, item)
			continue
		}
		item.Commit = rs.sha
		doc.Sources = append(doc.Sources, item)
		lf.Sources[name] = pack.LockedSource{
			Repo:   manifest.Sources[name].Repo,
			Ref:    manifest.Sources[name].Ref,
			Commit: rs.sha,
			Path:   manifest.Sources[name].Path,
		}
	}
	extends, warn, err := extendsFromLockfile(importLock, manifest)
	if err != nil {
		return nil, err
	}
	if warn != "" {
		doc.Warnings = append(doc.Warnings, warn)
	}
	if len(extends) > 0 {
		lf.Extends = map[string]pack.LockedExtends{}
		doc.Extends = map[string]LockExtendsDoc{}
		for name, ext := range extends {
			lf.Extends[name] = ext
			doc.Extends[name] = LockExtendsDoc{Repo: redactRepo(ext.Repo), Commit: ext.Commit}
		}
	}
	if unresolved {
		doc.Status = "unresolved"
		return doc, nil
	}
	if err := pack.WriteLockFile(lockPath, lf); err != nil {
		return nil, err
	}
	doc.Status = "written"
	return doc, nil
}

// refuseNewerPackLock rejects a lock written by a newer gridctl before
// pack lock clones or overwrites it. A missing file is not an error.
func refuseNewerPackLock(lockPath string) error {
	data, err := os.ReadFile(lockPath) // #nosec G304 -- lock path is next to the caller-selected manifest
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := pack.ParseLockFile(data); err != nil && errors.Is(err, pack.ErrNewerPackLockVersion) {
		return err
	}
	return nil
}

func checkLock(lockPath string, manifest *pack.Manifest, digest string, doc *LockDoc) (*LockDoc, error) {
	data, err := os.ReadFile(lockPath) // #nosec G304 -- lock path is next to the caller-selected manifest
	if err != nil {
		if os.IsNotExist(err) {
			doc.Status = "stale"
			doc.Reason = "lock file is missing"
			return doc, nil
		}
		return nil, err
	}
	lf, err := pack.ParseLockFile(data)
	if err != nil {
		return nil, err
	}
	if reason := lockStaleReason(manifest, digest, lf); reason != "" {
		doc.Status = "stale"
		doc.Reason = reason
		return doc, nil
	}
	doc.Status = "unchanged"
	return doc, nil
}

func lockStaleReason(manifest *pack.Manifest, digest string, lf *pack.LockFile) string {
	if lf == nil {
		return "lock file is missing"
	}
	names := sortedSourceNames(manifest)
	var locked []string
	for name := range lf.Sources {
		locked = append(locked, name)
	}
	slices.Sort(locked)
	for _, name := range names {
		if _, ok := lf.Sources[name]; !ok {
			return "source " + name + " added"
		}
	}
	for _, name := range locked {
		if _, ok := manifest.Sources[name]; !ok {
			return "source " + name + " removed"
		}
	}
	if lf.ManifestDigest != digest {
		return "digest differs"
	}
	return ""
}

func sortedSourceNames(manifest *pack.Manifest) []string {
	if manifest == nil {
		return nil
	}
	names := make([]string, 0, len(manifest.Sources))
	for name := range manifest.Sources {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func extendsFromLockfile(lockPath string, manifest *pack.Manifest) (map[string]pack.LockedExtends, string, error) {
	if manifest == nil || manifest.Stack.Extends == "" {
		return nil, "", nil
	}
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		return nil, "", err
	}
	base, err := findPack(lf, manifest.Stack.Extends)
	if err != nil {
		if errors.Is(err, ErrNotImported) {
			return nil, fmt.Sprintf("base pack %q is not added; extends entry omitted", manifest.Stack.Extends), nil
		}
		return nil, "", err
	}
	return map[string]pack.LockedExtends{
		manifest.Stack.Extends: {Repo: base.Source.Repo, Commit: base.Source.CommitSHA},
	}, "", nil
}

// decideLock reads a cloned pack's lock and decides whether add honors it.
// A newer lock version is an error regardless of flags. --locked refuses a
// stale or absent lock before the caller clones sources or writes.
func decideLock(cloneRoot string, manifest *pack.Manifest, opts AddOptions) (*lockPlan, error) {
	plan := &lockPlan{}
	if manifest == nil {
		return plan, nil
	}
	plan.report = len(manifest.Sources) > 0 || manifest.Stack.Extends != ""
	if opts.Locked && opts.IgnoreLock {
		return nil, &packError{reason: ErrLockFlags, msg: "--locked and --ignore-lock cannot be used together"}
	}
	if !plan.report {
		return plan, nil
	}
	digest := pack.ManifestDigest(manifest)
	path := filepath.Join(cloneRoot, pack.LockFileName)
	data, err := os.ReadFile(path) // #nosec G304 -- lock name is fixed under the clone root
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if opts.Locked {
			return nil, lockStaleError("lock file is missing")
		}
		state := "absent"
		if opts.IgnoreLock {
			state = "ignored"
		}
		plan.Summary = &LockSummary{State: state, Path: pack.LockFileName, Digest: digest}
		return plan, nil
	}
	lf, err := pack.ParseLockFile(data)
	if err != nil {
		return nil, err
	}
	plan.Extends = lf.Extends
	reason := lockStaleReason(manifest, digest, lf)
	if opts.IgnoreLock {
		plan.Summary = &LockSummary{State: "ignored", Path: pack.LockFileName, Digest: digest, Reason: reason}
		return plan, nil
	}
	if reason != "" {
		if opts.Locked {
			return nil, lockStaleError(reason)
		}
		plan.Summary = &LockSummary{State: "stale", Path: pack.LockFileName, Digest: digest, Reason: reason}
		return plan, nil
	}
	plan.Summary = &LockSummary{State: "honored", Path: pack.LockFileName, Digest: lf.ManifestDigest}
	plan.Commits = map[string]string{}
	for name, src := range lf.Sources {
		plan.Commits[name] = src.Commit
	}
	return plan, nil
}

func lockStaleError(reason string) error {
	return &packError{
		reason: ErrLockStale,
		msg:    fmt.Sprintf("lock is stale (%s); run 'gridctl pack lock' in the pack repository or re-run without --locked", reason),
	}
}

// extendsLockNote compares an imported base with the lock's extends entry.
// A repo mismatch is an unresolved detail. A commit mismatch is a note, and
// under --locked an error returned before any write.
func extendsLockNote(base packSource, locked pack.LockedExtends, lockedFlag bool) (note, unresolved string, err error) {
	if locked.Repo != "" && base.Source.Repo != locked.Repo {
		return "", fmt.Sprintf("base pack %q is added from %s, lock expects %s", base.Pack.Name, redactRepo(base.Source.Repo), redactRepo(locked.Repo)), nil
	}
	if locked.Commit != "" && base.Source.CommitSHA != locked.Commit {
		note = fmt.Sprintf("base %s at %s, lock expects %s", base.Pack.Name, skills.ShortSHA(base.Source.CommitSHA), skills.ShortSHA(locked.Commit))
		if lockedFlag {
			return "", "", &packError{
				reason: ErrLockStale,
				msg:    fmt.Sprintf("lock is stale (%s); run 'gridctl pack lock' in the pack repository or re-run without --locked", note),
			}
		}
		return note, "", nil
	}
	return "", "", nil
}
