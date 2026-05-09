// Package devserver hosts the HTTP surface the agent IDE talks to.
// Every endpoint is read-only — code is canon, the IDE never writes
// back through this server. The shape mirrors what the frontend's
// `lib/agent-api.ts` consumes: list skills, fetch a skill's parsed
// graph, subscribe to file-watcher events, scaffold a starter
// project.
package devserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/agent/dev/parser"
	"github.com/gridctl/gridctl/pkg/agent/dev/watcher"
)

// Server bundles the parser + watcher into the HTTP routes the IDE
// consumes. One Server is constructed per `gridctl agent dev`
// invocation; tests build it with NewServer(root, nil) to skip the
// watcher subscription.
type Server struct {
	root    string
	watcher *watcher.Watcher
	logger  *slog.Logger
}

// SkillEntry is the per-skill summary returned by GET /api/agent/skills.
type SkillEntry struct {
	Name      string `json:"name"`
	Lang      string `json:"lang"`
	Dir       string `json:"dir"`
	NodeCount int    `json:"node_count"`
	HasError  bool   `json:"has_error,omitempty"`
}

// NewServer constructs a Server rooted at the given directory. A
// nil watcher disables the SSE endpoint — useful for unit tests
// that want only the parser surface.
func NewServer(root string, w *watcher.Watcher) (*Server, error) {
	if root == "" {
		return nil, errors.New("devserver: root is required")
	}
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("devserver: stat root: %w", err)
	}
	return &Server{
		root:    root,
		watcher: w,
		logger:  slog.Default(),
	}, nil
}

// SetLogger overrides the slog.Logger the server logs requests on.
func (s *Server) SetLogger(l *slog.Logger) {
	if l != nil {
		s.logger = l
	}
}

// Handler returns the HTTP handler with all dev-server routes
// registered. The returned handler scopes paths to /api/agent/dev/*
// and /api/agent/skills/*; mount it under the parent API server in
// production or directly via http.ListenAndServe in tests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agent/dev/skills", s.handleListSkills)
	mux.HandleFunc("GET /api/agent/dev/skills/{name}", s.handleGetSkill)
	mux.HandleFunc("GET /api/agent/dev/events", s.handleEvents)
	return mux
}

// listSkillDirs walks the project root looking for SKILL.md files
// alongside a typed handler (skill.go or skill.ts). Returns the
// per-skill metadata in name-sorted order.
func (s *Server) listSkillDirs() ([]SkillEntry, error) {
	entries := []SkillEntry{}
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if base == "node_modules" || base == ".git" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "SKILL.md" {
			return nil
		}
		dir := filepath.Dir(path)
		name := filepath.Base(dir)
		// Project-root SKILL.md uses the project directory name as
		// the skill identifier so a single-skill repo doesn't need
		// a nested directory.
		if dir == s.root {
			name = filepath.Base(s.root)
		}
		lang := ""
		if _, err := os.Stat(filepath.Join(dir, "skill.go")); err == nil {
			lang = "go"
		} else if _, err := os.Stat(filepath.Join(dir, "skill.ts")); err == nil {
			lang = "ts"
		}
		rel, _ := filepath.Rel(s.root, dir)
		if rel == "" {
			rel = "."
		}
		entry := SkillEntry{
			Name: name,
			Lang: lang,
			Dir:  rel,
		}
		if lang != "" {
			g, err := parser.ParseSkill(name, dir)
			if err == nil {
				entry.NodeCount = len(g.Nodes)
				entry.HasError = g.ParseError != ""
			}
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// handleListSkills returns the project's recognised typed skills.
func (s *Server) handleListSkills(w http.ResponseWriter, _ *http.Request) {
	entries, err := s.listSkillDirs()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"skills": entries})
}

// handleGetSkill returns a single skill's parsed graph. The path
// parameter `{name}` matches the directory base name returned by
// listSkillDirs.
func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, "skill name is required", http.StatusBadRequest)
		return
	}
	entries, err := s.listSkillDirs()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, e := range entries {
		if e.Name != name {
			continue
		}
		g, err := parser.ParseSkill(name, filepath.Join(s.root, e.Dir))
		if err != nil {
			writeJSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, g)
		return
	}
	writeJSONError(w, fmt.Sprintf("skill %q not found", name), http.StatusNotFound)
}

// handleEvents streams watcher events as SSE. We use SSE rather
// than WebSockets so we don't add a new dependency; the IDE only
// consumes one direction (server-to-client) anyway.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.watcher == nil {
		writeJSONError(w, "watcher not configured", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, cancel := s.watcher.Subscribe()
	defer cancel()

	// Send a primer so the client knows the stream is live.
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			body, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			// Use a relative path in the wire shape so the IDE
			// renders client-friendly identifiers.
			rel, relErr := filepath.Rel(s.root, ev.Path)
			if relErr == nil {
				body, _ = json.Marshal(map[string]any{
					"path": strings.ReplaceAll(rel, string(os.PathSeparator), "/"),
					"lang": ev.Lang,
					"op":   ev.Op,
					"time": ev.Time,
				})
			}
			fmt.Fprintf(w, "data: %s\n\n", body)
			flusher.Flush()
		}
	}
}

// writeJSON encodes v as a JSON response.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a uniform JSON error envelope.
func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
