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
	a, err := skills.GetAgent(m.registryDir, agent)
	if err != nil {
		return nil, fmt.Errorf("unknown agent %q: %w", agent, err)
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

		// Force-resync the pair so the recorded hashes match the adopted
		// content (the projected file drifted from the recorded hash by
		// definition when Changed).
		sres := m.materialize(skills.InstalledAgent{Name: agent, Dir: a.Dir}, t, lf, SyncOptions{Force: true})
		if sres.Action == ActionError {
			return fmt.Errorf("re-syncing %s to %s after adopt: %s", agent, client, sres.Error)
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
