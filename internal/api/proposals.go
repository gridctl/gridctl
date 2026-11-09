package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/proposals"
	"github.com/gridctl/gridctl/pkg/secreport"
)

// proposalCreateMaxBytes bounds the create document: a 256 KiB diff plus
// JSON escaping, evidence, reason, and source.
const proposalCreateMaxBytes = 512 * 1024

// proposalsListSchemaVersion versions the list document shared with
// `gridctl proposals list --format json`.
const proposalsListSchemaVersion = 1

var errProposalBodyTooLarge = errors.New("request body too large")

// SetProposalStore injects the proposal store. Tests use it so handlers
// never touch the real home directory. The store is stateless, so one
// instance is safe for the process.
func (s *Server) SetProposalStore(store *proposals.Store) {
	s.proposalStoreOverride = store
}

// proposalStore returns the injected store, or a fresh store for
// ~/.gridctl/proposals. The daemon holds no proposal state.
func (s *Server) proposalStore() *proposals.Store {
	if s.proposalStoreOverride != nil {
		return s.proposalStoreOverride
	}
	return proposals.New()
}

// proposalTargets builds a resolver over the server's live stores. Any
// store that is not available is left nil; the target reports
// ErrStoreUnavailable when it needs it.
func (s *Server) proposalTargets() proposals.Resolver {
	t := &proposals.Targets{
		SkillPins: s.skillPinStore,
		Stack:     s.stackName,
		LockPath:  s.contextLockPath(),
	}
	if s.registryServer != nil {
		t.Registry = s.registryServer.Store()
	}
	if mgr, err := s.contextsMgr(); err == nil {
		t.Context = mgr
	}
	if store, err := s.contextPins(); err == nil {
		t.ContextPins = store
	}
	if hp, err := s.loadHookPins(); err == nil {
		t.HookPins = hp
	}
	if s.proposalResolverWrap != nil {
		return s.proposalResolverWrap(t)
	}
	return t
}

// proposalStatus maps a proposals error to an HTTP status. Order matters:
// a pin error and a failed restore are server faults even though they
// also carry other sentinels.
func proposalStatus(err error) int {
	var werr *proposals.WriteError
	switch {
	case errors.Is(err, proposals.ErrPinFailed):
		return http.StatusInternalServerError
	case errors.As(err, &werr) && werr.RestoreFailed():
		return http.StatusInternalServerError
	case errors.Is(err, proposals.ErrStoreUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, proposals.ErrNotFound), errors.Is(err, proposals.ErrTargetNotFound):
		return http.StatusNotFound
	case errors.Is(err, proposals.ErrBaseStale), errors.Is(err, proposals.ErrTargetDrift), errors.Is(err, proposals.ErrNotPending):
		return http.StatusConflict
	case errors.Is(err, proposals.ErrApplyFailed):
		return http.StatusUnprocessableEntity
	case errors.Is(err, proposals.ErrInvalidInput),
		errors.Is(err, proposals.ErrReasonRequired),
		errors.Is(err, proposals.ErrKindUnsupported),
		errors.Is(err, proposals.ErrDiffUnsupported),
		approvalInputStatus(err):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// proposalErrorText drops the package prefix so the API returns the same
// sentence the CLI prints.
func proposalErrorText(err error) string {
	return strings.TrimPrefix(err.Error(), "proposals: ")
}

func decodeBoundedBody(r *http.Request, dest any, maxBytes int64) error {
	if r == nil || r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("reading request body: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return errProposalBodyTooLarge
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	return json.Unmarshal(data, dest)
}

// proposalListDoc is the list document for REST and the CLI.
type proposalListDoc struct {
	SchemaVersion int                   `json:"schema_version"`
	Proposals     []*proposals.Proposal `json:"proposals"`
	Skipped       []proposals.Skipped   `json:"skipped"`
	Truncated     bool                  `json:"truncated"`
}

// proposalFields renders a record as a JSON object so approve responses
// can carry attribution fields beside it.
func proposalFields(p *proposals.Proposal) (map[string]any, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// handleCreateProposal stages a proposal.
// POST /api/proposals
func (s *Server) handleCreateProposal(w http.ResponseWriter, r *http.Request) {
	var req proposals.CreateRequest
	if err := decodeBoundedBody(r, &req, proposalCreateMaxBytes); err != nil {
		if errors.Is(err, errProposalBodyTooLarge) {
			writeJSONError(w, "Request body too large", http.StatusBadRequest)
			return
		}
		writeJSONError(w, "Invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Principal = mcp.PrincipalFromContext(r.Context())
	p, err := s.proposalStore().Create(r.Context(), req, s.proposalTargets())
	if err != nil {
		writeJSONError(w, proposalErrorText(err), proposalStatus(err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(p)
}

// handleListProposals lists proposals, pending first then newest first.
// GET /api/proposals?status=&kind=
func (s *Server) handleListProposals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := s.proposalStore().List(r.Context(), proposals.Filter{Status: q.Get("status"), Kind: q.Get("kind")})
	if err != nil {
		writeJSONError(w, proposalErrorText(err), proposalStatus(err))
		return
	}
	writeJSON(w, newProposalListDoc(res))
}

func newProposalListDoc(res proposals.ListResult) proposalListDoc {
	doc := proposalListDoc{
		SchemaVersion: proposalsListSchemaVersion,
		Proposals:     res.Proposals,
		Skipped:       res.Skipped,
		Truncated:     res.Truncated,
	}
	if doc.Proposals == nil {
		doc.Proposals = []*proposals.Proposal{}
	}
	if doc.Skipped == nil {
		doc.Skipped = []proposals.Skipped{}
	}
	return doc
}

// handleGetProposal returns one proposal with base_current and pin_status
// refreshed against the live stores.
// GET /api/proposals/{id}
func (s *Server) handleGetProposal(w http.ResponseWriter, r *http.Request) {
	p, err := s.proposalStore().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSONError(w, proposalErrorText(err), proposalStatus(err))
		return
	}
	proposals.Annotate(r.Context(), p, s.proposalTargets())
	writeJSON(w, p)
}

type approveProposalRequest struct {
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
	ApprovedBy string   `json:"approved_by"`
}

// handleApproveProposal approves a pending proposal. On the apply path a
// skill change refreshes the registry router so the gateway's prompt and
// tool views and the pin sync see the new content.
// POST /api/proposals/{id}/approve
func (s *Server) handleApproveProposal(w http.ResponseWriter, r *http.Request) {
	var body approveProposalRequest
	if err := decodeApproveBody(r, &body); err != nil {
		if errors.Is(err, errApproveBodyTooLarge) {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONError(w, "Invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	approvedBy, approvedBySource, err := resolveApprovalAttribution(r, body.ApprovedBy)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := s.proposalStore().Approve(r.Context(), r.PathValue("id"), proposals.ApproveRequest{
		Reason:           body.Reason,
		Evidence:         body.Evidence,
		ApprovedBy:       approvedBy,
		ApprovedBySource: approvedBySource,
	}, s.proposalTargets())
	if p != nil && p.Verdict != nil && p.Verdict.Applied && p.Kind == proposals.KindSkill {
		s.refreshRegistryRouter()
	}
	if err != nil && p == nil {
		writeJSONError(w, proposalErrorText(err), proposalStatus(err))
		return
	}
	fields, ferr := proposalFields(p)
	if ferr != nil {
		writeJSONError(w, "Failed to encode proposal: "+ferr.Error(), http.StatusInternalServerError)
		return
	}
	fields["applied"] = p.Verdict != nil && p.Verdict.Applied
	evidenceCount := 0
	if p.Verdict != nil {
		evidenceCount = len(p.Verdict.Evidence)
	}
	if err != nil {
		// The writes landed but the re-pin failed: the record is approved
		// and carries pin_error, so the body still holds it.
		fields["error"] = proposalErrorText(err)
		fields["approved_by"] = approvedBy
		fields["approved_by_source"] = approvedBySource
		fields["evidence_count"] = evidenceCount
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(proposalStatus(err))
		_ = json.NewEncoder(w).Encode(fields)
		return
	}
	writeApproveJSON(w, fields, approvedBy, approvedBySource, evidenceCount)
}

type rejectProposalRequest struct {
	Reason     string `json:"reason"`
	ApprovedBy string `json:"approved_by"`
}

// handleRejectProposal records a rejection. Nothing is written to the
// target or any approvals log.
// POST /api/proposals/{id}/reject
func (s *Server) handleRejectProposal(w http.ResponseWriter, r *http.Request) {
	var body rejectProposalRequest
	if err := decodeApproveBody(r, &body); err != nil {
		if errors.Is(err, errApproveBodyTooLarge) {
			writeJSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONError(w, "Invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	approvedBy, approvedBySource, err := resolveApprovalAttribution(r, body.ApprovedBy)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := s.proposalStore().Reject(r.Context(), r.PathValue("id"), proposals.RejectRequest{
		Reason:           body.Reason,
		ApprovedBy:       approvedBy,
		ApprovedBySource: approvedBySource,
	})
	if err != nil {
		writeJSONError(w, proposalErrorText(err), proposalStatus(err))
		return
	}
	fields, ferr := proposalFields(p)
	if ferr != nil {
		writeJSONError(w, "Failed to encode proposal: "+ferr.Error(), http.StatusInternalServerError)
		return
	}
	writeApproveJSON(w, fields, approvedBy, approvedBySource, 0)
}

// proposalReportView counts pending proposals for the security report. A
// listing error makes the view unavailable rather than an empty pass.
func (s *Server) proposalReportView(ctx context.Context) *secreport.ProposalView {
	res, err := s.proposalStore().List(ctx, proposals.Filter{Status: proposals.StatusPending})
	if err != nil {
		slog.Warn("security report: proposal store unavailable", "error", err)
		return &secreport.ProposalView{}
	}
	return &secreport.ProposalView{Available: true, Pending: len(res.Proposals)}
}
