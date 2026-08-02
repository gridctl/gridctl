package skillpins

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/registry"
)

// schemeV1Prefix marks digests computed under the current skill hash scheme:
// the canonical (parse-rendered) SKILL.md for the skill hash, raw bytes for
// supporting files. Following the pkg/pins lesson, the prefix travels with
// every stored digest so a future scheme change never presents as drift.
const schemeV1Prefix = "s1:"

// SkillSource is the read surface the pin store needs from the registry.
// *registry.Store satisfies it; defining it here keeps the dependency
// pointing from skillpins into registry, never back.
type SkillSource interface {
	ListSkills() []*registry.AgentSkill
	GetSkill(name string) (*registry.AgentSkill, error)
	ListFiles(skillName string) ([]registry.SkillFile, error)
	ReadFile(skillName, filePath string) ([]byte, error)
}

// ErrDigestUnavailable marks a digest pass that could not read the skill's
// content (an unreadable supporting file, e.g. a dangling symlink). It is
// deliberately distinct from registry.ErrNotFound: consumers must never
// confuse "cannot hash this skill" with "this skill does not exist" — the
// former is a fail-closed condition, the latter a reset hint.
var ErrDigestUnavailable = errors.New("skill content could not be hashed")

// CanonicalSkillHash digests the canonical rendering of a skill's SKILL.md.
// Hashing the parse-rendered form (not raw bytes) is what keeps the hash
// stable across frontmatter normalization: import, editor save, and
// projection all round-trip through registry.ParseSkillMD/RenderSkillMD, so
// a semantically unchanged skill can never manufacture pin drift.
//
// The gridctl-managed `state` field is excluded from the hash input: a
// draft/active/disabled toggle is an exposure decision made through gridctl
// itself, not a content change, and must not trip a gate built for
// out-of-band edits.
func CanonicalSkillHash(sk *registry.AgentSkill) (string, error) {
	cp := *sk
	cp.State = ""
	data, err := registry.RenderSkillMD(&cp)
	if err != nil {
		return "", fmt.Errorf("skillpins: rendering canonical SKILL.md for %q: %w", sk.Name, err)
	}
	sum := sha256.Sum256(data)
	return schemeV1Prefix + hex.EncodeToString(sum[:]), nil
}

// hashBytes digests raw file content under the current scheme.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return schemeV1Prefix + hex.EncodeToString(sum[:])
}

// digestedFile reports whether a supporting file participates in the pin
// digest set. Excluded: directories (walked, not hashed), dotfiles in any
// path component (.origin.json mutates on every import/sync and would
// manufacture drift; other dotfiles are tool metadata by convention), and
// atomic-write temp files.
func digestedFile(f registry.SkillFile) bool {
	if f.IsDir {
		return false
	}
	if strings.HasSuffix(f.Path, ".tmp") {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(f.Path), "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

// ComputeDigests hashes a skill's whole document set: the canonical SKILL.md
// plus every digested supporting file, sorted by path. File-read failures
// wrap ErrDigestUnavailable with the underlying cause flattened (%v, not
// %w): src wraps registry.ErrNotFound into per-file errors, and letting
// that chain leak would make an unreadable file indistinguishable from a
// deleted skill.
func ComputeDigests(src SkillSource, sk *registry.AgentSkill) (skillHash string, files []FileDigest, err error) {
	skillHash, err = CanonicalSkillHash(sk)
	if err != nil {
		return "", nil, err
	}

	entries, err := src.ListFiles(sk.Name)
	if err != nil {
		return "", nil, fmt.Errorf("skillpins: %w: listing files for %q: %v", ErrDigestUnavailable, sk.Name, err)
	}

	// A subdirectory holding its own SKILL.md is a separate registered
	// skill (the registry loads nested skill dirs); its tree belongs to its
	// own pin, never to the parent's digest set.
	nestedRoots := make([]string, 0, 2)
	for _, f := range entries {
		p := filepath.ToSlash(f.Path)
		if strings.HasSuffix(p, "/SKILL.md") {
			nestedRoots = append(nestedRoots, strings.TrimSuffix(p, "SKILL.md"))
		}
	}
	underNestedSkill := func(p string) bool {
		for _, root := range nestedRoots {
			if strings.HasPrefix(p, root) {
				return true
			}
		}
		return false
	}

	for _, f := range entries {
		p := filepath.ToSlash(f.Path)
		if !digestedFile(f) || underNestedSkill(p) {
			continue
		}
		data, err := src.ReadFile(sk.Name, f.Path)
		if err != nil {
			return "", nil, fmt.Errorf("skillpins: %w: reading %s in %q: %v", ErrDigestUnavailable, f.Path, sk.Name, err)
		}
		files = append(files, FileDigest{Path: p, Digest: hashBytes(data)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return skillHash, files, nil
}

// CompositeHash folds a skill hash and its file digests into one fingerprint,
// the value approvals bind to (the pkg/pins HashTools precedent): capture it
// when rendering a diff, compare it at approve time, and reject on mismatch
// so content cannot change between review and approval.
func CompositeHash(skillHash string, files []FileDigest) string {
	var b strings.Builder
	b.WriteString(skillHash)
	for _, f := range files {
		b.WriteString("\n")
		b.WriteString(f.Path)
		b.WriteString("\x00")
		b.WriteString(f.Digest)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// diffFileSets names the per-file changes between a pinned digest set and the
// current one. Inputs are sorted by path; outputs are sorted too.
func diffFileSets(pinned, current []FileDigest) (added, removed, modified []string) {
	old := make(map[string]string, len(pinned))
	for _, f := range pinned {
		old[f.Path] = f.Digest
	}
	seen := make(map[string]bool, len(current))
	for _, f := range current {
		seen[f.Path] = true
		prev, ok := old[f.Path]
		switch {
		case !ok:
			added = append(added, f.Path)
		case prev != f.Digest:
			modified = append(modified, f.Path)
		}
	}
	for _, f := range pinned {
		if !seen[f.Path] {
			removed = append(removed, f.Path)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(modified)
	return added, removed, modified
}
