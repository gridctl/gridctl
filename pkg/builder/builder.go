package builder

import (
	"context"
	"fmt"

	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/logging"
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

	// Build the image
	imageID, err := BuildImage(ctx, b.cli, plan.EffectiveProjectRoot, plan.Dockerfile, plan.ImageTag, opts.BuildArgs, opts.NoCache, logger)
	if err != nil {
		return nil, fmt.Errorf("building image: %w", err)
	}

	return &BuildResult{
		ImageID:  imageID,
		ImageTag: plan.ImageTag,
		Cached:   false,
	}, nil
}
