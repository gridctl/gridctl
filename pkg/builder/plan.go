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

	"github.com/go-git/go-git/v5/plumbing"
	gitpkg "github.com/gridctl/gridctl/pkg/git"
	"github.com/gridctl/gridctl/pkg/logging"
)

const buildIdentityVersion = "dockerfile-v1"
const generatedDockerfileName = ".gridctl.Dockerfile"

type pypiReleaseResolver interface {
	Resolve(ctx context.Context, project, version, explicitPython string) (*PyPIRelease, error)
}

type pypiVersionResolver interface {
	Versions(ctx context.Context, project string) (*PyPIVersions, error)
}

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
	logBuildPhase(opts, "resolving_source")

	projectPath := ""
	if opts.SourceType == "local" {
		projectPath = opts.ProjectPath
	}
	declared := SourceIdentity{
		Type: opts.SourceType, URL: opts.URL, Ref: opts.Ref,
		Path: opts.Path, ProjectPath: projectPath, Dockerfile: opts.Dockerfile,
		Package: opts.Package,
	}
	resolved := declared
	projectRoot := opts.Path
	var cleanup func() error
	mutableRef := false
	pythonVersion := ""
	generatedDockerfile := ""

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

		logBuildPhase(opts, "preparing_context")
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
		head, err := repo.Reference(plumbing.HEAD, false)
		if err != nil {
			_ = cleanup()
			return nil, fmt.Errorf("reading resolved ref state: %w", err)
		}
		mutableRef = head.Type() == plumbing.SymbolicReference
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
		logBuildPhase(opts, "preparing_context")
	case "pypi":
		if b.pypiResolver == nil {
			return nil, fmt.Errorf("PyPI resolver is not configured")
		}
		release, err := b.pypiResolver.Resolve(ctx, opts.Package, opts.Ref, opts.Python)
		if err != nil {
			return nil, err
		}
		command, err := ResolveConsoleCommand(opts.Command, release.Package, release.Metadata.ConsoleScripts)
		if err != nil {
			return nil, err
		}
		pythonVersion = release.Python
		logBuildPhase(opts, "preparing_context")
		logBuildPhase(opts, "generating_dockerfile")
		generatedDockerfile, err = GeneratePythonDockerfile(ctx, PythonBuildSpec{
			Python: pythonVersion, Package: release.Package, Version: release.Version,
			Extras: opts.Extras, With: opts.With, Packages: opts.Packages, Command: command,
		})
		if err != nil {
			return nil, fmt.Errorf("generating Python Dockerfile: %w", err)
		}
		projectRoot, cleanup, err = materializePythonContext(ctx, "", generatedDockerfile)
		if err != nil {
			return nil, err
		}
		opts.Command = command
		resolved.Package = release.Package
		resolved.Ref = release.Version
		resolved.Version = release.Version
		resolved.Artifact = release.Artifact.Filename
		resolved.ArtifactSHA256 = release.Artifact.SHA256
	default:
		return nil, fmt.Errorf("unknown source type: %s", opts.SourceType)
	}

	dockerfile := generatedDockerfileName
	if opts.SourceType != "pypi" {
		if opts.Runtime == "python" && opts.Dockerfile == "" {
			subproject := opts.ProjectPath
			if opts.SourceType == "git" {
				subproject = opts.Path
			}
			selectedRoot, err := resolveSubprojectRoot(projectRoot, subproject)
			if err != nil {
				return nil, closePlanSource(cleanup, err)
			}
			metadata, err := ParsePythonProject(ctx, selectedRoot)
			if err != nil {
				return nil, closePlanSource(cleanup, err)
			}
			pythonVersion, err = SelectPythonVersion(metadata.RequiresPython, opts.Python)
			if err != nil {
				return nil, closePlanSource(cleanup, err)
			}
			command, err := ResolveConsoleCommand(opts.Command, metadata.Name, metadata.ConsoleScripts)
			if err != nil {
				return nil, closePlanSource(cleanup, err)
			}
			logBuildPhase(opts, "generating_dockerfile")
			generatedDockerfile, err = GeneratePythonDockerfile(ctx, PythonBuildSpec{
				Python: pythonVersion, Extras: opts.Extras, With: opts.With, Packages: opts.Packages,
				Command: command, Local: true, Locked: metadata.HasUVLock,
			})
			if err != nil {
				return nil, closePlanSource(cleanup, fmt.Errorf("generating Python Dockerfile: %w", err))
			}
			generatedRoot, generatedCleanup, err := materializePythonContext(ctx, selectedRoot, generatedDockerfile)
			if err != nil {
				return nil, closePlanSource(cleanup, err)
			}
			if cleanup != nil {
				if err := cleanup(); err != nil {
					_ = generatedCleanup()
					return nil, fmt.Errorf("cleaning source worktree: %w", err)
				}
			}
			projectRoot = generatedRoot
			cleanup = generatedCleanup
			opts.Command = command
			resolved.Package = metadata.Name
			resolved.Version = metadata.Version
		} else {
			explicitPythonDockerfile := opts.Runtime == "python" && opts.Dockerfile != ""
			if explicitPythonDockerfile {
				if opts.SourceType == "git" && opts.Path != "" {
					selectedRoot, err := resolveSubprojectRoot(projectRoot, opts.Path)
					if err != nil {
						return nil, closePlanSource(cleanup, err)
					}
					projectRoot = selectedRoot
				}
			}
			var err error
			dockerfile, err = resolveDockerfile(projectRoot, opts.Dockerfile)
			if err != nil {
				return nil, closePlanSource(cleanup, err)
			}
			if explicitPythonDockerfile {
				opts.Logger.Info("Found configured Dockerfile; building from it.")
			}
		}
	}
	resolved.Dockerfile = dockerfile

	contentDigest, err := digestBuildContext(ctx, projectRoot)
	if err != nil {
		if cleanup != nil {
			_ = cleanup()
		}
		return nil, fmt.Errorf("digesting build context: %w", err)
	}
	buildArgs := opts.BuildArgs
	if len(buildArgs) == 0 {
		buildArgs = nil
	}
	command := opts.Command
	if len(command) == 0 {
		command = nil
	}
	identityInput := struct {
		Version       string
		Source        SourceIdentity
		ContentDigest string
		Dockerfile    string
		BuildArgs     map[string]string
		Command       []string
		Platform      string
		Python        string
	}{buildIdentityVersion, resolved, contentDigest, dockerfile, buildArgs, command, opts.Platform, pythonVersion}
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
	} else if resolved.Version != "" {
		pin = resolved.Version
	}
	provenance := BuildProvenance{SourceContentDigest: contentDigest, TargetPlatform: opts.Platform}
	if generatedDockerfile != "" {
		template := DefaultPythonTemplate()
		provenance.GeneratorVersion = template.Version
		provenance.BaseImage = template.PythonImages[pythonVersion]
		provenance.UVImage = template.UVImage
	}

	return &ResolvedBuildPlan{
		DeclaredIdentity:     declared,
		ResolvedIdentity:     resolved,
		EffectiveProjectRoot: projectRoot,
		Python:               pythonVersion,
		Command:              command,
		Dockerfile:           dockerfile,
		GeneratedDockerfile:  generatedDockerfile,
		BuildInputDigest:     buildDigest,
		ImageTag:             GenerateImageTag(opts.Stack, opts.ServerName, pin, buildDigest),
		MutableRef:           mutableRef,
		Provenance:           provenance,
		cleanup:              cleanup,
	}, nil
}

func logBuildPhase(opts BuildOptions, phase string) {
	opts.Logger.Info("MCP server build phase", "server", opts.ServerName, "phase", phase)
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
		} else {
			return "", fmt.Errorf("configured Dockerfile %s was not found: %w", configured, err)
		}
	}
	for _, candidate := range []string{"Dockerfile", "dockerfile", "Containerfile"} {
		if _, err := os.Stat(filepath.Join(contextPath, candidate)); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no Dockerfile found in %s", contextPath)
}

func resolveSubprojectRoot(sourceRoot, subproject string) (string, error) {
	root, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return "", fmt.Errorf("resolving source root: %w", err)
	}
	if subproject == "" {
		return root, nil
	}
	if filepath.IsAbs(subproject) || filepath.Clean(subproject) != subproject || subproject == "." || strings.HasPrefix(subproject, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("python project path must be a clean relative path within the source root")
	}
	candidate, err := filepath.EvalSymlinks(filepath.Join(root, subproject))
	if err != nil {
		return "", fmt.Errorf("resolving Python project path: %w", err)
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("python project path escapes the source root")
	}
	info, err := os.Stat(candidate)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("python project path is not a directory: %s", subproject)
	}
	return candidate, nil
}

func materializePythonContext(ctx context.Context, sourceRoot, dockerfile string) (string, func() error, error) {
	worktreesDir, err := BuilderWorktreesDir()
	if err != nil {
		return "", nil, fmt.Errorf("getting builder worktree directory: %w", err)
	}
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return "", nil, fmt.Errorf("creating builder worktree directory: %w", err)
	}
	destination, err := os.MkdirTemp(worktreesDir, "python-")
	if err != nil {
		return "", nil, fmt.Errorf("creating generated Python context: %w", err)
	}
	cleanup := func() error { return os.RemoveAll(destination) }
	if sourceRoot != "" {
		if err := copyPythonContext(ctx, sourceRoot, destination); err != nil {
			_ = cleanup()
			return "", nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(destination, generatedDockerfileName), []byte(dockerfile), 0600); err != nil {
		_ = cleanup()
		return "", nil, fmt.Errorf("writing generated Python Dockerfile: %w", err)
	}
	ignore := ".git\n.gridctl\n__pycache__\n*.pyc\n.venv\nvenv\nbuild\ndist\n*.egg-info\n"
	if err := os.WriteFile(filepath.Join(destination, ".dockerignore"), []byte(ignore), 0600); err != nil {
		_ = cleanup()
		return "", nil, fmt.Errorf("writing generated Python .dockerignore: %w", err)
	}
	return destination, cleanup, nil
}

func copyPythonContext(ctx context.Context, sourceRoot, destination string) error {
	resolvedRoot, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return fmt.Errorf("resolving Python source context: %w", err)
	}
	return filepath.Walk(sourceRoot, func(sourcePath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceRoot, sourcePath)
		if err != nil || rel == "." {
			return err
		}
		if info.IsDir() && isExcludedPythonContextDir(info.Name()) {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".pyc") {
			return nil
		}
		targetPath := filepath.Join(destination, rel)
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(sourcePath)
			if err != nil {
				return err
			}
			resolvedTarget, err := filepath.EvalSymlinks(sourcePath)
			if err != nil {
				return err
			}
			targetRel, err := filepath.Rel(resolvedRoot, resolvedTarget)
			if filepath.IsAbs(linkTarget) || err != nil || targetRel == ".." || strings.HasPrefix(targetRel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("python source symlink escapes project root: %s", rel)
			}
			return os.Symlink(linkTarget, targetPath)
		}
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file in Python source context: %s", rel)
		}
		source, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			_ = source.Close()
			return err
		}
		_, copyErr := io.Copy(target, source)
		closeTargetErr := target.Close()
		closeSourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeTargetErr != nil {
			return closeTargetErr
		}
		return closeSourceErr
	})
}

func isExcludedPythonContextDir(name string) bool {
	switch name {
	case ".git", ".gridctl", "__pycache__", ".venv", "venv", "build", "dist":
		return true
	default:
		return strings.HasSuffix(name, ".egg-info")
	}
}

func closePlanSource(cleanup func() error, cause error) error {
	if cleanup == nil {
		return cause
	}
	if err := cleanup(); err != nil {
		return fmt.Errorf("%w (also cleaning source: %v)", cause, err)
	}
	return cause
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
