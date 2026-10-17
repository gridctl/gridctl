//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/servercard"
)

func TestServerCard(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	mockBin := filepath.Join(dir, "mock")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", mockBin, ".")
	build.Dir = filepath.Join(root, "examples/_mock-servers/mock-mcp-server")
	if err := build.Run(); err != nil {
		t.Fatalf("mock build: %v", err)
	}
	gridctl := filepath.Join(dir, "gridctl")
	build = exec.CommandContext(ctx, "go", "build", "-race", "-o", gridctl, ".")
	build.Dir = filepath.Join(root, "cmd/gridctl")
	if err := build.Run(); err != nil {
		t.Fatalf("gridctl build: %v", err)
	}

	gw := mcp.NewGateway()
	gw.SetName("lab gateway")
	gw.SetVersion("9.9.9")
	gw.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{
		"release": {Description: "Release bundle", Servers: []string{"fetch"}},
	}))
	server := api.NewServer(gw, nil)
	t.Cleanup(server.Close)
	server.SetAuth("bearer", "secret", "")
	server.SetAllowedOrigins([]string{"https://ui.example"})
	server.SetStackName("lab")
	httpSrv := httptest.NewServer(server.Handler())
	t.Cleanup(httpSrv.Close)

	schema, err := servercard.CompileVendoredSchema(filepath.Join(root, "pkg/servercard/testdata/schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	client := httpSrv.Client()
	for _, path := range []string{"/mcp/server-card", "/groups/release/mcp/server-card", "/.well-known/ai-catalog.json"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "https://other.example")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s status %d: %s", path, res.StatusCode, body)
		}
		if res.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("%s ACAO = %q", path, res.Header.Get("Access-Control-Allow-Origin"))
		}
		if res.Header.Get("ETag") == "" || !strings.Contains(res.Header.Get("Cache-Control"), "max-age=3600") {
			t.Fatalf("%s cache headers = %v", path, res.Header)
		}
		again, err := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		again.Header.Set("If-None-Match", res.Header.Get("ETag"))
		cached, err := client.Do(again)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, cached.Body)
		_ = cached.Body.Close()
		if cached.StatusCode != http.StatusNotModified {
			t.Fatalf("%s matching ETag = %d", path, cached.StatusCode)
		}
	}

	cardRes, err := client.Get(httpSrv.URL + "/mcp/server-card")
	if err != nil {
		t.Fatal(err)
	}
	cardBody, _ := io.ReadAll(cardRes.Body)
	_ = cardRes.Body.Close()
	var card servercard.ServerCard
	if err := json.Unmarshal(cardBody, &card); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(jsonInstance(t, cardBody)); err != nil {
		t.Fatalf("schema: %v", err)
	}
	discovered := discoverVersions(t, httpSrv.URL, "secret")
	if strings.Join(card.Remotes[0].SupportedProtocolVersions, ",") != strings.Join(discovered, ",") {
		t.Fatalf("card versions %v != discover %v", card.Remotes[0].SupportedProtocolVersions, discovered)
	}
	groupRes, err := client.Get(httpSrv.URL + "/groups/release/mcp/server-card")
	if err != nil {
		t.Fatal(err)
	}
	groupBody, _ := io.ReadAll(groupRes.Body)
	_ = groupRes.Body.Close()
	var group servercard.ServerCard
	if err := json.Unmarshal(groupBody, &group); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(group.Title, "release") || group.Meta["io.gridctl/group"] != "release" {
		t.Fatalf("group card = %#v", group)
	}
	if strings.Contains(string(cardBody), "secret") || strings.Contains(string(groupBody), "secret") {
		t.Fatal("card contained a credential")
	}

	mockPort := freePort(t)
	mock := exec.CommandContext(ctx, mockBin, "-port", strconv.Itoa(mockPort), "-server-card")
	if err := mock.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mock.Process.Kill(); _ = mock.Wait() })
	waitForPort(t, ctx, mockPort)
	stackPath := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stackPath, []byte("name: card-add\nnetwork:\n  name: cardnet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	addURL := fmt.Sprintf("http://127.0.0.1:%d/mcp", mockPort)
	out := runCardAdd(t, ctx, gridctl, stackPath, addURL, false)
	var doc struct {
		Card struct {
			Found bool   `json:"found"`
			URL   string `json:"url"`
		} `json:"card"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !doc.Card.Found || doc.Card.URL != addURL {
		t.Fatalf("card doc = %#v", doc.Card)
	}
	written, err := os.ReadFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), addURL) || !strings.Contains(string(written), "${var:") {
		t.Fatalf("stack missing card URL or placeholder:\n%s", written)
	}
	if strings.Contains(string(written), "secret-token") {
		t.Fatalf("stack contains a literal credential:\n%s", written)
	}

	plainPath := filepath.Join(dir, "plain.yaml")
	if err := os.WriteFile(plainPath, []byte("name: card-plain\nnetwork:\n  name: cardnet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plain := runCardAdd(t, ctx, gridctl, plainPath, addURL, true)
	if err := json.Unmarshal(plain, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Card.Found {
		t.Fatal("--no-card reported a card")
	}
	plainStack, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plainStack), addURL) || strings.Contains(string(plainStack), "auth:") {
		t.Fatalf("plain block = %s", plainStack)
	}

	offPort := freePort(t)
	off := exec.CommandContext(ctx, mockBin, "-port", strconv.Itoa(offPort), "-server-card", "-server-card-origin", "http://evil.example/mcp")
	if err := off.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = off.Process.Kill(); _ = off.Wait() })
	waitForPort(t, ctx, offPort)
	offStack := filepath.Join(dir, "off.yaml")
	if err := os.WriteFile(offStack, []byte("name: card-off\nnetwork:\n  name: cardnet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, gridctl, "add", fmt.Sprintf("http://127.0.0.1:%d/mcp", offPort), "--file", offStack, "--yes", "--format", "json")
	cmd.Env = os.Environ()
	combined, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(combined), "off-origin") {
		t.Fatalf("off-origin err=%v output=%s", err, combined)
	}
}

func runCardAdd(t *testing.T, ctx context.Context, bin, stack, rawURL string, noCard bool) []byte {
	t.Helper()
	args := []string{"add", rawURL, "--file", stack, "--yes", "--format", "json"}
	if noCard {
		args = append(args, "--no-card")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("add failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatal(err)
	}
	return out
}

func discoverVersions(t *testing.T, base, token string) []string {
	t.Helper()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"card","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	req, err := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "server/discover")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("discover status %d: %s", res.StatusCode, raw)
	}
	var resp struct {
		Result struct {
			SupportedVersions []string `json:"supportedVersions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("discover decode: %v\n%s", err, raw)
	}
	return resp.Result.SupportedVersions
}

func jsonInstance(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
