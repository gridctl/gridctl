// Package versions stores retained gridctl binaries under a home-scoped
// directory so a standalone install can roll back without a download.
//
// Layout: manifest.json (0644), .lock, and <tag>/gridctl (0755). The
// store knows nothing about GitHub, tarballs, or daemons. Callers copy
// a file in with Retain and install it themselves.
package versions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// ManifestSchemaVersion is the current manifest schema. A newer
	// value is refused rather than rewritten (Article XVII).
	ManifestSchemaVersion = 1

	manifestName = "manifest.json"
	lockName     = ".lock"
	binaryName   = "gridctl"

	dirMode      = 0o755
	manifestMode = 0o644
	binaryMode   = 0o755

	// lockTimeout bounds the wait for the cross-process lock when the
	// caller context has no earlier deadline. Retries match state.WithLock.
	lockTimeout = 5 * time.Second
	lockRetry   = 100 * time.Millisecond
)

const (
	// OriginReplaced marks a binary copied from the executable that was
	// about to be replaced.
	OriginReplaced = "replaced"
	// OriginDownloaded marks a binary copied from a verified release archive.
	OriginDownloaded = "downloaded"
)

var (
	// ErrNewerManifest marks a manifest written by a newer gridctl.
	// Callers must not rewrite it.
	ErrNewerManifest = errors.New("versions manifest written by a newer gridctl")
	// ErrChecksumMismatch marks a retained binary whose SHA256 does not
	// match the manifest. The installed binary must not be replaced.
	ErrChecksumMismatch = errors.New("retained binary checksum mismatch")
	// ErrInvalidTag marks a tag that is not safe to use as a single
	// directory name under the store.
	ErrInvalidTag = errors.New("invalid version tag")
	// ErrSymlinkSource marks a retain source that is a symlink resolving
	// outside the allowed temp directory or executable path.
	ErrSymlinkSource = errors.New("retain source symlink resolves outside allowed paths")
)

// Schemas is the schema advertisement a binary reported at retention
// time. Nil means the retaining release did not record one (serialized
// as null). Pin fields are strings because the on-disk constants are
// strings; an absent pin field decodes as "".
type Schemas struct {
	DaemonState int    `json:"daemon_state"`
	ProjectLock int    `json:"project_lock"`
	ImportLock  int    `json:"import_lock"`
	ToolPins    string `json:"tool_pins"`
	SkillPins   string `json:"skill_pins"`
}

// Entry is one retained binary.
type Entry struct {
	Tag        string    `json:"tag"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	RetainedAt time.Time `json:"retained_at"`
	Origin     string    `json:"origin"`
	Schemas    *Schemas  `json:"schemas"`
}

// Manifest is the on-disk index. Active is the tag last installed.
// Previous is the tag to restore when rollback is given no tag.
type Manifest struct {
	SchemaVersion int     `json:"schema_version"`
	Active        string  `json:"active"`
	Previous      string  `json:"previous"`
	Entries       []Entry `json:"entries"`
}

// Store is an exclusively locked versions directory. Close releases the lock.
type Store struct {
	dir    string
	file   *os.File
	unlock func() error
	man    Manifest
	roots  []string
	now    func() time.Time
}

// Open creates dir (0755) if needed and acquires a cross-process exclusive
// flock on .lock. A manifest with schema_version greater than
// ManifestSchemaVersion returns ErrNewerManifest and does not hold the lock.
// A missing manifest is an empty store.
func Open(ctx context.Context, dir string) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, errors.New("versions directory is empty")
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("creating versions directory: %w", err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return nil, fmt.Errorf("setting versions directory mode: %w", err)
	}

	lockPath := filepath.Join(dir, lockName)
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, manifestMode)
	if err != nil {
		return nil, fmt.Errorf("opening versions lock: %w", err)
	}
	unlock, err := acquireLock(ctx, file, dir)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	s := &Store{dir: dir, file: file, unlock: unlock}
	man, err := loadManifest(filepath.Join(dir, manifestName))
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	s.man = man
	return s, nil
}

func acquireLock(ctx context.Context, file *os.File, dir string) (func() error, error) {
	deadline := time.Now().Add(lockTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unlock, err := tryFileLock(file)
		if err == nil {
			return unlock, nil
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("timeout acquiring versions lock in %s (another upgrade may be in progress)", dir)
		}
		timer := time.NewTimer(lockRetry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Close releases the lock. It is safe to call more than once.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.unlock != nil {
		err = s.unlock()
		s.unlock = nil
	}
	if s.file != nil {
		if cerr := s.file.Close(); err == nil {
			err = cerr
		}
		s.file = nil
	}
	return err
}

// AllowSources records files and directories a symlink retain source may
// resolve into. A symlink that resolves outside every allowed path is
// refused. A regular file is accepted. When no sources have been allowed,
// every symlink is refused.
func (s *Store) AllowSources(paths ...string) {
	s.roots = append(s.roots, paths...)
}

// Retain copies srcPath into <tag>/gridctl through a temp sibling and
// rename, then upserts the entry. Re-retaining an existing tag with the
// same SHA256 is a no-op. A different SHA256 replaces the file and entry.
// On every write, entries whose file is missing are dropped from the manifest.
func (s *Store) Retain(ctx context.Context, tag, srcPath, origin string, schemas *Schemas) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateTag(tag); err != nil {
		return err
	}
	if origin != OriginReplaced && origin != OriginDownloaded {
		return fmt.Errorf("origin must be %q or %q", OriginReplaced, OriginDownloaded)
	}
	if err := s.checkSource(srcPath); err != nil {
		return err
	}
	sum, size, err := hashFile(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("hashing retain source: %w", err)
	}
	if existing, ok := s.entry(tag); ok && strings.EqualFold(existing.SHA256, sum) {
		if _, statErr := os.Stat(s.binaryPath(tag)); statErr == nil {
			return nil
		}
	}
	if err := s.copyBinary(ctx, tag, srcPath); err != nil {
		return err
	}
	entry := Entry{
		Tag:        tag,
		SHA256:     sum,
		Size:       size,
		RetainedAt: s.nowUTC().Truncate(time.Second),
		Origin:     origin,
		Schemas:    cloneSchemas(schemas),
	}
	s.upsert(entry)
	return s.write()
}

// Lookup returns the entry and the binary path. ok is false when the tag
// is not in the manifest. The file may be missing until the next write
// drops the entry.
func (s *Store) Lookup(tag string) (Entry, string, bool) {
	e, ok := s.entry(tag)
	if !ok {
		return Entry{}, "", false
	}
	return e, s.binaryPath(tag), true
}

// Verify recomputes the binary SHA256 and compares it to the manifest.
// ErrChecksumMismatch includes the expected and actual digests.
func (s *Store) Verify(ctx context.Context, tag string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, path, ok := s.Lookup(tag)
	if !ok {
		return fmt.Errorf("retained tag %s not found", tag)
	}
	actual, _, err := hashFile(ctx, path)
	if err != nil {
		return fmt.Errorf("hashing retained %s: %w", tag, err)
	}
	if !strings.EqualFold(entry.SHA256, actual) {
		return fmt.Errorf("%w for %s\n  expected: %s\n  actual:   %s", ErrChecksumMismatch, tag, entry.SHA256, actual)
	}
	return nil
}

// SetActive records the installed tag and the rollback tag, then writes
// the manifest. Entries whose file is missing are dropped.
func (s *Store) SetActive(active, previous string) error {
	if active != "" {
		if err := validateTag(active); err != nil {
			return err
		}
	}
	if previous != "" {
		if err := validateTag(previous); err != nil {
			return err
		}
	}
	s.man.Active = active
	s.man.Previous = previous
	return s.write()
}

// Prune removes oldest RetainedAt entries first until the count is at or
// below keep. Active and Previous are never removed, so the floor can
// exceed keep. Removed tags are returned. Directories that are not in the
// manifest are left in place (see Orphans).
func (s *Store) Prune(ctx context.Context, keep int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if keep < 0 {
		return nil, fmt.Errorf("keep must be >= 0")
	}
	s.dropMissing()
	protected := map[string]struct{}{}
	if s.man.Active != "" {
		protected[s.man.Active] = struct{}{}
	}
	if s.man.Previous != "" {
		protected[s.man.Previous] = struct{}{}
	}
	type victim struct {
		tag string
		at  time.Time
	}
	var victims []victim
	for _, e := range s.man.Entries {
		if _, ok := protected[e.Tag]; ok {
			continue
		}
		victims = append(victims, victim{e.Tag, e.RetainedAt})
	}
	sort.Slice(victims, func(i, j int) bool {
		if victims[i].at.Equal(victims[j].at) {
			return victims[i].tag < victims[j].tag
		}
		return victims[i].at.Before(victims[j].at)
	})
	excess := len(s.man.Entries) - keep
	if excess < 0 {
		excess = 0
	}
	if excess > len(victims) {
		excess = len(victims)
	}
	removed := make([]string, 0, excess)
	dropped := make(map[string]struct{}, excess)
	for i := 0; i < excess; i++ {
		tag := victims[i].tag
		if err := os.RemoveAll(s.tagDir(tag)); err != nil {
			return removed, fmt.Errorf("removing %s: %w", tag, err)
		}
		removed = append(removed, tag)
		dropped[tag] = struct{}{}
	}
	if len(dropped) > 0 {
		kept := make([]Entry, 0, len(s.man.Entries)-len(dropped))
		for _, e := range s.man.Entries {
			if _, ok := dropped[e.Tag]; ok {
				continue
			}
			kept = append(kept, e)
		}
		s.man.Entries = kept
	}
	if err := s.write(); err != nil {
		return removed, err
	}
	return removed, nil
}

// Entries returns a copy of the manifest entries, newest RetainedAt first.
func (s *Store) Entries() []Entry {
	out := append([]Entry(nil), s.man.Entries...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].RetainedAt.Equal(out[j].RetainedAt) {
			return out[i].Tag > out[j].Tag
		}
		return out[i].RetainedAt.After(out[j].RetainedAt)
	})
	return out
}

// Active returns the tag last recorded as installed.
func (s *Store) Active() string { return s.man.Active }

// Previous returns the tag recorded for a tagless rollback.
func (s *Store) Previous() string { return s.man.Previous }

// Orphans returns directory names present on disk but absent from the
// manifest, sorted. They are never deleted.
func (s *Store) Orphans() []string {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	known := make(map[string]struct{}, len(s.man.Entries))
	for _, e := range s.man.Entries {
		known[e.Tag] = struct{}{}
	}
	var out []string
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		name := ent.Name()
		if _, ok := known[name]; ok {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ValidateTag reports whether tag is a single directory name with no
// separator and no "..". Rollback callers also require a semver tag.
func ValidateTag(tag string) error {
	return validateTag(tag)
}

func validateTag(tag string) error {
	if tag == "" || tag == "." || tag == ".." || strings.HasPrefix(tag, ".") {
		return fmt.Errorf("%w: %q", ErrInvalidTag, tag)
	}
	if strings.Contains(tag, "..") || strings.ContainsAny(tag, `/\`) || tag != filepath.Base(tag) {
		return fmt.Errorf("%w: %q", ErrInvalidTag, tag)
	}
	return nil
}

func (s *Store) entry(tag string) (Entry, bool) {
	for _, e := range s.man.Entries {
		if e.Tag == tag {
			return e, true
		}
	}
	return Entry{}, false
}

func (s *Store) upsert(entry Entry) {
	for i := range s.man.Entries {
		if s.man.Entries[i].Tag == entry.Tag {
			s.man.Entries[i] = entry
			return
		}
	}
	s.man.Entries = append(s.man.Entries, entry)
}

func (s *Store) tagDir(tag string) string {
	return filepath.Join(s.dir, tag)
}

func (s *Store) binaryPath(tag string) string {
	return filepath.Join(s.tagDir(tag), binaryName)
}

func (s *Store) nowUTC() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func (s *Store) checkSource(src string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat retain source: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("retain source %s is not a regular file", src)
		}
		return nil
	}
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return fmt.Errorf("resolving retain source: %w", err)
	}
	if !s.sourceAllowed(resolved) {
		return fmt.Errorf("%w: %s resolves to %s", ErrSymlinkSource, src, resolved)
	}
	return nil
}

func (s *Store) sourceAllowed(resolved string) bool {
	resolved = filepath.Clean(resolved)
	for _, root := range s.roots {
		root = filepath.Clean(root)
		if resolved == root {
			return true
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		return true
	}
	return false
}

func (s *Store) copyBinary(ctx context.Context, tag, srcPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := s.tagDir(tag)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("creating %s: %w", tag, err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return fmt.Errorf("setting %s mode: %w", tag, err)
	}
	in, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening retain source: %w", err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(dir, ".gridctl-*")
	if err != nil {
		return fmt.Errorf("creating temp binary: %w", err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, ctxReader{ctx: ctx, r: in}); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("copying retain source: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp binary: %w", err)
	}
	if err := os.Chmod(tmpPath, binaryMode); err != nil {
		return fmt.Errorf("setting binary mode: %w", err)
	}
	dest := s.binaryPath(tag)
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("renaming retained binary: %w", err)
	}
	ok = true
	return nil
}

func (s *Store) dropMissing() {
	kept := make([]Entry, 0, len(s.man.Entries))
	present := make(map[string]struct{}, len(s.man.Entries))
	for _, e := range s.man.Entries {
		if _, err := os.Stat(s.binaryPath(e.Tag)); err != nil {
			slog.Warn("dropping retained entry with missing binary", "tag", e.Tag)
			continue
		}
		kept = append(kept, e)
		present[e.Tag] = struct{}{}
	}
	s.man.Entries = kept
	if _, ok := present[s.man.Active]; !ok {
		s.man.Active = ""
	}
	if _, ok := present[s.man.Previous]; !ok {
		s.man.Previous = ""
	}
}

func (s *Store) write() error {
	s.dropMissing()
	s.man.SchemaVersion = ManifestSchemaVersion
	if s.man.Entries == nil {
		s.man.Entries = []Entry{}
	}
	data, err := json.MarshalIndent(s.man, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding versions manifest: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(s.dir, manifestName)
	if err := atomicWriteFile(path, data, manifestMode); err != nil {
		return fmt.Errorf("writing versions manifest: %w", err)
	}
	return nil
}

func loadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{SchemaVersion: ManifestSchemaVersion, Entries: []Entry{}}, nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("reading versions manifest: %w", err)
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return Manifest{}, fmt.Errorf("parsing versions manifest: %w", err)
	}
	if man.SchemaVersion > ManifestSchemaVersion {
		return Manifest{}, fmt.Errorf("%w: %s has schema_version %d", ErrNewerManifest, path, man.SchemaVersion)
	}
	if man.Entries == nil {
		man.Entries = []Entry{}
	}
	return man, nil
}

func hashFile(ctx context.Context, path string) (string, int64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, ctxReader{ctx: ctx, r: f})
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func cloneSchemas(in *Schemas) *Schemas {
	if in == nil {
		return nil
	}
	cp := *in
	return &cp
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("setting temp file permissions: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
