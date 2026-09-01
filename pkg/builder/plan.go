package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/logging"
)

const buildIdentityVersion = "dockerfile-v1"

var imagePartPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

const maxImagePartLength = 48

// Resolve converts mutable source declaration into an immutable build plan.
func (b *Builder) Resolve(ctx context.Context, opts BuildOptions) (*ResolvedBuildPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Logger == nil {
		opts.Logger = logging.NewDiscardLogger()
	}

	declared := SourceIdentity{
		Type: opts.SourceType, URL: opts.URL, Ref: opts.Ref,
		Path: opts.Path, Dockerfile: opts.Dockerfile,
	}
	resolved := declared
	projectRoot := opts.Path
	var cleanup func() error

	switch opts.SourceType {
	case "git":
		if opts.URL == "" {
			return nil, fmt.Errorf("git URL is required")
		}
		ref := opts.Ref
		if ref == "" {
			ref = "main"
		}
		resolved.Ref = ref

		worktreesDir, err := BuilderWorktreesDir()
		if err != nil {
			return nil, fmt.Errorf("getting builder worktree directory: %w", err)
		}
		if err := os.MkdirAll(worktreesDir, 0755); err != nil {
			return nil, fmt.Errorf("creating builder worktree directory: %w", err)
		}
		projectRoot, err = os.MkdirTemp(worktreesDir, "source-")
		if err != nil {
			return nil, fmt.Errorf("creating builder worktree: %w", err)
		}
		cleanup = func() error { return os.RemoveAll(projectRoot) }

		repo, err := gitpkg.Clone(ctx, projectRoot, gitpkg.CloneOptions{
			URL: opts.URL, AllTags: true, Auth: opts.Auth,
		}, opts.Logger)
		if err != nil {
			_ = cleanup()
			return nil, fmt.Errorf("cloning repository: %w", err)
		}
		if err := gitpkg.Fetch(ctx, projectRoot, gitpkg.FetchOptions{
			AllTags: true, AllBranches: true, Auth: opts.Auth,
		}, opts.Logger); err != nil {
			_ = cleanup()
			return nil, fmt.Errorf("fetching repository: %w", err)
		}
		resolved.Commit, err = gitpkg.SyncWorktree(ctx, repo, ref)
		if err != nil {
			_ = cleanup()
			return nil, fmt.Errorf("checking out resolved ref: %w", err)
		}
	case "local":
		if opts.Path == "" {
			return nil, fmt.Errorf("local path is required")
		}
		info, err := os.Stat(opts.Path)
		if err != nil {
			return nil, fmt.Errorf("source path not found: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("source path is not a directory: %s", opts.Path)
		}
	default:
		return nil, fmt.Errorf("unknown source type: %s", opts.SourceType)
	}

	dockerfile, err := resolveDockerfile(projectRoot, opts.Dockerfile)
	if err != nil {
		if cleanup != nil {
			_ = cleanup()
		}
		return nil, err
	}
	resolved.Dockerfile = dockerfile

	contentDigest, err := digestBuildContext(ctx, projectRoot)
	if err != nil {
		if cleanup != nil {
			_ = cleanup()
		}
		return nil, fmt.Errorf("digesting build context: %w", err)
	}
	identityInput := struct {
		Version       string
		Source        SourceIdentity
		ContentDigest string
		Dockerfile    string
		BuildArgs     map[string]string
		Command       []string
		Platform      string
	}{buildIdentityVersion, resolved, contentDigest, dockerfile, opts.BuildArgs, opts.Command, opts.Platform}
	encoded, err := json.Marshal(identityInput)
	if err != nil {
		if cleanup != nil {
			_ = cleanup()
		}
		return nil, fmt.Errorf("encoding build identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	buildDigest := hex.EncodeToString(digest[:])
	pin := "local"
	if resolved.Commit != "" {
		pin = resolved.Commit[:12]
	}

	return &ResolvedBuildPlan{
		DeclaredIdentity:     declared,
		ResolvedIdentity:     resolved,
		EffectiveProjectRoot: projectRoot,
		Command:              opts.Command,
		Dockerfile:           dockerfile,
		BuildInputDigest:     buildDigest,
		ImageTag:             GenerateImageTag(opts.Stack, opts.ServerName, pin, buildDigest),
		Provenance:           BuildProvenance{SourceContentDigest: contentDigest, TargetPlatform: opts.Platform},
		cleanup:              cleanup,
	}, nil
}

// GenerateImageTag creates an OCI-compatible tag that cannot alias distinct build inputs.
func GenerateImageTag(stack, server, pin, buildDigest string) string {
	if len(buildDigest) < 12 {
		digest := sha256.Sum256([]byte(buildDigest))
		buildDigest = hex.EncodeToString(digest[:])
	}
	return fmt.Sprintf("gridctl-%s-%s:%s-%s",
		sanitizeImagePart(stack), sanitizeImagePart(server),
		sanitizeImagePart(pin), strings.ToLower(buildDigest[:12]))
}

func sanitizeImagePart(value string) string {
	original := strings.ToLower(value)
	sanitized := strings.Trim(imagePartPattern.ReplaceAllString(original, "-"), "-._")
	if sanitized == "" {
		sanitized = "source"
	}
	changed := sanitized != value
	if len(sanitized) > maxImagePartLength {
		sanitized = strings.TrimRight(sanitized[:maxImagePartLength-9], "-._")
		changed = true
	}
	if changed {
		digest := sha256.Sum256([]byte(value))
		sanitized += "-" + hex.EncodeToString(digest[:4])
	}
	return sanitized
}

func resolveDockerfile(contextPath, configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(filepath.Join(contextPath, configured)); err == nil {
			return configured, nil
		}
	}
	for _, candidate := range []string{"Dockerfile", "dockerfile", "Containerfile"} {
		if _, err := os.Stat(filepath.Join(contextPath, candidate)); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no Dockerfile found in %s", contextPath)
}

func digestBuildContext(ctx context.Context, contextPath string) (string, error) {
	h := sha256.New()
	excludes := getExcludePatterns(contextPath)
	err := filepath.Walk(contextPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(contextPath, path)
		if err != nil || rel == "." {
			return err
		}
		for _, pattern := range excludes {
			matchedRel, _ := filepath.Match(pattern, rel)
			matchedBase, _ := filepath.Match(pattern, filepath.Base(rel))
			if matchedRel || matchedBase {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		_, _ = io.WriteString(h, rel+"\x00"+info.Mode().String()+"\x00")
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
