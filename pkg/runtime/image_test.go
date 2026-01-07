package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/image"
)

func TestEnsureImage_AlreadyExists(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"nginx:latest"}},
		},
	}

	ctx := context.Background()
	err := EnsureImage(ctx, mock, "nginx:latest")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.PulledImages) != 0 {
		t.Errorf("expected no pulls, got %v", mock.PulledImages)
	}
}

func TestEnsureImage_ExistsWithoutTag(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"nginx:latest"}},
		},
	}

	ctx := context.Background()
	// Image name without tag should match image:latest
	err := EnsureImage(ctx, mock, "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.PulledImages) != 0 {
		t.Errorf("expected no pulls, got %v", mock.PulledImages)
	}
}

func TestEnsureImage_PullsNew(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{}, // No images locally
	}

	ctx := context.Background()
	err := EnsureImage(ctx, mock, "postgres:16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.PulledImages) != 1 || mock.PulledImages[0] != "postgres:16" {
		t.Errorf("expected 'postgres:16' to be pulled, got %v", mock.PulledImages)
	}
}

func TestEnsureImage_PullError(t *testing.T) {
	mock := &MockDockerClient{
		Images:         []image.Summary{},
		ImagePullError: errors.New("pull failed"),
	}

	ctx := context.Background()
	err := EnsureImage(ctx, mock, "nonexistent:image")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestEnsureImage_ListError(t *testing.T) {
	mock := &MockDockerClient{
		ImageListError: errors.New("list failed"),
	}

	ctx := context.Background()
	err := EnsureImage(ctx, mock, "nginx:latest")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestImageExists_Found(t *testing.T) {
	mock := &MockDockerClient{
		Images: []image.Summary{
			{RepoTags: []string{"nginx:1.21", "nginx:latest"}},
			{RepoTags: []string{"postgres:16"}},
		},
	}

	tests := []struct {
		name      string
		imageName string
		want      bool
	}{
		{"exact match", "nginx:1.21", true},
		{"latest match", "nginx:latest", true},
		{"different tag", "postgres:16", true},
		{"not found", "redis:latest", false},
		{"without tag matches latest", "nginx", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			exists, err := ImageExists(ctx, mock, tc.imageName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if exists != tc.want {
				t.Errorf("ImageExists(%q) = %v, want %v", tc.imageName, exists, tc.want)
			}
		})
	}
}

func TestImageExists_Error(t *testing.T) {
	mock := &MockDockerClient{
		ImageListError: errors.New("list failed"),
	}

	ctx := context.Background()
	_, err := ImageExists(ctx, mock, "nginx:latest")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
