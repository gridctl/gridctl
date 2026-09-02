package builder

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/logging"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// mockDockerClient is a mock implementation of DockerClient for builder tests.
type mockDockerClient struct {
	imageBuildFn    func(ctx context.Context, buildContext io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error)
	imageListFn     func(ctx context.Context, options image.ListOptions) ([]image.Summary, error)
	imageBuildError error
	calls           []string
}

func (m *mockDockerClient) recordCall(name string) {
	m.calls = append(m.calls, name)
}

func (m *mockDockerClient) ImageBuild(ctx context.Context, buildContext io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error) {
	m.recordCall("ImageBuild")
	if m.imageBuildFn != nil {
		return m.imageBuildFn(ctx, buildContext, options)
	}
	if m.imageBuildError != nil {
		return build.ImageBuildResponse{}, m.imageBuildError
	}
	body := `{"aux":{"ID":"sha256:mock123"}}
{"stream":"Successfully built mock123\n"}`
	return build.ImageBuildResponse{
		Body: io.NopCloser(strings.NewReader(body)),
	}, nil
}

// Unused interface methods (required by DockerClient)
func (m *mockDockerClient) ContainerCreate(context.Context, *container.Config, *container.HostConfig, *network.NetworkingConfig, *v1.Platform, string) (container.CreateResponse, error) {
	return container.CreateResponse{}, nil
}
func (m *mockDockerClient) ContainerStart(context.Context, string, container.StartOptions) error {
	return nil
}
func (m *mockDockerClient) ContainerStop(context.Context, string, container.StopOptions) error {
	return nil
}
func (m *mockDockerClient) ContainerRestart(context.Context, string, container.StopOptions) error {
	return nil
}
func (m *mockDockerClient) ContainerRemove(context.Context, string, container.RemoveOptions) error {
	return nil
}
func (m *mockDockerClient) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	return nil, nil
}
func (m *mockDockerClient) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	return container.InspectResponse{}, nil
}
func (m *mockDockerClient) ContainerAttach(context.Context, string, container.AttachOptions) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, nil
}
func (m *mockDockerClient) ContainerLogs(context.Context, string, container.LogsOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockDockerClient) NetworkList(context.Context, network.ListOptions) ([]network.Summary, error) {
	return nil, nil
}
func (m *mockDockerClient) NetworkCreate(context.Context, string, network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}
func (m *mockDockerClient) NetworkRemove(context.Context, string) error { return nil }
func (m *mockDockerClient) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	if m.imageListFn != nil {
		return m.imageListFn(ctx, options)
	}
	return nil, nil
}
func (m *mockDockerClient) ImagePull(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (m *mockDockerClient) Ping(context.Context) (types.Ping, error) { return types.Ping{}, nil }
func (m *mockDockerClient) Close() error                             { return nil }

var _ dockerclient.DockerClient = &mockDockerClient{}

// newTestLogger returns a discard logger for tests.
func newTestLogger() *slog.Logger {
	return logging.NewDiscardLogger()
}

func TestNew(t *testing.T) {
	mock := &mockDockerClient{}
	b := New(mock)
	if b == nil {
		t.Fatal("expected non-nil Builder")
	}
	if b.cli != mock {
		t.Error("expected Builder to use provided client")
	}
}

func TestBuild_LogsDiagnosticsWithServer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	docker := &mockDockerClient{imageBuildFn: func(context.Context, io.Reader, build.ImageBuildOptions) (build.ImageBuildResponse, error) {
		return build.ImageBuildResponse{Body: io.NopCloser(strings.NewReader(`{"stream":"transferring build context\n"}
{"aux":{"ID":"sha256:mock123"}}`))}, nil
	}}
	_, err := New(docker).Build(context.Background(), BuildOptions{
		Stack: "demo", ServerName: "fetch", SourceType: "local", Path: dir, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	output := logs.String()
	for _, want := range []string{"msg=\"build output\"", "line=\"transferring build context\"", "server=fetch", "phase=building_image", "level=INFO"} {
		if !strings.Contains(output, want) {
			t.Errorf("build diagnostics missing %q: %s", want, output)
		}
	}
}

func TestBuild_LocalSource(t *testing.T) {
	// Create a temp dir with a Dockerfile
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	mock := &mockDockerClient{}
	b := New(mock)

	result, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       dir,
		Stack:      "test",
		ServerName: "local",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ImageID != "sha256:mock123" {
		t.Errorf("expected image ID 'sha256:mock123', got %q", result.ImageID)
	}
	if strings.Contains(result.ImageTag, ":latest") {
		t.Errorf("expected content-addressed tag, got %q", result.ImageTag)
	}
	if result.Cached {
		t.Error("expected Cached to be false")
	}
}

func TestBuild_LocalSource_MissingDockerfile(t *testing.T) {
	// Create a temp dir without a Dockerfile
	dir := t.TempDir()

	mock := &mockDockerClient{}
	b := New(mock)

	_, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       dir,
	})
	if err == nil {
		t.Fatal("expected error for missing Dockerfile")
	}
	if !strings.Contains(err.Error(), "no Dockerfile found") {
		t.Errorf("expected 'no Dockerfile found' error, got %q", err.Error())
	}
}

func TestBuild_LocalSource_AlternativeDockerfile(t *testing.T) {
	// Create a temp dir with "Containerfile" instead of "Dockerfile"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Containerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatalf("write Containerfile: %v", err)
	}

	mock := &mockDockerClient{}
	b := New(mock)

	result, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ImageID != "sha256:mock123" {
		t.Errorf("expected image ID 'sha256:mock123', got %q", result.ImageID)
	}
}

func TestBuild_LocalSource_CustomDockerfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "custom.Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatalf("write custom.Dockerfile: %v", err)
	}

	mock := &mockDockerClient{}
	b := New(mock)

	result, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       dir,
		Dockerfile: "custom.Dockerfile",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ImageID != "sha256:mock123" {
		t.Errorf("expected image ID 'sha256:mock123', got %q", result.ImageID)
	}
}

func TestBuild_UnknownSourceType(t *testing.T) {
	mock := &mockDockerClient{}
	b := New(mock)

	_, err := b.Build(context.Background(), BuildOptions{
		SourceType: "unknown",
	})
	if err == nil {
		t.Fatal("expected error for unknown source type")
	}
	if !strings.Contains(err.Error(), "unknown source type") {
		t.Errorf("expected 'unknown source type' error, got %q", err.Error())
	}
}

func TestBuild_LocalSource_PathNotExist(t *testing.T) {
	mock := &mockDockerClient{}
	b := New(mock)

	_, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       "/nonexistent/path",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestBuild_LocalSource_PathIsFile(t *testing.T) {
	// Create a file (not a directory)
	tmpFile := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(tmpFile, []byte("data"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	mock := &mockDockerClient{}
	b := New(mock)

	_, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       tmpFile,
	})
	if err == nil {
		t.Fatal("expected error when path is a file")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' error, got %q", err.Error())
	}
}

func TestBuild_GitSource_MissingURL(t *testing.T) {
	mock := &mockDockerClient{}
	b := New(mock)

	_, err := b.Build(context.Background(), BuildOptions{
		SourceType: "git",
	})
	if err == nil {
		t.Fatal("expected error for missing git URL")
	}
	if !strings.Contains(err.Error(), "git URL is required") {
		t.Errorf("expected 'git URL is required' error, got %q", err.Error())
	}
}

func TestBuild_LocalSource_EmptyPath(t *testing.T) {
	mock := &mockDockerClient{}
	b := New(mock)

	_, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       "",
	})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "local path is required") {
		t.Errorf("expected 'local path is required' error, got %q", err.Error())
	}
}

func TestBuild_NilLogger(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	mock := &mockDockerClient{}
	b := New(mock)

	// Build with nil Logger should use discard logger (no panic)
	result, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       dir,
		Logger:     nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestBuild_WithBuildArgs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	var capturedOptions build.ImageBuildOptions
	mock := &mockDockerClient{
		imageBuildFn: func(ctx context.Context, buildContext io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error) {
			capturedOptions = options
			body := `{"aux":{"ID":"sha256:mock123"}}`
			return build.ImageBuildResponse{
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		},
	}
	b := New(mock)

	_, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       dir,
		BuildArgs:  map[string]string{"DEBUG": "true", "VERSION": "1.0"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(capturedOptions.BuildArgs) != 2 {
		t.Errorf("expected 2 build args, got %d", len(capturedOptions.BuildArgs))
	}
	if v := capturedOptions.BuildArgs["DEBUG"]; v == nil || *v != "true" {
		t.Error("expected build arg DEBUG=true")
	}
	if v := capturedOptions.BuildArgs["VERSION"]; v == nil || *v != "1.0" {
		t.Error("expected build arg VERSION=1.0")
	}
}

func TestBuild_NoCache(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	var capturedOptions build.ImageBuildOptions
	mock := &mockDockerClient{
		imageBuildFn: func(ctx context.Context, buildContext io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error) {
			capturedOptions = options
			body := `{"aux":{"ID":"sha256:mock123"}}`
			return build.ImageBuildResponse{
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		},
	}
	b := New(mock)

	_, err := b.Build(context.Background(), BuildOptions{
		SourceType: "local",
		Path:       dir,
		NoCache:    true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !capturedOptions.NoCache {
		t.Error("expected NoCache to be true in build options")
	}
}

func TestBuild_ReusesImageWithMatchingDigestLabel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	imageBuildCalled := false
	mock := &mockDockerClient{
		imageListFn: func(_ context.Context, options image.ListOptions) ([]image.Summary, error) {
			labels := options.Filters.Get("label")
			if len(labels) != 1 || !strings.HasPrefix(labels[0], LabelBuildInputDigest+"=") {
				t.Fatalf("cache label filter = %v", labels)
			}
			digest := strings.TrimPrefix(labels[0], LabelBuildInputDigest+"=")
			return []image.Summary{{ID: "sha256:cached", Labels: map[string]string{LabelBuildInputDigest: digest}}}, nil
		},
		imageBuildFn: func(context.Context, io.Reader, build.ImageBuildOptions) (build.ImageBuildResponse, error) {
			imageBuildCalled = true
			return build.ImageBuildResponse{}, nil
		},
	}
	result, err := New(mock).Build(context.Background(), BuildOptions{SourceType: "local", Path: dir, Stack: "test", ServerName: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cached || result.ImageID != "sha256:cached" || imageBuildCalled {
		t.Fatalf("result = %+v, imageBuildCalled = %v", result, imageBuildCalled)
	}
}

func TestBuild_SetsProvenanceLabels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var labels map[string]string
	mock := &mockDockerClient{imageBuildFn: func(_ context.Context, _ io.Reader, options build.ImageBuildOptions) (build.ImageBuildResponse, error) {
		labels = options.Labels
		return build.ImageBuildResponse{Body: io.NopCloser(strings.NewReader(`{"aux":{"ID":"sha256:new"}}`))}, nil
	}}
	if _, err := New(mock).Build(context.Background(), BuildOptions{SourceType: "local", Path: dir, Stack: "test", ServerName: "demo"}); err != nil {
		t.Fatal(err)
	}
	if labels[LabelBuildInputDigest] == "" || labels[LabelSourceDigest] == "" {
		t.Fatalf("labels = %v", labels)
	}
}
