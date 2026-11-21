package hookpins

import (
	"encoding/json"
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
		ExpectedHash:     vr.CompositeHash,
		Reason:           "reviewed the script",
		Evidence:         []string{"https://example.com/review/1"},
		ApprovedBy:       "ada",
		ApprovedBySource: pinapprovals.SourceDeclared,
		Findings:         req.Findings,
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
	path := filepath.Join(dir, "hooks", "global.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
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
	pinPath := filepath.Join(dir, "hooks", "global.json")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinPath, []byte("{\"version\":\"1\",\"hooks\":{}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_CorruptResetsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks", "global.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	ps := NewWithPath(dir)
	err := ps.Load()
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if _, ok := ps.Get("block"); ok {
		t.Fatal("corrupt load left a pin")
	}
}

func TestSync_MissingHookKeptAndReported(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	writeHook(t, hookDir, `{"name":"block"}`, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(hookDir); err != nil {
		t.Fatal(err)
	}
	res, err := ps.Sync(t.Context(), reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Missing) != 1 || res.Missing[0] != "block" {
		t.Fatalf("missing = %v", res.Missing)
	}
	if _, ok := ps.Get("block"); !ok {
		t.Fatal("missing hook pin was pruned")
	}
}

func TestApprove_CleanScanClearsFindings(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	writeHook(t, hookDir, `{"name":"block"}`, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ps.Pin(t.Context(), PinRequest{
		Name:     "block",
		Dir:      hookDir,
		Source:   SourceLocal,
		Findings: []pins.Finding{{Code: "hook-scan", Severity: pins.SeverityWarn, Message: "old"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ps.Approve(t.Context(), "block", reg, ApproveRequest{Scanned: true}); err != nil {
		t.Fatalf("clean approval = %v", err)
	}
	got, ok := ps.Get("block")
	if !ok || len(got.Findings) != 0 || got.Status != StatusPinned {
		t.Fatalf("pin = %+v ok=%v", got, ok)
	}
}

func TestLoad_ProjectsApprovalsFromLog(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	writeHook(t, hookDir, `{"name":"block"}`, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}
	if err := ps.Approve(t.Context(), "block", reg, ApproveRequest{Reason: "kept in the log"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ps.path)
	if err != nil {
		t.Fatal(err)
	}
	var pf PinFile
	if err := json.Unmarshal(data, &pf); err != nil {
		t.Fatal(err)
	}
	for _, pin := range pf.Hooks {
		pin.Approvals = nil
		pin.CompositeHash = ""
	}
	stripped, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ps.path, stripped, 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded := NewWithPath(dir)
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	pin, ok := reloaded.Get("block")
	if !ok || len(pin.Approvals) != 1 || pin.Approvals[0].Reason != "kept in the log" || pin.CompositeHash == "" {
		t.Fatalf("reloaded = %+v", pin)
	}
}

func TestApprove_RefusesCredentialBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	writeHook(t, hookDir, `{"name":"block"}`, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ps.path)
	if err != nil {
		t.Fatal(err)
	}
	err = ps.Approve(t.Context(), "block", reg, ApproveRequest{Evidence: []string{"Bearer abc"}})
	if !errors.Is(err, pinapprovals.ErrCredentialShaped) {
		t.Fatalf("err = %v", err)
	}
	after, err := os.ReadFile(ps.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("refused approval rewrote the pin file")
	}
	if _, statErr := os.Stat(ps.logPath); !os.IsNotExist(statErr) {
		t.Fatalf("refused approval wrote the approvals log: %v", statErr)
	}
}

func TestApprove_RefusesApproverBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	writeHook(t, hookDir, `{"name":"block"}`, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ps.path)
	if err != nil {
		t.Fatal(err)
	}
	err = ps.Approve(t.Context(), "block", reg, ApproveRequest{ApprovedBy: "Bearer abc", ApprovedBySource: pinapprovals.SourceDeclared})
	if !errors.Is(err, pinapprovals.ErrCredentialShaped) {
		t.Fatalf("err = %v", err)
	}
	after, err := os.ReadFile(ps.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("refused approver rewrote the pin file")
	}
	if _, statErr := os.Stat(ps.logPath); !os.IsNotExist(statErr) {
		t.Fatalf("refused approver wrote the approvals log: %v", statErr)
	}
}

func TestApprove_LogAppendPrecedesPinRewrite(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	writeHook(t, hookDir, `{"name":"block"}`, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}
	// The pin file stays readable so Approve loads the record and appends
	// the log. The directory is not writable, so the pin rewrite fails after
	// that append. Approve reloads from disk, so a missing path would return
	// ErrNotPinned before the log write.
	pinDir := filepath.Dir(ps.path)
	if err := os.Chmod(pinDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pinDir, 0o755) })
	err := ps.Approve(t.Context(), "block", reg, ApproveRequest{Reason: "after the log"})
	_ = os.Chmod(pinDir, 0o755)
	if err == nil {
		t.Fatal("expected pin rewrite to fail")
	}
	data, err := os.ReadFile(ps.logPath)
	if err != nil {
		t.Fatalf("approvals log missing after failed pin rewrite: %v", err)
	}
	if !strings.Contains(string(data), "after the log") {
		t.Fatalf("log = %s", data)
	}
}

func TestVerifyApprove_RejectsPathName(t *testing.T) {
	root := t.TempDir()
	registryDir := filepath.Join(root, "home", "registry")
	outside := filepath.Join(root, "home", "outside")
	marker := "OUTSIDE_HOOK_MARKER"
	writeHook(t, outside, `{"name":"outside","description":"`+marker+`"}`, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(filepath.Join(root, "pins"))
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	// Pin accepts a caller-supplied directory. A poisoned key must not let
	// Verify or Approve follow that name out of the registry.
	if err := ps.Pin(t.Context(), PinRequest{Name: "../../outside", Dir: outside, Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}

	name := "../../outside"
	vr, err := ps.Verify(name, registryDir)
	if err == nil || vr != nil {
		t.Fatalf("verify = %+v, %v; want name rejection", vr, err)
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotPinned) || errors.Is(err, ErrHashMismatch) || strings.Contains(err.Error(), marker) {
		t.Fatalf("verify treated the escaped name as a hook: %v", err)
	}
	if !strings.Contains(err.Error(), "hook name") {
		t.Fatalf("verify error = %v", err)
	}
	if _, err := ps.CurrentCompositeHash(name, registryDir); err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("composite hash = %v", err)
	}
	err = ps.Approve(t.Context(), name, registryDir, ApproveRequest{ExpectedHash: "deadbeef", Reason: "no"})
	if err == nil || errors.Is(err, ErrHashMismatch) || errors.Is(err, ErrNotPinned) || strings.Contains(err.Error(), marker) {
		t.Fatalf("approve = %v", err)
	}
}

func TestCurrentCompositeHash_NeedsNoStore(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	writeHook(t, filepath.Join(reg, "hooks", "block"), `{"name":"block","description":"d","dialect":"claude-code"}`, "#!/bin/sh\nexit 0\n")

	got, err := CurrentCompositeHash("block", reg)
	if err != nil {
		t.Fatal(err)
	}
	ps := NewWithPath(dir)
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: filepath.Join(reg, "hooks", "block"), Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}
	pin, ok := ps.Get("block")
	if !ok || got == "" || pin.CompositeHash != got {
		t.Fatalf("hash = %q, pinned = %+v", got, pin)
	}
	viaStore, err := ps.CurrentCompositeHash("block", reg)
	if err != nil || viaStore != got {
		t.Fatalf("store hash = %q, %v; want %q", viaStore, err, got)
	}
	if _, err := CurrentCompositeHash("../escape", reg); err == nil {
		t.Fatal("escaped name accepted")
	}
}

func TestPin_ChangedBytesHoldsPinAsDrift(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	body := `{"name":"block"}`
	writeHook(t, hookDir, body, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	finding := pins.Finding{Code: "hook-scan", Severity: pins.SeverityWarn, Message: "old"}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal, Findings: []pins.Finding{finding}}); err != nil {
		t.Fatal(err)
	}
	first, _ := ps.Get("block")

	writeHook(t, hookDir, `{"name":"block","description":"changed"}`, "#!/bin/sh\necho changed\n")
	err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal})
	if !errors.Is(err, ErrPinDrift) {
		t.Fatalf("re-pin with changed bytes = %v, want ErrPinDrift", err)
	}
	got, ok := ps.Get("block")
	if !ok || got.Status != StatusDrift {
		t.Fatalf("pin = %+v ok=%v, want drift", got, ok)
	}
	if got.Document != body || got.DefinitionHash != first.DefinitionHash || !equalDigests(got.Files, first.Files) {
		t.Fatalf("held pin replaced pinned bytes: %+v", got)
	}
	if len(got.Findings) != 1 || got.Findings[0].Message != "old" {
		t.Fatalf("held pin findings = %+v", got.Findings)
	}
	if len(got.Approvals) != 0 {
		t.Fatalf("held pin recorded approvals %+v", got.Approvals)
	}

	// The drift status is saved, not only held in memory.
	fresh := NewWithPath(dir)
	if err := fresh.Load(); err != nil {
		t.Fatal(err)
	}
	saved, _ := fresh.Get("block")
	if saved == nil || saved.Status != StatusDrift || saved.Document != body {
		t.Fatalf("saved pin = %+v", saved)
	}
	if _, err := os.Stat(filepath.Join(dir, "approvals", "hooks", "global.json")); !os.IsNotExist(err) {
		t.Fatalf("held pin wrote an approvals log: %v", err)
	}

	// Approve is the only path to the new bytes.
	vr, err := ps.Verify("block", reg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ps.Approve(t.Context(), "block", reg, ApproveRequest{ExpectedHash: vr.CompositeHash, Scanned: true}); err != nil {
		t.Fatal(err)
	}
	got, _ = ps.Get("block")
	if got.Status != StatusPinned || got.CompositeHash != vr.CompositeHash {
		t.Fatalf("approved pin = %+v", got)
	}
}

func TestPin_EqualBytesRefreshes(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	writeHook(t, hookDir, `{"name":"block"}`, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}
	first, _ := ps.Get("block")
	origin := &skillpins.OriginRef{Repo: "https://example.com/pack", Ref: "main", CommitSHA: "def"}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Origin: origin}); err != nil {
		t.Fatalf("re-pin with equal bytes = %v", err)
	}
	got, _ := ps.Get("block")
	if got.Status != StatusPinned || got.CompositeHash != first.CompositeHash {
		t.Fatalf("pin = %+v", got)
	}
	if got.Origin == nil || got.Origin.CommitSHA != "def" || !got.PinnedAt.Equal(first.PinnedAt) {
		t.Fatalf("refresh did not update origin or kept wrong pinned_at: %+v", got)
	}
}

func TestPin_ChangedBytesHeldWithoutStoredComposite(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	hookDir := filepath.Join(reg, "hooks", "block")
	body := `{"name":"block"}`
	writeHook(t, hookDir, body, "#!/bin/sh\nexit 0\n")
	ps := NewWithPath(dir)
	if err := ps.Load(); err != nil {
		t.Fatal(err)
	}
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal}); err != nil {
		t.Fatal(err)
	}

	// Strip composite_hash by hand, as a hand-edited record would.
	pinPath := filepath.Join(dir, "hooks", "global.json")
	raw, err := os.ReadFile(pinPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	rec := doc["hooks"].(map[string]any)["block"].(map[string]any)
	if _, ok := rec["composite_hash"]; !ok {
		t.Fatal("fixture pin file has no composite_hash to strip")
	}
	delete(rec, "composite_hash")
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	writeHook(t, hookDir, body, "#!/bin/sh\necho changed\n")
	if err := ps.Pin(t.Context(), PinRequest{Name: "block", Dir: hookDir, Source: SourceLocal}); !errors.Is(err, ErrPinDrift) {
		t.Fatalf("re-pin without stored composite = %v, want ErrPinDrift", err)
	}
	got, _ := ps.Get("block")
	if got.Status != StatusDrift || got.Document != body {
		t.Fatalf("pin = %+v", got)
	}
}
