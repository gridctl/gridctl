package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gridctl/gridctl/pkg/dockerclient"
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
	var contextPath string
	var err error

	switch opts.SourceType {
	case "git":
		contextPath, err = b.prepareGitSource(opts)
	case "local":
		contextPath, err = b.prepareLocalSource(opts)
	default:
		return nil, fmt.Errorf("unknown source type: %s", opts.SourceType)
	}

	if err != nil {
		return nil, fmt.Errorf("preparing source: %w", err)
	}

	// Determine dockerfile path
	dockerfile := opts.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	// Check if Dockerfile exists
	dockerfilePath := filepath.Join(contextPath, dockerfile)
	if _, err := os.Stat(dockerfilePath); err != nil {
		// Try common alternatives
		alternatives := []string{"Dockerfile", "dockerfile", "Containerfile"}
		found := false
		for _, alt := range alternatives {
			altPath := filepath.Join(contextPath, alt)
			if _, err := os.Stat(altPath); err == nil {
				dockerfile = alt
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("no Dockerfile found in %s", contextPath)
		}
	}

	// Build the image
	imageID, err := BuildImage(ctx, b.cli, contextPath, dockerfile, opts.Tag, opts.BuildArgs, opts.NoCache)
	if err != nil {
		return nil, fmt.Errorf("building image: %w", err)
	}

	return &BuildResult{
		ImageID:  imageID,
		ImageTag: opts.Tag,
		Cached:   false,
	}, nil
}

func (b *Builder) prepareGitSource(opts BuildOptions) (string, error) {
	if opts.URL == "" {
		return "", fmt.Errorf("git URL is required")
	}

	ref := opts.Ref
	if ref == "" {
		ref = "main"
	}

	return CloneOrUpdate(opts.URL, ref)
}

func (b *Builder) prepareLocalSource(opts BuildOptions) (string, error) {
	if opts.Path == "" {
		return "", fmt.Errorf("local path is required")
	}

	// Verify path exists
	info, err := os.Stat(opts.Path)
	if err != nil {
		return "", fmt.Errorf("source path not found: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source path is not a directory: %s", opts.Path)
	}

	return opts.Path, nil
}

// GenerateTag creates a deterministic image tag for an agent.
func GenerateTag(stack, agentName string) string {
	return fmt.Sprintf("gridctl-%s-%s:latest", stack, agentName)
}
