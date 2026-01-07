package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"agentlab/pkg/dockerclient"

	"github.com/docker/docker/api/types/image"
)

// EnsureImage pulls the image if it doesn't exist locally.
func EnsureImage(ctx context.Context, cli dockerclient.DockerClient, imageName string) error {
	// Check if image exists locally
	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == imageName || tag == imageName+":latest" {
				return nil // Image exists
			}
		}
	}

	// Pull the image
	fmt.Printf("  Pulling image %s...\n", imageName)
	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Stream pull progress
	if err := streamPullProgress(reader); err != nil {
		return fmt.Errorf("streaming pull progress: %w", err)
	}

	return nil
}

// pullProgress represents a Docker pull progress message.
type pullProgress struct {
	Status         string `json:"status"`
	Progress       string `json:"progress"`
	ProgressDetail struct {
		Current int64 `json:"current"`
		Total   int64 `json:"total"`
	} `json:"progressDetail"`
	ID string `json:"id"`
}

// streamPullProgress reads and displays Docker pull progress.
func streamPullProgress(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	lastStatus := ""

	for {
		var p pullProgress
		if err := decoder.Decode(&p); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		// Only print status changes to avoid too much output
		status := p.Status
		if p.ID != "" {
			status = p.ID + ": " + p.Status
		}
		if p.Progress != "" {
			status += " " + p.Progress
		}

		// Simple progress indicator
		if status != lastStatus && !strings.Contains(p.Status, "Pulling") {
			if p.Status == "Pull complete" || p.Status == "Already exists" {
				fmt.Printf("    %s: %s\n", p.ID, p.Status)
			}
			lastStatus = status
		}
	}

	// Consume any remaining output
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

// ImageExists checks if an image exists locally.
func ImageExists(ctx context.Context, cli dockerclient.DockerClient, imageName string) (bool, error) {
	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("listing images: %w", err)
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if tag == imageName || tag == imageName+":latest" {
				return true, nil
			}
		}
	}
	return false, nil
}

// Suppress unused import warning
var _ = os.Stdout
