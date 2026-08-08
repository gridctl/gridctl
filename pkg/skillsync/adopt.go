package skillsync

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// AdoptRefusal is a user-actionable "nothing to adopt" outcome (the
// projection is symlinked, empty, or otherwise not adoptable), distinct
// from infrastructure errors. The CLI maps it to exit 1.
type AdoptRefusal struct {
	msg string
}

func (e *AdoptRefusal) Error() string { return e.msg }

// AdoptResult describes what adopt pulled back into the registry.
type AdoptResult struct {
	Skill  string `json:"skill"`
	Client string `json:"client"`
	// Target is the projected copy the files came from.
	Target string `json:"target"`
	// RegistryDir is the registry skill directory written into.
	RegistryDir string `json:"registry_dir"`
	// BackupFile is the SKILL.md.pre-<sha> backup written registry-side
	// before the overwrite (empty when SKILL.md did not change).
	BackupFile string `json:"backup_file,omitempty"`
	// ChangedFiles lists the relative paths written back.
	ChangedFiles []string `json:"changed_files"`
	// PolicyKeysRestored reports that the projected SKILL.md carried a
	// stack model preference rewrite whose keys were restored to the
	// author's declaration before write-back: policy-owned deltas are
	// never adopted into the registry canonical.
	PolicyKeysRestored bool `json:"policy_keys_restored,omitempty"`
}

// adoptSkipped are gridctl-managed metadata files never pulled back:
// .origin.json is import tracking (a stale projected copy must not
// clobber registry-side state), and SKILL.md.pre-* are prior hand-edit
// backups.
func adoptSkipped(rel string) bool {
	base := filepath.Base(rel)
	return base == ".origin.json" || strings.HasPrefix(base, "SKILL.md.pre-")
}

// Adopt pulls a hand-edited copy projection back into the registry
// skill (chezmoi re-add semantics, the skill-kind sibling of
// `gridctl ctx adopt`). Changed files are written through the
// pkg/skills local-edit conventions: the prior SKILL.md is backed up as
// SKILL.md.pre-<sha> and the import origin is left untouched, so the
// next `gridctl skill update` sees the adopted content as local edits
// and refuses to clobber it without --force. The (skill, client) pair
// is then force-resynced so its hashes return to in-sync; other clients
// projecting the skill go stale, which is correct: the canon changed.
func (m *Manager) Adopt(ctx context.Context, skill, client string) (*AdoptResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t, ok := FindTarget(client)
	if !ok {
		return nil, fmt.Errorf("%w: %q (known clients: %s)", ErrUnknownClient, client, strings.Join(SupportedSlugs(), ", "))
	}
	sk, err := m.source.GetSkill(skill)
	if err != nil {
		return nil, fmt.Errorf("unknown skill %q: %w", skill, err)
	}

	var result *AdoptResult
	err = m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		entry := lf.entry(skill, client)
		if entry == nil {
			return fmt.Errorf("%w: %s", ErrNotProjected, skill)
		}
		if entry.Channel != ChannelCopy {
			return &AdoptRefusal{msg: client + " is symlinked; the registry copy is the source of truth, so there is nothing to adopt"}
		}

		src := m.skillSourceDir(sk)
		res := &AdoptResult{Skill: skill, Client: client, Target: entry.Target, RegistryDir: src}

		if err := validateAdoptedSkill(entry.Target, skill); err != nil {
			return err
		}
		changed, err := changedFiles(entry.Target, src)
		if err != nil {
			return err
		}

		// Policy-owned deltas are not adoptable: when the projection
		// carries a model policy rewrite, the keys still holding the
		// policy's own value (recorded at rewrite time) are restored to
		// the author's canonical declaration before write-back, so an
		// adopt can never poison the registry canonical with a
		// policy-resolved value. A model key the user deliberately edited
		// to some other value is the user's edit and adopts like any
		// other change. A SKILL.md whose only delta was the policy
		// rewrite drops out of the changed set entirely.
		var restoredSkillMD []byte
		if entry.ModelValue != "" && slicesContains(changed, "SKILL.md") {
			restored, policyOnly, rerr := restoreAuthorModel(entry.Target, src, entry.ModelValue)
			if rerr != nil {
				return rerr
			}
			switch {
			case policyOnly:
				changed = slicesRemove(changed, "SKILL.md")
				res.PolicyKeysRestored = true
			case restored != nil:
				restoredSkillMD = restored
				res.PolicyKeysRestored = true
			}
		}
		res.ChangedFiles = changed

		if len(changed) > 0 {
			// Registry-side backup first, following the pkg/skills
			// .pre-<sha> convention (sha from the import origin when the
			// skill tracks one, "local" otherwise).
			sha := ""
			if skills.HasOrigin(src) {
				if origin, oerr := skills.ReadOrigin(src); oerr == nil {
					sha = skills.ShortSHA(origin.CommitSHA)
				} else {
					// A corrupt origin only degrades the backup suffix to
					// "local"; the backup itself still happens.
					slog.Debug("adopt could not read the skill's import origin", "skill", skill, "error", oerr)
				}
			}
			backup, berr := skills.BackupSkillFileInDir(src, sha)
			if berr != nil {
				return fmt.Errorf("backing up registry skill: %w", berr)
			}
			res.BackupFile = backup
			// One cancellation check, then run the writes to completion:
			// aborting mid-set would leave the registry skill half
			// adopted. Failures name the recovery path for the same
			// reason.
			if err := ctx.Err(); err != nil {
				return err
			}
			for _, rel := range changed {
				data, rerr := os.ReadFile(filepath.Join(entry.Target, rel)) // #nosec G304 -- walking the managed projection
				if rerr != nil {
					return fmt.Errorf("reading projected %s: %w (the registry skill may be partially adopted; the previous SKILL.md is kept as %s)", rel, rerr, backup)
				}
				if rel == "SKILL.md" && restoredSkillMD != nil {
					data = restoredSkillMD
				}
				dest := filepath.Join(src, rel)
				if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
					return fmt.Errorf("creating %s: %w (the registry skill may be partially adopted; the previous SKILL.md is kept as %s)", filepath.Dir(dest), err, backup)
				}
				if err := project.AtomicWriteFile(dest, data); err != nil {
					return fmt.Errorf("writing %s into the registry: %w (the registry skill may be partially adopted; the previous SKILL.md is kept as %s)", rel, err, backup)
				}
			}
		}

		// Re-sync the pair so the recorded hashes match the adopted
		// content. A rewritten projection is deliberately not
		// re-materialized: this manager may hold no policy (CLI adopt has
		// no stack context) and a force-resync would revert the rewrite,
		// and under a live policy a re-render here would use the
		// pre-adopt in-memory skill. The lock is refreshed to on-disk
		// truth instead; the next policy-aware sync reconciles anything
		// left stale.
		if entry.ModelValue != "" {
			destHash, herr := treeHash(entry.Target)
			if herr != nil {
				return fmt.Errorf("hashing projected copy after adopt: %w", herr)
			}
			srcHash, herr := treeHash(src)
			if herr != nil {
				return fmt.Errorf("hashing registry skill after adopt: %w", herr)
			}
			// The rewrite marker describes current bytes: when the adopt
			// equalized the projection and the canonical (the user's own
			// model edit was adopted), nothing rewritten remains.
			mv := entry.ModelValue
			if destHash == srcHash {
				mv = ""
			}
			m.record(lf, skill, client, ChannelCopy, entry.Target, srcHash, destHash, entry.ChannelReason, mv, "")
		} else {
			// Force-resync the pair so the recorded hashes match the
			// adopted content; the projected copy is backed up before the
			// replace (it drifted from the recorded hash by definition).
			sres := m.materialize(sk, t, ChannelCopy, lf, SyncOptions{Force: true})
			if sres.Action == ActionError {
				return fmt.Errorf("re-syncing %s to %s after adopt: %s", skill, client, sres.Error)
			}
		}
		if err := saveView(pl, lf); err != nil {
			return err
		}
		result = res
		return nil
	})
	return result, err
}

// restoreAuthorModel rebuilds a projected SKILL.md with the model keys
// the policy wrote (still holding the applied value) restored to the
// registry canonical's declarations (author intent), undoing the policy
// rewrite while keeping every real user edit. Keys the user edited to a
// different value are theirs and are left alone. It returns policyOnly
// true when the restored bytes match the canonical (raw or
// parse-rendered), meaning the policy rewrite was the file's only
// delta; restored is nil when no key holds the applied value (the whole
// delta is the user's).
//
// Restoration prefers line surgery on the projected bytes over a
// parse/re-render, so an adopt adds no normalization beyond what the
// projection itself already carries; the parse/render path is the
// fallback for the rare metadata-key cases surgery cannot express.
func restoreAuthorModel(target, src, applied string) (restored []byte, policyOnly bool, err error) {
	projRaw, err := os.ReadFile(filepath.Join(target, "SKILL.md")) // #nosec G304 -- fixed name inside the managed projection
	if err != nil {
		return nil, false, fmt.Errorf("reading projected SKILL.md: %w", err)
	}
	canonRaw, err := os.ReadFile(filepath.Join(src, "SKILL.md")) // #nosec G304 -- fixed name inside the registry skill dir
	if err != nil {
		return nil, false, fmt.Errorf("reading registry SKILL.md: %w", err)
	}
	proj, err := registry.ParseSkillMD(projRaw)
	if err != nil {
		return nil, false, fmt.Errorf("parsing projected SKILL.md: %w", err)
	}
	canon, err := registry.ParseSkillMD(canonRaw)
	if err != nil {
		return nil, false, fmt.Errorf("parsing registry SKILL.md: %w", err)
	}

	appliedNorm := registry.NormalizeModelValue(applied)
	holdsApplied := func(v any) bool {
		s, ok := v.(string)
		return ok && registry.NormalizeModelValue(s) == appliedNorm
	}
	topOwned := holdsApplied(proj.Extra["model"])
	metaOwned := map[string]bool{}
	for _, key := range []string{"preferred-model", "model"} {
		if v, ok := proj.Metadata[key]; ok && registry.NormalizeModelValue(v) == appliedNorm {
			metaOwned[key] = true
		}
	}
	if !topOwned && len(metaOwned) == 0 {
		// Every model key was edited away from the policy value: the
		// delta is entirely the user's.
		return nil, false, nil
	}

	var restoredBytes []byte
	_, projHasMetaModel := proj.Metadata["model"]
	_, projHasMetaPreferred := proj.Metadata["preferred-model"]
	if topOwned && len(metaOwned) == 0 && !projHasMetaModel && !projHasMetaPreferred {
		// Common case: only the top-level key is involved. Line surgery
		// keeps every other projected byte verbatim.
		canonTop, _ := canon.Extra["model"].(string)
		if out, ok := registry.RewriteTopLevelModelLine(projRaw, canonTop); ok {
			restoredBytes = out
		}
	}
	if restoredBytes == nil {
		if topOwned {
			if v, ok := canon.Extra["model"]; ok {
				if proj.Extra == nil {
					proj.Extra = map[string]any{}
				}
				proj.Extra["model"] = v
			} else {
				delete(proj.Extra, "model")
			}
		}
		for key := range metaOwned {
			if v, ok := canon.Metadata[key]; ok {
				proj.Metadata[key] = v
			} else {
				delete(proj.Metadata, key)
			}
		}
		restoredBytes, err = registry.RenderSkillMD(proj)
		if err != nil {
			return nil, false, fmt.Errorf("rendering restored SKILL.md: %w", err)
		}
	}

	if bytes.Equal(restoredBytes, projRaw) {
		return nil, false, nil
	}
	if bytes.Equal(restoredBytes, canonRaw) {
		return restoredBytes, true, nil
	}
	canonNorm, err := registry.RenderSkillMD(canon)
	if err != nil {
		return nil, false, fmt.Errorf("rendering registry SKILL.md: %w", err)
	}
	if bytes.Equal(restoredBytes, canonNorm) {
		return restoredBytes, true, nil
	}
	return restoredBytes, false, nil
}

// slicesContains reports whether list holds v.
func slicesContains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// slicesRemove returns list without v, preserving order.
func slicesRemove(list []string, v string) []string {
	out := list[:0]
	for _, s := range list {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}

// validateAdoptedSkill refuses projected content the registry could not
// serve: a missing or empty SKILL.md (mirroring context adopt's
// empty-content refusal), one that does not parse, or one that renames
// the skill.
func validateAdoptedSkill(target, skill string) error {
	data, err := os.ReadFile(filepath.Join(target, "SKILL.md")) // #nosec G304 -- fixed name inside the managed projection
	if err != nil {
		if os.IsNotExist(err) {
			return &AdoptRefusal{msg: fmt.Sprintf("projected copy %s has no SKILL.md; refusing to adopt", target)}
		}
		return fmt.Errorf("reading projected SKILL.md: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return &AdoptRefusal{msg: fmt.Sprintf("projected content in %s is empty; refusing to adopt an empty skill", target)}
	}
	parsed, perr := registry.ParseSkillMD(data)
	if perr != nil {
		return &AdoptRefusal{msg: fmt.Sprintf("projected SKILL.md in %s is not a valid skill file: %v", target, perr)}
	}
	if parsed.Name != skill {
		return &AdoptRefusal{msg: fmt.Sprintf("projected SKILL.md in %s names the skill %q, not %q; adopt cannot rename skills", target, parsed.Name, skill)}
	}
	return nil
}

// changedFiles walks the projected copy and returns the relative paths
// whose content differs from (or is missing in) the registry skill dir,
// in deterministic order. Files present only registry-side are kept:
// adopt pulls edits back, it does not mirror deletions. Symlinks inside
// the copy are not compared or adopted; a symlink the user added by
// hand does not survive the post-adopt resync (the pre-replace backup
// under skillsync-backups preserves it).
func changedFiles(target, src string) ([]string, error) {
	var changed []string
	err := filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, rerr := filepath.Rel(target, path)
		if rerr != nil {
			return rerr
		}
		if adoptSkipped(rel) {
			return nil
		}
		got, rerr := os.ReadFile(path) // #nosec G304 -- walking the managed projection
		if rerr != nil {
			return rerr
		}
		want, werr := os.ReadFile(filepath.Join(src, rel)) // #nosec G304 -- same rel path under the registry skill dir
		if werr != nil {
			if os.IsNotExist(werr) {
				changed = append(changed, rel)
				return nil
			}
			return werr
		}
		if !bytes.Equal(got, want) {
			changed = append(changed, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("comparing %s against the registry: %w", target, err)
	}
	sort.Strings(changed)
	return changed, nil
}
