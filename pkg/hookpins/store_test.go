package hookpins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/pinapprovals"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

func writeHook(t *testing.T, dir, body, script string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hook.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPinVerifyApproveReset(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	body := `{"name":"block","description":"d","dialect":"claude-code"}`
	writeHook(t, hookDir, body, "#!/bin/sh\nexit 0\n")

	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	req := PinRequest{
		Name:   "block",
		Dir:    hookDir,
		Source: SourceGit,
		Origin: &skillpins.OriginRef{Repo: "https://example.com/pack", Ref: "main", CommitSHA: "abc"},
		Findings: []pins.Finding{{
			Code: "hook-scan", Severity: pins.SeverityWarn, Message: "world-writable permissions",
		}},
	}
	if err := ps.Pin(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	got, ok := ps.Get("block")
	if !ok || got.Status != StatusPinned || got.DefinitionHash == "" || got.CompositeHash == "" {
		t.Fatalf("pin = %+v ok=%v", got, ok)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("findings = %+v", got.Findings)
	}

	vr, err := ps.Verify("block", reg)
	if err != nil {
		t.Fatal(err)
	}
	if vr.Status != StatusPinned {
		t.Fatalf("verify status = %s", vr.Status)
	}

	if err := os.WriteFile(filepath.Join(hookDir, "hook.json"), []byte(body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vr, err = ps.Verify("block", reg)
	if err != nil {
		t.Fatal(err)
	}
	if vr.Status != StatusDrift || vr.Diff == nil || !vr.Diff.DocumentChanged() {
		t.Fatalf("drift verify = %+v", vr)
	}
	if _, err := ps.Sync(t.Context(), reg); err != nil {
		t.Fatal(err)
	}
	got, _ = ps.Get("block")
	if got.Status != StatusDrift {
		t.Fatalf("sync status = %s, want drift", got.Status)
	}
	// Sync must not replace the pinned document.
	if got.Document != body {
		t.Fatalf("sync re-pinned document %q", got.Document)
	}

	if err := ps.Approve(t.Context(), "block", reg, ApproveRequest{ExpectedHash: "nope", Reason: "reviewed"}); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("approve mismatch = %v", err)
	}
	if err := ps.Approve(t.Context(), "block", reg, ApproveRequest{ExpectedHash: vr.CompositeHash}); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("approve without reason = %v", err)
	}
	if err := ps.Approve(t.Context(), "block", reg, ApproveRequest{
		ExpectedHash: vr.CompositeHash,
		Reason:       "reviewed the script",
		Evidence:     []string{"https://example.com/review/1"},
		ApprovedBy:   "ada",
		ApprovedBySource: pinapprovals.SourceDeclared,
		Findings:     req.Findings,
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = ps.Get("block")
	if got.Status != StatusPinned || len(got.Approvals) != 1 || got.Approvals[0].Reason != "reviewed the script" {
		t.Fatalf("approved pin = %+v", got)
	}
	logPath := filepath.Join(dir, "approvals", "hooks", "global.json")
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "reviewed the script") {
		t.Fatalf("approval log = %s", raw)
	}

	if err := ps.Reset(t.Context(), "block"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ps.Get("block"); ok {
		t.Fatal("reset left the pin")
	}
	// History stays in the log.
	raw, err = os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(raw), "reviewed the script") {
		t.Fatalf("reset dropped history: %v %s", err, raw)
	}
}

func TestLoad_RejectsNewerVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	if err := os.WriteFile(path, []byte("{\"version\":\"2\",\"hooks\":{}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ps := NewWithPath(dir)
	err := ps.Load()
	if !errors.Is(err, ErrNewerVersion) {
		t.Fatalf("err = %v, want ErrNewerVersion", err)
	}
	if !strings.Contains(err.Error(), "written by a newer gridctl") {
		t.Fatalf("error %q should use the newer-gridctl wording", err.Error())
	}
}

func TestLoad_EmptyAndVersion1(t *testing.T) {
	dir := t.TempDir()
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte("{\"version\":\"1\",\"hooks\":{}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
}


