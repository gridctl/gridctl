package builder

import (
	"log/slog"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

// BuildOptions contains options for building an image.
type BuildOptions struct {
	// Source configuration
	SourceType string // "git" or "local"
	URL        string // Git URL (for git source)
	Ref        string // Git ref/branch (for git source)
	Path       string // Local path (for local source)

	// Build configuration
	Dockerfile string            // Path to Dockerfile within context
	Tag        string            // Image tag to use
	BuildArgs  map[string]string // Build arguments

	// Cache control
	NoCache bool // Force rebuild, ignore cache

	// Auth carries an already-resolved git auth method for private repository
	// clones. Nil means an unauthenticated clone (the public-repo default).
	// Resolution from a declarative SourceAuth happens upstream so that this
	// package never has to know about vaults or credential references.
	Auth transport.AuthMethod

	// Logger for build operations (optional, defaults to discard)
	Logger *slog.Logger
}

// BuildResult contains the result of a build operation.
type BuildResult struct {
	ImageID  string // Docker image ID
	ImageTag string // Image tag
	Cached   bool   // Whether the build was cached
}
