package builder

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/pkg/dockerclient"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/archive"
)

// BuildImage builds a Docker image from a context directory.
func BuildImage(ctx context.Context, cli dockerclient.DockerClient, contextPath, dockerfile, tag string, buildArgs map[string]string, noCache bool, logger *slog.Logger) (string, error) {
	logger.Info("building image", "tag", tag)

	// Verify Dockerfile exists
	dockerfilePath := filepath.Join(contextPath, dockerfile)
	if _, err := os.Stat(dockerfilePath); err != nil {
		return "", fmt.Errorf("dockerfile not found at %s: %w", dockerfilePath, err)
	}

	// Create tar archive of the build context
	buildContext, err := archive.TarWithOptions(contextPath, &archive.TarOptions{
		ExcludePatterns: getExcludePatterns(contextPath),
	})
	if err != nil {
		return "", fmt.Errorf("creating build context: %w", err)
	}
	defer buildContext.Close()

	// Convert build args to the format Docker expects
	dockerBuildArgs := make(map[string]*string)
	for k, v := range buildArgs {
		val := v
		dockerBuildArgs[k] = &val
	}

	// Build the image
	resp, err := cli.ImageBuild(ctx, buildContext, types.ImageBuildOptions{
		Dockerfile: dockerfile,
		Tags:       []string{tag},
		BuildArgs:  dockerBuildArgs,
		Remove:     true, // Remove intermediate containers
		NoCache:    noCache,
	})
	if err != nil {
		return "", fmt.Errorf("building image: %w", err)
	}
	defer resp.Body.Close()

	// Stream build output
	imageID, err := streamBuildOutput(resp.Body, logger)
	if err != nil {
		return "", err
	}

	logger.Info("built image", "tag", tag)
	return imageID, nil
}

// buildOutput represents a Docker build output message.
type buildOutput struct {
	Stream      string          `json:"stream"`
	Error       string          `json:"error"`
	ErrorDetail json.RawMessage `json:"errorDetail"`
	Aux         struct {
		ID string `json:"ID"`
	} `json:"aux"`
}

// streamBuildOutput reads and displays Docker build output.
func streamBuildOutput(reader io.Reader, logger *slog.Logger) (string, error) {
	decoder := json.NewDecoder(reader)
	var imageID string

	for {
		var output buildOutput
		if err := decoder.Decode(&output); err != nil {
			if err == io.EOF {
				break
			}
			return "", fmt.Errorf("decoding build output: %w", err)
		}

		// Check for errors
		if output.Error != "" {
			return "", fmt.Errorf("build error: %s", output.Error)
		}

		// Capture image ID from aux messages
		if output.Aux.ID != "" {
			imageID = output.Aux.ID
		}

		// Log build steps (filter noise)
		if output.Stream != "" {
			stream := strings.TrimSpace(output.Stream)
			if stream != "" && (strings.HasPrefix(stream, "Step") ||
				strings.HasPrefix(stream, "Successfully") ||
				strings.HasPrefix(stream, "---")) {
				logger.Debug("build output", "line", stream)
			}
		}
	}

	return imageID, nil
}

// getExcludePatterns returns patterns to exclude from the build context.
func getExcludePatterns(contextPath string) []string {
	patterns := []string{
		".git",
		".gitignore",
		"node_modules",
		"__pycache__",
		"*.pyc",
		".env",
		".env.*",
	}

	// Check for .dockerignore
	dockerignore := filepath.Join(contextPath, ".dockerignore")
	if data, err := os.ReadFile(dockerignore); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				patterns = append(patterns, line)
			}
		}
	}

	return patterns
}

// CreateTarFromDir creates a tar archive from a directory.
// This is a simpler alternative if archive.TarWithOptions has issues.
func CreateTarFromDir(dir string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	go func() {
		tw := tar.NewWriter(pw)
		defer tw.Close()
		defer pw.Close()

		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Get relative path
			relPath, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}

			// Skip .git directory
			if strings.HasPrefix(relPath, ".git") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Create tar header
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = relPath

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			// Write file content
			if !info.IsDir() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				defer file.Close()
				if _, err := io.Copy(tw, file); err != nil {
					return err
				}
			}

			return nil
		})

		if err != nil {
			pw.CloseWithError(err)
		}
	}()

	return pr, nil
}
