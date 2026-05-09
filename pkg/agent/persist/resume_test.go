package persist

import (
	"testing"
)

func TestBuildResumePlanFromNode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	defer store.CloseAll() //nolint:errcheck // best-effort

	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(EventRunStarted, RunStartedPayload{Skill: "x", Flavor: "ts"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	for _, id := range []string{"n1", "n2", "n3"} {
		if _, err := rec.Record(EventNodeEnter, NodeEnterPayload{NodeID: id}); err != nil {
			t.Fatalf("Record: %v", err)
		}
		if _, err := rec.Record(EventNodeExit, NodeExitPayload{NodeID: id, Success: true}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	plan, err := store.BuildResumePlan(runID, "n2")
	if err != nil {
		t.Fatalf("BuildResumePlan: %v", err)
	}
	if plan.FromNodeID != "n2" {
		t.Fatalf("expected FromNodeID=n2, got %q", plan.FromNodeID)
	}
	// Replay should be: run_started, n1.enter, n1.exit (3 events) — i.e. up to but not including n2 enter.
	if len(plan.Replay) != 3 {
		t.Fatalf("expected 3 replay events, got %d", len(plan.Replay))
	}
	if plan.Replay[0].Type != EventRunStarted {
		t.Fatalf("expected RunStarted first, got %s", plan.Replay[0].Type)
	}
	if plan.Replay[2].Type != EventNodeExit {
		t.Fatalf("expected NodeExit last in replay, got %s", plan.Replay[2].Type)
	}
	if plan.LastSeq != 3 {
		t.Fatalf("expected LastSeq=3, got %d", plan.LastSeq)
	}
	if plan.Started == nil || plan.Started.Skill != "x" {
		t.Fatalf("expected Started.Skill=x, got %+v", plan.Started)
	}
}

func TestBuildResumePlanDefaultsToLastExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	defer store.CloseAll() //nolint:errcheck // best-effort

	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(EventRunStarted, RunStartedPayload{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventNodeEnter, NodeEnterPayload{NodeID: "n1"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventNodeExit, NodeExitPayload{NodeID: "n1", Success: true}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventNodeEnter, NodeEnterPayload{NodeID: "n2"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	plan, err := store.BuildResumePlan(runID, "")
	if err != nil {
		t.Fatalf("BuildResumePlan: %v", err)
	}
	// Should resume after n1.exit — 3 events (run_started, n1.enter, n1.exit).
	if len(plan.Replay) != 3 {
		t.Fatalf("expected 3 replay events, got %d", len(plan.Replay))
	}
	if plan.Replay[len(plan.Replay)-1].Type != EventNodeExit {
		t.Fatalf("expected last replay event to be NodeExit, got %s", plan.Replay[len(plan.Replay)-1].Type)
	}
}

func TestBuildResumePlanNodeNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	defer store.CloseAll() //nolint:errcheck // best-effort

	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(EventRunStarted, RunStartedPayload{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = store.BuildResumePlan(runID, "missing")
	if err == nil {
		t.Fatal("expected error for missing node")
	}
}

func TestBuildResumePlanSurfacesPendingApproval(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := NewStore(dir)
	defer store.CloseAll() //nolint:errcheck // best-effort

	runID := NewRunID()
	rec, err := store.OpenWriter(runID)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if _, err := rec.Record(EventRunStarted, RunStartedPayload{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := rec.Record(EventApprovalRequest, ApprovalRequestPayload{ApprovalID: "ap_1"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	plan, err := store.BuildResumePlan(runID, "")
	if err != nil {
		t.Fatalf("BuildResumePlan: %v", err)
	}
	if plan.PendingApproval != "ap_1" {
		t.Fatalf("expected PendingApproval=ap_1, got %q", plan.PendingApproval)
	}
}
