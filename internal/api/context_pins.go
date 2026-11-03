package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/ctxpins"
	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/skills"
)

// apiContextPinner adapts the server's stateless context pin store to the
// contexts manager. The store is resolved on each call so a test that
// replaces it is visible without rebuilding the manager.
type apiContextPinner struct {
	srv      *Server
	lockPath string
}

func (p *apiContextPinner) Sync(ctx context.Context, docs []contexts.PinDocument) (*contexts.PinSyncResult, error) {
	store, err := p.srv.contextPins()
	if err != nil {
		return nil, err
	}
	res, err := store.Sync(ctx, ctxpins.DocSource{Docs: annotateAPIContextDocs(docs, p.lockPath)})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &contexts.PinSyncResult{}, nil
	}
	return &contexts.PinSyncResult{Pinned: res.Pinned, Drifted: res.Drifted, Missing: res.Missing}, nil
}

func (p *apiContextPinner) Rename(ctx context.Context, from, to string) error {
	store, err := p.srv.contextPins()
	if err != nil {
		return err
	}
	return store.Rename(ctx, from, to)
}

func annotateAPIContextDocs(docs []contexts.PinDocument, lockPath string) []ctxpins.Document {
	out := make([]ctxpins.Document, len(docs))
	for i, d := range docs {
		out[i] = ctxpins.Document{
			Name: d.Name, Description: d.Description, Body: d.Body,
			Raw: d.Raw, Pack: d.Pack, ReadErr: d.ReadErr,
		}
	}
	return ctxpins.AnnotatePack(out, lockPath)
}

// SetContextPinStore overrides the context pin store. Call it before the
// first request; the store is stateless, so a later write through another
// Store on the same path is visible on the next call.
func (s *Server) SetContextPinStore(store *ctxpins.Store) {
	s.contextPinStore = store
}

func (s *Server) contextPins() (*ctxpins.Store, error) {
	s.contextPinsOnce.Do(func() {
		if s.contextPinStore != nil {
			return
		}
		store := ctxpins.New()
		enabled, ignore := ctxpins.DeployedScanConfig()
		store.SetScanConfig(enabled, ignore)
		s.contextPinStore = store
	})
	if s.contextPinStore == nil {
		return nil, errors.New("context pin store unavailable")
	}
	return s.contextPinStore, nil
}

func (s *Server) contextPinSource(ctx context.Context, mgr *contexts.Manager) (ctxpins.Source, error) {
	docs, err := mgr.PinDocuments(ctx)
	if err != nil {
		return nil, err
	}
	return ctxpins.DocSource{Docs: annotateAPIContextDocs(docs, s.contextLockPath())}, nil
}

func (s *Server) contextLockPath() string {
	if s.skillLockPath != "" {
		return s.skillLockPath
	}
	return skills.LockFilePath()
}

// ctxPinsSummaryDoc is the additive pins object on context read and sync.
type ctxPinsSummaryDoc struct {
	Drifted  []string `json:"drifted"`
	Missing  []string `json:"missing"`
	Unpinned []string `json:"unpinned"`
}

func emptyCtxPinsSummary() ctxPinsSummaryDoc {
	return ctxPinsSummaryDoc{Drifted: []string{}, Missing: []string{}, Unpinned: []string{}}
}

// contextPinSummary is observe-only. A pin-file error is logged and does
// not fail the context document: ctx status and the editor must keep
// working when the pin file is corrupt or newer than this binary.
func (s *Server) contextPinSummary(ctx context.Context, mgr *contexts.Manager) (ctxPinsSummaryDoc, error) {
	store, err := s.contextPins()
	if err != nil {
		return ctxPinsSummaryDoc{}, err
	}
	records, err := store.GetAll(ctx)
	if err != nil {
		slog.Warn("context: pin summary unavailable", "error", err)
		return emptyCtxPinsSummary(), nil
	}
	src, err := s.contextPinSource(ctx, mgr)
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

func (s *Server) contextFragmentPinStatus(ctx context.Context, mgr *contexts.Manager, name string) (string, error) {
	store, err := s.contextPins()
	if err != nil {
		return "", err
	}
	pin, _, err := store.Get(ctx, name)
	if err != nil {
		// name is a request path value. The pin-file error already names
		// the file, so the fragment is omitted rather than logged raw.
		slog.Warn("context: fragment pin status unavailable", "error", err)
		return "unpinned", nil
	}
	src, err := s.contextPinSource(ctx, mgr)
	if err != nil {
		return "", err
	}
	doc, err := src.GetDocument(ctx, name)
	if err != nil {
		if errors.Is(err, ctxpins.ErrNoDocument) {
			return "unpinned", nil
		}
		return "", err
	}
	return ctxpins.PinStatus(pin, doc), nil
}

type contextPinDiffResponse struct {
	Fragment      string         `json:"fragment"`
	Status        string         `json:"status"`
	CompositeHash string         `json:"composite_hash"`
	OldDocument   string         `json:"old_document,omitempty"`
	NewDocument   string         `json:"new_document,omitempty"`
	Findings      []pins.Finding `json:"findings"`
}

// handleListContextPins returns every context pin, keyed by fragment name.
// GET /api/context/pins
//
// An empty store is {} with 200, never 503: context pins are stackless and
// the UI polls this endpoint.
func (s *Server) handleListContextPins(w http.ResponseWriter, r *http.Request) {
	store, err := s.contextPins()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pins, err := store.GetAll(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pins == nil {
		pins = map[string]*ctxpins.FragmentPin{}
	}
	writeJSON(w, pins)
}

// handleGetContextPin returns one pin record.
// GET /api/context/pins/{name}
func (s *Server) handleGetContextPin(w http.ResponseWriter, r *http.Request) {
	store, err := s.contextPins()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := r.PathValue("name")
	pin, ok, err := store.Get(r.Context(), name)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		writeJSONError(w, "No pin found for fragment: "+name, http.StatusNotFound)
		return
	}
	writeJSON(w, pin)
}

// handleContextPinDiff returns the review diff. ErrDigestUnavailable is
// checked before ErrNoDocument so an unreadable fragment is not a reset hint.
// GET /api/context/pins/{name}/diff
func (s *Server) handleContextPinDiff(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	store, err := s.contextPins()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	src, err := s.contextPinSource(r.Context(), mgr)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := r.PathValue("name")
	vr, err := store.Verify(r.Context(), name, src)
	switch {
	case errors.Is(err, ctxpins.ErrDigestUnavailable):
		writeJSONError(w, "Fragment content could not be hashed: "+err.Error(), http.StatusInternalServerError)
		return
	case errors.Is(err, ctxpins.ErrNoDocument), errors.Is(err, ctxpins.ErrNotPinned):
		writeJSONError(w, "No pin found for fragment: "+name, http.StatusNotFound)
		return
	case err != nil:
		writeJSONError(w, "Failed to verify fragment: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resp := contextPinDiffResponse{
		Fragment:      name,
		Status:        vr.Status,
		CompositeHash: vr.CompositeHash,
		Findings:      []pins.Finding{},
	}
	if d := vr.Diff; d != nil {
		resp.OldDocument = d.OldDocument
		resp.NewDocument = d.NewDocument
		resp.Findings = d.Findings
		if resp.Findings == nil {
			resp.Findings = []pins.Finding{}
		}
	}
	writeJSON(w, resp)
}

type approveContextPinRequest struct {
	ExpectedHash string   `json:"expected_hash"`
	Reason       string   `json:"reason"`
	Evidence     []string `json:"evidence"`
	ApprovedBy   string   `json:"approved_by"`
}

// handleApproveContextPin re-pins the current fragment content.
// POST /api/context/pins/{name}/approve
func (s *Server) handleApproveContextPin(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	store, err := s.contextPins()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := r.PathValue("name")
	var body approveContextPinRequest
	if err := decodeApproveBody(r, &body); err != nil {
		if errors.Is(err, errApproveBodyTooLarge) {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONError(w, "Invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := pinapprovals.ValidateReason(body.Reason); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	evidence, err := pinapprovals.ValidateReferences(body.Evidence)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	approvedBy, approvedBySource, err := resolveApprovalAttribution(r, body.ApprovedBy)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	src, err := s.contextPinSource(r.Context(), mgr)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = store.Approve(r.Context(), name, src, ctxpins.ApproveRequest{
		ExpectedHash:     body.ExpectedHash,
		Reason:           body.Reason,
		Evidence:         evidence,
		ApprovedBy:       approvedBy,
		ApprovedBySource: approvedBySource,
	})
	switch {
	case approvalInputStatus(err):
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, ctxpins.ErrDigestUnavailable):
		writeJSONError(w, "Fragment content could not be hashed: "+err.Error(), http.StatusInternalServerError)
		return
	case errors.Is(err, ctxpins.ErrNoDocument):
		// Approve re-pins a present fragment that has no record. 404 is
		// only for a fragment that is not in the context store.
		writeJSONError(w, "Fragment not found: "+name, http.StatusNotFound)
		return
	case errors.Is(err, ctxpins.ErrHashMismatch):
		writeJSONError(w, "Fragment content changed since the reviewed diff; fetch the diff again and re-review", http.StatusConflict)
		return
	case errors.Is(err, ctxpins.ErrReasonRequired):
		writeJSONError(w, "This fragment carries unresolved advisory findings; approving it requires a reason", http.StatusBadRequest)
		return
	case err != nil:
		writeJSONError(w, "Failed to approve fragment pin: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeApproveJSON(w, map[string]any{"fragment": name, "status": "approved"}, approvedBy, approvedBySource, len(evidence))
}

// handleResetContextPin deletes one pin record. The approvals log is kept.
// DELETE /api/context/pins/{name}
func (s *Server) handleResetContextPin(w http.ResponseWriter, r *http.Request) {
	store, err := s.contextPins()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := r.PathValue("name")
	if _, ok, err := store.Get(r.Context(), name); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	} else if !ok {
		writeJSONError(w, "No pin found for fragment: "+name, http.StatusNotFound)
		return
	}
	if err := store.Reset(r.Context(), name); err != nil {
		if errors.Is(err, ctxpins.ErrNotPinned) {
			writeJSONError(w, "No pin found for fragment: "+name, http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to reset fragment pin: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
