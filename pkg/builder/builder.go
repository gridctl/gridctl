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
	cli dockerclient.DockerClient
}

// New creates a new Builder instance.
func New(cli dockerclient.DockerClient) *Builder {
	return &Builder{cli: cli}
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
		return nil, fmt.Errorf("preparing source: %w", err)
	}
	defer func() { _ = plan.Close() }()
	if !opts.NoCache {
		cachedID, err := b.cachedImage(ctx, plan)
		if err != nil {
			return nil, fmt.Errorf("checking image cache: %w", err)
		}
		if cachedID != "" {
			return &BuildResult{ImageID: cachedID, ImageTag: plan.ImageTag, Cached: true}, nil
		}
	}

	// Build the image
	imageID, err := buildImage(ctx, b.cli, plan.EffectiveProjectRoot, plan.Dockerfile, plan.ImageTag, opts.BuildArgs, plan.ImageLabels(), opts.NoCache, logger)
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
