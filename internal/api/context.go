package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gridctl/gridctl/pkg/contexts"
)

// contextsMgr returns the global-context manager, lazily built against
// the real home directory. Tests inject a temp-dir manager via
// SetContextsManager before the first request. Context endpoints are
// pure file operations and work in stackless mode.
func (s *Server) contextsMgr() (*contexts.Manager, error) {
	s.contextsOnce.Do(func() {
		if s.contextsManager != nil {
			return
		}
		mgr, err := contexts.NewManager()
		if err != nil {
			s.contextsErr = err
			return
		}
		s.contextsManager = mgr
	})
	return s.contextsManager, s.contextsErr
}

// SetContextsManager overrides the global-context manager. Must be
// called before the server handles its first request (it races with the
// lazy sync.Once initialization otherwise); tests call it during setup.
func (s *Server) SetContextsManager(m *contexts.Manager) {
	s.contextsManager = m
}

// contextErrorStatus maps pkg/contexts sentinel errors to HTTP statuses.
// unknownStatus lets path-param endpoints report an unknown slug as 404
// while body-param endpoints report it as 400.
func contextErrorStatus(err error, unknownStatus int) int {
	switch {
	case errors.Is(err, contexts.ErrUnknownClient):
		return unknownStatus
	case errors.Is(err, contexts.ErrUnsupported):
		return http.StatusBadRequest
	case errors.Is(err, contexts.ErrNotAvailable), errors.Is(err, contexts.ErrNotSynced),
		errors.Is(err, contexts.ErrCanonicalExists):
		return http.StatusConflict
	// Adopt refusals: user-actionable conflicts whose message the UI
	// renders verbatim (the prose names the alternatives).
	case errors.Is(err, contexts.ErrAdoptRequiresFragment),
		errors.Is(err, contexts.ErrAdoptRefusesCompiled),
		errors.Is(err, contexts.ErrAdoptLossyRender),
		errors.Is(err, contexts.ErrAdoptImportShim),
		errors.Is(err, contexts.ErrFragmentsInactive):
		return http.StatusConflict
	case errors.Is(err, contexts.ErrNoFragment):
		return http.StatusNotFound
	case errors.Is(err, contexts.ErrBadFragmentName):
		return http.StatusBadRequest
	case errors.Is(err, contexts.ErrNoCanonical):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// respondContextDoc writes the refreshed canonical + per-client document.
func (s *Server) respondContextDoc(w http.ResponseWriter, r *http.Request, mgr *contexts.Manager) {
	doc, err := s.buildContextDoc(r, mgr)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, doc)
}

// contextDoc is the GET /api/context (and PUT response) payload.
type contextDoc struct {
	Canonical struct {
		Path    string `json:"path"`
		Exists  bool   `json:"exists"`
		Content string `json:"content"`
	} `json:"canonical"`
	FragmentsActive bool                    `json:"fragments_active,omitempty"`
	NeedsSync       bool                    `json:"needs_sync"`
	Clients         []contexts.ClientStatus `json:"clients"`
}

// buildContextDoc assembles the canonical + per-client state document.
func (s *Server) buildContextDoc(r *http.Request, mgr *contexts.Manager) (contextDoc, error) {
	var doc contextDoc
	doc.Canonical.Path = mgr.CanonicalPath()
	doc.FragmentsActive = mgr.FragmentsActive()
	if content, err := mgr.CanonicalContent(); err == nil {
		doc.Canonical.Exists = true
		doc.Canonical.Content = content
	} else if !errors.Is(err, contexts.ErrNoCanonical) {
		return doc, err
	}
	statuses, err := mgr.Statuses(r.Context())
	if err != nil {
		return doc, err
	}
	doc.Clients = statuses
	doc.NeedsSync = contexts.NeedsSync(statuses)
	return doc, nil
}

// handleContextGet returns the canonical content and per-client state.
// GET /api/context
func (s *Server) handleContextGet(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.respondContextDoc(w, r, mgr)
}

// handleContextPut saves the canonical content (creating it when absent)
// and returns the refreshed document.
// PUT /api/context
func (s *Server) handleContextPut(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Content == "" {
		writeJSONError(w, "content is required", http.StatusBadRequest)
		return
	}
	if err := mgr.SaveCanonical(body.Content); err != nil {
		// Marker-collision rejections are client errors, not server faults.
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondContextDoc(w, r, mgr)
}

// handleContextScan lists what already exists at each client's likely
// global context location, for the setup flow. Never writes.
// GET /api/context/scan
func (s *Server) handleContextScan(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"entries": mgr.Scan()})
}

// handleContextInit bootstraps the canonical file from a chosen source.
// POST /api/context/init  body: {source: "template"|"client"|"file", client?, path?, force?}
func (s *Server) handleContextInit(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var body struct {
		Source string `json:"source"`
		Client string `json:"client"`
		Path   string `json:"path"`
		Force  bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	switch body.Source {
	case "template":
		err = mgr.InitFromTemplate(body.Force)
	case "client":
		if body.Client == "" {
			writeJSONError(w, "client is required for source=client", http.StatusBadRequest)
			return
		}
		err = mgr.InitFromClient(body.Client, body.Force)
	case "file":
		if body.Path == "" {
			writeJSONError(w, "path is required for source=file", http.StatusBadRequest)
			return
		}
		err = mgr.InitFromFile(body.Path, body.Force)
	default:
		writeJSONError(w, "source must be one of: template, client, file", http.StatusBadRequest)
		return
	}
	if err != nil {
		writeJSONError(w, err.Error(), contextErrorStatus(err, http.StatusBadRequest))
		return
	}
	s.respondContextDoc(w, r, mgr)
}

// handleContextSync projects the canonical context to clients.
// POST /api/context/sync  body: {clients?: [slug...], force?, dry_run?}
func (s *Server) handleContextSync(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var body struct {
		Clients []string `json:"clients"`
		Force   bool     `json:"force"`
		DryRun  bool     `json:"dry_run"`
	}
	// An empty body means "sync everything".
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	opts := contexts.SyncOptions{Force: body.Force, DryRun: body.DryRun}

	var results []contexts.SyncResult
	if len(body.Clients) == 0 {
		results, err = mgr.SyncAll(r.Context(), opts)
		if err != nil {
			writeJSONError(w, err.Error(), contextErrorStatus(err, http.StatusBadRequest))
			return
		}
	} else {
		for _, slug := range body.Clients {
			rs, serr := mgr.SyncClientDetailed(r.Context(), slug, opts)
			if serr != nil {
				// Bad requests abort; a per-client runtime failure becomes
				// an error row so earlier writes are still reported.
				if errors.Is(serr, contexts.ErrUnknownClient) || errors.Is(serr, contexts.ErrUnsupported) ||
					errors.Is(serr, contexts.ErrNoCanonical) || errors.Is(serr, contexts.ErrNewerLockVersion) {
					writeJSONError(w, serr.Error(), contextErrorStatus(serr, http.StatusBadRequest))
					return
				}
				rs = []contexts.SyncResult{{Slug: slug, Name: slug, Action: contexts.ActionError, Error: serr.Error()}}
			}
			results = append(results, rs...)
		}
	}

	writeJSON(w, map[string]any{
		"dry_run":      opts.DryRun,
		"has_failures": contexts.HasFailures(results),
		"results":      results,
	})
}

// handleContextAdopt pulls a client's managed content into the canon.
// The optional body scopes the adopt in fragments mode: `fragment` does
// a lossless per-fragment adopt (identity multi-file targets only), and
// `into` captures a compiled target's whole managed body into one
// designated fragment (the CLI's `ctx adopt <client> [fragment]` and
// `--into` shapes, one to one). An absent or empty body keeps the
// original whole-client semantics.
// POST /api/context/adopt/{slug}
func (s *Server) handleContextAdopt(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slug := r.PathValue("slug")

	var req struct {
		Fragment string `json:"fragment,omitempty"`
		Into     string `json:"into,omitempty"`
	}
	if err := decodeOptionalBody(r, &req); err != nil {
		writeJSONError(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Fragment != "" && req.Into != "" {
		writeJSONError(w, "pass either fragment or into, not both", http.StatusBadRequest)
		return
	}

	switch {
	case req.Fragment != "":
		err = mgr.AdoptFragment(r.Context(), slug, req.Fragment)
	case req.Into != "":
		err = mgr.AdoptInto(r.Context(), slug, req.Into)
	default:
		err = mgr.Adopt(r.Context(), slug)
	}
	if err != nil {
		writeJSONError(w, err.Error(), contextErrorStatus(err, http.StatusNotFound))
		return
	}
	s.respondContextDoc(w, r, mgr)
}

// handleContextUnsync removes a client's managed artifact.
// POST /api/context/unsync/{slug}
func (s *Server) handleContextUnsync(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slug := r.PathValue("slug")
	results, err := mgr.Unsync(r.Context(), slug)
	if err != nil {
		writeJSONError(w, err.Error(), contextErrorStatus(err, http.StatusNotFound))
		return
	}
	if len(results) == 1 {
		writeJSON(w, results[0])
		return
	}
	writeJSON(w, map[string]any{"results": results})
}

// handleContextDiff returns the canonical-vs-target unified diff.
// GET /api/context/diff/{slug}
func (s *Server) handleContextDiff(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slug := r.PathValue("slug")
	fragment := r.URL.Query().Get("fragment")
	var diff string
	var derr error
	if fragment != "" {
		diff, derr = mgr.Diff(r.Context(), slug, fragment)
	} else {
		diff, derr = mgr.Diff(r.Context(), slug)
	}
	if derr != nil {
		writeJSONError(w, derr.Error(), contextErrorStatus(derr, http.StatusNotFound))
		return
	}
	writeJSON(w, map[string]any{"slug": slug, "fragment": fragment, "diff": diff})
}

// handleContextFragmentsList lists rule fragments.
// GET /api/context/fragments
func (s *Server) handleContextFragmentsList(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !mgr.FragmentsActive() {
		writeJSON(w, map[string]any{"active": false, "fragments": []any{}})
		return
	}
	frags, err := mgr.ListFragments()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type row struct {
		Name        string   `json:"name"`
		Description string   `json:"description,omitempty"`
		Paths       []string `json:"paths,omitempty"`
		Content     string   `json:"content"`
		Bytes       int      `json:"bytes"`
		Position    int      `json:"position"`
	}
	rows := make([]row, 0, len(frags))
	for i, f := range frags {
		rows = append(rows, row{
			Name: f.Name, Description: f.Description, Paths: f.Paths,
			Content: string(f.Raw), Bytes: len(f.Raw), Position: i + 1,
		})
	}
	writeJSON(w, map[string]any{"active": true, "fragments": rows})
}

// handleContextFragmentPut creates or updates a fragment.
// PUT /api/context/fragments/{name}  body: {content: "..."}
func (s *Server) handleContextFragmentPut(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := r.PathValue("name")
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Content == "" {
		// Create via AddFragment scaffold when content omitted.
		res, aerr := mgr.AddFragment(name, "")
		if aerr != nil {
			status := http.StatusInternalServerError
			if errors.Is(aerr, contexts.ErrFragmentExists) || errors.Is(aerr, contexts.ErrBadFragmentName) {
				status = http.StatusBadRequest
			}
			writeJSONError(w, aerr.Error(), status)
			return
		}
		writeJSON(w, map[string]any{"name": name, "path": res.CreatedPath, "migrated": res.Migrated})
		return
	}
	// Validate client-supplied content up front so malformed frontmatter
	// maps to 400, not a store-level 500.
	if _, perr := contexts.ParseFragment(name, []byte(body.Content)); perr != nil {
		writeJSONError(w, perr.Error(), http.StatusBadRequest)
		return
	}
	res, ierr := mgr.InstallFragmentBytes(name, []byte(body.Content))
	if ierr != nil {
		status := http.StatusInternalServerError
		if errors.Is(ierr, contexts.ErrBadFragmentName) {
			status = http.StatusBadRequest
		}
		writeJSONError(w, ierr.Error(), status)
		return
	}
	writeJSON(w, map[string]any{"name": name, "saved": true, "migrated": res.Migrated})
}

// handleContextFragmentDelete removes a fragment.
// DELETE /api/context/fragments/{name}
func (s *Server) handleContextFragmentDelete(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	name := r.PathValue("name")
	backup, err := mgr.RemoveFragment(name)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, contexts.ErrFragmentsInactive) {
			status = http.StatusConflict
		}
		writeJSONError(w, err.Error(), status)
		return
	}
	writeJSON(w, map[string]any{"name": name, "backup": backup})
}
