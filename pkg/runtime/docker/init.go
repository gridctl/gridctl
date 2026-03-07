package docker

import (
	"context"

	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/runtime"
)

func init() {
	// Register factory function for runtime.New()
	runtime.NewFunc = newOrchestrator

	// Register factory function for runtime.NewWithInfo()
	runtime.NewWithInfoFunc = newOrchestratorWithInfo

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

// newOrchestratorWithInfo creates an Orchestrator using explicit RuntimeInfo.
func newOrchestratorWithInfo(info *runtime.RuntimeInfo) (*runtime.Orchestrator, error) {
	dockerRT, err := NewWithInfo(info)
	if err != nil {
		return nil, err
	}
	bldr := &dockerBuilderAdapter{builder.New(dockerRT.Client())}
	orch := runtime.NewOrchestrator(dockerRT, bldr)
	orch.SetRuntimeInfo(info)
	return orch, nil
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
		Logger:     opts.Logger,
	})
	if err != nil {
		return nil, err
	}
	return &runtime.BuildResult{
		ImageTag: result.ImageTag,
		Cached:   result.Cached,
	}, nil
}
