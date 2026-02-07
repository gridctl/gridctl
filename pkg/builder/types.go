package builder

import "log/slog"

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

	// Logger for build operations (optional, defaults to discard)
	Logger *slog.Logger
}

// BuildResult contains the result of a build operation.
type BuildResult struct {
	ImageID  string // Docker image ID
	ImageTag string // Image tag
	Cached   bool   // Whether the build was cached
}
