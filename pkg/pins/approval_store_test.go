package pins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pinapprovals"
)

func TestPinStore_ApproveRecordsHistory(t *testing.T) {
	dir := t.TempDir()
	ps := NewWithPath(dir, "lab")
	tools := []mcp.Tool{{Name: "echo", Description: "says hello"}}
	if _, err := ps.VerifyOrPin("echo", tools); err != nil {
		t.Fatal(err)
	}
	hash, err := HashTools(tools)
	if err != nil {
		t.Fatal(err)
	}
	req := ApproveRequest{
		ExpectedHash:     hash,
		Reason:           "reviewed the schema",
		Evidence:         []string{" https://example.test/pr/1 ", "https://example.test/pr/1", "plan/review.md"},
		ApprovedBy:       "wcollins",
		ApprovedBySource: pinapprovals.SourceDeclared,
	}
	if err := ps.Approve(t.Context(), "echo", tools, req); err != nil {
		t.Fatal(err)
	}
	sp, ok := ps.GetServer("echo")
	if !ok || len(sp.Approvals) != 1 {
		t.Fatalf("approvals = %+v", sp)
	}
	got := sp.Approvals[0]
	if !got.Bound || got.ApprovedHash != sp.ServerHash || got.ApprovedHash != hash {
		t.Fatalf("entry = %+v server_hash %s", got, sp.ServerHash)
	}
	if got.ApprovedBy != "wcollins" || got.ApprovedBySource != pinapprovals.SourceDeclared {
		t.Fatalf("approver = %q %q", got.ApprovedBy, got.ApprovedBySource)
	}
	if len(got.Evidence) != 2 || got.Evidence[0] != "https://example.test/pr/1" {
		t.Fatalf("evidence = %#v", got.Evidence)
	}

	if err := ps.Approve(t.Context(), "echo", tools, ApproveRequest{Reason: "second"}); err != nil {
		t.Fatal(err)
	}
	sp, _ = ps.GetServer("echo")
	if len(sp.Approvals) != 2 || sp.Approvals[0].Bound || sp.Approvals[0].ApprovedHash != sp.ServerHash {
		t.Fatalf("second = %+v hash %s", sp.Approvals, sp.ServerHash)
	}
}

func TestPinStore_ApproveRefusesApproverBeforeWrite(t *testing.T) {
	dir := t.TempDir()
	ps := NewWithPath(dir, "lab")
	tools := []mcp.Tool{{Name: "echo", Description: "says hello"}}
	if _, err := ps.VerifyOrPin("echo", tools); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ps.path)
	if err != nil {
		t.Fatal(err)
	}
	err = ps.Approve(t.Context(), "echo", tools, ApproveRequest{ApprovedBy: "Bearer abc"})
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
		t.Fatal("refused approver wrote the approvals log")
	}
}

func TestPinStore_LoadProjectsApprovalsFromLog(t *testing.T) {
	dir := t.TempDir()
	ps := NewWithPath(dir, "lab")
	tools := []mcp.Tool{{Name: "echo", Description: "says hello"}}
	if _, err := ps.VerifyOrPin("echo", tools); err != nil {
		t.Fatal(err)
	}
	if err := ps.Approve(t.Context(), "echo", tools, ApproveRequest{Reason: "kept in the log"}); err != nil {
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
	for _, sp := range pf.Servers {
		sp.Approvals = nil
	}
	stripped, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ps.path, stripped, 0o600); err != nil {
		t.Fatal(err)
	}
	reloaded := NewWithPath(dir, "lab")
	if err := reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	sp, ok := reloaded.GetServer("echo")
	if !ok || len(sp.Approvals) != 1 || sp.Approvals[0].Reason != "kept in the log" || sp.Approvals[0].ApprovedHash != sp.ServerHash {
		t.Fatalf("reloaded = %+v", sp)
	}
}

func TestPinStore_LoadNewerApprovalsLog(t *testing.T) {
	dir := t.TempDir()
	ps := NewWithPath(dir, "lab")
	if err := os.MkdirAll(filepath.Dir(ps.logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ps.logPath, []byte(`{"version":"2","stack":"lab","kind":"tool","subjects":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ps.Load()
	if !errors.Is(err, ErrNewerVersion) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), ps.logPath) {
		t.Fatalf("message = %q, path %s", err.Error(), ps.logPath)
	}
}
