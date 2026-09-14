package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/distribution/reference"
	"github.com/gridctl/gridctl/pkg/dockerclient"

	"github.com/docker/docker/api/types/image"
)

// EnsureImage pulls the image if it doesn't exist locally.
func EnsureImage(ctx context.Context, cli dockerclient.DockerClient, imageName string, logger *slog.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing images: %w", err)
	}

	for _, img := range images {
		if imageCached(img, imageName) {
			return nil
		}
	}

	// Pull the image
	logger.Info("pulling image", "image", imageName)
	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", imageName, err)
	}
	defer reader.Close()

	// Stream pull progress
	if err := streamPullProgress(reader, logger); err != nil {
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
func streamPullProgress(reader io.Reader, logger *slog.Logger) error {
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
				logger.Debug("pull progress", "id", p.ID, "status", p.Status)
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
	if err := ctx.Err(); err != nil {
		return false, err
	}
	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("listing images: %w", err)
	}

	for _, img := range images {
		if imageCached(img, imageName) {
			return true, nil
		}
	}
	return false, nil
}

func imageCached(img image.Summary, imageName string) bool {
	ref, err := reference.ParseAnyReference(imageName)
	if err != nil {
		return tagCached(img, imageName)
	}
	digested, hasDigest := ref.(reference.Digested)
	if hasDigest {
		want := digested.Digest().String()
		named, hasName := ref.(reference.Named)
		if !hasName && imageIDMatches(img.ID, want) {
			return true
		}
		for _, rd := range img.RepoDigests {
			got, parseErr := reference.ParseAnyReference(rd)
			if parseErr != nil {
				if rd == imageName {
					return true
				}
				continue
			}
			gotDigest, ok := got.(reference.Digested)
			if !ok || gotDigest.Digest().String() != want {
				continue
			}
			if !hasName {
				return true
			}
			gotNamed, ok := got.(reference.Named)
			if !ok {
				continue
			}
			if named.Name() == gotNamed.Name() || reference.FamiliarName(named) == reference.FamiliarName(gotNamed) {
				return true
			}
		}
		return false
	}
	return tagCached(img, imageName)
}

func tagCached(img image.Summary, imageName string) bool {
	for _, tag := range img.RepoTags {
		if tag == imageName || tag == imageName+":latest" {
			return true
		}
	}
	return false
}

func imageIDMatches(id, digest string) bool {
	return id != "" && (id == digest || strings.EqualFold(id, digest))
}
