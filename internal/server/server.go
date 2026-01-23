package server

import (
	"io/fs"
	"net/http"
	"strings"
)

// Server handles HTTP requests for the Gridctl application.
type Server struct {
	webFS  fs.FS
	mux    *http.ServeMux
	addr   string
}

// New creates a new Server instance.
func New(addr string, webFS fs.FS) *Server {
	s := &Server{
		webFS: webFS,
		mux:   http.NewServeMux(),
		addr:  addr,
	}
	s.routes()
	return s
}

// routes sets up the HTTP routes.
func (s *Server) routes() {
	// Serve static files from embedded filesystem with SPA fallback
	s.mux.Handle("/", s.spaHandler())
}

// spaHandler returns a handler that serves static files and falls back to index.html for SPA routing.
func (s *Server) spaHandler() http.Handler {
	fileServer := http.FileServer(http.FS(s.webFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Try to serve the file directly
		if path != "/" {
			// Check if file exists
			cleanPath := strings.TrimPrefix(path, "/")
			if _, err := fs.Stat(s.webFS, cleanPath); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// For SPA: serve index.html for all non-asset routes
		// This allows client-side routing to work
		if !strings.Contains(path, ".") || path == "/" {
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found
		http.NotFound(w, r)
	})
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	return http.ListenAndServe(s.addr, s.mux)
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	return s.mux
}
