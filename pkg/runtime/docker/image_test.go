package docker

import (
	"context"
	"fmt"
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
