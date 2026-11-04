package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/hookpins"
	"github.com/gridctl/gridctl/pkg/hooksync"
	"github.com/gridctl/gridctl/pkg/pins"
)

func setupHookTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	srv, regServer := setupRegistryTestServer(t)
	registryDir := regServer.Store().Dir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv.SetHooksManager(hooksync.NewManagerWithHome(home, registryDir))
	srv.SetHookPinStore(hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins")))
	return srv, registryDir, home
}

func seedHook(t *testing.T, registryDir, name, script string) {
	t.Helper()
	dir := filepath.Join(registryDir, "hooks", name, "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"name":"` + name + `","description":"block force push","dialect":"claude-code","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"./scripts/run.sh"}]}]}}`)
	if err := os.WriteFile(filepath.Join(registryDir, "hooks", name, "hook.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func pinSeededHook(t *testing.T, home, registryDir, name string, findings []pins.Finding) {
	t.Helper()
	ps := hookpins.NewWithPath(filepath.Join(home, ".gridctl", "pins"))
	if err := ps.Pin(t.Context(), hookpins.PinRequest{
		Name:     name,
		Dir:      filepath.Join(registryDir, "hooks", name),
		Findings: findings,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHandleHooks_ListEmptyAndAdoptMissing(t *testing.T) {
	srv, _, _ := setupHookTestServer(t)
	rec := doJSON(t, srv, http.MethodGet, "/api/registry/hooks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", rec.Code, rec.Body.String())
	}
	var list registryHookList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Hooks == nil || len(list.Hooks) != 0 {
		t.Fatalf("hooks = %#v, want empty array", list.Hooks)
	}
	if len(list.UnsupportedClients) != len(hooksync.UnsupportedClients()) {
		t.Fatalf("unsupported = %d, want %d", len(list.UnsupportedClients), len(hooksync.UnsupportedClients()))
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/project/hooks/status", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/project/hooks/adopt", `{"hook":"block","client":"claude-code"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("adopt = %d, want 404", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/project/hooks/unsync", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty unsync = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/hook-pins/block/reset", `{}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("reset POST = %d, want 404", rec.Code)
	}
}

func TestHandleHooks_GetSyncAndPinReset(t *testing.T) {
	srv, registryDir, home := setupHookTestServer(t)
	seedHook(t, registryDir, "block", "#!/bin/sh\necho no\n")
	pinSeededHook(t, home, registryDir, "block", nil)

	rec := doJSON(t, srv, http.MethodGet, "/api/registry/hooks/block", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", rec.Code, rec.Body.String())
	}
	var detail registryHook
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Name != "block" || detail.Description == "" || len(detail.Events) != 1 || detail.PinStatus != hookpins.StatusPinned {
		t.Fatalf("detail = %+v", detail)
	}
	if len(detail.Files) == 0 || !strings.Contains(detail.Document, "run.sh") || len(detail.Hooks) == 0 {
		t.Fatalf("definition payload incomplete: %+v", detail)
	}

	rec = doJSON(t, srv, http.MethodPost, "/api/project/hooks/sync", `{"hooks":["block"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"copied"`) {
		t.Fatalf("sync = %d: %s", rec.Code, rec.Body.String())
	}
	script := filepath.Join(home, ".gridctl", "hooks", "block", "scripts", "run.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("script copy missing: %v", err)
	}

	rec = doJSON(t, srv, http.MethodGet, "/api/hook-pins/block", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"pinned"`) {
		t.Fatalf("pin get = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodDelete, "/api/hook-pins/block", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("pin reset = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodGet, "/api/hook-pins/block", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("pin after reset = %d, want 404", rec.Code)
	}
}

func TestHandleHooks_DeleteRefusesUnsyncRows(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, home string)
		want   string
	}{
		{
			name: "skipped-drift",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				path := filepath.Join(home, ".claude", "settings.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "gridctl hook block", "hand edited", 1)
				if edited == string(data) {
					t.Fatalf("settings had no gridctl marker:\n%s", data)
				}
				if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "skipped-drift",
		},
		{
			name: "skipped-unmanaged",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("not json"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "skipped-unmanaged",
		},
		{
			name: "error",
			mutate: func(t *testing.T, home string) {
				t.Helper()
				path := filepath.Join(home, ".gridctl", "project.lock.yaml")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "client: claude-code", "client: retired", 1)
				if edited == string(data) {
					t.Fatalf("lock had no claude-code client:\n%s", data)
				}
				if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "error",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, registryDir, home := setupHookTestServer(t)
			seedHook(t, registryDir, "block", "#!/bin/sh\necho no\n")
			pinSeededHook(t, home, registryDir, "block", nil)
			rec := doJSON(t, srv, http.MethodPost, "/api/project/hooks/sync", `{"hooks":["block"]}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("sync = %d: %s", rec.Code, rec.Body.String())
			}
			tc.mutate(t, home)
			rec = doJSON(t, srv, http.MethodDelete, "/api/registry/hooks/block", "")
			if rec.Code != http.StatusConflict {
				t.Fatalf("delete = %d, want 409: %s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.want) || !strings.Contains(body, "unsync") || !strings.Contains(body, "--force") {
				t.Fatalf("body = %s", body)
			}
			if tc.name != "error" && !strings.Contains(body, "claude-code") {
				t.Fatalf("body does not name the client: %s", body)
			}
			if tc.name == "error" && !strings.Contains(body, "retired") {
				t.Fatalf("body does not name the client: %s", body)
			}
			if _, err := os.Stat(filepath.Join(registryDir, "hooks", "block", "hook.json")); err != nil {
				t.Fatalf("registry hook removed despite refusal: %v", err)
			}
		})
	}
}

func TestHandleHookPins_ApproveRequiresReason(t *testing.T) {
	srv, registryDir, home := setupHookTestServer(t)
	seedHook(t, registryDir, "block", "#!/bin/sh\nchmod 777 \"$0\"\n")
	pinSeededHook(t, home, registryDir, "block", []pins.Finding{{
		Code: "hook-scan", Severity: pins.SeverityWarn, Message: "world-writable permissions",
	}})
	rec := doJSON(t, srv, http.MethodGet, "/api/hook-pins/block/diff", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("diff = %d: %s", rec.Code, rec.Body.String())
	}
	var diff hookPinDiffResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &diff); err != nil {
		t.Fatal(err)
	}
	if diff.Hook != "block" || diff.CompositeHash == "" {
		t.Fatalf("diff = %+v", diff)
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/hook-pins/block/approve", `{"expect":"`+diff.CompositeHash+`"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "reason") {
		t.Fatalf("approve without reason = %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/api/hook-pins/block/approve", `{"expect":"`+diff.CompositeHash+`","reason":"reviewed the script"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", rec.Code, rec.Body.String())
	}
}
