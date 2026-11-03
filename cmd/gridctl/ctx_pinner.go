package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/ctxpins"
	"github.com/gridctl/gridctl/pkg/skills"
)

// contextStorePinner adapts the stateless context pin store to the
// contexts.Pinner interface. Provenance is filled here so pkg/contexts
// never imports the pin store or the skills lockfile.
type contextStorePinner struct {
	store    *ctxpins.Store
	lockPath string
}

func (p *contextStorePinner) Sync(ctx context.Context, docs []contexts.PinDocument) (*contexts.PinSyncResult, error) {
	if p == nil || p.store == nil {
		return &contexts.PinSyncResult{}, nil
	}
	res, err := p.store.Sync(ctx, ctxpins.DocSource{Docs: annotateContextDocs(docs, p.lockPath)})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &contexts.PinSyncResult{}, nil
	}
	return &contexts.PinSyncResult{Pinned: res.Pinned, Drifted: res.Drifted, Missing: res.Missing}, nil
}

func (p *contextStorePinner) Rename(ctx context.Context, from, to string) error {
	if p == nil || p.store == nil {
		return nil
	}
	return p.store.Rename(ctx, from, to)
}

// newContextManager builds the global-context manager and attaches a
// store-backed pinner. Every ctx command and pack apply goes through this
// helper so a write pass pins whether or not the caller remembers to.
func newContextManager() (*contexts.Manager, error) {
	mgr, err := contexts.NewManager()
	if err != nil {
		return nil, err
	}
	attachContextPinner(mgr, skills.LockFilePath())
	return mgr, nil
}

func attachContextPinner(mgr *contexts.Manager, lockPath string) *ctxpins.Store {
	store := ctxpins.New()
	enabled, ignore := ctxpins.DeployedScanConfig()
	store.SetScanConfig(enabled, ignore)
	mgr.SetPinner(&contextStorePinner{store: store, lockPath: lockPath})
	return store
}

func annotateContextDocs(docs []contexts.PinDocument, lockPath string) []ctxpins.Document {
	out := make([]ctxpins.Document, len(docs))
	for i, d := range docs {
		out[i] = ctxpins.Document{
			Name:        d.Name,
			Description: d.Description,
			Body:        d.Body,
			Raw:         d.Raw,
			Pack:        d.Pack,
			ReadErr:     d.ReadErr,
		}
	}
	return ctxpins.AnnotatePack(out, lockPath)
}

func contextDocumentSource(ctx context.Context, mgr *contexts.Manager) (ctxpins.Source, error) {
	docs, err := mgr.PinDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return ctxpins.DocSource{Docs: annotateContextDocs(docs, skills.LockFilePath())}, nil
}

// ctxPinsSummaryDoc is the additive pins object on ctx status and ctx sync.
type ctxPinsSummaryDoc struct {
	Drifted  []string `json:"drifted"`
	Missing  []string `json:"missing"`
	Unpinned []string `json:"unpinned"`
}

func loadContextPinSummary(ctx context.Context, mgr *contexts.Manager) (ctxPinsSummaryDoc, error) {
	store := ctxpins.New()
	enabled, ignore := ctxpins.DeployedScanConfig()
	store.SetScanConfig(enabled, ignore)
	records, err := store.GetAll(ctx)
	if err != nil {
		return ctxPinsSummaryDoc{}, err
	}
	src, err := contextDocumentSource(ctx, mgr)
	if err != nil {
		return ctxPinsSummaryDoc{}, err
	}
	docs, err := src.ListDocuments(ctx)
	if err != nil {
		return ctxPinsSummaryDoc{}, err
	}
	sum := ctxpins.Summarize(records, docs)
	return ctxPinsSummaryDoc{Drifted: sum.Drifted, Missing: sum.Missing, Unpinned: sum.Unpinned}, nil
}

func formatContextPinSummary(sum ctxPinsSummaryDoc) string {
	if len(sum.Drifted) == 0 && len(sum.Missing) == 0 && len(sum.Unpinned) == 0 {
		return ""
	}
	var parts []string
	if len(sum.Drifted) > 0 {
		parts = append(parts, "Pin drift: "+strings.Join(sum.Drifted, ", "))
	}
	if len(sum.Missing) > 0 {
		parts = append(parts, "missing: "+strings.Join(sum.Missing, ", "))
	}
	if len(sum.Unpinned) > 0 {
		parts = append(parts, "unpinned: "+strings.Join(sum.Unpinned, ", "))
	}
	return strings.Join(parts, "; ") + " (review with 'gridctl ctx pins diff')"
}

func writeContextPinSummary(w io.Writer, sum ctxPinsSummaryDoc) {
	if line := formatContextPinSummary(sum); line != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, line)
	}
}

// printContextPinAfterWrite reports a first pin or pin drift for the
// fragment a ctx add or ctx edit just wrote. A pin-store error is printed
// and does not fail the write: the fragment is already on disk.
func printContextPinAfterWrite(ctx context.Context, stdout, stderr io.Writer, mgr *contexts.Manager, name string, added bool) {
	res, err := mgr.PinNow(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "warning: fragment pin pass failed: %v\n", err)
		return
	}
	if res == nil {
		return
	}
	if added {
		fmt.Fprintf(stdout, "Pinned fragment %q\n", name)
		return
	}
	drifted := false
	for _, n := range res.Drifted {
		if n == name {
			drifted = true
			break
		}
	}
	pinned := false
	for _, n := range res.Pinned {
		if n == name {
			pinned = true
			break
		}
	}
	if drifted {
		hash := contextPinExpect(ctx, mgr, name)
		fmt.Fprintf(stdout, "Pin drift: %s. Review with 'gridctl ctx pins diff %s', then approve with 'gridctl ctx pins approve %s --expect %s'.\n", name, name, name, hash)
		return
	}
	if pinned {
		fmt.Fprintf(stdout, "Pinned fragment %q\n", name)
	}
}

func contextPinExpect(ctx context.Context, mgr *contexts.Manager, name string) string {
	src, err := contextDocumentSource(ctx, mgr)
	if err != nil {
		return ""
	}
	doc, err := src.GetDocument(ctx, name)
	if err != nil || doc.ReadErr != nil {
		return ""
	}
	return ctxpins.CompositeHash(ctxpins.Hash(doc.Raw))
}
