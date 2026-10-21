//go:build integration && !windows

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"gopkg.in/yaml.v3"
)

func TestSkills_ForegroundDaemon(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	home := t.TempDir()
	bin := filepath.Join(home, "gridctl")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", bin, "./cmd/gridctl")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	registryDir := filepath.Join(home, ".gridctl", "registry")
	alphaDir := filepath.Join(registryDir, "skills", "alpha")
	writeSkill(t, registryDir, "denied", "active")
	write(filepath.Join(alphaDir, "SKILL.md"), []byte("---\nname: alpha\ndescription: Alpha skill\nstate: active\nlicense: MIT\nacceptance_criteria:\n  - Check results\ncustom: value\nmetadata:\n  author: tester\n---\n\n# Alpha\n\nTest instructions.\n"))
	for path, data := range map[string][]byte{
		"scripts/run.sh": []byte("echo alpha\n"), "references/notes.md": []byte("# Notes\n"),
		"helper.py": []byte("print('alpha')\n"), "assets/logo.png": {137, 80, 78, 71, 0, 255}, ".hidden": []byte("hidden"),
	} {
		write(filepath.Join(alphaDir, path), data)
	}
	outside := filepath.Join(home, "outside")
	write(outside, []byte("outside"))
	if err := os.Symlink(outside, filepath.Join(alphaDir, "scripts", "link")); err != nil {
		t.Fatal(err)
	}
	stack := filepath.Join(home, "stack.yaml")
	write(stack, []byte("version: \"1\"\nname: skills-extension\nskills:\n  deny: [denied]\nmcp-servers: []\n"))
	port := freePort(t)
	logFile, err := os.Create(filepath.Join(home, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1"}
	daemon := exec.CommandContext(ctx, bin, "apply", stack, "--foreground", "--port", fmt.Sprint(port))
	daemon.Env, daemon.Stdout, daemon.Stderr = env, logFile, logFile
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = daemon.Process.Signal(syscall.SIGTERM)
		if err := daemon.Wait(); err != nil {
			t.Errorf("daemon exit: %v", err)
		}
		if t.Failed() {
			data, _ := os.ReadFile(logFile.Name())
			t.Logf("daemon:\n%s", data)
		}
	}()
	waitForPort(t, ctx, port)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 10 * time.Second}
	call := func(session, method string, params map[string]any) (map[string]any, string) {
		t.Helper()
		if params == nil {
			params = map[string]any{}
		}
		if session == "" && method != "initialize" {
			params["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientInfo": map[string]any{"name": "skills-test", "version": "1"}, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/mcp", bytes.NewReader(mustJSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
			req.Header.Set("MCP-Protocol-Version", "2025-11-25")
		} else if method != "initialize" {
			req.Header.Set("MCP-Protocol-Version", "2026-07-28")
			req.Header.Set("Mcp-Method", method)
			if method == "resources/read" {
				req.Header.Set("Mcp-Name", params["uri"].(string))
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("%s HTTP %d: %v", method, resp.StatusCode, err)
		}
		return body, resp.Header.Get("Mcp-Session-Id")
	}
	result := func(session, method string, params map[string]any) map[string]any {
		t.Helper()
		body, _ := call(session, method, params)
		res, ok := body["result"].(map[string]any)
		if !ok {
			t.Fatalf("%s: %v", method, body)
		}
		return res
	}
	checkError := func(session, method, uri string, code int) {
		t.Helper()
		body, _ := call(session, method, map[string]any{"uri": uri})
		e, ok := body["error"].(map[string]any)
		if !ok || e["code"] != float64(code) {
			t.Fatalf("%s %s: %v", method, uri, body)
		}
		if code != -32601 && e["data"].(map[string]any)["uri"] != uri {
			t.Fatalf("error URI: %v", e)
		}
	}
	checkCaps := func(res map[string]any) {
		t.Helper()
		caps := res["capabilities"].(map[string]any)
		if caps["resources"] == nil || !reflect.DeepEqual(caps["extensions"].(map[string]any)[mcp.SkillsExtensionID], map[string]any{"directoryRead": true}) {
			t.Fatalf("caps: %v", caps)
		}
	}
	checkCaps(result("", "server/discover", nil))
	initParams := func(declare bool) map[string]any {
		caps := map[string]any{}
		if declare {
			caps["extensions"] = map[string]any{mcp.SkillsExtensionID: map[string]any{}}
		}
		return map[string]any{"protocolVersion": "2025-11-25", "clientInfo": map[string]any{"name": "skills-test", "version": "1"}, "capabilities": caps}
	}
	initialized, session := call("", "initialize", initParams(true))
	checkCaps(initialized["result"].(map[string]any))
	if session == "" {
		t.Fatal("missing session")
	}
	undeclared, plain := call("", "initialize", initParams(false))
	if undeclared["result"].(map[string]any)["capabilities"].(map[string]any)["extensions"] != nil {
		t.Fatalf("undeclared caps: %v", undeclared)
	}
	for _, method := range []string{"skills/list", "skills/get", "resources/directory/read"} {
		checkError(plain, method, "skill://registry/alpha/SKILL.md", -32601)
	}
	listed := result("", "skills/list", nil)
	if listed["resultType"] != "complete" || listed["ttlMs"] != float64(60000) || listed["cacheScope"] != "private" {
		t.Fatalf("cache: %v", listed)
	}
	if !reflect.DeepEqual(listed, result("", "skills/list", nil)) {
		t.Fatal("list is not deterministic")
	}
	entries := listed["skills"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries: %v", entries)
	}
	entry := entries[0].(map[string]any)
	uri := "skill://registry/alpha/SKILL.md"
	if entry["uri"] != uri {
		t.Fatalf("entry: %v", entry)
	}
	fm := entry["frontmatter"].(map[string]any)
	if fm["name"] != "alpha" || fm["state"] != nil || fm["custom"] != "value" || fm["acceptance_criteria"] == nil {
		t.Fatalf("frontmatter: %v", fm)
	}
	manifest := entry["resources"].([]any)
	if len(manifest) != 5 {
		t.Fatalf("manifest: %v", manifest)
	}
	digests := map[string]string{}
	for _, item := range manifest {
		file := item.(map[string]any)
		fileURI := file["uri"].(string)
		digests[strings.TrimPrefix(fileURI, "skill://registry/alpha/")] = file["digest"].(string)
		read := result("", "resources/read", map[string]any{"uri": fileURI})
		content := read["contents"].([]any)[0].(map[string]any)
		var data []byte
		if blob, ok := content["blob"].(string); ok {
			data, err = base64.StdEncoding.DecodeString(blob)
			if err != nil {
				t.Fatal(err)
			}
			if content["mimeType"] != "image/png" {
				t.Fatalf("binary MIME: %v", content)
			}
		} else {
			data = []byte(content["text"].(string))
		}
		if float64(len(data)) != file["size"] || fmt.Sprintf("sha256:%x", sha256.Sum256(data)) != file["digest"] {
			t.Fatalf("manifest mismatch: %v", file)
		}
		if strings.HasSuffix(fileURI, ".md") && content["mimeType"] != "text/markdown" {
			t.Fatalf("markdown MIME: %v", content)
		}
		if fileURI == uri {
			var frontmatter map[string]any
			if err := yaml.Unmarshal(bytes.SplitN(data, []byte("---"), 3)[1], &frontmatter); err != nil {
				t.Fatal(err)
			}
			var normalized map[string]any
			if err := json.Unmarshal(mustJSON(frontmatter), &normalized); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fm, normalized) {
				t.Fatalf("frontmatter mismatch: %v / %v", fm, normalized)
			}
		}
	}
	for _, path := range []string{"SKILL.md", "helper.py", "assets/logo.png", "references/notes.md", "scripts/run.sh"} {
		if digests[path] == "" {
			t.Fatalf("missing %s", path)
		}
	}
	for _, sid := range []string{"", session} {
		got := result(sid, "skills/get", map[string]any{"uri": uri})
		if !reflect.DeepEqual(got["skill"], entry) || got["nextCursor"] != nil {
			t.Fatalf("get: %v", got)
		}
		if sid != "" && (got["ttlMs"] != nil || got["cacheScope"] != nil || got["resultType"] != nil) {
			t.Fatalf("handshake cache: %v", got)
		}
		for _, bad := range []string{"skill://registry/denied/SKILL.md", "skill://registry/missing/SKILL.md", "skill://registry/alpha/", "skill://alpha/SKILL.md"} {
			checkError(sid, "skills/get", bad, -32602)
		}
		code := -32602
		if sid != "" {
			code = -32002
		}
		for _, bad := range []string{".hidden", "scripts/link", "../denied/SKILL.md"} {
			checkError(sid, "resources/read", "skill://registry/alpha/"+bad, code)
		}
		checkError(sid, "resources/read", "skill://registry/denied/SKILL.md", code)
		for _, bad := range []string{uri, "skill://registry/alpha/"} {
			checkError(sid, "resources/directory/read", bad, -32602)
		}
	}
	children := result("", "resources/directory/read", map[string]any{"uri": "skill://registry/alpha"})["resources"].([]any)
	if len(children) != 5 {
		t.Fatalf("children: %v", children)
	}
	for _, item := range children {
		child := item.(map[string]any)
		switch child["name"] {
		case "alpha":
			if child["uri"] != uri || child["description"] != "Alpha skill" {
				t.Fatalf("SKILL child: %v", child)
			}
		case "assets", "references", "scripts":
			if child["mimeType"] != "inode/directory" {
				t.Fatalf("directory: %v", child)
			}
		case "helper.py":
		default:
			t.Fatalf("unexpected child: %v", child)
		}
	}
	scripts := result("", "resources/directory/read", map[string]any{"uri": "skill://registry/alpha/scripts"})["resources"].([]any)
	if len(scripts) != 1 || scripts[0].(map[string]any)["name"] != "run.sh" {
		t.Fatalf("scripts: %v", scripts)
	}
	resources := result("", "resources/list", nil)["resources"].([]any)
	if len(resources) != 2 || resources[0].(map[string]any)["uri"] != "skills://registry/alpha" || resources[1].(map[string]any)["uri"] != uri || resources[1].(map[string]any)["name"] != "alpha" || resources[1].(map[string]any)["mimeType"] != "text/markdown" {
		t.Fatalf("resources: %v", resources)
	}
	cli := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = env
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%v: %v\n%s\n%s", args, err, out, stderr.String())
		}
		return out
	}
	cli("skill", "pins", "verify")
	t.Setenv("GRIDCTL_HOME", home)
	pins := skillpins.New("skills-extension")
	if err := pins.Load(); err != nil {
		t.Fatal(err)
	}
	pin, ok := pins.Get("alpha")
	if !ok || strings.TrimPrefix(pin.SkillHash, "s1:") != strings.TrimPrefix(digests["SKILL.md"], "sha256:") {
		t.Fatalf("pin: %+v", pin)
	}
	if len(pin.Files) != 4 {
		t.Fatalf("pin files: %v", pin.Files)
	}
	for _, file := range pin.Files {
		if strings.TrimPrefix(file.Digest, "s1:") != strings.TrimPrefix(digests[file.Path], "sha256:") {
			t.Fatalf("pin file: %v", file)
		}
	}
	var cliSkills []map[string]any
	if err := json.Unmarshal(cli("skill", "list", "--json"), &cliSkills); err != nil {
		t.Fatal(err)
	}
	for _, sk := range cliSkills {
		summary := sk["manifest"].(map[string]any)
		if summary["digest"] == "" || summary["files"] == nil || summary["bytes"] == nil || summary["dynamic"] != false {
			t.Fatalf("summary: %v", summary)
		}
	}
	if !bytes.Contains(cli("status", "--json"), []byte(mcp.SkillsExtensionID)) {
		t.Fatal("CLI status omitted skills extension")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/status", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	err = json.NewDecoder(resp.Body).Decode(&status)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(status["extensions"], []any{mcp.SkillsExtensionID}) {
		t.Fatalf("status: %v", status)
	}
	write(filepath.Join(alphaDir, "references/notes.md"), []byte("# Updated notes\n"))
	deadline := time.Now().Add(10 * time.Second)
	for {
		updated := result("", "skills/get", map[string]any{"uri": uri})["skill"].(map[string]any)["resources"].([]any)
		changed := []string{}
		for _, item := range updated {
			file := item.(map[string]any)
			path := strings.TrimPrefix(file["uri"].(string), "skill://registry/alpha/")
			if file["digest"] != digests[path] {
				changed = append(changed, path)
			}
		}
		if err := pins.Load(); err != nil {
			t.Fatal(err)
		}
		pin, _ = pins.Get("alpha")
		if reflect.DeepEqual(changed, []string{"references/notes.md"}) && pin.Status == skillpins.StatusDrift {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refresh: changed=%v pin=%+v", changed, pin)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, name := range []string{"alpha", "denied"} {
		if err := os.RemoveAll(filepath.Join(registryDir, "skills", name)); err != nil {
			t.Fatal(err)
		}
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		caps := result("", "server/discover", nil)["capabilities"].(map[string]any)
		if caps["resources"] == nil && caps["extensions"] == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("empty registry still declared: %v", caps)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, sid := range []string{"", session} {
		for _, method := range []string{"skills/list", "skills/get", "resources/directory/read"} {
			checkError(sid, method, uri, -32601)
		}
	}
}
