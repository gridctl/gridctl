package builder

import (
	"context"
	"fmt"

	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/logging"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// Builder handles building images from source.
type Builder struct {
	cli              dockerclient.DockerClient
	pypiResolver     pypiReleaseResolver
	pypiVersionIndex pypiVersionResolver
}

// New creates a new Builder instance.
func New(cli dockerclient.DockerClient) *Builder {
	resolver := NewPyPIResolver(nil)
	return &Builder{cli: cli, pypiResolver: resolver, pypiVersionIndex: resolver}
}

// Versions returns selectable public PyPI releases for package-source UIs.
func (b *Builder) Versions(ctx context.Context, project string) (*PyPIVersions, error) {
	if b.pypiVersionIndex == nil {
		return nil, fmt.Errorf("PyPI version resolver is not configured")
	}
	return b.pypiVersionIndex.Versions(ctx, project)
}

// Plan resolves build inputs and checks whether the resulting image is
// already cached. A nil Docker client leaves Cached false, allowing callers
// to preview resolution on hosts without a running container runtime.
func (b *Builder) Plan(ctx context.Context, opts BuildOptions) (*ResolvedBuildPlan, error) {
	plan, err := b.Resolve(ctx, opts)
	if err != nil {
		return nil, err
	}
	if opts.NoCache || b.cli == nil {
		return plan, nil
	}
	cachedID, err := b.cachedImage(ctx, plan)
	if err != nil {
		_ = plan.Close()
		return nil, fmt.Errorf("checking image cache: %w", err)
	}
	plan.Cached = cachedID != ""
	return plan, nil
}

// Build builds an image from the given options.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	logger := opts.Logger
	if logger == nil {
		logger = logging.NewDiscardLogger()
	}

	opts.Logger = logger
	plan, err := b.Resolve(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = plan.Close() }()
	if !opts.NoCache {
		cachedID, err := b.cachedImage(ctx, plan)
		if err != nil {
			return nil, fmt.Errorf("checking image cache: %w", err)
		}
		if cachedID != "" {
			logger.Info("MCP server build phase", "server", opts.ServerName, "phase", "building_image", "cached", true)
			return &BuildResult{ImageID: cachedID, ImageTag: plan.ImageTag, Cached: true}, nil
		}
	}

	// Build the image
	logger.Info("MCP server build phase", "server", opts.ServerName, "phase", "building_image", "cached", false)
	buildLogger := logger.With("server", opts.ServerName, "phase", "building_image")
	imageID, err := buildImage(ctx, b.cli, plan.EffectiveProjectRoot, plan.Dockerfile, plan.ImageTag, opts.BuildArgs, plan.ImageLabels(), opts.NoCache, buildLogger)
	if err != nil {
		return nil, fmt.Errorf("building image: %w", err)
	}

	return &BuildResult{
		ImageID:  imageID,
		ImageTag: plan.ImageTag,
		Cached:   false,
	}, nil
}

func (b *Builder) cachedImage(ctx context.Context, plan *ResolvedBuildPlan) (string, error) {
	images, err := b.cli.ImageList(ctx, image.ListOptions{Filters: filters.NewArgs(
		filters.Arg("reference", plan.ImageTag),
		filters.Arg("label", LabelBuildInputDigest+"="+plan.BuildInputDigest),
	)})
	if err != nil {
		return "", err
	}
	for _, candidate := range images {
		if candidate.Labels[LabelBuildInputDigest] == plan.BuildInputDigest {
			return candidate.ID, nil
		}
	}
	return "", nil
}
