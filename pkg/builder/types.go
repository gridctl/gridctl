package builder

import (
	"log/slog"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

// BuildOptions contains options for building an image.
type BuildOptions struct {
	Stack      string // Stack name used in image identity
	ServerName string // Logical MCP server name used in image identity

	// Source configuration
	SourceType string // "git" or "local"
	URL        string // Git URL (for git source)
	Ref        string // Git ref/branch (for git source)
	Path       string // Local path (for local source)

	// Build configuration
	Dockerfile string            // Path to Dockerfile within context
	BuildArgs  map[string]string // Build arguments
	Command    []string          // Runtime command that affects the image plan
	Platform   string            // Target OCI platform, empty means runtime default

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

// SourceIdentity records the declared and immutable identities of a build source.
type SourceIdentity struct {
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Path       string `json:"path,omitempty"`
	Dockerfile string `json:"dockerfile,omitempty"`
	Commit     string `json:"commit,omitempty"`
}

// BuildProvenance identifies the inputs used to produce a resolved build plan.
type BuildProvenance struct {
	SourceContentDigest string `json:"sourceContentDigest"`
	TargetPlatform      string `json:"targetPlatform,omitempty"`
}

// ResolvedBuildPlan is the immutable input to an image build.
type ResolvedBuildPlan struct {
	DeclaredIdentity     SourceIdentity  `json:"declaredIdentity"`
	ResolvedIdentity     SourceIdentity  `json:"resolvedIdentity"`
	EffectiveProjectRoot string          `json:"effectiveProjectRoot"`
	Command              []string        `json:"command,omitempty"`
	Dockerfile           string          `json:"dockerfile"`
	GeneratedDockerfile  string          `json:"generatedDockerfile,omitempty"`
	BuildInputDigest     string          `json:"buildInputDigest"`
	ImageTag             string          `json:"imageTag"`
	Cached               bool            `json:"cached"`
	Provenance           BuildProvenance `json:"provenance"`

	cleanup func() error
}

// Close releases temporary source material owned by the plan.
func (p *ResolvedBuildPlan) Close() error {
	if p == nil || p.cleanup == nil {
		return nil
	}
	err := p.cleanup()
	p.cleanup = nil
	return err
}

// BuildResult contains the result of a build operation.
type BuildResult struct {
	ImageID  string // Docker image ID
	ImageTag string // Image tag
	Cached   bool   // Whether the build was cached
}
