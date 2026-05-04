package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/telemetry"
)

// telemetryRequestMaxBytes caps the JSON body of telemetry PATCH requests.
// Bodies are tiny (a handful of keys); 64 KiB matches setToolsRequestMaxBytes
// so no realistic payload is ever near the limit.
const telemetryRequestMaxBytes = 64 * 1024

// telemetryPatchResponse is the success envelope for both PATCH endpoints.
// The refreshed Inventory snapshot lets the UI render the header pill and
// graph node indicators without a follow-up GET — the gateway has not
// rebuilt yet, but the on-disk footprint is what the next render needs.
type telemetryPatchResponse struct {
	Success   bool                        `json:"success"`
	Inventory []telemetry.InventoryRecord `json:"inventory"`
}

// handlePatchStackTelemetry updates the top-level telemetry mapping in the
// active stack YAML. The persistence path uses the same lock + hash + atomic-
// write pattern as setServerTools and handleStackAppend, so concurrent
// callers serialize, external edits between read and write are detected as
// HTTP 409, and a mid-write crash leaves the original file intact.
//
// PATCH /api/stack/telemetry
func (s *Server) handlePatchStackTelemetry(w http.ResponseWriter, r *http.Request) {
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, telemetryRequestMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req stackTelemetryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !req.hasChanges() {
		writeJSONError(w, "Request must include at least one persist or retention field", http.StatusBadRequest)
		return
	}

	mu := stackFileLock(s.stackFile)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	originalHash := sha256.Sum256(original)

	updated, err := patchStackTelemetry(original, req.Persist, req.Retention)
	if err != nil {
		writeJSONError(w, "Failed to patch stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.validatePatchedStack(w, updated, "telemetry patch"); err != nil {
		return
	}

	fireBetweenReadsHook()

	current, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to re-read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if sha256.Sum256(current) != originalHash {
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	}

	if err := atomicWrite(s.stackFile, updated); err != nil {
		writeJSONError(w, "Failed to write stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if s.reloadHandler != nil {
		// Fire-and-forget: the watcher will also see this change and trigger a
		// reload, but invoking it here makes the UI feel snappy when watching
		// is disabled or temporarily blocked. Errors here surface as 502 with
		// the same code handleSetServerTools uses.
		if result, err := s.reloadHandler.Reload(r.Context()); err != nil {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				err.Error(),
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		} else if !result.Success {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				result.Message,
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
	}

	writeJSON(w, telemetryPatchResponse{
		Success:   true,
		Inventory: s.telemetryInventoryOrEmpty(),
	})
}

// handlePatchServerTelemetry updates the per-server telemetry mapping. Same
// safe-rewrite contract as handlePatchStackTelemetry.
//
// PATCH /api/mcp-servers/{name}/telemetry
func (s *Server) handlePatchServerTelemetry(w http.ResponseWriter, r *http.Request) {
	serverName := r.PathValue("name")
	if serverName == "" {
		writeJSONError(w, "Server name is required", http.StatusBadRequest)
		return
	}
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, telemetryRequestMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var req serverTelemetryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	delta, clearAll, err := parseServerPersist(req.Persist)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !clearAll && !delta.hasOps() {
		writeJSONError(w, "Request must include at least one persist override or persist:null to clear", http.StatusBadRequest)
		return
	}

	mu := stackFileLock(s.stackFile)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	originalHash := sha256.Sum256(original)

	updated, err := patchServerTelemetry(original, serverName, delta, clearAll)
	switch {
	case err == nil:
		// proceed
	case errors.Is(err, errServerNotFound):
		writeJSONError(w, "MCP server not found: "+serverName, http.StatusNotFound)
		return
	default:
		writeJSONError(w, "Failed to patch stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.validatePatchedStack(w, updated, "server telemetry patch"); err != nil {
		return
	}

	fireBetweenReadsHook()

	current, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to re-read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if sha256.Sum256(current) != originalHash {
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	}

	if err := atomicWrite(s.stackFile, updated); err != nil {
		writeJSONError(w, "Failed to write stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if s.reloadHandler != nil {
		if result, err := s.reloadHandler.Reload(r.Context()); err != nil {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				err.Error(),
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		} else if !result.Success {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				result.Message,
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
	}

	writeJSON(w, telemetryPatchResponse{
		Success:   true,
		Inventory: s.telemetryInventoryOrEmpty(),
	})
}

// handleGetTelemetryInventory returns one record per (server, signal) pair
// that has at least one file on disk.
//
// GET /api/telemetry/inventory
func (s *Server) handleGetTelemetryInventory(w http.ResponseWriter, r *http.Request) {
	records, err := telemetry.Inventory(s.stackName, "")
	if err != nil {
		writeJSONError(w, "Failed to read telemetry inventory: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if records == nil {
		records = []telemetry.InventoryRecord{}
	}
	writeJSON(w, records)
}

// handleDeleteTelemetry wipes telemetry files for the requested scope.
// Both query parameters are optional; when both are empty the wipe covers
// every server and signal in the stack.
//
// DELETE /api/telemetry?server={name}&signal={logs|metrics|traces}
func (s *Server) handleDeleteTelemetry(w http.ResponseWriter, r *http.Request) {
	if s.stackName == "" {
		writeJSONError(w, "No stack loaded", http.StatusServiceUnavailable)
		return
	}
	server := r.URL.Query().Get("server")
	signal := r.URL.Query().Get("signal")
	if signal != "" && !telemetry.IsValidSignal(signal) {
		writeJSONError(w, "Invalid signal: "+signal+" (expected logs, metrics, or traces)", http.StatusBadRequest)
		return
	}

	if err := telemetry.Wipe(s.stackName, server, signal); err != nil {
		writeJSONError(w, "Failed to wipe telemetry: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, telemetryPatchResponse{
		Success:   true,
		Inventory: s.telemetryInventoryOrEmpty(),
	})
}

// validatePatchedStack parses updated YAML bytes through the typed schema and
// the standard validator. On failure it writes the same 422 envelope as
// handleStackAppend and returns a sentinel non-nil error so the caller can
// short-circuit. On success it returns nil with no body written.
func (s *Server) validatePatchedStack(w http.ResponseWriter, updated []byte, label string) error {
	var stack config.Stack
	if err := yaml.Unmarshal(updated, &stack); err != nil {
		writeJSONError(w, "Failed to parse patched stack: "+err.Error(), http.StatusInternalServerError)
		return err
	}
	config.ExpandStackVarsWithEnv(&stack)
	stack.SetDefaults()
	if result := config.ValidateWithIssues(&stack); !result.Valid {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":      "stack validation failed after " + label,
			"validation": result,
		})
		return errors.New("validation failed")
	}
	return nil
}

// telemetryInventoryOrEmpty wraps telemetry.Inventory so handler responses
// always carry a JSON array (never null) for the inventory field. Errors are
// swallowed and reported as an empty list — the inventory is informational
// alongside the primary success status, and surfacing a partial failure here
// would mask the more useful patch/wipe-succeeded signal.
func (s *Server) telemetryInventoryOrEmpty() []telemetry.InventoryRecord {
	if s.stackName == "" {
		return []telemetry.InventoryRecord{}
	}
	records, err := telemetry.Inventory(s.stackName, "")
	if err != nil || records == nil {
		return []telemetry.InventoryRecord{}
	}
	return records
}
