package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"gopkg.in/yaml.v3"
)

func TestImportOpenCode_CLI(t *testing.T) {
	bin := buildImportBinary(t)

	t.Run("help documents single-file policy", func(t *testing.T) {
		stdout, stderr, code := runImportBin(t, bin, t.TempDir(), t.TempDir(), "import", "--help")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s", code, stderr)
		}
		help := stdout + stderr
		for _, want := range []string{"--source-config", "opencode.jsonc", "config.json", "compatibility-preserving single-file choice", "XDG_CONFIG_HOME", "every OpenCode shape"} {
			if !strings.Contains(help, want) {
				t.Errorf("help missing %q", want)
			}
		}
	})

	t.Run("rejects source-config without a client", func(t *testing.T) {
		_, stderr, code := runImportBin(t, bin, t.TempDir(), t.TempDir(), "import", "--source-config", "x.json")
		if code != 1 || !strings.Contains(stderr, "requires a client argument") || !strings.Contains(stderr, "multi-client scan") {
			t.Fatalf("exit %d stderr=%s", code, stderr)
		}
	})

	t.Run("imports three native servers", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, _ := openCodeCLIPaths(home)
		writeImportFixture(t, jsonPath, nativeThreeJSON)
		before := hashFile(t, jsonPath)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d\nstdout=%s\nstderr=%s", code, stdout, stderr)
		}
		doc := parseImportJSON(t, stdout)
		if doc.Summary.Found != 3 || doc.Summary.Imported != 3 || doc.Summary.Skipped != 0 || doc.SchemaVersion != 1 {
			t.Fatalf("summary = %+v", doc.Summary)
		}
		if len(doc.Sources) != 1 || doc.Sources[0].Path != jsonPath || doc.Sources[0].Status != "selected" {
			t.Fatalf("sources = %+v", doc.Sources)
		}
		if !strings.Contains(stdout, `"source_path":`) || !strings.Contains(stdout, jsonPath) {
			t.Fatalf("JSON omitted candidate path provenance:\n%s", stdout)
		}
		if strings.Contains(stdout, `"source": "opencode.json"`) {
			t.Fatal("source slug was replaced with a filename")
		}
		if doc.BackupPath == "" {
			t.Fatal("missing backup path")
		}
		if _, err := os.Stat(doc.BackupPath); err != nil {
			t.Fatal(err)
		}
		if hashFile(t, jsonPath) != before {
			t.Fatal("client source was modified")
		}
		if strings.Contains(stdout, "synthetic-secret") || strings.Contains(stderr, "synthetic-secret") {
			t.Fatal("output resolved or disclosed a secret marker")
		}
		alpha := stackServer(t, stack, "alpha")
		want := []string{"node", "/synthetic/a b.js", "", "--label=a b", `C:\synthetic\x`}
		if !equalStrings(alpha.Command, want) {
			t.Fatalf("alpha command = %#v", alpha.Command)
		}
		if alpha.Transport != "stdio" || alpha.Env["REGION"] != "us-west" || alpha.Env["API_KEY"] != "${FIXTURE_TOKEN}" {
			t.Fatalf("alpha = %+v", alpha)
		}
		beta := stackServer(t, stack, "beta")
		if !equalStrings(beta.Command, []string{"uvx", "synthetic-server"}) {
			t.Fatalf("beta = %#v", beta.Command)
		}
		gamma := stackServer(t, stack, "gamma")
		if !equalStrings(gamma.Command, []string{"npx", "-y", "synthetic-server"}) {
			t.Fatalf("gamma = %#v", gamma.Command)
		}
	})

	t.Run("dry-run writes nothing", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, _ := openCodeCLIPaths(home)
		writeImportFixture(t, jsonPath, nativeThreeJSON)
		stack := writeStack(t, root)
		before := hashFile(t, stack)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseImportJSON(t, stdout)
		if !doc.DryRun || doc.Summary.Imported != 0 || doc.Summary.Found != 3 {
			t.Fatalf("summary = %+v dry=%v", doc.Summary, doc.DryRun)
		}
		if hashFile(t, stack) != before {
			t.Fatal("dry-run modified the stack")
		}
		if matches, _ := filepath.Glob(stack + ".gridctl-backup-*"); len(matches) != 0 {
			t.Fatalf("dry-run created backups: %v", matches)
		}
	})

	t.Run("remote https control", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, _ := openCodeCLIPaths(home)
		writeImportFixture(t, jsonPath, `{"mcp":{"docs":{"type":"remote","url":"https://docs.example.com/mcp"}}}`)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		docs := stackServer(t, stack, "docs")
		if docs.URL != "https://docs.example.com/mcp" || docs.Transport != "http" {
			t.Fatalf("docs = %+v", docs)
		}
	})

	t.Run("jsonc only and both-file precedence", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, jsoncPath := openCodeCLIPaths(home)
		if err := os.MkdirAll(filepath.Dir(jsoncPath), 0o755); err != nil {
			t.Fatal(err)
		}
		writeImportFixture(t, jsoncPath, "\xef\xbb\xbf{\n// comment\n\"mcp\":{\"docs\":{\"type\":\"remote\",\"url\":\"https://docs.example.com/mcp\"},},\n}")
		writeImportFixture(t, filepath.Join(filepath.Dir(jsonPath), "config.json"), `{"mcp":{"hidden":{"type":"remote","url":"https://hidden.example/mcp"}}}`)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if strings.Contains(string(mustRead(t, stack)), "hidden.example") {
			t.Fatal("config.json was imported")
		}
		doc := parseImportJSON(t, stdout)
		joined := strings.Join(doc.Sources[0].Notes, "\n") + stderr
		for _, want := range []string{"config.json", "compatibility-preserving single-file choice", "prefers opencode.jsonc for writes", jsoncPath} {
			if !strings.Contains(joined, want) {
				t.Errorf("jsonc-only diagnostics missing %q\n%s", want, joined)
			}
		}
		if stackServer(t, stack, "docs").URL == "" {
			t.Fatal("jsonc server was not imported")
		}

		home = t.TempDir()
		root = t.TempDir()
		jsonPath, jsoncPath = openCodeCLIPaths(home)
		writeImportFixture(t, jsonPath, `{"mcp":{"fromjson":{"type":"remote","url":"https://json.example/mcp"}}}`)
		writeImportFixture(t, jsoncPath, `{"mcp":{"fromjsonc":{"type":"remote","url":"https://jsonc.example/mcp"}}}`)
		stack = writeStack(t, root)
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		text := string(mustRead(t, stack))
		if strings.Contains(text, "jsonc.example") || !strings.Contains(text, "json.example") {
			t.Fatalf("precedence failed:\n%s", text)
		}
		doc = parseImportJSON(t, stdout)
		if doc.Sources[0].Path != jsonPath || doc.Sources[0].AlternatePath != jsoncPath {
			t.Fatalf("sources = %+v", doc.Sources)
		}
		joined = strings.Join(doc.Sources[0].Notes, "\n")
		if !strings.Contains(joined, jsonPath) || !strings.Contains(joined, jsoncPath) || !strings.Contains(joined, "was not merged") {
			t.Fatalf("notes = %v", doc.Sources[0].Notes)
		}
	})

	t.Run("malformed selected file does not fall back", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, jsoncPath := openCodeCLIPaths(home)
		writeImportFixture(t, jsonPath, "{not-json")
		writeImportFixture(t, jsoncPath, `{"mcp":{"fromjsonc":{"type":"remote","url":"https://jsonc.example/mcp"}}}`)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseImportJSON(t, stdout)
		if len(doc.Sources) != 1 || doc.Sources[0].Status != "malformed" || !strings.Contains(doc.Sources[0].Detail, "not valid JSON") {
			t.Fatalf("sources = %+v", doc.Sources)
		}
		if strings.Contains(string(mustRead(t, stack)), "jsonc.example") {
			t.Fatal("malformed JSON fell through to JSONC")
		}
		assertServersNull(t, stdout)
		if strings.Contains(stdout, "not-json") || strings.Contains(stderr, "not-json") {
			t.Fatal("parse diagnostic included file contents")
		}
	})

	t.Run("explicit source and ignored overrides", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, _ := openCodeCLIPaths(home)
		if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
			t.Fatal(err)
		}
		writeImportFixture(t, jsonPath, `{"mcp":{"global":{"type":"remote","url":"https://global.example/mcp"}}}`)
		xdg := filepath.Join(home, "xdg-config", "opencode", "opencode.json")
		writeImportFixture(t, xdg, `{"mcp":{"xdg":{"type":"remote","url":"https://xdg.example/mcp"}}}`)
		project := filepath.Join(root, "project.jsonc")
		writeImportFixture(t, project, "{\n// project\n\"mcp\":{\"proj\":{\"type\":\"remote\",\"url\":\"https://project.example/mcp\"},},\n}")
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		text := string(mustRead(t, stack))
		if strings.Contains(text, "xdg.example") || strings.Contains(text, "project.example") || !strings.Contains(text, "global.example") {
			t.Fatalf("automatic scan saw an override:\n%s", text)
		}

		stack = writeStack(t, root)
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--source-config", "project.jsonc", "--format", "json")
		if code != 0 {
			t.Fatalf("explicit exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if stackServer(t, stack, "proj").URL != "https://project.example/mcp" {
			t.Fatalf("explicit project import failed:\n%s", mustRead(t, stack))
		}
		if strings.Contains(string(mustRead(t, stack)), "global.example") {
			t.Fatal("explicit import fell back to the global file")
		}

		missing := filepath.Join(root, "missing.jsonc")
		stack = writeStack(t, root)
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--source-config", missing, "--format", "json")
		if code != 0 {
			t.Fatalf("missing explicit exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseImportJSON(t, stdout)
		if doc.Sources[0].Status != "missing" || !strings.Contains(doc.Sources[0].Detail, "does not exist") {
			t.Fatalf("sources = %+v", doc.Sources)
		}
		if strings.Contains(string(mustRead(t, stack)), "global.example") {
			t.Fatal("missing explicit path fell back")
		}
		assertServersNull(t, stdout)
	})

	t.Run("xdg file works only with source-config", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, _ := openCodeCLIPaths(home)
		if err := os.MkdirAll(filepath.Dir(jsonPath), 0o755); err != nil {
			t.Fatal(err)
		}
		xdg := filepath.Join(home, "xdg-config", "opencode", "opencode.jsonc")
		writeImportFixture(t, xdg, `{"mcp":{"xdg":{"type":"remote","url":"https://xdg.example/mcp"}}}`)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if strings.Contains(string(mustRead(t, stack)), "xdg.example") {
			t.Fatal("XDG file was scanned automatically")
		}
		stack = writeStack(t, root)
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--source-config", xdg, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if stackServer(t, stack, "xdg").URL != "https://xdg.example/mcp" {
			t.Fatal("explicit XDG path was not imported")
		}
	})

	t.Run("skip reasons in text and json", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, _ := openCodeCLIPaths(home)
		secretFile := filepath.Join(root, "synthetic.txt")
		writeImportFixture(t, secretFile, "SYNTHETIC_FILE_CONTENTS_MUST_NOT_APPEAR")
		body := `{
  "mcp": {
    "off": {"type":"local","command":["node","a.js"],"enabled":false},
    "workdir": {"type":"local","command":["node","a.js"],"cwd":"./synthetic-workspace"},
    "file": {"type":"local","command":["node","a.js"],"environment":{"TOKEN":"{file:./synthetic.txt}"}},
    "badname": {"type":"local","command":["node","a.js"],"environment":{"TOKEN":"{env:FOO-BAR}"}},
    "embedded": {"type":"local","command":["node","prefix-{env:FIXTURE_TOKEN}.js"]},
    "kept": {"type":"local","command":["node","ok.js"],"timeout":7000}
  }
}`
		writeImportFixture(t, jsonPath, body)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack)
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		text := stdout + stderr
		for _, want := range []string{"enabled is false", "working directory is set", "native {file} expressions are not imported", "not a shell identifier", "embedded or unsupported native {env} expression", "timeout was not transferred"} {
			if !strings.Contains(text, want) {
				t.Errorf("text missing %q\n%s", want, text)
			}
		}
		for _, absent := range []string{"synthetic-workspace", "synthetic.txt", "FOO-BAR", "FIXTURE_TOKEN", "SYNTHETIC_FILE_CONTENTS_MUST_NOT_APPEAR", "7000"} {
			if strings.Contains(text, absent) {
				t.Errorf("text disclosed %q", absent)
			}
		}
		if !strings.Contains(string(mustRead(t, stack)), "ok.js") {
			t.Fatal("importable server was skipped")
		}

		stack = writeStack(t, root)
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("json exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if !json.Valid([]byte(stdout)) || strings.Contains(stdout, "OpenCode source") {
			t.Fatalf("stdout is not a single JSON document:\n%s", stdout)
		}
		doc := parseImportJSON(t, stdout)
		if doc.Summary.Imported != 1 || doc.Summary.Skipped != 5 || doc.Summary.Found != 6 {
			t.Fatalf("summary = %+v", doc.Summary)
		}
		raw := stdout + stderr
		for _, want := range []string{`"skip_reason": "disabled"`, `"skip_reason": "untransferred_option"`, `"skip_reason": "unsupported_native_expression"`, jsonPath} {
			if !strings.Contains(raw, want) {
				t.Errorf("json missing %q", want)
			}
		}
	})

	t.Run("all skipped still lists reasons", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, _ := openCodeCLIPaths(home)
		writeImportFixture(t, jsonPath, `{"mcp":{"off":{"type":"local","command":["node","a.js"],"enabled":false},"bad":{"type":"local","command":["node","a.js"],"environment":{"TOKEN":"{file:./synthetic.txt}"}}}}`)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseImportJSON(t, stdout)
		if doc.Summary.Found != 2 || doc.Summary.Skipped != 2 || doc.Summary.Imported != 0 {
			t.Fatalf("summary = %+v", doc.Summary)
		}
		text := stderr
		if !strings.Contains(text, "enabled is false") || !strings.Contains(text, "native {file} expressions are not imported") {
			t.Fatalf("stderr missing skip reasons:\n%s", text)
		}
		if strings.Contains(stdout+stderr, "synthetic.txt") {
			t.Fatal("skip reason disclosed a path")
		}
	})

	t.Run("empty enumeration preserves null servers", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		jsonPath, _ := openCodeCLIPaths(home)
		writeImportFixture(t, jsonPath, `{"mcp":{}}`)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseImportJSON(t, stdout)
		if doc.SchemaVersion != 1 || doc.Summary.Imported != 0 || doc.Sources[0].Status != "empty" {
			t.Fatalf("doc = %+v", doc)
		}
		assertServersNull(t, stdout)
	})
}

const nativeThreeJSON = `{
  "mcp": {
    "alpha": {
      "type": "local",
      "command": ["node", "/synthetic/a b.js", "", "--label=a b", "C:\\synthetic\\x"],
      "environment": {"REGION": "us-west", "API_KEY": "{env:FIXTURE_TOKEN}"}
    },
    "beta": {"type": "local", "command": ["uvx", "synthetic-server"]},
    "gamma": {"type": "local", "command": ["npx", "-y", "synthetic-server"]}
  }
}`

func buildImportBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gridctl")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func runImportBin(t *testing.T, bin, home, cwd string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = importChildEnv(home)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	stdout, stderr = outBuf.String(), errBuf.String()
	checkImportGolden(t, home, cwd, args, stdout, stderr, code)
	return stdout, stderr, code
}

func importChildEnv(home string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GRIDCTL_HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "xdg-config"),
		"XDG_DATA_HOME=" + filepath.Join(home, "xdg-data"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "xdg-cache"),
		"TMPDIR=" + filepath.Join(home, "tmp"),
		"NO_COLOR=1",
		"LANG=C.UTF-8",
	}
	if runtime.GOOS == "windows" {
		env = append(env,
			"APPDATA="+filepath.Join(home, "AppData", "Roaming"),
			"USERPROFILE="+home,
		)
	}
	return env
}

func openCodeCLIPaths(home string) (jsonPath, jsoncPath string) {
	if runtime.GOOS == "windows" {
		jsonPath = filepath.Join(home, "AppData", "Roaming", "opencode", "opencode.json")
	} else {
		jsonPath = filepath.Join(home, ".config", "opencode", "opencode.json")
	}
	return jsonPath, strings.TrimSuffix(jsonPath, ".json") + ".jsonc"
}

func writeImportFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeStack(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte("name: import-fixture\nmcp-servers: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func hashFile(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func stackServer(t *testing.T, path, name string) config.MCPServer {
	t.Helper()
	var stack config.Stack
	if err := yaml.Unmarshal(mustRead(t, path), &stack); err != nil {
		t.Fatal(err)
	}
	for _, server := range stack.MCPServers {
		if server.Name == name {
			return server
		}
	}
	t.Fatalf("server %s missing in %s", name, path)
	return config.MCPServer{}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type importCLIDoc struct {
	SchemaVersion int    `json:"schema_version"`
	BackupPath    string `json:"backup_path"`
	DryRun        bool   `json:"dry_run"`
	Sources       []struct {
		Path          string   `json:"path"`
		Status        string   `json:"status"`
		Detail        string   `json:"detail"`
		AlternatePath string   `json:"alternate_path"`
		Notes         []string `json:"notes"`
	} `json:"sources"`
	Summary struct {
		Found    int `json:"found"`
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
	} `json:"summary"`
}

func parseImportJSON(t *testing.T, stdout string) importCLIDoc {
	t.Helper()
	var doc importCLIDoc
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return doc
}

func assertServersNull(t *testing.T, stdout string) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["servers"]) != "null" {
		t.Fatalf("servers = %s", raw["servers"])
	}
	if string(raw["schema_version"]) != "1" {
		t.Fatalf("schema_version = %s", raw["schema_version"])
	}
}
