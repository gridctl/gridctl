package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gridctl/gridctl/internal/stackedit"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/packops"
	"github.com/gridctl/gridctl/pkg/skills"
	"github.com/gridctl/gridctl/pkg/state"
)

const (
	errCodeOverrideRequired = "override_required"
	patchEnabledMaxBytes    = 4 * 1024
)

type patchEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}

type patchEnabledResponse struct {
	Server     string `json:"server"`
	Enabled    bool   `json:"enabled"`
	Changed    bool   `json:"changed"`
	Reloaded   bool   `json:"reloaded"`
	ReloadedAt string `json:"reloadedAt,omitempty"`
}

// handlePatchServerEnabled toggles enabled on a base stack file and reloads.
// When the write target would be an override, it refuses before any write.
//
// PATCH /api/mcp-servers/{name}
func (s *Server) handlePatchServerEnabled(w http.ResponseWriter, r *http.Request) {
	serverName := r.PathValue("name")
	if serverName == "" {
		writeJSONError(w, "Server name is required", http.StatusBadRequest)
		return
	}
	if s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, patchEnabledMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var req patchEnabledRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Enabled == nil {
		writeJSONError(w, "Request body must include enabled", http.StatusBadRequest)
		return
	}

	if path, required := s.overrideWriteRequired(); required {
		writeStructuredError(w, http.StatusConflict, errCodeOverrideRequired,
			"This stack writes enable and disable to an override file.",
			"Use gridctl enable or gridctl disable. The override is "+path+".")
		return
	}

	if !s.serverDeclared(serverName) {
		writeJSONError(w, "MCP server not found: "+serverName, http.StatusNotFound)
		return
	}

	changed, err := writeServerEnabled(s.stackFile, serverName, *req.Enabled, s.candidateValidator(r.Context()))
	switch {
	case err == nil:
	case errors.Is(err, stackedit.ErrServerNotFound), errors.Is(err, errServerNotFound):
		writeJSONError(w, "MCP server not found: "+serverName, http.StatusNotFound)
		return
	case errors.Is(err, errStackModified):
		writeStructuredError(w, http.StatusConflict, errCodeStackModified,
			"The stack file was modified outside the canvas.",
			"Reload the file to see the latest contents, then re-apply your changes.")
		return
	default:
		if writeCandidateValidation(w, err) {
			return
		}
		if errors.Is(err, errCandidateInvalid) {
			writeJSONError(w, candidateRefusalMessage(err), http.StatusBadRequest)
			return
		}
		writeJSONError(w, "Failed to update stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := patchEnabledResponse{
		Server:   serverName,
		Enabled:  *req.Enabled,
		Changed:  changed,
		Reloaded: false,
	}
	if !changed {
		writeJSON(w, resp)
		return
	}
	if handler := s.ReloadHandler(); handler != nil {
		result, err := handler.Reload(r.Context())
		if err != nil {
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				err.Error(),
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
		if !result.Success {
			if writePreflightFailure(w, result) {
				return
			}
			writeStructuredError(w, http.StatusBadGateway, errCodeReloadFailed,
				result.Message,
				"The stack file was saved but the hot reload failed. Check gridctl logs.")
			return
		}
		resp.Reloaded = true
		resp.ReloadedAt = time.Now().UTC().Format(time.RFC3339)
	}
	writeJSON(w, resp)
}

func writeServerEnabled(path, name string, enabled bool, validate func([]byte) error) (bool, error) {
	mu := stackFileLock(path)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	originalHash := sha256.Sum256(original)
	updated, changed, err := stackedit.SetServerEnabled(original, name, enabled)
	if err != nil || !changed {
		return changed, err
	}
	if validate != nil {
		if err := validate(updated); err != nil {
			return false, wrapCandidateInvalid(err)
		}
	}
	fireBetweenReadsHook()
	current, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if sha256.Sum256(current) != originalHash {
		return false, errStackModified
	}
	if err := atomicWrite(path, updated); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) serverDeclared(name string) bool {
	if h := s.ReloadHandler(); h != nil && h.CurrentConfig() != nil {
		for _, srv := range h.CurrentConfig().MCPServers {
			if srv.Name == name {
				return true
			}
		}
	}
	if s.stackFile == "" {
		return false
	}
	data, err := os.ReadFile(s.stackFile)
	if err != nil {
		return false
	}
	stack, err := config.DecodeStack(data)
	if err != nil {
		return false
	}
	for _, srv := range stack.MCPServers {
		if srv.Name == name {
			return true
		}
	}
	return false
}

// overrideWriteRequired reports whether enable or disable must write an
// override instead of the base file. The REST toggle never edits either
// file in that case.
func (s *Server) overrideWriteRequired() (string, bool) {
	if s.noOverride {
		return "", false
	}
	if s.overridePath != "" {
		return s.overridePath, true
	}
	if path := s.effectiveOverridePath(); path != "" {
		return path, true
	}
	if s.stackFile != "" {
		if _, path, ok := packOverrideTarget(s.stackFile); ok {
			return path, true
		}
		sibling := filepath.Join(filepath.Dir(s.stackFile), config.OverrideFileName)
		if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
			return sibling, true
		}
	}
	return "", false
}

func (s *Server) effectiveOverridePath() string {
	if s.noOverride {
		return ""
	}
	if h := s.ReloadHandler(); h != nil && h.CurrentConfig() != nil && h.CurrentConfig().OverridePath != "" {
		return h.CurrentConfig().OverridePath
	}
	if s.overridePath != "" {
		if info, err := os.Stat(s.overridePath); err == nil && !info.IsDir() {
			return s.overridePath
		}
	}
	return ""
}

func (s *Server) appendDisabledServers(statuses []MCPServerStatus) []MCPServerStatus {
	h := s.ReloadHandler()
	if h == nil || h.CurrentConfig() == nil {
		return statuses
	}
	stack := h.CurrentConfig()
	seen := map[string]bool{}
	for i := range statuses {
		seen[statuses[i].Name] = true
		if fields := stack.Overrides[statuses[i].Name]; len(fields) > 0 {
			statuses[i].Overrides = append([]string(nil), fields...)
		}
	}
	for _, srv := range stack.MCPServers {
		if srv.IsEnabled() || seen[srv.Name] {
			continue
		}
		statuses = append(statuses, disabledStatus(srv, stack.Overrides[srv.Name]))
		seen[srv.Name] = true
	}
	return statuses
}

func disabledStatus(srv config.MCPServer, overrides []string) MCPServerStatus {
	off := false
	status := MCPServerStatus{
		Name:         srv.Name,
		Transport:    disabledTransport(srv),
		Initialized:  false,
		Tools:        []string{},
		Enabled:      &off,
		External:     srv.IsExternal(),
		LocalProcess: srv.IsLocalProcess(),
		SSH:          srv.IsSSH(),
		OpenAPI:      srv.IsOpenAPI(),
		Description:  srv.Description,
	}
	if len(overrides) > 0 {
		status.Overrides = append([]string(nil), overrides...)
	}
	return status
}

func disabledTransport(srv config.MCPServer) string {
	switch {
	case srv.IsExternal():
		return "external"
	case srv.IsLocalProcess():
		return "local"
	case srv.IsSSH():
		return "ssh"
	case srv.IsOpenAPI():
		return "openapi"
	case srv.IsA2A():
		return "a2a"
	}
	if srv.Transport != "" {
		return srv.Transport
	}
	return "http"
}

func (s *Server) overrideLoadOptions() []config.LoadOption {
	if s.noOverride {
		return []config.LoadOption{config.WithoutOverride()}
	}
	if s.overridePath != "" {
		return []config.LoadOption{config.WithOverride(s.overridePath)}
	}
	return nil
}

// packOverrideTarget reports the pack override path when stackFile sits
// under a locked pack checkout.
func packOverrideTarget(stackFile string) (pack, path string, ok bool) {
	home, err := state.Home()
	if err != nil {
		return "", "", false
	}
	return packops.LockedPackOverride(home, skills.LockFilePath(), stackFile)
}
