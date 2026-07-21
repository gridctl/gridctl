package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

// pinsToolDiff is the wire form of pins.ToolDiff. Findings merge the pin-time
// scan results with the cross-server shadowing check (P006), which runs here
// because only the API layer has the router's full tool inventory in hand.
type pinsToolDiff struct {
	Name           string         `json:"name"`
	OldHash        string         `json:"old_hash"`
	NewHash        string         `json:"new_hash"`
	OldDescription string         `json:"old_description"`
	NewDescription string         `json:"new_description"`
	Findings       []pins.Finding `json:"findings"`
	// GroupsRewriting names the tool groups whose overrides rewrite this
	// tool's description. Advisory: those rewrites were written against the
	// old upstream definition and should be reviewed against the drift.
	GroupsRewriting []string `json:"groups_rewriting,omitempty"`
}

// pinsDiffResponse is the document returned by GET /api/pins/{server}/diff.
// Slices are always non-nil so consumers see [] rather than null.
// LiveServerHash is the fingerprint Approve would store for the live tools;
// pass it back as expected_server_hash on approve to bind the approval to
// this reviewed snapshot.
type pinsDiffResponse struct {
	Server         string         `json:"server"`
	Status         string         `json:"status"`
	LiveServerHash string         `json:"live_server_hash"`
	ModifiedTools  []pinsToolDiff `json:"modified_tools"`
	NewTools       []string       `json:"new_tools"`
	RemovedTools   []string       `json:"removed_tools"`
}

// rewritingFor maps a drifted tool's raw name to the groups whose overrides
// rewrite its description; nil disables the cross-reference.
func buildPinsDiffResponse(vr *pins.VerifyResult, liveServerHash string, shadow map[string][]pins.Finding, rewritingFor func(toolName string) []string) pinsDiffResponse {
	resp := pinsDiffResponse{
		Server:         vr.ServerName,
		Status:         vr.Status,
		LiveServerHash: liveServerHash,
		ModifiedTools:  make([]pinsToolDiff, 0, len(vr.ModifiedTools)),
		NewTools:       vr.NewTools,
		RemovedTools:   vr.RemovedTools,
	}
	if resp.NewTools == nil {
		resp.NewTools = []string{}
	}
	if resp.RemovedTools == nil {
		resp.RemovedTools = []string{}
	}
	for _, d := range vr.ModifiedTools {
		findings := append(append([]pins.Finding{}, d.Findings...), shadow[d.Name]...)
		var groupsRewriting []string
		if rewritingFor != nil {
			groupsRewriting = rewritingFor(d.Name)
		}
		resp.ModifiedTools = append(resp.ModifiedTools, pinsToolDiff{
			Name:            d.Name,
			OldHash:         d.OldHash,
			NewHash:         d.NewHash,
			OldDescription:  d.OldDescription,
			NewDescription:  d.NewDescription,
			Findings:        findings,
			GroupsRewriting: groupsRewriting,
		})
	}
	return resp
}

// scanContext bundles the per-request state for the P006 cross-server
// shadowing check: the live tool inventory (built once per request, not per
// server) and the store's scan settings. A nil scanContext means scanning is
// off and every shadow helper becomes a no-op.
type scanContext struct {
	inventory map[string][]string
	ignore    []string
}

// newScanContext snapshots the router inventory and scan config, or returns
// nil when the scanner is disabled.
func (s *Server) newScanContext() *scanContext {
	if s.pinStore == nil || !s.pinStore.ScanEnabled() {
		return nil
	}
	inventory := make(map[string][]string)
	for _, client := range s.gateway.Router().Clients() {
		tools := client.Tools()
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			names = append(names, t.Name)
		}
		inventory[client.Name()] = names
	}
	return &scanContext{inventory: inventory, ignore: s.pinStore.ScanIgnoreCodes()}
}

// shadowFindings runs the P006 check for the given tools of serverName.
func (sc *scanContext) shadowFindings(serverName string, tools []mcp.Tool) map[string][]pins.Finding {
	if sc == nil {
		return nil
	}
	out := make(map[string][]pins.Finding)
	for _, t := range tools {
		if findings := pins.FilterFindings(pins.ScanShadowing(t, serverName, sc.inventory), sc.ignore); len(findings) > 0 {
			out[t.Name] = findings
		}
	}
	return out
}

// handleListPins returns all servers' pin records.
// GET /api/pins
//
// When schema pinning is not configured the store is nil. That is a normal,
// expected state rather than an error, so this returns 200 with an empty object
// (the same shape as a configured-but-empty store). The UI polls this endpoint
// on every refresh cycle; returning a 5xx here would log a console error on each
// poll for stacks that simply do not enable pinning.
func (s *Server) handleListPins(w http.ResponseWriter, r *http.Request) {
	if s.pinStore == nil {
		writeJSON(w, map[string]any{})
		return
	}
	servers := s.pinStore.GetAll()
	sc := s.newScanContext()
	for name, sp := range servers {
		decorateServerPins(sc, name, sp)
	}
	writeJSON(w, servers)
}

// decorateServerPins overlays P006 shadowing findings onto a server's pin
// records. Stored findings cover the pin-time checks (P001-P005); shadowing
// is relative to the CURRENT set of registered servers, so it is recomputed
// per read from stored descriptions. sp is the deep copy returned by the
// store's getters, so appending in place never touches store state.
func decorateServerPins(sc *scanContext, serverName string, sp *pins.ServerPins) {
	if sc == nil || sp == nil {
		return
	}
	tools := make([]mcp.Tool, 0, len(sp.Tools))
	for _, rec := range sp.Tools {
		tools = append(tools, mcp.Tool{Name: rec.Name, Description: rec.Description})
	}
	shadow := sc.shadowFindings(serverName, tools)
	for name, extra := range shadow {
		if rec, ok := sp.Tools[name]; ok {
			rec.Findings = append(rec.Findings, extra...)
		}
	}
}

// handleGetServerPins returns the pin record for a single server.
// GET /api/pins/{server}
func (s *Server) handleGetServerPins(w http.ResponseWriter, r *http.Request) {
	if s.pinStore == nil {
		writeJSONError(w, "Pin store not available", http.StatusServiceUnavailable)
		return
	}
	serverName := r.PathValue("server")
	sp, ok := s.pinStore.GetServer(serverName)
	if !ok {
		writeJSONError(w, "No pins found for server: "+serverName, http.StatusNotFound)
		return
	}
	decorateServerPins(s.newScanContext(), serverName, sp)
	writeJSON(w, sp)
}

// handlePinsDiff compares a server's pinned tool definitions against its live
// tools and returns the per-tool delta (modified, new, removed). The diff is
// recomputed on demand via the read-only Verify path; nothing is persisted, so
// viewing a diff never mutates pin state.
// GET /api/pins/{server}/diff
func (s *Server) handlePinsDiff(w http.ResponseWriter, r *http.Request) {
	if s.pinStore == nil {
		writeJSONError(w, "Pin store not available", http.StatusServiceUnavailable)
		return
	}
	serverName := r.PathValue("server")

	// A server with no pins has nothing to diff against; mirror the
	// get-server semantics rather than returning an empty diff.
	if _, ok := s.pinStore.GetServer(serverName); !ok {
		writeJSONError(w, "No pins found for server: "+serverName, http.StatusNotFound)
		return
	}

	client := s.gateway.Router().GetClient(serverName)
	if client == nil {
		writeJSONError(w, "Server not found in gateway: "+serverName, http.StatusNotFound)
		return
	}

	tools := client.Tools()
	vr, err := s.pinStore.Verify(serverName, tools)
	if err != nil {
		writeJSONError(w, "Failed to compute diff: "+err.Error(), http.StatusInternalServerError)
		return
	}

	liveHash, err := pins.HashTools(tools)
	if err != nil {
		writeJSONError(w, "Failed to fingerprint live tools: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, buildPinsDiffResponse(vr, liveHash, s.newScanContext().shadowFindings(serverName, tools),
		func(toolName string) []string {
			return s.gateway.CurrentGroupPolicy().GroupsRewritingTool(mcp.PrefixTool(serverName, toolName))
		}))
}

// handleApprovePins re-pins the current tool definitions for a server, clearing drift.
// POST /api/pins/{server}/approve
//
// The optional JSON body {"expected_server_hash": "..."} binds the approval
// to a reviewed diff: when present, the live tools must still hash to that
// fingerprint (as returned by the diff endpoint's live_server_hash) or the
// approval is rejected with 409, so definitions that changed after review
// can never be pinned unseen. An empty body preserves the unconditional
// approve for existing callers.
func (s *Server) handleApprovePins(w http.ResponseWriter, r *http.Request) {
	if s.pinStore == nil {
		writeJSONError(w, "Pin store not available", http.StatusServiceUnavailable)
		return
	}
	serverName := r.PathValue("server")

	var body struct {
		ExpectedServerHash string `json:"expected_server_hash"`
	}
	if r.Body != nil {
		// A missing or empty body is fine; only a malformed one is an error.
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeJSONError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Verify the server has existing pins before approving.
	if _, ok := s.pinStore.GetServer(serverName); !ok {
		writeJSONError(w, "No pins found for server: "+serverName, http.StatusNotFound)
		return
	}

	// Fetch the current live tools from the gateway router.
	client := s.gateway.Router().GetClient(serverName)
	if client == nil {
		writeJSONError(w, "Server not found in gateway: "+serverName, http.StatusNotFound)
		return
	}
	tools := client.Tools()

	if body.ExpectedServerHash != "" {
		liveHash, err := pins.HashTools(tools)
		if err != nil {
			writeJSONError(w, "Failed to fingerprint live tools: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if liveHash != body.ExpectedServerHash {
			writeJSONError(w,
				"Tool definitions changed since the reviewed diff; fetch the diff again and re-review",
				http.StatusConflict)
			return
		}
	}

	if err := s.pinStore.Approve(serverName, tools); err != nil {
		writeJSONError(w, "Failed to approve pins: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"server":     serverName,
		"tool_count": len(tools),
		"status":     "approved",
	})
}

// handleResetPins deletes the pin record for a server.
// DELETE /api/pins/{server}
func (s *Server) handleResetPins(w http.ResponseWriter, r *http.Request) {
	if s.pinStore == nil {
		writeJSONError(w, "Pin store not available", http.StatusServiceUnavailable)
		return
	}
	serverName := r.PathValue("server")

	if _, ok := s.pinStore.GetServer(serverName); !ok {
		writeJSONError(w, "No pins found for server: "+serverName, http.StatusNotFound)
		return
	}

	if err := s.pinStore.Reset(serverName); err != nil {
		writeJSONError(w, "Failed to reset pins: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
