package agentsync

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skills"
)

// AdoptRefusal is a user-actionable "nothing to adopt" outcome, distinct
// from infrastructure errors. The CLI maps it to exit 1.
type AdoptRefusal struct {
	msg string
}

func (e *AdoptRefusal) Error() string { return e.msg }

// AdoptResult describes what adopt pulled back into the canonical store.
type AdoptResult struct {
	Agent  string `json:"agent"`
	Client string `json:"client"`
	// Target is the projected file the content came from.
	Target string `json:"target"`
	// CanonicalFile is the canonical AGENT.md written into.
	CanonicalFile string `json:"canonical_file"`
	// BackupFile is the AGENT.md.pre-<sha> backup written store-side
	// before the overwrite (empty when the content did not change).
	BackupFile string `json:"backup_file,omitempty"`
	// Changed reports whether the projected content differed from canon.
	Changed bool `json:"changed"`
	// PolicyKeysRestored reports that the projected file carried a stack
	// model preference rewrite whose keys were restored to the author's
	// declaration before write-back: policy-owned deltas are never
	// adopted into the canonical store.
	PolicyKeysRestored bool `json:"policy_keys_restored,omitempty"`
}

// Adopt pulls a hand-edited projected agent file back into the
// canonical store (the agent-kind sibling of `gridctl ctx adopt`). The
// prior AGENT.md is backed up as AGENT.md.pre-<sha> and the import
// origin is left untouched, so the next `gridctl skill update` sees the
// adopted content as a local edit and refuses to clobber it without
// --force. The (agent, client) pair is then force-resynced so its
// hashes return to in-sync.
func (m *Manager) Adopt(ctx context.Context, agent, client string) (*AdoptResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t, ok := FindTarget(client)
	if !ok {
		return nil, fmt.Errorf("%w: %q (known clients: %s)", ErrUnknownClient, client, strings.Join(SupportedSlugs(), ", "))
	}
	// Rendered targets are one-way: the dialect dropped canonical keys at
	// render time, so pulling its bytes back would corrupt the store with
	// lossy client-dialect content. Gate before any validation runs.
	if t.Render != nil {
		return nil, &AdoptRefusal{msg: fmt.Sprintf(
			"%s's projection is a lossy %s render and cannot flow back into the canonical store; adopt the identity projection instead ('gridctl skill project adopt --kind agent %s --client claude-code'), or hand-maintain the file and detach it with 'gridctl skill project unsync --kind agent %s --clients %s'",
			t.Name, t.Slug, agent, agent, t.Slug)}
	}
	a, err := skills.GetAgent(m.registryDir, agent)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrUnknownAgent, agent, err)
	}

	var result *AdoptResult
	err = m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		lf := viewFromLock(pl)
		entry := lf.entry(agent, client)
		if entry == nil {
			return fmt.Errorf("%w: %s", ErrNotProjected, agent)
		}

		canonicalFile := filepath.Join(a.Dir, "AGENT.md")
		res := &AdoptResult{Agent: agent, Client: client, Target: entry.Target, CanonicalFile: canonicalFile}

		projected, err := validateAdoptedAgent(entry.Target, agent)
		if err != nil {
			return err
		}
		canon, err := os.ReadFile(canonicalFile) // #nosec G304 -- fixed name inside the managed store
		if err != nil {
			return fmt.Errorf("reading canonical AGENT.md: %w", err)
		}

		// Policy-owned deltas are not adoptable: when the projection
		// carries a model policy rewrite whose value is still in place,
		// the model key is restored to the author's canonical declaration
		// before any comparison or write-back, so an adopt can never
		// poison the canonical store with a policy-resolved value. A
		// projection whose only delta was the policy key then compares
		// equal and adopts nothing. A model key the user deliberately
		// edited to some other value is the user's edit and adopts like
		// any other change.
		if entry.ModelValue != "" {
			projModel, projScalarOK := declaredAgentModel(projected)
			if projScalarOK && registry.NormalizeModelValue(projModel) == registry.NormalizeModelValue(entry.ModelValue) {
				// The projected model is the policy's own write, so it MUST
				// be restored before write-back. Any failure here is a hard
				// refusal: falling through would adopt the policy-resolved
				// value into the canonical store, the exact poisoning the
				// invariant forbids.
				canonModel, scalarOK := declaredAgentModel(canon)
				if !scalarOK {
					return &AdoptRefusal{msg: fmt.Sprintf(
						"cannot adopt %s's copy of %s: the canonical model declaration is not a single-line scalar, so the projection's policy-written model key cannot be restored to it; edit the canonical AGENT.md first", client, agent)}
				}
				restored, rok := rewriteAgentModel(projected, canonModel)
				if !rok {
					return &AdoptRefusal{msg: fmt.Sprintf(
						"cannot adopt %s's copy of %s: its policy-written model key could not be restored to the author's declaration (the frontmatter is not line-rewritable); revert the model line by hand and re-run adopt", client, agent)}
				}
				if !bytes.Equal(restored, projected) {
					res.PolicyKeysRestored = true
				}
				projected = restored
			}
		}

		if !bytes.Equal(projected, canon) {
			res.Changed = true
			// Store-side backup first, following the pkg/skills .pre-<sha>
			// convention (sha from the import origin when the agent tracks
			// one, "local" otherwise).
			sha := ""
			if skills.HasOrigin(a.Dir) {
				if origin, oerr := skills.ReadOrigin(a.Dir); oerr == nil {
					sha = skills.ShortSHA(origin.CommitSHA)
				} else {
					slog.Debug("adopt could not read the agent's import origin", "agent", agent, "error", oerr)
				}
			}
			backup, berr := backupAgentFile(canonicalFile, sha)
			if berr != nil {
				return fmt.Errorf("backing up canonical AGENT.md: %w", berr)
			}
			res.BackupFile = backup
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := project.AtomicWriteFile(canonicalFile, projected); err != nil {
				return fmt.Errorf("writing %s: %w (the previous AGENT.md is kept as %s)", canonicalFile, err, backup)
			}
		}

		// Re-sync the pair so the recorded hashes match the adopted
		// content. A rewritten projection is deliberately not
		// re-materialized: this manager may hold no policy (CLI adopt has
		// no stack context) and a force-resync would revert the rewrite.
		// The lock is refreshed to on-disk truth instead; the next
		// policy-aware sync reconciles anything left stale.
		if entry.ModelValue != "" {
			diskProjected, exists, derr := readIfExists(entry.Target)
			if derr != nil || !exists {
				return fmt.Errorf("re-reading projected file after adopt: %v", derr)
			}
			newCanon, cerr := os.ReadFile(canonicalFile) // #nosec G304 -- fixed name inside the managed store
			if cerr != nil {
				return fmt.Errorf("re-reading canonical AGENT.md after adopt: %w", cerr)
			}
			// The rewrite marker describes current bytes: when the adopt
			// equalized the projection and the canonical (the user's own
			// model edit was adopted), nothing rewritten remains.
			mv := entry.ModelValue
			if bytes.Equal(diskProjected, newCanon) {
				mv = ""
			}
			m.record(lf, agent, client, entry.Target, contentHash(diskProjected), contentHash(newCanon), mv, "")
		} else {
			// Force-resync the pair so the recorded hashes match the
			// adopted content (the projected file drifted from the
			// recorded hash by definition when Changed).
			sres := m.materialize(skills.InstalledAgent{Name: agent, Dir: a.Dir}, t, lf, SyncOptions{Force: true})
			if sres.Action == ActionError {
				return fmt.Errorf("re-syncing %s to %s after adopt: %s", agent, client, sres.Error)
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

// validateAdoptedAgent refuses projected content the store could not
// serve: a missing or empty file, one that does not parse as an agent
// definition, or one that renames the agent.
func validateAdoptedAgent(target, agent string) ([]byte, error) {
	data, err := os.ReadFile(target) // #nosec G304 -- path comes from the lockfile
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &AdoptRefusal{msg: fmt.Sprintf("projected file %s is gone; refusing to adopt", target)}
		}
		return nil, fmt.Errorf("reading projected agent file: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, &AdoptRefusal{msg: fmt.Sprintf("projected content in %s is empty; refusing to adopt an empty agent", target)}
	}
	parsed, perr := skills.ParseAgentMD(data)
	if perr != nil {
		return nil, &AdoptRefusal{msg: fmt.Sprintf("projected file %s is not a valid agent definition: %v", target, perr)}
	}
	if parsed.Name != "" && parsed.Name != agent {
		return nil, &AdoptRefusal{msg: fmt.Sprintf("projected file %s names the agent %q, not %q; adopt cannot rename agents", target, parsed.Name, agent)}
	}
	return data, nil
}

// backupAgentFile copies AGENT.md to AGENT.md.pre-<sha> (or .pre-local)
// in the same directory, mirroring skills.BackupSkillFileInDir.
func backupAgentFile(canonicalFile, sha string) (string, error) {
	suffix := "local"
	if sha != "" {
		suffix = sha
	}
	data, err := os.ReadFile(canonicalFile) // #nosec G304 -- fixed name inside the managed store
	if err != nil {
		return "", err
	}
	backup := canonicalFile + ".pre-" + suffix
	if err := project.AtomicWriteFile(backup, data); err != nil {
		return "", err
	}
	return backup, nil
}
