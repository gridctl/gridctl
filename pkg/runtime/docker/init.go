package docker

import (
	"context"

	"agentlab/pkg/builder"
	"agentlab/pkg/runtime"
)

func init() {
	// Register factory function for runtime.New()
	runtime.NewFunc = newOrchestrator

	// Register helper functions
	runtime.GetContainerHostPortFunc = GetContainerHostPort
}

// newOrchestrator creates a new Orchestrator with a DockerRuntime.
func newOrchestrator() (*runtime.Orchestrator, error) {
	dockerRT, err := New()
	if err != nil {
		return nil, err
	}
	bldr := &dockerBuilderAdapter{builder.New(dockerRT.Client())}
	return runtime.NewOrchestrator(dockerRT, bldr), nil
}

// dockerBuilderAdapter wraps builder.Builder to implement the runtime.Builder interface.
type dockerBuilderAdapter struct {
	inner *builder.Builder
}

func (a *dockerBuilderAdapter) Build(ctx context.Context, opts runtime.BuildOptions) (*runtime.BuildResult, error) {
	result, err := a.inner.Build(ctx, builder.BuildOptions{
		SourceType: opts.SourceType,
		URL:        opts.URL,
		Ref:        opts.Ref,
		Path:       opts.Path,
		Dockerfile: opts.Dockerfile,
		Tag:        opts.Tag,
		BuildArgs:  opts.BuildArgs,
		NoCache:    opts.NoCache,
	})
	if err != nil {
		return nil, err
	}
	return &runtime.BuildResult{
		ImageTag: result.ImageTag,
		Cached:   result.Cached,
	}, nil
}
