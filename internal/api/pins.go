package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gridctl/gridctl/pkg/pins"
)

// pinsToolDiff is the wire form of pins.ToolDiff.
type pinsToolDiff struct {
	Name           string `json:"name"`
	OldHash        string `json:"old_hash"`
	NewHash        string `json:"new_hash"`
	OldDescription string `json:"old_description"`
	NewDescription string `json:"new_description"`
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

func buildPinsDiffResponse(vr *pins.VerifyResult, liveServerHash string) pinsDiffResponse {
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
		resp.ModifiedTools = append(resp.ModifiedTools, pinsToolDiff{
			Name:           d.Name,
			OldHash:        d.OldHash,
			NewHash:        d.NewHash,
			OldDescription: d.OldDescription,
			NewDescription: d.NewDescription,
		})
	}
	return resp
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
	writeJSON(w, servers)
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

	writeJSON(w, buildPinsDiffResponse(vr, liveHash))
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
