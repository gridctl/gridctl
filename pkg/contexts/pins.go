package contexts

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PinDocument is one fragment, or the canonical AGENTS.md, as a pinner
// sees it. Pack is left empty here; the CLI and API adapters fill it from
// the skills lockfile. ReadErr is per file so one unreadable fragment does
// not skip pinning for the rest.
type PinDocument struct {
	Name        string
	Description string
	Body        string
	Raw         []byte
	Pack        string
	ReadErr     error
}

// PinSyncResult is the pinner's summary of one pass. Slices are whatever
// the pinner returns; callers treat a nil result as an empty pass.
type PinSyncResult struct {
	Pinned  []string
	Drifted []string
	Missing []string
}

// Pinner records fragment content against a human approval. pkg/contexts
// does not import the pin store; cmd and internal/api attach an implementation
// at manager construction. A nil pinner leaves projection unchanged.
type Pinner interface {
	Sync(ctx context.Context, docs []PinDocument) (*PinSyncResult, error)
	Rename(ctx context.Context, from, to string) error
}

// SetPinner attaches the pin store. Call it before the manager is shared.
// A nil pinner disables pinning, which is how tests that build a bare
// manager keep today's migration behavior.
func (m *Manager) SetPinner(p Pinner) {
	m.pinner = p
}

// PinNow runs one pin pass over the current store. It does not project.
// A nil pinner returns an empty result.
func (m *Manager) PinNow(ctx context.Context) (*PinSyncResult, error) {
	if m.pinner == nil {
		return &PinSyncResult{}, nil
	}
	docs, err := m.PinDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return m.pinner.Sync(ctx, docs)
}

// PinDocuments returns the pin inputs. Fragments mode yields one document
// per *.md file, with a per-file ReadErr instead of a whole-list failure.
// Single-file mode yields one document named AGENTS.md, or an empty list
// when the canonical file does not exist. A non-existence read is not a
// ReadErr: the name is absent, so a stored pin is missing rather than drift.
func (m *Manager) PinDocuments(ctx context.Context) ([]PinDocument, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.FragmentsActive() {
		return m.fragmentPinDocuments(ctx)
	}
	return m.canonicalPinDocument()
}

func (m *Manager) canonicalPinDocument() ([]PinDocument, error) {
	data, err := os.ReadFile(m.CanonicalPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return []PinDocument{{Name: canonicalFileName, ReadErr: err}}, nil
	}
	return []PinDocument{{
		Name: canonicalFileName,
		Body: string(data),
		Raw:  data,
	}}, nil
}

func (m *Manager) fragmentPinDocuments(ctx context.Context) ([]PinDocument, error) {
	entries, err := os.ReadDir(m.FragmentsDir())
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	docs := make([]PinDocument, 0, len(names))
	for _, fileName := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(fileName, ".md")
		path := filepath.Join(m.FragmentsDir(), fileName)
		data, err := os.ReadFile(path)
		if err != nil {
			docs = append(docs, PinDocument{Name: name, ReadErr: err})
			continue
		}
		f, err := ParseFragment(name, data)
		if err != nil {
			docs = append(docs, PinDocument{Name: name, Raw: append([]byte(nil), data...), ReadErr: err})
			continue
		}
		docs = append(docs, PinDocument{
			Name:        f.Name,
			Description: f.Description,
			Body:        f.Body,
			Raw:         append([]byte(nil), data...),
		})
	}
	return docs, nil
}

// pinBeforeProject runs the pinner when one is set and the pass writes.
// A pinner error is logged and does not fail the sync. The returned names
// are the fragments currently in pin drift.
func (m *Manager) pinBeforeProject(ctx context.Context, opts SyncOptions) []string {
	if opts.DryRun || m.pinner == nil {
		return nil
	}
	docs, err := m.PinDocuments(ctx)
	if err != nil {
		slog.Warn("contexts: fragment pin pass failed", "error", err)
		return nil
	}
	res, err := m.pinner.Sync(ctx, docs)
	if err != nil {
		slog.Warn("contexts: fragment pin pass failed", "error", err)
		return nil
	}
	if res == nil {
		return nil
	}
	return append([]string(nil), res.Drifted...)
}

// annotatePinDrift marks projection rows whose source is in pin drift.
// Multi-file rows are marked per fragment. Single-file and compiled rows
// represent the whole document. Detail is filled only when the row has none,
// so an existing render note is not overwritten.
func annotatePinDrift(results []SyncResult, drifted []string) {
	if len(drifted) == 0 {
		return
	}
	set := make(map[string]bool, len(drifted))
	for _, name := range drifted {
		set[name] = true
	}
	detail := "pin drift: " + strings.Join(drifted, ", ") + "; review with 'gridctl ctx pins diff'"
	for i := range results {
		if !rowInPinDrift(&results[i], set) {
			continue
		}
		results[i].PinDrift = true
		if results[i].Detail == "" {
			results[i].Detail = detail
		}
	}
}

func rowInPinDrift(r *SyncResult, drifted map[string]bool) bool {
	if r.Fragment != "" {
		return drifted[r.Fragment]
	}
	if r.Mode == ModeMultiFile {
		return false
	}
	if r.Mode == ModeCompiled {
		return len(drifted) > 0
	}
	return drifted[canonicalFileName]
}
