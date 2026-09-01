package builder

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultPyPIURL    = "https://pypi.org/pypi"
	maxPyPIMetadata   = 4 << 20
	maxPythonArtifact = 32 << 20
)

var distributionNamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)
var exactPEP440Pattern = regexp.MustCompile(`(?i)^v?(?:[0-9]+!)?[0-9]+(?:\.[0-9]+)*(?:(?:a|b|rc)[0-9]+)?(?:[._-]?post[0-9]+)?(?:[._-]?dev[0-9]+)?(?:\+[a-z0-9]+(?:[._-][a-z0-9]+)*)?$`)

// PyPIArtifact records the immutable release file selected for safe metadata
// inspection. uv remains free to choose its compatible install artifact.
type PyPIArtifact struct {
	Filename       string `json:"filename"`
	URL            string `json:"url"`
	Packagetype    string `json:"packageType"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	RequiresPython string `json:"requiresPython,omitempty"`
}

// PythonPackageMetadata contains package metadata read without importing code.
type PythonPackageMetadata struct {
	Name           string   `json:"name"`
	Version        string   `json:"version,omitempty"`
	RequiresPython string   `json:"requiresPython,omitempty"`
	ConsoleScripts []string `json:"consoleScripts,omitempty"`
}

// PyPIRelease is an exact, public release resolved through official PyPI.
type PyPIRelease struct {
	Package        string                `json:"package"`
	Version        string                `json:"version"`
	RequiresPython string                `json:"requiresPython,omitempty"`
	Python         string                `json:"python"`
	Artifact       PyPIArtifact          `json:"artifact"`
	Metadata       PythonPackageMetadata `json:"metadata"`
}

// PyPIResolver performs bounded requests against the public PyPI JSON API.
type PyPIResolver struct {
	client  *http.Client
	baseURL string
}

// NewPyPIResolver creates a resolver. A nil client uses a bounded default.
func NewPyPIResolver(client *http.Client) *PyPIResolver {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	} else if client.Timeout == 0 {
		copy := *client
		copy.Timeout = 15 * time.Second
		client = &copy
	}
	return &PyPIResolver{client: client, baseURL: defaultPyPIURL}
}

// Resolve resolves and inspects one exact public PyPI release.
func (r *PyPIResolver) Resolve(ctx context.Context, project, version, explicitPython string) (*PyPIRelease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !distributionNamePattern.MatchString(project) {
		return nil, fmt.Errorf("invalid PyPI project name %q", project)
	}
	if !exactPEP440Pattern.MatchString(version) || strings.EqualFold(version, "latest") {
		return nil, fmt.Errorf("PyPI version must be an exact published PEP 440 version")
	}

	endpoint := strings.TrimRight(r.baseURL, "/") + "/" + url.PathEscape(project) + "/json"
	var payload struct {
		Info struct {
			Name           string `json:"name"`
			Version        string `json:"version"`
			RequiresPython string `json:"requires_python"`
		} `json:"info"`
		Releases map[string][]struct {
			Filename       string `json:"filename"`
			Packagetype    string `json:"packagetype"`
			URL            string `json:"url"`
			Size           int64  `json:"size"`
			Yanked         bool   `json:"yanked"`
			RequiresPython string `json:"requires_python"`
			Digests        struct {
				SHA256 string `json:"sha256"`
			} `json:"digests"`
		} `json:"releases"`
	}
	status, err := r.getJSON(ctx, endpoint, &payload)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		//nolint:staticcheck // The public Error Contract requires this punctuation.
		return nil, fmt.Errorf("No PyPI project named %s.", project)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("PyPI returned HTTP %d for %s", status, project)
	}
	if canonicalDistributionName(payload.Info.Name) != canonicalDistributionName(project) {
		return nil, fmt.Errorf("PyPI returned project %q for %q", payload.Info.Name, project)
	}
	files, ok := payload.Releases[version]
	if !ok || len(files) == 0 {
		//nolint:staticcheck // The public Error Contract requires this punctuation.
		return nil, fmt.Errorf("%s is not a published version of %s. Latest is %s.", version, payload.Info.Name, payload.Info.Version)
	}
	allYanked := true
	artifacts := make([]PyPIArtifact, 0, len(files))
	for _, file := range files {
		if file.Yanked {
			continue
		}
		allYanked = false
		if len(file.Digests.SHA256) != sha256.Size*2 || file.Size < 0 || file.Size > maxPythonArtifact {
			continue
		}
		artifacts = append(artifacts, PyPIArtifact{
			Filename: file.Filename, URL: file.URL, Packagetype: file.Packagetype,
			SHA256: file.Digests.SHA256, Size: file.Size, RequiresPython: file.RequiresPython,
		})
	}
	if allYanked {
		//nolint:staticcheck // The public Error Contract requires this punctuation.
		return nil, fmt.Errorf("%s of %s is yanked on PyPI. Choose a non-yanked version.", version, payload.Info.Name)
	}
	artifact, ok := selectMetadataArtifact(artifacts)
	if !ok {
		return nil, fmt.Errorf("PyPI release %s of %s has no safe artifact metadata", version, payload.Info.Name)
	}
	if err := r.validateArtifactURL(artifact.URL); err != nil {
		return nil, err
	}
	requiresPython := artifact.RequiresPython
	if requiresPython == "" {
		requiresPython = payload.Info.RequiresPython
	}
	python, err := SelectPythonVersion(requiresPython, explicitPython)
	if err != nil {
		return nil, err
	}
	metadata := PythonPackageMetadata{Name: payload.Info.Name, Version: version, RequiresPython: requiresPython}
	if artifact.Packagetype == "bdist_wheel" {
		metadata, err = r.inspectWheel(ctx, artifact)
		if err != nil {
			return nil, fmt.Errorf("inspecting %s: %w", artifact.Filename, err)
		}
		if canonicalDistributionName(metadata.Name) != canonicalDistributionName(payload.Info.Name) || metadata.Version != version {
			return nil, fmt.Errorf("wheel metadata identity does not match %s==%s", payload.Info.Name, version)
		}
	}
	return &PyPIRelease{Package: payload.Info.Name, Version: version, RequiresPython: requiresPython, Python: python, Artifact: artifact, Metadata: metadata}, nil
}

func (r *PyPIResolver) getJSON(ctx context.Context, endpoint string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := r.do(req)
	if err != nil {
		return 0, fmt.Errorf("requesting PyPI metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return resp.StatusCode, nil
	}
	reader := io.LimitReader(resp.Body, maxPyPIMetadata+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, err
	}
	if len(data) > maxPyPIMetadata {
		return 0, fmt.Errorf("PyPI metadata exceeds %d bytes", maxPyPIMetadata)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return 0, fmt.Errorf("decoding PyPI metadata: %w", err)
	}
	return resp.StatusCode, nil
}

func (r *PyPIResolver) do(req *http.Request) (*http.Response, error) {
	client := *r.client
	base, _ := url.Parse(r.baseURL)
	originalCheck := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many PyPI redirects")
		}
		host := next.URL.Hostname()
		if host != base.Hostname() && host != "files.pythonhosted.org" {
			return fmt.Errorf("PyPI redirect to untrusted host %q", host)
		}
		if originalCheck != nil {
			return originalCheck(next, via)
		}
		return nil
	}
	return client.Do(req)
}

func (r *PyPIResolver) inspectWheel(ctx context.Context, artifact PyPIArtifact) (PythonPackageMetadata, error) {
	if err := r.validateArtifactURL(artifact.URL); err != nil {
		return PythonPackageMetadata{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return PythonPackageMetadata{}, err
	}
	resp, err := r.do(req)
	if err != nil {
		return PythonPackageMetadata{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return PythonPackageMetadata{}, fmt.Errorf("artifact returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPythonArtifact+1))
	if err != nil {
		return PythonPackageMetadata{}, err
	}
	if len(data) > maxPythonArtifact || artifact.Size > 0 && int64(len(data)) != artifact.Size {
		return PythonPackageMetadata{}, fmt.Errorf("artifact size does not match bounded PyPI metadata")
	}
	digest := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), artifact.SHA256) {
		return PythonPackageMetadata{}, fmt.Errorf("artifact SHA-256 digest does not match PyPI metadata")
	}
	return InspectWheelMetadata(ctx, data)
}

func (r *PyPIResolver) validateArtifactURL(value string) error {
	parsed, err := url.Parse(value)
	base, _ := url.Parse(r.baseURL)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("artifact URL is not an official PyPI HTTPS URL")
	}
	if r.baseURL == defaultPyPIURL {
		if parsed.Scheme != "https" || parsed.Hostname() != "files.pythonhosted.org" {
			return fmt.Errorf("artifact URL is not an official PyPI HTTPS URL")
		}
	} else if parsed.Hostname() != base.Hostname() {
		return fmt.Errorf("artifact URL does not match the configured test index")
	}
	return nil
}

// InspectWheelMetadata reads standards metadata from a wheel without importing
// or executing package code.
func InspectWheelMetadata(ctx context.Context, data []byte) (PythonPackageMetadata, error) {
	if err := ctx.Err(); err != nil {
		return PythonPackageMetadata{}, err
	}
	if len(data) > maxPythonArtifact {
		return PythonPackageMetadata{}, fmt.Errorf("wheel exceeds %d bytes", maxPythonArtifact)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return PythonPackageMetadata{}, fmt.Errorf("opening wheel: %w", err)
	}
	var result PythonPackageMetadata
	metadataFiles := 0
	entryPointFiles := 0
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return PythonPackageMetadata{}, err
		}
		clean := path.Clean(file.Name)
		if clean != file.Name || strings.HasPrefix(clean, "../") || file.UncompressedSize64 > 1<<20 {
			return PythonPackageMetadata{}, fmt.Errorf("unsafe wheel metadata path or size: %s", file.Name)
		}
		if !strings.Contains(clean, ".dist-info/") {
			continue
		}
		base := path.Base(clean)
		if base != "METADATA" && base != "entry_points.txt" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return PythonPackageMetadata{}, err
		}
		content, readErr := io.ReadAll(io.LimitReader(rc, 1<<20+1))
		closeErr := rc.Close()
		if readErr != nil {
			return PythonPackageMetadata{}, readErr
		}
		if closeErr != nil {
			return PythonPackageMetadata{}, closeErr
		}
		if base == "METADATA" {
			metadataFiles++
			if metadataFiles > 1 {
				return PythonPackageMetadata{}, fmt.Errorf("wheel contains multiple core metadata files")
			}
			for _, line := range strings.Split(string(content), "\n") {
				key, value, found := strings.Cut(line, ":")
				if !found {
					continue
				}
				switch strings.ToLower(key) {
				case "name":
					result.Name = strings.TrimSpace(value)
				case "version":
					result.Version = strings.TrimSpace(value)
				case "requires-python":
					result.RequiresPython = strings.TrimSpace(value)
				}
			}
		} else {
			entryPointFiles++
			if entryPointFiles > 1 {
				return PythonPackageMetadata{}, fmt.Errorf("wheel contains multiple entry-point files")
			}
			result.ConsoleScripts = parseEntryPoints(content)
		}
	}
	if result.Name == "" || result.Version == "" {
		return PythonPackageMetadata{}, fmt.Errorf("wheel has no complete core metadata")
	}
	sort.Strings(result.ConsoleScripts)
	return result, nil
}

func parseEntryPoints(content []byte) []string {
	inConsoleScripts := false
	var scripts []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inConsoleScripts = strings.EqualFold(line, "[console_scripts]")
			continue
		}
		if inConsoleScripts {
			name, _, found := strings.Cut(line, "=")
			name = strings.TrimSpace(name)
			if found && distributionNamePattern.MatchString(name) {
				scripts = append(scripts, name)
			}
		}
	}
	return scripts
}

func selectMetadataArtifact(artifacts []PyPIArtifact) (PyPIArtifact, bool) {
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Filename < artifacts[j].Filename })
	for _, artifact := range artifacts {
		if artifact.Packagetype == "bdist_wheel" && (strings.HasSuffix(artifact.Filename, "-py3-none-any.whl") || strings.HasSuffix(artifact.Filename, "-py2.py3-none-any.whl")) {
			return artifact, true
		}
	}
	for _, artifact := range artifacts {
		if artifact.Packagetype == "sdist" {
			return artifact, true
		}
	}
	return PyPIArtifact{}, false
}

func canonicalDistributionName(value string) string {
	return strings.Trim(regexp.MustCompile(`[-_.]+`).ReplaceAllString(strings.ToLower(value), "-"), "-")
}
