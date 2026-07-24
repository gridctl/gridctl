// Client link endpoints: the UI counterpart of the stack's declarative
// link: block. POST/DELETE write BOTH the client's own config (via the
// provisioner) and the stack.yaml link: entry (via stackedit), so the UI
// and the file never diverge. These endpoints write files in the
// operator's home directory — the same local-operator capability the
// vault and stack-editing endpoints already assume.
package api

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/gridctl/gridctl/internal/stackedit"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/provisioner"

	"gopkg.in/yaml.v3"
)

const (
	// errCodeUnknownClient is returned on 404 when the slug matches no
	// registered provisioner.
	errCodeUnknownClient = "unknown_client"
	// errCodeClientNotDetected is returned on 422 when the client is not
	// installed on this machine.
	errCodeClientNotDetected = "client_not_detected"
	// errCodeLinkConflict is returned on 409 when the client config already
	// carries a foreign entry under the target name. Nothing is written.
	errCodeLinkConflict = "link_conflict"
	// errCodeStackNotUpdated is returned on 500 when the client config write
	// succeeded but the stack file changed externally in the window before
	// our write. Both facts are reported; nothing is rolled back.
	errCodeStackNotUpdated = "stack_not_updated"
	// errCodeUnknownGroup is returned on 422 when the request references a
	// group the stack does not declare — mirroring validateLinks, so the
	// API cannot write an undeployable stack.
	errCodeUnknownGroup = "unknown_group"
)

// linkClientRequest carries the optional entry fields for POST
// /api/clients/{slug}/link and its preview.
type linkClientRequest struct {
	Group    string `json:"group,omitempty"`
	ClientID string `json:"clientId,omitempty"`
	Name     string `json:"name,omitempty"`
}

// linkClientResponse echoes the applied state.
type linkClientResponse struct {
	Client        string `json:"client"`
	ServerName    string `json:"serverName"`
	Linked        bool   `json:"linked"`
	Declared      bool   `json:"declared"`
	AlreadyLinked bool   `json:"alreadyLinked,omitempty"`
	ConfigPath    string `json:"configPath,omitempty"`
}

// linkPreviewResponse is the dry-run payload: the client config before and
// after the link, plus the unified diff of the stack.yaml change. Nothing
// is written.
type linkPreviewResponse struct {
	Client     string `json:"client"`
	ServerName string `json:"serverName"`
	ConfigPath string `json:"configPath"`
	Before     string `json:"before"`
	After      string `json:"after"`
	StackDiff  string `json:"stackDiff"`
}

// handleLinkClient links a client to the gateway and declares it in the
// stack's link: block. The dual write spans two I/O domains and cannot be
// atomic; the order is: validate and precompute the stack patch (nothing
// written on failure), then the client config write (the side effect users
// notice), then the stack write. A stack conflict after a successful link
// reports both facts and never rolls the link back.
//
// POST /api/clients/{slug}/link
func (s *Server) handleLinkClient(w http.ResponseWriter, r *http.Request) {
	prov, entry, ok := s.resolveLinkRequest(w, r)
	if !ok {
		return
	}

	configPath, found := prov.Detect()
	if !found {
		writeStructuredError(w, http.StatusUnprocessableEntity, errCodeClientNotDetected,
			prov.Name()+" is not detected on this system.",
			"Install the client, or link it on the machine where it runs.")
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

	// Mirror validateLinks: a declared group must exist in groups:, or the
	// write would produce a stack that fails the next LoadStack.
	if entry.Group != "" && !stackDeclaresGroup(original, entry.Group) {
		writeStructuredError(w, http.StatusUnprocessableEntity, errCodeUnknownGroup,
			"The stack does not declare group "+strconv.Quote(entry.Group)+".",
			"Add the group to the groups: block first, or drop the group option.")
		return
	}

	// Precompute the stack patch before touching the client config so a
	// malformed stack file rejects the request with no host write.
	updated, err := stackedit.UpsertLinkEntry(original, entry)
	if err != nil {
		writeJSONError(w, "Failed to update stack: "+err.Error(), http.StatusInternalServerError)
		return
	}

	opts := linkOptionsForDeclared(entry, s.gatewayPortOrDefault())
	alreadyLinked := false
	switch err := prov.Link(configPath, opts); {
	case err == nil:
		// linked
	case errors.Is(err, provisioner.ErrAlreadyLinked):
		// Declaring an already-linked client adopts it; the stack write below
		// is the meaningful half.
		alreadyLinked = true
	case errors.Is(err, provisioner.ErrConflict):
		writeStructuredError(w, http.StatusConflict, errCodeLinkConflict,
			"The client config already has a '"+opts.ServerName+"' entry with unexpected contents.",
			"Run 'gridctl link "+entry.Client+" --force' to overwrite it, then declare the client again.")
		return
	default:
		writeJSONError(w, "Failed to link "+prov.Name()+": "+err.Error(), http.StatusInternalServerError)
		return
	}

	fireBetweenReadsHook()

	current, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeStructuredError(w, http.StatusInternalServerError, errCodeStackNotUpdated,
			prov.Name()+" is linked, but the stack file could not be re-read: "+err.Error(),
			"Add the client to the link: block manually or retry.")
		return
	}
	if sha256.Sum256(current) != originalHash {
		writeStructuredError(w, http.StatusInternalServerError, errCodeStackNotUpdated,
			prov.Name()+" is linked, but stack.yaml was modified externally and was not updated.",
			"Reload the file and declare the client again; the link itself succeeded.")
		return
	}
	if err := atomicWrite(s.stackFile, updated); err != nil {
		writeStructuredError(w, http.StatusInternalServerError, errCodeStackNotUpdated,
			prov.Name()+" is linked, but writing stack.yaml failed: "+err.Error(),
			"Add the client to the link: block manually.")
		return
	}

	slog.Default().Info("client linked and declared", "client", entry.Client, "serverName", opts.ServerName)
	writeJSON(w, linkClientResponse{
		Client:        entry.Client,
		ServerName:    opts.ServerName,
		Linked:        true,
		Declared:      true,
		AlreadyLinked: alreadyLinked,
		ConfigPath:    configPath,
	})
}

// handleUnlinkClient removes the client's gateway entry and its link:
// declaration. The whole flow runs under the stack-file lock, and the
// removal patch is precomputed BEFORE the client-config write, mirroring
// POST: a broken stack file rejects the request with no host write, and a
// stack failure after a successful unlink reports both facts.
//
// DELETE /api/clients/{slug}/link
func (s *Server) handleUnlinkClient(w http.ResponseWriter, r *http.Request) {
	prov, entry, ok := s.resolveLinkRequest(w, r)
	if !ok {
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

	// The declared entry (when present) fixes the server name to remove;
	// an undeclared client falls back to the request fields or the default.
	if declared, found := declaredEntryFromBytes(original, entry.Client); found {
		entry = declared
	}
	serverName := entry.EffectiveName()

	updated, patchErr := stackedit.RemoveLinkEntry(original, entry.Client)
	notDeclared := errors.Is(patchErr, stackedit.ErrNoLinkBlock) || errors.Is(patchErr, stackedit.ErrEntryNotDeclared)
	if patchErr != nil && !notDeclared {
		// A stack file that fails to parse must not be mistaken for "not
		// declared": reject before touching the client config.
		writeJSONError(w, "Failed to update stack: "+patchErr.Error(), http.StatusInternalServerError)
		return
	}

	unlinked := false
	if configPath, found := prov.Detect(); found {
		switch err := prov.Unlink(configPath, serverName); {
		case err == nil:
			unlinked = true
		case errors.Is(err, provisioner.ErrNotLinked):
			// Declared but never linked here; removing the declaration is
			// still meaningful.
		default:
			writeJSONError(w, "Failed to unlink "+prov.Name()+": "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if notDeclared {
		if !unlinked {
			writeJSONError(w, "Client is neither linked nor declared", http.StatusNotFound)
			return
		}
		// Unlinked but never declared: nothing to remove from the stack.
		writeJSON(w, linkClientResponse{Client: entry.Client, ServerName: serverName})
		return
	}

	fireBetweenReadsHook()

	current, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeStructuredError(w, http.StatusInternalServerError, errCodeStackNotUpdated,
			prov.Name()+" is unlinked, but the stack file could not be re-read: "+err.Error(),
			"Remove the link: entry manually.")
		return
	}
	if sha256.Sum256(current) != originalHash {
		writeStructuredError(w, http.StatusInternalServerError, errCodeStackNotUpdated,
			prov.Name()+" is unlinked, but stack.yaml was modified externally and still declares it.",
			"Reload the file and remove the link: entry manually.")
		return
	}
	if err := atomicWrite(s.stackFile, updated); err != nil {
		writeStructuredError(w, http.StatusInternalServerError, errCodeStackNotUpdated,
			prov.Name()+" is unlinked, but writing stack.yaml failed: "+err.Error(),
			"Remove the link: entry manually.")
		return
	}

	slog.Default().Info("client unlinked and undeclared", "client", entry.Client, "serverName", serverName)
	writeJSON(w, linkClientResponse{Client: entry.Client, ServerName: serverName})
}

// handleLinkPreview computes the client config before/after for a link
// plus the stack.yaml unified diff, without writing either file.
//
// POST /api/clients/{slug}/link/preview
func (s *Server) handleLinkPreview(w http.ResponseWriter, r *http.Request) {
	prov, entry, ok := s.resolveLinkRequest(w, r)
	if !ok {
		return
	}

	configPath, found := prov.Detect()
	if !found {
		writeStructuredError(w, http.StatusUnprocessableEntity, errCodeClientNotDetected,
			prov.Name()+" is not detected on this system.",
			"Install the client, or link it on the machine where it runs.")
		return
	}

	opts := linkOptionsForDeclared(entry, s.gatewayPortOrDefault())
	before, after, err := provisioner.DryRunDiff(configPath, prov, opts)
	if err != nil {
		writeJSONError(w, "Failed to compute config diff: "+err.Error(), http.StatusInternalServerError)
		return
	}

	original, err := os.ReadFile(s.stackFile)
	if err != nil {
		writeJSONError(w, "Failed to read stack file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	stackDiff := ""
	if updated, err := stackedit.UpsertLinkEntry(original, entry); err == nil {
		stackDiff = unifiedDiff(string(original), string(updated), 3)
	}

	writeJSON(w, linkPreviewResponse{
		Client:     entry.Client,
		ServerName: opts.ServerName,
		ConfigPath: configPath,
		Before:     before,
		After:      after,
		StackDiff:  stackDiff,
	})
}

// resolveLinkRequest applies the guards shared by the three link handlers:
// provisioner registry present, stack file configured, known slug, and a
// parseable optional body. Writes the error response itself when ok is
// false.
func (s *Server) resolveLinkRequest(w http.ResponseWriter, r *http.Request) (provisioner.ClientProvisioner, config.LinkEntry, bool) {
	var entry config.LinkEntry

	if s.provisioners == nil || s.stackFile == "" {
		writeJSONError(w, "No stack file configured", http.StatusServiceUnavailable)
		return nil, entry, false
	}

	slug := r.PathValue("slug")
	prov, ok := s.provisioners.FindBySlug(slug)
	if !ok {
		writeStructuredError(w, http.StatusNotFound, errCodeUnknownClient,
			"Unknown client "+strconv.Quote(slug)+".",
			"See GET /api/clients for the supported slugs.")
		return nil, entry, false
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, setToolsRequestMaxBytes))
	if err != nil {
		writeJSONError(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
		return nil, entry, false
	}
	var req linkClientRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return nil, entry, false
		}
	}

	entry = config.LinkEntry{Client: slug, Group: req.Group, ClientID: req.ClientID, Name: req.Name}
	return prov, entry, true
}

// linkOptionsForDeclared maps a link entry to LinkOptions exactly as
// `gridctl link` flags and the apply reconcile do: group links target the
// group endpoint and rename the entry, and client_id rides the URL.
func linkOptionsForDeclared(entry config.LinkEntry, port int) provisioner.LinkOptions {
	baseURL := provisioner.GatewayURL(port)
	if entry.Group != "" {
		baseURL = provisioner.GroupGatewayURL(port, entry.Group)
	}
	return provisioner.LinkOptions{
		GatewayURL: provisioner.AppendClientParam(baseURL, entry.ClientID),
		Port:       port,
		ServerName: entry.EffectiveName(),
		ClientID:   entry.ClientID,
		Group:      entry.Group,
	}
}

// gatewayPortOrDefault extracts the port from the configured gateway
// address, falling back to the default 8180.
func (s *Server) gatewayPortOrDefault() int {
	if u, err := url.Parse(s.gatewayAddr); err == nil {
		if p, err := strconv.Atoi(u.Port()); err == nil && p > 0 {
			return p
		}
	}
	return 8180
}

// declaredLinks reads the stack's link: block with a raw parse (no vault,
// no expansion — the block never carries variables). Errors yield nil;
// declared state is advisory on read paths.
func (s *Server) declaredLinks() []config.LinkEntry {
	if s.stackFile == "" {
		return nil
	}
	data, err := os.ReadFile(s.stackFile)
	if err != nil {
		return nil
	}
	var st config.Stack
	if err := yaml.Unmarshal(data, &st); err != nil {
		return nil
	}
	return st.Link
}

// declaredEntryFromBytes returns the declared entry for slug from already
// read stack bytes, so lock-holding callers never re-read the file.
func declaredEntryFromBytes(data []byte, slug string) (config.LinkEntry, bool) {
	var st config.Stack
	if err := yaml.Unmarshal(data, &st); err != nil {
		return config.LinkEntry{}, false
	}
	for _, e := range st.Link {
		if e.Client == slug {
			return e, true
		}
	}
	return config.LinkEntry{}, false
}

// stackDeclaresGroup reports whether the stack bytes declare the group.
func stackDeclaresGroup(data []byte, group string) bool {
	var st config.Stack
	if err := yaml.Unmarshal(data, &st); err != nil {
		return false
	}
	_, ok := st.Groups[group]
	return ok
}
