package builder

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestGeneratePythonDockerfile_PyPI(t *testing.T) {
	dockerfile, err := GeneratePythonDockerfile(context.Background(), PythonBuildSpec{
		Python: "3.12", Package: "mcp-demo", Version: "1.2.3",
		Extras: []string{"SSE", "sse"}, With: []string{"httpx>=0.27"},
		Packages: []string{"libpq5", "curl", "curl"}, Command: []string{"mcp-demo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"python:3.12.11-slim-bookworm@sha256:",
		"ghcr.io/astral-sh/uv:0.8.14@sha256:",
		"apt-get install -y --no-install-recommends curl libpq5",
		"uv tool install 'mcp-demo[sse]==1.2.3' --with 'httpx>=0.27'",
		"USER gridctl",
		`CMD ["mcp-demo"]`,
	}
	for _, check := range checks {
		if !strings.Contains(dockerfile, check) {
			t.Errorf("Dockerfile missing %q:\n%s", check, dockerfile)
		}
	}
}

func TestGeneratePythonDockerfile_LocalLockModes(t *testing.T) {
	locked, err := GeneratePythonDockerfile(context.Background(), PythonBuildSpec{Python: "3.11", Local: true, Locked: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(locked, "uv sync --locked --no-dev --no-editable") {
		t.Fatal("locked project does not use uv sync")
	}
	if !strings.Contains(locked, "chown -R gridctl:gridctl /app /opt/gridctl") {
		t.Fatal("locked project does not give the runtime user ownership of /app")
	}
	unlocked, err := GeneratePythonDockerfile(context.Background(), PythonBuildSpec{Python: "3.11", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unlocked, "uv tool install /app") {
		t.Fatal("unlocked project does not use documented package install")
	}
}

func TestGeneratePythonDockerfile_LocalPackageOptions(t *testing.T) {
	unlocked, err := GeneratePythonDockerfile(context.Background(), PythonBuildSpec{Python: "3.11", Local: true, Extras: []string{"sse"}, With: []string{"httpx>=0.27"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unlocked, "uv tool install '/app[sse]' --with 'httpx>=0.27'") {
		t.Fatalf("unlocked project options missing:\n%s", unlocked)
	}
	locked, err := GeneratePythonDockerfile(context.Background(), PythonBuildSpec{Python: "3.11", Local: true, Locked: true, Extras: []string{"sse"}, With: []string{"httpx>=0.27"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(locked, "uv sync --locked --no-dev --no-editable --extra sse") || !strings.Contains(locked, "uv pip install --python /app/.venv/bin/python 'httpx>=0.27'") {
		t.Fatalf("locked project options missing:\n%s", locked)
	}
}

func TestGeneratePythonDockerfile_RejectsInjection(t *testing.T) {
	_, err := GeneratePythonDockerfile(context.Background(), PythonBuildSpec{Python: "3.12", Package: "demo", Version: "1.0", Packages: []string{"curl;id"}})
	if err == nil {
		t.Fatal("unsafe Debian package accepted")
	}
	_, err = GeneratePythonDockerfile(context.Background(), PythonBuildSpec{Python: "3.12", Package: "demo", Version: "1.0", With: []string{"safe; touch /tmp/x"}})
	if err == nil {
		t.Fatal("unsafe dependency accepted")
	}
}

func TestDefaultPythonTemplateConfig_PinsImages(t *testing.T) {
	config := DefaultPythonTemplate()
	digest := regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
	if config.Version != PythonTemplateVersion || !digest.MatchString(config.UVImage) {
		t.Fatalf("invalid template config: %+v", config)
	}
	for _, version := range []string{"3.10", "3.11", "3.12", "3.13"} {
		if !digest.MatchString(config.PythonImages[version]) {
			t.Errorf("Python %s image is not digest-pinned", version)
		}
	}
	config.PythonImages["3.12"] = "mutated"
	if DefaultPythonTemplate().PythonImages["3.12"] == "mutated" {
		t.Fatal("callers can mutate the default template")
	}
}
