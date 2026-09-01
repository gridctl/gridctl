package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const maxPythonMetadataFile = 1 << 20

var setupStringPattern = regexp.MustCompile(`(?m)\b(name|version|python_requires)\s*=\s*["']([^"']+)["']`)
var setupConsoleScriptPattern = regexp.MustCompile(`["']([A-Za-z0-9][A-Za-z0-9._-]*)\s*=\s*[^"']+["']`)

// PythonProjectMetadata describes static project metadata needed to generate a
// build. Dynamic metadata is deliberately not evaluated on the host.
type PythonProjectMetadata struct {
	Name           string   `json:"name"`
	Version        string   `json:"version,omitempty"`
	RequiresPython string   `json:"requiresPython,omitempty"`
	ConsoleScripts []string `json:"consoleScripts,omitempty"`
	HasUVLock      bool     `json:"hasUVLock"`
	SourceFile     string   `json:"sourceFile"`
}

// ParsePythonProject reads pyproject.toml or setup.py as data without invoking
// Python, a build backend, or project code.
func ParsePythonProject(ctx context.Context, projectRoot string) (*PythonProjectMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving Python project path: %w", err)
	}
	for _, filename := range []string{"pyproject.toml", "setup.py"} {
		path := filepath.Join(root, filename)
		info, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() || info.Size() > maxPythonMetadataFile {
			//nolint:staticcheck // Python is a proper noun.
			return nil, fmt.Errorf("Python project metadata %s is not a bounded regular file", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var metadata *PythonProjectMetadata
		if filename == "pyproject.toml" {
			metadata, err = parsePyProject(data)
		} else {
			metadata, err = parseSetupPy(data)
		}
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		metadata.SourceFile = filename
		_, lockErr := os.Stat(filepath.Join(root, "uv.lock"))
		metadata.HasUVLock = lockErr == nil
		return metadata, nil
	}
	//nolint:staticcheck // The public Error Contract requires this punctuation.
	return nil, fmt.Errorf("No Dockerfile or Python project metadata was found in %s. Add a Dockerfile, pyproject.toml, or setup.py.", projectRoot)
}

func parsePyProject(data []byte) (*PythonProjectMetadata, error) {
	var document struct {
		Project struct {
			Name           string            `toml:"name"`
			Version        string            `toml:"version"`
			RequiresPython string            `toml:"requires-python"`
			Scripts        map[string]string `toml:"scripts"`
			Dynamic        []string          `toml:"dynamic"`
		} `toml:"project"`
	}
	if err := toml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if document.Project.Name == "" || containsString(document.Project.Dynamic, "name") {
		return nil, fmt.Errorf("project.name must be static")
	}
	metadata := &PythonProjectMetadata{
		Name: document.Project.Name, Version: document.Project.Version,
		RequiresPython: document.Project.RequiresPython,
	}
	for script := range document.Project.Scripts {
		if !distributionNamePattern.MatchString(script) {
			return nil, fmt.Errorf("invalid console script name %q", script)
		}
		metadata.ConsoleScripts = append(metadata.ConsoleScripts, script)
	}
	sort.Strings(metadata.ConsoleScripts)
	return metadata, nil
}

func parseSetupPy(data []byte) (*PythonProjectMetadata, error) {
	text := string(data)
	metadata := &PythonProjectMetadata{}
	for _, match := range setupStringPattern.FindAllStringSubmatch(text, -1) {
		switch match[1] {
		case "name":
			metadata.Name = match[2]
		case "version":
			metadata.Version = match[2]
		case "python_requires":
			metadata.RequiresPython = match[2]
		}
	}
	if metadata.Name == "" {
		return nil, fmt.Errorf("setup.py name must be a static string literal")
	}
	consoleIndex := strings.Index(text, "console_scripts")
	if consoleIndex >= 0 {
		section := text[consoleIndex:]
		if end := strings.Index(section, "]"); end >= 0 {
			section = section[:end]
		}
		for _, match := range setupConsoleScriptPattern.FindAllStringSubmatch(section, -1) {
			metadata.ConsoleScripts = append(metadata.ConsoleScripts, match[1])
		}
	}
	sort.Strings(metadata.ConsoleScripts)
	return metadata, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// ResolveConsoleCommand applies the deterministic Python console-script
// selection contract. An explicit command always wins.
func ResolveConsoleCommand(explicit []string, projectName string, scripts []string) ([]string, error) {
	if len(explicit) > 0 {
		return append([]string(nil), explicit...), nil
	}
	candidates := append([]string(nil), scripts...)
	sort.Strings(candidates)
	if len(candidates) == 1 {
		return []string{candidates[0]}, nil
	}
	normalizedProject := canonicalDistributionName(projectName)
	var matching []string
	for _, candidate := range candidates {
		if canonicalDistributionName(candidate) == normalizedProject {
			matching = append(matching, candidate)
		}
	}
	if len(matching) == 1 {
		return []string{matching[0]}, nil
	}
	if len(candidates) == 0 {
		//nolint:staticcheck // The public Error Contract requires this punctuation.
		return nil, fmt.Errorf("This package installs no commands. Set the server command explicitly.")
	}
	//nolint:staticcheck // The public Error Contract requires this punctuation.
	return nil, fmt.Errorf("This package provides commands: %s. Set the server command to one of them.", strings.Join(candidates, ", "))
}
