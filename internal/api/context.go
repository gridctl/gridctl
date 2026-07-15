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
	NeedsSync bool                    `json:"needs_sync"`
	Clients   []contexts.ClientStatus `json:"clients"`
}

// buildContextDoc assembles the canonical + per-client state document.
func (s *Server) buildContextDoc(r *http.Request, mgr *contexts.Manager) (contextDoc, error) {
	var doc contextDoc
	doc.Canonical.Path = mgr.CanonicalPath()
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
			res, serr := mgr.SyncClient(r.Context(), slug, opts)
			if serr != nil {
				// Bad requests abort; a per-client runtime failure becomes
				// an error row so earlier writes are still reported.
				if errors.Is(serr, contexts.ErrUnknownClient) || errors.Is(serr, contexts.ErrUnsupported) ||
					errors.Is(serr, contexts.ErrNoCanonical) || errors.Is(serr, contexts.ErrNewerLockVersion) {
					writeJSONError(w, serr.Error(), contextErrorStatus(serr, http.StatusBadRequest))
					return
				}
				res = contexts.SyncResult{Slug: slug, Name: slug, Action: contexts.ActionError, Error: serr.Error()}
			}
			results = append(results, res)
		}
	}

	writeJSON(w, map[string]any{
		"dry_run":      opts.DryRun,
		"has_failures": contexts.HasFailures(results),
		"results":      results,
	})
}

// handleContextAdopt pulls a client's managed content into the canon.
// POST /api/context/adopt/{slug}
func (s *Server) handleContextAdopt(w http.ResponseWriter, r *http.Request) {
	mgr, err := s.contextsMgr()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	slug := r.PathValue("slug")
	if err := mgr.Adopt(r.Context(), slug); err != nil {
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
	res, err := mgr.Unsync(r.Context(), slug)
	if err != nil {
		writeJSONError(w, err.Error(), contextErrorStatus(err, http.StatusNotFound))
		return
	}
	writeJSON(w, res)
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
	diff, err := mgr.Diff(r.Context(), slug)
	if err != nil {
		writeJSONError(w, err.Error(), contextErrorStatus(err, http.StatusNotFound))
		return
	}
	writeJSON(w, map[string]any{"slug": slug, "diff": diff})
}
