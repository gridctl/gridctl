package modelsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const opencodeWithComments = `{
  // my editor setup
  "$schema": "https://opencode.ai/config.json",
  "theme": "dark",
  "model": "anthropic/claude-sonnet",
  "mcp": {
    "gridctl": { "type": "remote", "url": "http://localhost:8180/mcp" }
  }
}
`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpsertProvider_PreservesEverythingElse(t *testing.T) {
	path := writeTemp(t, "opencode.json", opencodeWithComments)
	value := map[string]any{
		"npm":     "@ai-sdk/openai-compatible",
		"name":    "LiteLLM",
		"options": map[string]any{"baseURL": "http://localhost:4000/v1", "apiKey": "{env:LITELLM_KEY}"},
	}
	if _, err := upsertProviderValue(path, "provider", "litellm", value); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	// Comments, the user's model pick, and the mcp block survive
	// byte-for-byte; only the provider subtree is new.
	for _, want := range []string{
		"// my editor setup",
		`"model": "anthropic/claude-sonnet",`,
		`"gridctl": { "type": "remote", "url": "http://localhost:8180/mcp" }`,
		`"provider"`,
		`{env:LITELLM_KEY}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}

	cur, exists, err := readProviderValue(path, "provider", "litellm")
	if err != nil || !exists {
		t.Fatalf("readProviderValue: %v exists=%v", err, exists)
	}
	if cur["npm"] != "@ai-sdk/openai-compatible" {
		t.Errorf("read back %v", cur)
	}

	// Removal restores the original bytes exactly (the empty container
	// gridctl added is removed with the value it wrapped).
	if _, existed, err := removeProviderValue(path, "provider", "litellm"); err != nil || !existed {
		t.Fatalf("remove: %v existed=%v", err, existed)
	}
	after, _ := os.ReadFile(path)
	for _, want := range []string{"// my editor setup", `"model": "anthropic/claude-sonnet",`} {
		if !strings.Contains(string(after), want) {
			t.Errorf("after removal missing %q:\n%s", want, after)
		}
	}
	if strings.Contains(string(after), "LiteLLM") {
		t.Errorf("provider value not removed:\n%s", after)
	}
}

func TestUpsertProvider_CreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "opencode.json")
	if _, err := upsertProviderValue(path, "providers", "litellm", map[string]any{"package": "x"}); err != nil {
		t.Fatal(err)
	}
	cur, exists, err := readProviderValue(path, "providers", "litellm")
	if err != nil || !exists || cur["package"] != "x" {
		t.Fatalf("round trip failed: %v exists=%v cur=%v", err, exists, cur)
	}
}

func TestRemoveProvider_MissingIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if _, existed, err := removeProviderValue(path, "provider", "litellm"); err != nil || existed {
		t.Fatalf("missing file: err=%v existed=%v", err, existed)
	}
	path = writeTemp(t, "opencode2.json", `{"theme":"dark"}`)
	if _, existed, err := removeProviderValue(path, "provider", "litellm"); err != nil || existed {
		t.Fatalf("missing key: err=%v existed=%v", err, existed)
	}
}

func TestResolveOpenCodeSchema(t *testing.T) {
	v2 := writeTemp(t, "v2.json", `{"providers": {"x": {}}}`)
	v1 := writeTemp(t, "v1.json", `{"provider": {"x": {}}}`)
	empty := writeTemp(t, "empty.json", ``)
	cases := []struct {
		declared, path, want string
	}{
		{"v1", v2, "v1"},
		{"v2", v1, "v2"},
		{"detect", v2, "v2"},
		{"detect", v1, "v1"},
		{"", empty, "v1"},
		{"detect", filepath.Join(t.TempDir(), "missing.json"), "v1"},
	}
	for _, tc := range cases {
		if got := ResolveOpenCodeSchema(tc.declared, tc.path); got != tc.want {
			t.Errorf("ResolveOpenCodeSchema(%q, %s) = %q, want %q", tc.declared, tc.path, got, tc.want)
		}
	}
}
