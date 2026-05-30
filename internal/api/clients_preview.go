package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// clientScopeImpact is the per-client before/after delta the commit gate renders
// as a plain-language impact summary ("loses gitlab; 12 of 43 tools visible").
type clientScopeImpact struct {
	Name          string   `json:"name"`
	Slug          string   `json:"slug"`
	BeforeServers int      `json:"beforeServers"`
	AfterServers  int      `json:"afterServers"`
	BeforeTools   int      `json:"beforeTools"`
	AfterTools    int      `json:"afterTools"`
	LostServers   []string `json:"lostServers"`
	GainedServers []string `json:"gainedServers"`
}

// scopePreviewResponse is the read-only preview of committing a client-scope
// draft: the exact stack.yaml patch that the matching PUT would write, plus the
// computed per-client consequences. Nothing is written.
type scopePreviewResponse struct {
	Client       string              `json:"client"`
	ProfileKey   string              `json:"profileKey"`
	CreatesBlock bool                `json:"createsBlock"`
	Lockout      bool                `json:"lockout"`
	TotalServers int                 `json:"totalServers"`
	TotalTools   int                 `json:"totalTools"`
	Diff         string              `json:"diff"`
	Selected     clientScopeImpact   `json:"selected"`
	Affected     []clientScopeImpact `json:"affected"`
}

// handleClientScopePreview computes what committing a per-client access draft
// would do, without touching the stack file. It returns the exact YAML patch
// (produced by the same yaml.Node round-trip the write path uses, so comments,
// ordering, and sibling profiles are faithful) and a per-client impact summary
// covering the edited client plus, when the draft would create the `clients:`
// block, the unlisted clients that flip to default-deny.
//
// POST /api/clients/{slug}/scope/preview
func (s *Server) handleClientScopePreview(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		writeJSONError(w, "Client slug is required", http.StatusBadRequest)
		return
	}
	profileKey := mcp.NormalizeClientID(slug)
	if profileKey == "" {
		writeStructuredError(w, http.StatusBadRequest, errCodeInvalidClient,
			"Client identifier is empty after normalization.",
			"Provide a non-empty client slug.")
		return
	}
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}
	if s.gateway == nil {
		writeJSONError(w, "Gateway unavailable", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, setToolsRequestMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req setClientScopeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Servers == nil && req.Tools == nil {
		writeJSONError(w, "Request must set servers and/or tools", http.StatusBadRequest)
		return
	}
	servers := normalizeScopeAxis(req.Servers)
	tools := normalizeScopeAxis(req.Tools)
	for _, v := range append(append([]string{}, derefStrings(servers)...), derefStrings(tools)...) {
		if v == "" {
			writeJSONError(w, "Server and tool names must be non-empty strings", http.StatusBadRequest)
			return
		}
	}
	// Same validation as the write path so the preview can't promise a scope the
	// write would reject with a 422.
	if code, msg := s.validateClientScope(derefStrings(servers), derefStrings(tools)); code != "" {
		writeStructuredError(w, http.StatusUnprocessableEntity, code, msg,
			"Refresh the workspace to pick up the current servers and tools, then try again.")
		return
	}

	// Exact patch: run the real round-trip against the on-disk file and diff.
	original, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := patchClientScope(original, profileKey, servers, tools)
	if err != nil {
		writeJSONError(w, "Failed to compute patch: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := scopePreviewResponse{
		Client:       slug,
		ProfileKey:   profileKey,
		CreatesBlock: !s.gateway.ClientAccessConfigured(),
		TotalServers: len(s.gateway.Status()),
		TotalTools:   len(s.gateway.CatalogToolNames()),
		Diff:         unifiedDiff(string(original), string(updated), 3),
	}

	before := s.gateway.ClientScope(slug)
	after := s.gateway.ClientScopePreview(slug, derefStrings(servers), derefStringsNil(tools))
	resp.Selected = impactFromScopes(slug, slug, before, after)
	// An empty resulting scope is the lockout footgun: a profiled client that can
	// reach nothing. The editor forbids saving it; flag it so the gate can block.
	resp.Lockout = after.Configured && !after.Unscoped &&
		len(after.Servers) == 0 && len(after.Tools) == 0

	// Cross-client consequence: creating the block flips every other listed
	// client to the default policy. Per-client scope is otherwise independent, so
	// an edit to an existing block touches no one else.
	if resp.CreatesBlock {
		resp.Affected = s.affectedByNewBlock(profileKey)
	}

	slog.Default().Debug("client scope preview computed", "client", profileKey,
		"createsBlock", resp.CreatesBlock, "lockout", resp.Lockout, "affected", len(resp.Affected))
	writeJSON(w, resp)
}

// affectedByNewBlock lists the linked clients (other than the one being edited)
// that lose access when a `clients:` block is created for the first time: with
// no block today they reach everything, and an unlisted client under the default
// (deny) policy reaches nothing.
func (s *Server) affectedByNewBlock(editedKey string) []clientScopeImpact {
	if s.provisioners == nil {
		return nil
	}
	serverName := s.linkServerName
	if serverName == "" {
		serverName = "gridctl"
	}
	var out []clientScopeImpact
	for _, info := range s.provisioners.AllClientInfo(serverName) {
		if !info.Linked || mcp.NormalizeClientID(info.Slug) == editedKey {
			continue
		}
		before := s.gateway.ClientScope(info.Slug)
		after := mcp.ClientScopeResult{Configured: true} // default-deny: reaches nothing
		out = append(out, impactFromScopes(info.Name, info.Slug, before, after))
	}
	return out
}

// impactFromScopes diffs a before/after scope into a UI-facing delta, computing
// which servers a client gains or loses.
func impactFromScopes(name, slug string, before, after mcp.ClientScopeResult) clientScopeImpact {
	beforeSet := make(map[string]bool, len(before.Servers))
	for _, s := range before.Servers {
		beforeSet[s] = true
	}
	afterSet := make(map[string]bool, len(after.Servers))
	for _, s := range after.Servers {
		afterSet[s] = true
	}
	var lost, gained []string
	for _, s := range before.Servers {
		if !afterSet[s] {
			lost = append(lost, s)
		}
	}
	for _, s := range after.Servers {
		if !beforeSet[s] {
			gained = append(gained, s)
		}
	}
	return clientScopeImpact{
		Name:          name,
		Slug:          slug,
		BeforeServers: len(before.Servers),
		AfterServers:  len(after.Servers),
		BeforeTools:   len(before.Tools),
		AfterTools:    len(after.Tools),
		LostServers:   lost,
		GainedServers: gained,
	}
}

// derefStringsNil returns the slice value of a possibly-nil *[]string, keeping
// nil as nil. Unlike derefStrings (which coerces nil to an empty slice for
// validation), the preview needs nil preserved so ClientScopePreview can tell
// "leave the tools axis untouched" apart from "replace tools with none".
func derefStringsNil(p *[]string) []string {
	if p == nil {
		return nil
	}
	return *p
}
