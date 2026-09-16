package docker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/image"

	"github.com/gridctl/gridctl/pkg/logging"
)

func TestImageExists_Found(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"test:latest"}},
		},
	}

	exists, err := ImageExists(context.Background(), mock, "test:latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected image to exist")
	}
}

func TestImageExists_FoundWithImplicitLatest(t *testing.T) {
	// When user specifies "test" without tag, it matches "test:latest"
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"test:latest"}},
		},
	}

	exists, err := ImageExists(context.Background(), mock, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected image to exist with implicit :latest")
	}
}

func TestImageExists_NotFound(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"other:latest"}},
		},
	}

	exists, err := ImageExists(context.Background(), mock, "test:latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected image to not exist")
	}
}

func TestImageExists_Empty(t *testing.T) {
	mock := &MockDockerClient{}

	exists, err := ImageExists(context.Background(), mock, "test:latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected image to not exist")
	}
}

func TestImageExists_MultipleImages(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"nginx:1.21"}},
			{RepoTags: []string{"test:v1", "test:latest"}},
			{RepoTags: []string{"redis:7"}},
		},
	}

	exists, err := ImageExists(context.Background(), mock, "test:v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected image to exist")
	}
}

func TestImageExists_Error(t *testing.T) {
	mock := &MockDockerClient{
		ImageListError: fmt.Errorf("list failed"),
	}

	_, err := ImageExists(context.Background(), mock, "test:latest")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEnsureImage_AlreadyExists(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"test:latest"}},
		},
	}
	logger := logging.NewDiscardLogger()

	err := EnsureImage(context.Background(), mock, "test:latest", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not pull when image exists
	if len(mock.PulledImages) != 0 {
		t.Errorf("expected no images pulled, got %v", mock.PulledImages)
	}
}

func TestEnsureImage_PullsWhenMissing(t *testing.T) {
	mock := &MockDockerClient{}
	logger := logging.NewDiscardLogger()

	err := EnsureImage(context.Background(), mock, "test:latest", logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.PulledImages) != 1 || mock.PulledImages[0] != "test:latest" {
		t.Errorf("expected 'test:latest' to be pulled, got %v", mock.PulledImages)
	}
}

func TestEnsureImage_ListError(t *testing.T) {
	mock := &MockDockerClient{
		ImageListError: fmt.Errorf("list failed"),
	}
	logger := logging.NewDiscardLogger()

	err := EnsureImage(context.Background(), mock, "test:latest", logger)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestImageExists_DigestUsesRepoDigests(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	mock := &MockDockerClient{
		Images: []image.Summary{
			{
				ID:          "sha256:" + strings.Repeat("b", 64),
				RepoTags:    []string{"nginx:1.21.0"},
				RepoDigests: []string{"nginx@" + digest},
			},
		},
	}
	exists, err := ImageExists(context.Background(), mock, "nginx@"+digest)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("digest reference should match RepoDigests")
	}
	exists, err = ImageExists(context.Background(), mock, "nginx:1.21.0@"+digest)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("tag+digest reference should match RepoDigests")
	}
}

func TestImageExists_DigestDoesNotMatchTagAlone(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	wrong := "sha256:" + strings.Repeat("c", 64)
	mock := &MockDockerClient{
		Images: []image.Summary{
			{
				RepoTags:    []string{"nginx:1.21.0"},
				RepoDigests: []string{"nginx@" + digest},
			},
		},
	}
	exists, err := ImageExists(context.Background(), mock, "nginx:1.21.0@"+wrong)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("wrong digest must not match on tag alone")
	}
}

func TestImageExists_DigestOnlyMatchesImageID(t *testing.T) {
	id := "sha256:" + strings.Repeat("d", 64)
	mock := &MockDockerClient{
		Images: []image.Summary{
			{ID: id, RepoTags: []string{"<none>:<none>"}},
		},
	}
	exists, err := ImageExists(context.Background(), mock, id)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("bare digest should match image ID")
	}
}

func TestEnsureImage_SkipsPullWhenDigestCached(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoDigests: []string{"alpine@" + digest}},
		},
	}
	err := EnsureImage(context.Background(), mock, "alpine:3.22@"+digest, logging.NewDiscardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.PulledImages) != 0 {
		t.Errorf("pulled %v, want none", mock.PulledImages)
	}
}

func TestImageExists_TagBehaviorUnchanged(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"test:latest"}},
		},
	}
	exists, err := ImageExists(context.Background(), mock, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("implicit latest")
	}
}

func TestImageExists_UnqualifiedMatchesLocalhostPrefix(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"localhost/gridctl-mcp-runtime-override:local"}},
		},
	}
	exists, err := ImageExists(context.Background(), mock, "gridctl-mcp-runtime-override:local")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("unqualified local tag should match Podman localhost prefix")
	}
}

func TestImageExists_UnqualifiedDoesNotMatchOtherRegistry(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"ghcr.io/gridctl/mcp-runtime-python:local"}},
		},
	}
	exists, err := ImageExists(context.Background(), mock, "gridctl-mcp-runtime-python:local")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("unqualified name must not match a different registry")
	}
}

func TestImageExists_LocalhostRequestDoesNotMatchUnqualified(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"gridctl-mcp-runtime-override:local"}},
		},
	}
	exists, err := ImageExists(context.Background(), mock, "localhost/gridctl-mcp-runtime-override:local")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("localhost-qualified request must not match an unqualified tag")
	}
}

func TestEnsureImage_SkipsPullForLocalhostPrefixedTag(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"localhost/gridctl-mcp-runtime-override:local"}},
		},
	}
	err := EnsureImage(context.Background(), mock, "gridctl-mcp-runtime-override:local", logging.NewDiscardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.PulledImages) != 0 {
		t.Errorf("pulled %v, want none", mock.PulledImages)
	}
}

func TestImageExists_FamiliarNameMatchesRepoDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoDigests: []string{"docker.io/library/alpine@" + digest}},
		},
	}
	exists, err := ImageExists(context.Background(), mock, "alpine:3.22@"+digest)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("familiar alpine name should match docker.io/library RepoDigest")
	}
}

func TestImageExists_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mock := &MockDockerClient{
		Images: []image.Summary{{RepoTags: []string{"test:latest"}}},
	}
	if _, err := ImageExists(ctx, mock, "test:latest"); err == nil {
		t.Fatal("expected canceled context error")
	}
	if err := EnsureImage(ctx, mock, "test:latest", logging.NewDiscardLogger()); err == nil {
		t.Fatal("expected canceled context error")
	}
	if len(mock.PulledImages) != 0 {
		t.Fatalf("pulled %v", mock.PulledImages)
	}
}

func TestEnsureImage_PullError(t *testing.T) {
	mock := &MockDockerClient{
		ImagePullError: fmt.Errorf("pull failed"),
	}
	logger := logging.NewDiscardLogger()

	err := EnsureImage(context.Background(), mock, "test:latest", logger)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
