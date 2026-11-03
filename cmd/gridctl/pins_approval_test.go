package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestPinsApproveBody(t *testing.T) {
	if body, err := pinsApproveBody("", "", "wcollins", nil); err != nil || body != nil {
		t.Fatalf("empty = %q %v", body, err)
	}
	for _, tc := range []struct {
		name   string
		expect string
		reason string
		refs   []string
	}{
		{name: "reason only", reason: "reviewed"},
		{name: "evidence only", refs: []string{"https://example.test/pr/1"}},
		{name: "expect only", expect: "h2:abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := pinsApproveBody(tc.expect, tc.reason, "wcollins", tc.refs)
			if err != nil || len(body) == 0 {
				t.Fatalf("body = %q %v", body, err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded["approved_by"] != "wcollins" {
				t.Fatalf("body = %s", body)
			}
			if tc.reason != "" && decoded["reason"] != tc.reason {
				t.Fatalf("reason = %v", decoded["reason"])
			}
			if tc.expect != "" && decoded["expected_server_hash"] != tc.expect {
				t.Fatalf("expect = %v", decoded["expected_server_hash"])
			}
			if len(tc.refs) > 0 {
				refs, _ := decoded["evidence"].([]any)
				if len(refs) != 1 {
					t.Fatalf("evidence = %v", decoded["evidence"])
				}
			}
		})
	}
}

func TestRecordedApprovalFrom(t *testing.T) {
	rec := recordedApprovalFrom([]byte(`{"approved_by":"principal","approved_by_source":"oauth","evidence_count":2}`))
	if !rec.OK || rec.Name != "principal" || rec.Source != "oauth" || rec.Count != 2 {
		t.Fatalf("rec = %+v", rec)
	}
	if rec := recordedApprovalFrom([]byte(`{"tool_count":1}`)); rec.OK {
		t.Fatal("missing evidence_count must not invent a source")
	}
}

func TestRenderPinsDiffText_LastApproval(t *testing.T) {
	when := time.Date(2026, 10, 2, 15, 4, 5, 0, time.UTC)
	doc := testDiffDoc(true)
	doc.Servers[0].LastApproval = &pinapprovals.Entry{
		ApprovedAt:       when,
		ApprovedBy:       "wcollins",
		ApprovedBySource: pinapprovals.SourceOSUser,
		ApprovedHash:     "h2:abc",
		Reason:           "reviewed the schema",
		Evidence:         []string{"https://example.test/pr/1"},
	}
	var buf strings.Builder
	renderPinsDiffText(&buf, doc)
	out := buf.String()
	for _, want := range []string{
		"last approved 2026-10-02T15:04:05Z by wcollins (os_user)",
		"reviewed the schema",
		"https://example.test/pr/1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("diff text missing %q:\n%s", want, out)
		}
	}
}

func TestRunPinsApprove_PostsBodyWithoutExpect(t *testing.T) {
	var gotBody []byte
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"server":"echo","tool_count":1,"status":"approved","approved_by":"daemon-user","approved_by_source":"os_user","evidence_count":1}`)
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	t.Setenv(state.HomeEnv, t.TempDir())
	prev := pinsStack
	pinsStack = "lab"
	t.Cleanup(func() { pinsStack = prev })
	if err := state.Save(&state.DaemonState{
		StackName: "lab",
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := runPinsApprove("echo", "", "reviewed", []string{"https://example.test/pr/1"}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/pins/echo/approve" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	var decoded map[string]any
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("body %q: %v", gotBody, err)
	}
	if decoded["reason"] != "reviewed" || decoded["expected_server_hash"] != "" {
		t.Fatalf("body = %s", gotBody)
	}
	refs, _ := decoded["evidence"].([]any)
	if len(refs) != 1 || refs[0] != "https://example.test/pr/1" {
		t.Fatalf("evidence = %v", decoded["evidence"])
	}

	gotBody = nil
	if err := runPinsApprove("echo", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if len(gotBody) != 0 {
		t.Fatalf("unconditional approve posted %q", gotBody)
	}
}

func TestRunSkillPinsDiff_JSONLastApproval(t *testing.T) {
	home := t.TempDir()
	t.Setenv(state.HomeEnv, home)
	prev := skillPinsStack
	skillPinsStack = "lab"
	t.Cleanup(func() { skillPinsStack = prev })

	regDir := filepath.Join(home, ".gridctl", "registry")
	skillDir := filepath.Join(regDir, "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const skillDoc = "---\nname: alpha\ndescription: A quiet helper.\nstate: active\n---\n\nNothing suspicious here.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.NewStore(regDir)
	if err := reg.Load(); err != nil {
		t.Fatal(err)
	}
	ps := skillpins.New("lab")
	if _, err := ps.Sync(reg); err != nil {
		t.Fatal(err)
	}
	if err := ps.Approve(t.Context(), "alpha", reg, skillpins.ApproveRequest{
		Reason:           "kept in the log",
		ApprovedBy:       "wcollins",
		ApprovedBySource: pinapprovals.SourceOSUser,
		Evidence:         []string{"https://example.test/pr/1"},
	}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	code := runSkillPinsDiff(&stdout, &stderr, "alpha", "json", "")
	if code != pinsExitOK {
		t.Fatalf("exit = %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	var doc skillPinsDiffDoc
	if err := json.Unmarshal([]byte(stdout.String()), &doc); err != nil {
		t.Fatalf("decode %s: %v", stdout.String(), err)
	}
	if doc.LastApproval == nil || doc.LastApproval.Reason != "kept in the log" || doc.LastApproval.ApprovedBy != "wcollins" {
		t.Fatalf("last_approval = %+v", doc.LastApproval)
	}
	if len(doc.LastApproval.Evidence) != 1 || doc.LastApproval.Evidence[0] != "https://example.test/pr/1" {
		t.Fatalf("evidence = %#v", doc.LastApproval.Evidence)
	}
}
