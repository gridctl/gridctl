package builder

import (
	"log/slog"
	"net/url"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

// BuildOptions contains options for building an image.
type BuildOptions struct {
	Stack      string // Stack name used in image identity
	ServerName string // Logical MCP server name used in image identity

	// Source configuration
	SourceType  string // "git" or "local"
	URL         string // Git URL (for git source)
	Ref         string // Git ref/branch (for git source)
	Path        string // Local path (for local source)
	ProjectPath string // Python project subdirectory below a local root
	Runtime     string // Empty for Dockerfile builds, "python" for generated builds
	Package     string // Public PyPI project name
	Python      string // Explicit Python minor version
	Extras      []string
	With        []string
	Packages    []string

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
	Type           string `json:"type"`
	URL            string `json:"url,omitempty"`
	Ref            string `json:"ref,omitempty"`
	Path           string `json:"path,omitempty"`
	ProjectPath    string `json:"projectPath,omitempty"`
	Dockerfile     string `json:"dockerfile,omitempty"`
	Commit         string `json:"commit,omitempty"`
	Package        string `json:"package,omitempty"`
	Version        string `json:"version,omitempty"`
	Artifact       string `json:"artifact,omitempty"`
	ArtifactSHA256 string `json:"artifactSha256,omitempty"`
}

// BuildProvenance identifies the inputs used to produce a resolved build plan.
type BuildProvenance struct {
	SourceContentDigest string `json:"sourceContentDigest"`
	TargetPlatform      string `json:"targetPlatform,omitempty"`
	GeneratorVersion    string `json:"generatorVersion,omitempty"`
	BaseImage           string `json:"baseImage,omitempty"`
	UVImage             string `json:"uvImage,omitempty"`
}

// ResolvedBuildPlan is the immutable input to an image build.
type ResolvedBuildPlan struct {
	DeclaredIdentity     SourceIdentity  `json:"declaredIdentity"`
	ResolvedIdentity     SourceIdentity  `json:"resolvedIdentity"`
	EffectiveProjectRoot string          `json:"effectiveProjectRoot"`
	Python               string          `json:"python,omitempty"`
	Command              []string        `json:"command,omitempty"`
	Dockerfile           string          `json:"dockerfile"`
	GeneratedDockerfile  string          `json:"generatedDockerfile,omitempty"`
	BuildInputDigest     string          `json:"buildInputDigest"`
	ImageTag             string          `json:"imageTag"`
	Cached               bool            `json:"cached"`
	MutableRef           bool            `json:"mutableRef"`
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

const (
	LabelBuildInputDigest = "io.gridctl.build-input-digest"
	LabelGeneratorVersion = "io.gridctl.generator-version"
	LabelSourceDigest     = "io.gridctl.source-digest"
	LabelBaseImage        = "io.gridctl.base-image"
	LabelUVImage          = "io.gridctl.uv-image"
)

// ImageLabels returns the non-secret provenance labels for the built image.
func (p *ResolvedBuildPlan) ImageLabels() map[string]string {
	labels := map[string]string{
		LabelBuildInputDigest: p.BuildInputDigest,
		LabelSourceDigest:     p.Provenance.SourceContentDigest,
	}
	if p.Provenance.GeneratorVersion != "" {
		labels[LabelGeneratorVersion] = p.Provenance.GeneratorVersion
	}
	if p.Provenance.BaseImage != "" {
		labels[LabelBaseImage] = p.Provenance.BaseImage
	}
	if p.Provenance.UVImage != "" {
		labels[LabelUVImage] = p.Provenance.UVImage
	}
	if p.DeclaredIdentity.URL != "" {
		if source := provenanceSourceURL(p.DeclaredIdentity.URL); source != "" {
			labels["org.opencontainers.image.source"] = source
		}
	}
	if p.ResolvedIdentity.Commit != "" {
		labels["org.opencontainers.image.revision"] = p.ResolvedIdentity.Commit
	}
	return labels
}

func provenanceSourceURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// BuildResult contains the result of a build operation.
type BuildResult struct {
	ImageID  string // Docker image ID
	ImageTag string // Image tag
	Cached   bool   // Whether the build was cached
}
