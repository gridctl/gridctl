package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
)

func TestImportProject_CLI(t *testing.T) {
	bin := buildImportBinary(t)

	t.Run("help documents project discovery", func(t *testing.T) {
		stdout, stderr, code := runImportBin(t, bin, t.TempDir(), t.TempDir(), "import", "--help")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s", code, stderr)
		}
		help := stdout + stderr
		for _, want := range []string{"--project-dir", "--scope", "--source-config", "gridctl's choice", ".mcp.json", "opencode.jsonc"} {
			if !strings.Contains(help, want) {
				t.Errorf("help missing %q", want)
			}
		}
		if strings.Contains(help, "only valid for 'gridctl import opencode'") || strings.Contains(help, "project files are not") {
			t.Fatalf("help still excludes project files:\n%s", help)
		}
	})

	t.Run("project files from repo and subdirectory", func(t *testing.T) {
		home := t.TempDir()
		parent := t.TempDir()
		repo := filepath.Join(parent, "repo")
		initImportRepo(t, repo)
		writeProjectFixture(t, repo)
		writeImportFixture(t, filepath.Join(parent, ".mcp.json"), `{"mcpServers":{"above":{"command":"above-bin"}}}`)
		stack := writeStack(t, repo)
		hashes := hashClientFiles(t, projectFixturePaths(repo))

		stdout, stderr, code := runImportBin(t, bin, home, repo, "import", "--all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d\nstdout=%s\nstderr=%s", code, stdout, stderr)
		}
		if !json.Valid([]byte(stdout)) {
			t.Fatalf("stdout is not one JSON document:\n%s", stdout)
		}
		doc := parseProjectImport(t, stdout)
		if doc.SchemaVersion != 1 {
			t.Fatalf("schema = %d", doc.SchemaVersion)
		}
		assertProjectFixture(t, doc, repo)
		if serverNamed(doc, "above") != nil {
			t.Fatal("file above the git root was imported")
		}
		for _, path := range projectFixturePaths(repo) {
			if hashFile(t, path) != hashes[path] {
				t.Fatalf("dry-run changed %s", path)
			}
		}

		sub := filepath.Join(repo, "sub")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code = runImportBin(t, bin, home, sub, "import", "--all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("subdir exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		subDoc := parseProjectImport(t, stdout)
		assertProjectFixture(t, subDoc, repo)

		sibling := filepath.Join(parent, "sibling")
		if err := os.MkdirAll(sibling, 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code = runImportBin(t, bin, home, sibling, "import", "--all", "--yes", "--no-vault", "--dry-run", "--format", "json", "--file", writeStack(t, sibling))
		if code != 0 {
			t.Fatalf("sibling exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if strings.Contains(stdout, "shared") || strings.Contains(stdout, `"scope":"project"`) || strings.Contains(stdout+stderr, "above-bin") {
			t.Fatalf("sibling saw project files:\nstdout=%s\nstderr=%s", stdout, stderr)
		}
	})

	t.Run("cursor precedence and identical merge", func(t *testing.T) {
		home := t.TempDir()
		repo := filepath.Join(t.TempDir(), "repo")
		initImportRepo(t, repo)
		writeImportFixture(t, filepath.Join(repo, ".cursor", "mcp.json"), `{"mcpServers":{"github":{"command":"project-bin"},"same":{"command":"same-bin"}}}`)
		writeImportFixture(t, filepath.Join(home, ".cursor", "mcp.json"), `{"mcpServers":{"github":{"command":"user-bin"},"same":{"command":"same-bin"}}}`)
		stack := writeStack(t, repo)
		stdout, stderr, code := runImportBin(t, bin, home, repo, "import", "--all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseProjectImport(t, stdout)
		github := serverNamed(doc, "github")
		if github == nil || github.SkipReason != "" || len(github.Origins) != 1 || github.Origins[0].Scope != "project" {
			t.Fatalf("project winner = %+v", github)
		}
		skipped := skippedNamed(doc, "github")
		if skipped == nil || skipped.SkipReason != "name_collision" {
			t.Fatalf("user collision = %+v", skipped)
		}
		warn := strings.Join(skipped.Warnings, "\n")
		if !strings.Contains(warn, "cursor (user)") || !strings.Contains(warn, "cursor (project)") {
			t.Fatalf("warning = %s", warn)
		}
		same := serverNamed(doc, "same")
		if same == nil || len(same.Origins) != 2 || len(same.Scopes) != 2 || same.Scopes[0] != "project" || same.Scopes[1] != "user" {
			t.Fatalf("merged = %+v", same)
		}
		if len(same.FoundIn) != 1 || same.FoundIn[0] != "cursor" || len(same.SourcePaths) != 2 {
			t.Fatalf("merged provenance = %+v", same)
		}
	})

	t.Run("claude local beats project", func(t *testing.T) {
		home := t.TempDir()
		repo := filepath.Join(t.TempDir(), "repo")
		initImportRepo(t, repo)
		abs := absImportPath(t, repo)
		other := filepath.Join(filepath.Dir(repo), "other")
		raw, err := json.Marshal(map[string]any{
			"projects": map[string]any{
				abs:   map[string]any{"mcpServers": map[string]any{"x": map[string]any{"command": "local-bin"}}},
				other: map[string]any{"mcpServers": map[string]any{"nope": map[string]any{"command": "other-bin"}}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		writeImportFixture(t, filepath.Join(home, ".claude.json"), string(raw))
		writeImportFixture(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers":{"x":{"command":"project-bin"}}}`)
		stack := writeStack(t, repo)
		stdout, stderr, code := runImportBin(t, bin, home, repo, "import", "--all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseProjectImport(t, stdout)
		if !hasScope(doc, "local") {
			t.Fatalf("sources = %+v", doc.Sources)
		}
		winner := serverNamed(doc, "x")
		if winner == nil || winner.SkipReason != "" || len(winner.Scopes) != 1 || winner.Scopes[0] != "local" {
			t.Fatalf("local winner = %+v", winner)
		}
		skipped := skippedNamed(doc, "x")
		if skipped == nil || skipped.SkipReason != "name_collision" {
			t.Fatalf("project collision = %+v", skipped)
		}
		if serverNamed(doc, "nope") != nil || skippedNamed(doc, "nope") != nil {
			t.Fatal("unrelated project key was imported")
		}
	})

	t.Run("opencode custom scope", func(t *testing.T) {
		home := t.TempDir()
		repo := filepath.Join(t.TempDir(), "repo")
		initImportRepo(t, repo)
		writeImportFixture(t, filepath.Join(repo, "opencode.jsonc"), `{"mcp":{"shared":{"type":"remote","url":"https://project.example/mcp"},"projonly":{"type":"remote","url":"https://proj.example/mcp"}}}`)
		custom := filepath.Join(home, "custom.json")
		writeImportFixture(t, custom, `{"mcp":{"shared":{"type":"remote","url":"https://custom.example/mcp"},"customonly":{"type":"remote","url":"https://custom.example/only"}}}`)
		dirFile := filepath.Join(home, "dir.json")
		writeImportFixture(t, dirFile, `{"mcp":{"fromdir":{"type":"remote","url":"https://dir.example/mcp"}}}`)
		contentFile := filepath.Join(home, "content.json")
		writeImportFixture(t, contentFile, `{"mcp":{"fromcontent":{"type":"remote","url":"https://content.example/mcp"}}}`)
		extra := []string{"OPENCODE_CONFIG=" + custom, "OPENCODE_CONFIG_DIR=" + filepath.Dir(dirFile), "OPENCODE_CONFIG_CONTENT=" + string(mustRead(t, contentFile))}
		stack := writeStack(t, repo)

		stdout, stderr, code := runImportBinEnv(t, bin, home, repo, extra, "import", "opencode", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("all exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseProjectImport(t, stdout)
		if !hasScope(doc, "custom") {
			t.Fatalf("sources = %+v", doc.Sources)
		}
		if strings.Contains(string(mustRead(t, stack)), "custom.example/mcp") || strings.Contains(string(mustRead(t, stack)), "dir.example") || strings.Contains(string(mustRead(t, stack)), "content.example") {
			t.Fatalf("custom or ignored override won:\n%s", mustRead(t, stack))
		}
		if !strings.Contains(string(mustRead(t, stack)), "project.example/mcp") || !strings.Contains(string(mustRead(t, stack)), "custom.example/only") {
			t.Fatalf("expected project winner and custom-only server:\n%s", mustRead(t, stack))
		}

		writeImportFixture(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"mcp":{"homesrv":{"type":"remote","url":"https://home.example/mcp"}}}`)
		stack = writeStack(t, repo)
		stdout, stderr, code = runImportBinEnv(t, bin, home, repo, extra, "import", "opencode", "--scope", "user", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("user exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		userDoc := parseProjectImport(t, stdout)
		if hasScope(userDoc, "custom") || hasScope(userDoc, "project") || hasScope(userDoc, "local") {
			t.Fatalf("user sources = %+v", userDoc.Sources)
		}
		if strings.Contains(string(mustRead(t, stack)), "custom.example") || strings.Contains(string(mustRead(t, stack)), "project.example") || !strings.Contains(string(mustRead(t, stack)), "home.example") {
			t.Fatalf("scope user candidate set changed:\n%s", mustRead(t, stack))
		}

		stack = writeStack(t, repo)
		stdout, stderr, code = runImportBinEnv(t, bin, home, repo, extra, "import", "opencode", "--scope", "project", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("project exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		projDoc := parseProjectImport(t, stdout)
		if hasScope(projDoc, "custom") || hasScope(projDoc, "user") {
			t.Fatalf("project sources = %+v", projDoc.Sources)
		}
		if strings.Contains(string(mustRead(t, stack)), "custom.example") {
			t.Fatal("scope project read OPENCODE_CONFIG")
		}
	})

	t.Run("scope user leaves placeholders and scope project ignores home", func(t *testing.T) {
		home := t.TempDir()
		repo := filepath.Join(t.TempDir(), "repo")
		initImportRepo(t, repo)
		writeImportFixture(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"homebin":{"command":"${CLAUDE_PROJECT_DIR:-.}/bin/server --flag"}}}`)
		writeImportFixture(t, filepath.Join(home, ".cursor", "mcp.json"), "{not-json home-secret-marker")
		writeImportFixture(t, filepath.Join(repo, ".cursor", "mcp.json"), `{"mcpServers":{"projcur":{"command":"proj-bin"}}}`)
		stack := writeStack(t, repo)

		stdout, stderr, code := runImportBin(t, bin, home, repo, "import", "--scope", "user", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("user exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		userDoc := parseProjectImport(t, stdout)
		if hasScope(userDoc, "project") || hasScope(userDoc, "local") || hasScope(userDoc, "custom") {
			t.Fatalf("user sources = %+v", userDoc.Sources)
		}
		text := string(mustRead(t, stack))
		if strings.Contains(text, "proj-bin") || strings.Contains(text, absImportPath(t, repo)+"/bin/server") {
			t.Fatalf("scope user substituted or read project:\n%s", text)
		}
		if !strings.Contains(text, "${CLAUDE_PROJECT_DIR:-.}/bin/server") {
			t.Fatalf("placeholder was rewritten:\n%s", text)
		}

		writeImportFixture(t, filepath.Join(home, ".claude.json"), "{not-json claude-secret-marker")
		writeImportFixture(t, filepath.Join(home, ".cursor", "mcp.json"), "{not-json home-secret-marker")
		stack = writeStack(t, repo)
		stdout, stderr, code = runImportBin(t, bin, home, repo, "import", "--scope", "project", "--all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("project exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		projDoc := parseProjectImport(t, stdout)
		if hasScope(projDoc, "user") || hasScope(projDoc, "local") || strings.Contains(stdout+stderr, "home-secret-marker") || strings.Contains(stdout+stderr, "not valid JSON") {
			t.Fatalf("project scan opened a home file:\nstdout=%s\nstderr=%s", stdout, stderr)
		}
		if serverNamed(projDoc, "projcur") == nil {
			t.Fatal("project server missing")
		}
	})

	t.Run("project-dir and invalid flags", func(t *testing.T) {
		home := t.TempDir()
		repo := filepath.Join(t.TempDir(), "repo")
		initImportRepo(t, repo)
		writeImportFixture(t, filepath.Join(repo, ".cursor", "mcp.json"), `{"mcpServers":{"elsewhere":{"command":"else-bin"}}}`)
		other := t.TempDir()
		stack := writeStack(t, other)
		stdout, stderr, code := runImportBin(t, bin, home, other, "import", "--all", "--yes", "--no-vault", "--dry-run", "--project-dir", repo, "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if serverNamed(parseProjectImport(t, stdout), "elsewhere") == nil {
			t.Fatalf("project-dir missed the repo:\n%s", stdout)
		}
		_, stderr, code = runImportBin(t, bin, home, other, "import", "--project-dir", filepath.Join(other, "missing"), "--dry-run")
		if code != 2 || !strings.Contains(stderr, "missing") || !strings.Contains(stderr, "not a directory") {
			t.Fatalf("bad dir exit %d stderr=%s", code, stderr)
		}
		_, stderr, code = runImportBin(t, bin, home, other, "import", "--scope", "nope", "--dry-run")
		if code != 2 || !strings.Contains(stderr, "all, user, project") {
			t.Fatalf("bad scope exit %d stderr=%s", code, stderr)
		}
	})

	t.Run("source-config reads one client file", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeImportFixture(t, filepath.Join(home, ".cursor", "mcp.json"), `{"mcpServers":{"homeonly":{"command":"home-bin"}}}`)
		writeImportFixture(t, filepath.Join(root, ".cursor", "mcp.json"), `{"mcpServers":{"projonly":{"command":"proj-bin"}}}`)
		explicit := filepath.Join(root, "x.json")
		writeImportFixture(t, explicit, `{"mcpServers":{"explicit":{"command":"explicit-bin"}}}`)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "cursor", "--source-config", "./x.json", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		text := string(mustRead(t, stack))
		if !strings.Contains(text, "explicit-bin") || strings.Contains(text, "home-bin") || strings.Contains(text, "proj-bin") {
			t.Fatalf("explicit scan was not exclusive:\n%s", text)
		}
		doc := parseProjectImport(t, stdout)
		if len(doc.Sources) != 1 || !strings.HasSuffix(doc.Sources[0].Path, "x.json") {
			t.Fatalf("sources = %+v", doc.Sources)
		}
	})

	t.Run("undetected client with a project file", func(t *testing.T) {
		home := t.TempDir()
		repo := filepath.Join(t.TempDir(), "repo")
		initImportRepo(t, repo)
		writeImportFixture(t, filepath.Join(repo, ".cursor", "mcp.json"), `{"mcpServers":{"onlyproj":{"command":"only-bin"}}}`)
		stack := writeStack(t, repo)
		stdout, stderr, code := runImportBin(t, bin, home, repo, "import", "cursor", "--all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if serverNamed(parseProjectImport(t, stdout), "onlyproj") == nil {
			t.Fatal("project file was ignored for an undetected client")
		}
		_, stderr, code = runImportBin(t, bin, home, t.TempDir(), "import", "cursor", "--all", "--yes", "--no-vault", "--dry-run", "--file", writeStack(t, t.TempDir()))
		if code != 1 || !strings.Contains(stderr, "client not detected") {
			t.Fatalf("missing client exit %d stderr=%s", code, stderr)
		}
	})

	t.Run("placeholders cwd and malformed sibling", func(t *testing.T) {
		home := t.TempDir()
		parent := t.TempDir()
		repo := filepath.Join(parent, "My Project")
		initImportRepo(t, repo)
		abs := absImportPath(t, repo)
		writeImportFixture(t, filepath.Join(repo, ".cursor", "mcp.json"), `{
			"mcpServers": {
				"tools": {"command": "python", "args": ["${workspaceFolder}/tools/server.py"], "env": {"CONFIG_PATH": "${workspaceFolder}/cfg"}},
				"input": {"command": "node", "args": ["${input:synthetic-input-secret}"]},
				"hdr": {"url": "https://example.test/mcp", "headers": {"X-Token": "${input:synthetic-header-secret}"}}
			}
		}`)
		writeImportFixture(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers":{"spaced":{"command":"${CLAUDE_PROJECT_DIR:-.}/bin/server --flag"}}}`)
		writeImportFixture(t, filepath.Join(repo, "opencode.jsonc"), `{"mcp":{"native":{"type":"local","command":["node","${workspaceFolder}/s.js"]},"occwd":{"type":"local","command":["node","a.js"],"cwd":"./synthetic-workspace"}}}`)
		writeImportFixture(t, filepath.Join(repo, ".gemini", "settings.json"), `{"mcpServers":{"projcwd":{"command":"echo","cwd":"./x"}}}`)
		writeImportFixture(t, filepath.Join(repo, ".roo", "mcp.json"), "{not-json roo-secret-marker")
		writeImportFixture(t, filepath.Join(repo, ".vscode", "mcp.json"), `{"servers":{"vsc":{"command":"code-bin"}}}`)
		writeImportFixture(t, filepath.Join(home, ".gemini", "settings.json"), `{"mcpServers":{"usercwd":{"command":"echo","cwd":"./x"}}}`)
		writeImportFixture(t, filepath.Join(home, ".config", "opencode", "opencode.json"), `{"mcp":{"usercwdoc":{"type":"local","command":["node","a.js"],"cwd":"./synthetic-workspace"}}}`)
		writeImportFixture(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"homeph":{"command":"${CLAUDE_PROJECT_DIR:-.}/bin/server --flag"}}}`)
		paths := []string{
			filepath.Join(repo, ".cursor", "mcp.json"),
			filepath.Join(repo, ".mcp.json"),
			filepath.Join(repo, "opencode.jsonc"),
			filepath.Join(repo, ".gemini", "settings.json"),
			filepath.Join(repo, ".roo", "mcp.json"),
			filepath.Join(repo, ".vscode", "mcp.json"),
			filepath.Join(home, ".gemini", "settings.json"),
			filepath.Join(home, ".claude.json"),
		}
		before := hashClientFiles(t, paths)
		stack := writeStack(t, repo)
		stdout, stderr, code := runImportBin(t, bin, home, repo, "import", "--all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if strings.Contains(stdout+stderr, "synthetic-input-secret") || strings.Contains(stdout+stderr, "synthetic-header-secret") || strings.Contains(stdout+stderr, "roo-secret-marker") || strings.Contains(stdout+stderr, "synthetic-workspace") || strings.Contains(stdout+stderr, `"./x"`) {
			t.Fatal("output disclosed a config value")
		}
		doc := parseProjectImport(t, stdout)
		roo := sourceBySuffix(doc, filepath.Join(".roo", "mcp.json"))
		if roo == nil || roo.Status != "malformed" {
			t.Fatalf("roo source = %+v", roo)
		}
		if serverNamed(doc, "vsc") == nil || serverNamed(doc, "tools") == nil {
			t.Fatal("valid sibling was dropped after a malformed file")
		}
		input := skippedNamed(doc, "input")
		if input == nil || input.SkipReason != "unsupported_native_expression" || !strings.Contains(strings.Join(input.Warnings, " "), "${input:...}") {
			t.Fatalf("input skip = %+v", input)
		}
		projcwd := skippedNamed(doc, "projcwd")
		if projcwd == nil || projcwd.SkipReason != "untransferred_option" || !strings.Contains(strings.Join(projcwd.Warnings, " "), "working directory cannot be transferred") {
			t.Fatalf("project cwd = %+v", projcwd)
		}
		occwd := skippedNamed(doc, "occwd")
		if occwd == nil || occwd.SkipReason != "untransferred_option" {
			t.Fatalf("opencode project cwd = %+v", occwd)
		}
		userOC := skippedNamed(doc, "usercwdoc")
		if userOC == nil || userOC.SkipReason != "untransferred_option" {
			t.Fatalf("opencode user cwd = %+v", userOC)
		}
		tools := stackServer(t, stack, "tools")
		if len(tools.Command) != 2 || tools.Command[1] != abs+"/tools/server.py" || tools.Env["CONFIG_PATH"] != abs+"/cfg" {
			t.Fatalf("tools = %#v env %#v", tools.Command, tools.Env)
		}
		if !strings.Contains(strings.Join(serverNamed(doc, "tools").Warnings, "\n"), "resolved ${workspaceFolder} to "+abs) {
			t.Fatalf("tools warnings = %v", serverNamed(doc, "tools").Warnings)
		}
		spaced := stackServer(t, stack, "spaced")
		if len(spaced.Command) != 2 || spaced.Command[0] != abs+"/bin/server" || spaced.Command[1] != "--flag" {
			t.Fatalf("spaced = %#v", spaced.Command)
		}
		native := stackServer(t, stack, "native")
		if len(native.Command) != 2 || native.Command[0] != "node" || native.Command[1] != abs+"/s.js" {
			t.Fatalf("native = %#v", native.Command)
		}
		userCwd := stackServer(t, stack, "usercwd")
		if len(userCwd.Command) != 1 || userCwd.Command[0] != "echo" {
			t.Fatalf("user cwd = %#v", userCwd.Command)
		}
		if !strings.Contains(strings.Join(serverNamed(doc, "usercwd").Warnings, "\n"), "cwd was not transferred") {
			t.Fatalf("user cwd warnings = %v", serverNamed(doc, "usercwd").Warnings)
		}
		homeph := stackServer(t, stack, "homeph")
		if len(homeph.Command) != 2 || homeph.Command[0] != abs+"/bin/server" {
			t.Fatalf("home placeholder = %#v", homeph.Command)
		}
		for path, sum := range before {
			if hashFile(t, path) != sum {
				t.Fatalf("client file changed: %s", path)
			}
			matches, _ := filepath.Glob(path + ".gridctl-backup-*")
			side, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".gridctl-backup-*"))
			if len(matches)+len(side) != 0 {
				t.Fatalf("backup next to client file %s: %v %v", path, matches, side)
			}
		}

		textStack := writeStack(t, repo)
		textOut, textErr, code := runImportBin(t, bin, home, repo, "import", "--all", "--yes", "--no-vault", "--dry-run", "--file", textStack)
		if code != 0 {
			t.Fatalf("text exit %d stderr=%s", code, textErr)
		}
		srcAt := strings.Index(textOut, "Sources:")
		planAt := strings.Index(textOut, "tools")
		if srcAt < 0 || planAt < 0 || srcAt > planAt {
			t.Fatalf("Sources block was not before candidates:\n%s", textOut)
		}
		if !strings.Contains(textOut, "cursor (project)") {
			t.Fatalf("text label missing scope:\n%s", textOut)
		}
	})
}

func initImportRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gogit.PlainInit(dir, false); err != nil {
		t.Fatal(err)
	}
}

func writeProjectFixture(t *testing.T, repo string) {
	t.Helper()
	writeImportFixture(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers":{"shared":{"command":"shared-bin"}}}`)
	writeImportFixture(t, filepath.Join(repo, ".cursor", "mcp.json"), `{"mcpServers":{"cur":{"command":"cursor-bin"}}}`)
	writeImportFixture(t, filepath.Join(repo, ".vscode", "mcp.json"), `{"servers":{"vsc":{"command":"code-bin"}}}`)
	writeImportFixture(t, filepath.Join(repo, ".roo", "mcp.json"), `{"mcpServers":{"roosrv":{"command":"roo-bin"}}}`)
	writeImportFixture(t, filepath.Join(repo, ".gemini", "settings.json"), `{"mcpServers":{"gem":{"command":"gem-bin"}}}`)
	writeImportFixture(t, filepath.Join(repo, ".zed", "settings.json"), `{"context_servers":{"zedsrv":{"command":"zed-bin"}}}`)
	writeImportFixture(t, filepath.Join(repo, "opencode.jsonc"), `{"mcp":{"oc":{"type":"remote","url":"https://oc.example/mcp"}}}`)
}

func projectFixturePaths(repo string) []string {
	return []string{
		filepath.Join(repo, ".mcp.json"),
		filepath.Join(repo, ".cursor", "mcp.json"),
		filepath.Join(repo, ".vscode", "mcp.json"),
		filepath.Join(repo, ".roo", "mcp.json"),
		filepath.Join(repo, ".gemini", "settings.json"),
		filepath.Join(repo, ".zed", "settings.json"),
		filepath.Join(repo, "opencode.jsonc"),
	}
}

func hashClientFiles(t *testing.T, paths []string) map[string][32]byte {
	t.Helper()
	out := make(map[string][32]byte, len(paths))
	for _, path := range paths {
		out[path] = hashFile(t, path)
	}
	return out
}

func absImportPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func runImportBinEnv(t *testing.T, bin, home, cwd string, extra []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = append(importChildEnv(home), extra...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return outBuf.String(), errBuf.String(), code
}

type projectImportDoc struct {
	SchemaVersion int `json:"schema_version"`
	Sources       []struct {
		Client  string   `json:"client"`
		Clients []string `json:"clients"`
		Scope   string   `json:"scope"`
		Path    string   `json:"path"`
		Status  string   `json:"status"`
		Detail  string   `json:"detail"`
	} `json:"sources"`
	Servers []projectServerDoc `json:"servers"`
}

type projectServerDoc struct {
	Name    string   `json:"name"`
	FoundIn []string `json:"found_in"`
	Origins []struct {
		Client string `json:"client"`
		Scope  string `json:"scope"`
		Path   string `json:"path"`
	} `json:"origins"`
	Scopes      []string `json:"scopes"`
	SkipReason  string   `json:"skip_reason"`
	Warnings    []string `json:"warnings"`
	Source      string   `json:"source"`
	SourcePaths []string `json:"source_paths"`
}

func parseProjectImport(t *testing.T, stdout string) projectImportDoc {
	t.Helper()
	var doc projectImportDoc
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return doc
}

func serverNamed(doc projectImportDoc, name string) *projectServerDoc {
	for i := range doc.Servers {
		if doc.Servers[i].Name == name && doc.Servers[i].SkipReason == "" {
			return &doc.Servers[i]
		}
	}
	return nil
}

func skippedNamed(doc projectImportDoc, name string) *projectServerDoc {
	for i := range doc.Servers {
		if doc.Servers[i].Name == name && doc.Servers[i].SkipReason != "" {
			return &doc.Servers[i]
		}
	}
	return nil
}

func hasScope(doc projectImportDoc, scope string) bool {
	for _, src := range doc.Sources {
		if src.Scope == scope {
			return true
		}
	}
	return false
}

func sourceBySuffix(doc projectImportDoc, suffix string) *struct {
	Client  string
	Clients []string
	Scope   string
	Path    string
	Status  string
	Detail  string
} {
	for _, src := range doc.Sources {
		if strings.HasSuffix(src.Path, suffix) {
			copy := struct {
				Client  string
				Clients []string
				Scope   string
				Path    string
				Status  string
				Detail  string
			}{src.Client, src.Clients, src.Scope, src.Path, src.Status, src.Detail}
			return &copy
		}
	}
	return nil
}

func assertProjectFixture(t *testing.T, doc projectImportDoc, repo string) {
	t.Helper()
	want := []string{"shared", "cur", "vsc", "roosrv", "gem", "zedsrv", "oc"}
	for _, name := range want {
		if serverNamed(doc, name) == nil {
			t.Errorf("missing server %s", name)
		}
	}
	shared := serverNamed(doc, "shared")
	if shared == nil || len(shared.Origins) != 2 || len(shared.Scopes) != 1 || shared.Scopes[0] != "project" {
		t.Fatalf("shared = %+v", shared)
	}
	clients := map[string]bool{}
	for _, o := range shared.Origins {
		clients[o.Client] = true
		if o.Scope != "project" || !strings.HasSuffix(o.Path, ".mcp.json") {
			t.Fatalf("shared origin = %+v", o)
		}
	}
	if !clients["claude-code"] || !clients["vscode"] {
		t.Fatalf("shared origins = %+v", shared.Origins)
	}
	mcp := sourceBySuffix(doc, ".mcp.json")
	if mcp == nil || mcp.Status != "selected" || len(mcp.Clients) != 2 {
		t.Fatalf(".mcp.json source = %+v", mcp)
	}
	for _, path := range projectFixturePaths(repo) {
		src := sourceBySuffix(doc, strings.TrimPrefix(path, repo+string(os.PathSeparator)))
		if src == nil || src.Scope != "project" || src.Status != "selected" {
			t.Errorf("source for %s = %+v", path, src)
		}
	}
}
