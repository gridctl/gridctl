package ctxpins

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
)

// Document is one fragment (or the canonical AGENTS.md) as the pin store
// sees it. Pack is filled by the CLI and API adapters from the skills
// lockfile; PinDocuments leaves it empty. ReadErr is a per-document read
// or parse failure. Callers must not turn that into a whole-list error:
// an unreadable fragment fails closed for itself and must not skip pinning
// for every other fragment.
type Document struct {
	Name        string
	Description string
	Body        string
	Raw         []byte
	Pack        string
	ReadErr     error
}

// Source is the read surface Sync, Verify, and Approve need. Defining it
// here keeps pkg/ctxpins free of pkg/contexts.
type Source interface {
	ListDocuments(ctx context.Context) ([]Document, error)
	GetDocument(ctx context.Context, name string) (Document, error)
}

// DocSource is a Source backed by an already-loaded document list.
type DocSource struct {
	Docs []Document
}

// ListDocuments returns the documents. Cancellation is honored before the
// copy is returned.
func (s DocSource) ListDocuments(ctx context.Context) ([]Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]Document, len(s.Docs))
	copy(out, s.Docs)
	return out, nil
}

// GetDocument returns the named document. A missing name wraps ErrNoDocument.
// A document with ReadErr set is returned as-is so the store can fail closed
// instead of treating an unreadable file as absent.
func (s DocSource) GetDocument(ctx context.Context, name string) (Document, error) {
	if err := ctx.Err(); err != nil {
		return Document{}, err
	}
	for _, d := range s.Docs {
		if d.Name == name {
			return d, nil
		}
	}
	return Document{}, fmt.Errorf("ctxpins: fragment %q: %w", name, ErrNoDocument)
}

// Summarize compares stored pins with the current documents. It does not
// write. An unreadable pinned document is drift. An unreadable unpinned
// document is omitted: there is nothing trustworthy to call unpinned or
// missing. Slices are non-nil and sorted.
func Summarize(records map[string]*FragmentPin, docs []Document) Summary {
	seen := make(map[string]bool, len(docs))
	var drifted, unpinned []string
	for _, d := range docs {
		if d.Name == "" {
			continue
		}
		seen[d.Name] = true
		pin := records[d.Name]
		if pin == nil {
			if d.ReadErr == nil {
				unpinned = append(unpinned, d.Name)
			}
			continue
		}
		if d.ReadErr != nil || pin.Status == StatusDrift || pin.Hash != Hash(d.Raw) {
			drifted = append(drifted, d.Name)
		}
	}
	var missing []string
	for name := range records {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(drifted)
	sort.Strings(missing)
	sort.Strings(unpinned)
	if drifted == nil {
		drifted = []string{}
	}
	if missing == nil {
		missing = []string{}
	}
	if unpinned == nil {
		unpinned = []string{}
	}
	return Summary{Drifted: drifted, Missing: missing, Unpinned: unpinned}
}

// AnnotatePack sets Document.Pack when the skills lockfile lists the
// fragment name under a pack source's Rules or RuleFiles. A lockfile read
// error leaves provenance empty and logs at DEBUG. The first pack name
// wins when more than one source lists the same fragment.
func AnnotatePack(docs []Document, lockPath string) []Document {
	if lockPath == "" || len(docs) == 0 {
		return docs
	}
	lf, err := skills.ReadLockFile(lockPath)
	if err != nil {
		slog.Debug("ctxpins: skills lockfile unreadable; provenance left empty", "path", lockPath, "error", err)
		return docs
	}
	packs := packNames(lf)
	if len(packs) == 0 {
		return docs
	}
	out := make([]Document, len(docs))
	copy(out, docs)
	for i := range out {
		if pack, ok := packs[out[i].Name]; ok {
			out[i].Pack = pack
		}
	}
	return out
}

func packNames(lf *skills.LockFile) map[string]string {
	if lf == nil {
		return nil
	}
	out := map[string]string{}
	for _, src := range lf.Sources {
		if src.Pack == nil || src.Pack.Name == "" {
			continue
		}
		name := src.Pack.Name
		for _, rule := range src.Pack.Rules {
			if _, ok := out[rule]; !ok {
				out[rule] = name
			}
		}
		for rule := range src.Pack.RuleFiles {
			if _, ok := out[rule]; !ok {
				out[rule] = name
			}
		}
	}
	return out
}

// DeployedScanConfig reads gateway.security.schema_pinning scan and
// scan_ignore from the single deployed stack. Zero stacks, more than one
// stack, or a stack that does not load all mean the defaults: scan on, no
// ignores. A nil Scan pointer is on. This never returns an error; stack
// resolution failure must not fail a ctx command.
func DeployedScanConfig() (bool, []string) {
	states, err := state.List()
	if err != nil || len(states) != 1 || states[0].StackFile == "" {
		return true, nil
	}
	stack, err := config.LoadStack(states[0].StackFile, config.WithQuietLoad())
	if err != nil || stack == nil {
		slog.Debug("ctxpins: deployed stack did not load; using default scan config", "error", err)
		return true, nil
	}
	if stack.Gateway == nil || stack.Gateway.Security == nil || stack.Gateway.Security.SchemaPinning == nil {
		return true, nil
	}
	sp := stack.Gateway.Security.SchemaPinning
	scan := true
	if sp.Scan != nil {
		scan = *sp.Scan
	}
	return scan, append([]string(nil), sp.ScanIgnore...)
}

// PinStatus reports pinned, drift, or unpinned for one document against an
// optional stored record. It does not write. An unreadable pinned document
// is drift.
func PinStatus(pin *FragmentPin, doc Document) string {
	if pin == nil {
		return "unpinned"
	}
	if doc.ReadErr != nil || pin.Status == StatusDrift || pin.Hash != Hash(doc.Raw) {
		return StatusDrift
	}
	return StatusPinned
}
