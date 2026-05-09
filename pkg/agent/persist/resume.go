package persist

import (
	"encoding/json"
	"fmt"
)

// ResumePlan is the projection a runtime consumes to restart a run
// from a chosen step. Phase E delivers the persistence half: reading
// the JSONL ledger and producing a plan whose Replay slice carries
// every event the runtime needs to rebuild state up to FromNode.
//
// The runtime-side half (handing the plan to eino's
// compose/checkpoint resume surface through the adapter) lands when
// graph execution drivers do; until then the plan is consumed by the
// CLI's `runs resume` to mark the run as resumed in the ledger and
// surface the rehydrated context to operators.
type ResumePlan struct {
	// RunID is the run being resumed.
	RunID string `json:"run_id"`

	// FromNodeID is the resume point. Empty means "resume from the
	// last completed step" — the runtime defaults to the highest
	// EventNodeExit it sees in the ledger.
	FromNodeID string `json:"from_node_id,omitempty"`

	// Replay is the event slice the runtime replays to rebuild
	// state. Always starts with the EventRunStarted record so
	// metadata (skill, input, parent) is available; ends just
	// before the resume point.
	Replay []Event `json:"replay"`

	// LastSeq is the seq of the last event in Replay. New events
	// emitted on resume must use Seq > LastSeq.
	LastSeq uint64 `json:"last_seq"`

	// PendingApproval is set when the resume target is an in-flight
	// approval gate. Carries the ApprovalID so a CLI consumer can
	// route the user toward `runs approve` instead of resume.
	PendingApproval string `json:"pending_approval,omitempty"`

	// Started captures the EventRunStarted payload as a convenience
	// for runtime initialisation; saves the runtime a second
	// ledger walk.
	Started *RunStartedPayload `json:"started,omitempty"`
}

// BuildResumePlan reads a run's ledger and projects a ResumePlan up
// to the requested resume point. Passing an empty fromNodeID resumes
// from the last completed step (the highest EventNodeExit observed).
func (s *Store) BuildResumePlan(runID, fromNodeID string) (ResumePlan, error) {
	events, err := s.Read(runID)
	if err != nil {
		return ResumePlan{}, err
	}
	if len(events) == 0 {
		return ResumePlan{}, fmt.Errorf("persist: run %q has no events", runID)
	}
	plan := ResumePlan{RunID: runID, FromNodeID: fromNodeID}

	// Identify the last EventNodeExit so the empty-fromNodeID case
	// can fall back to it. The plan's Replay slice is the prefix of
	// events up to and including that exit.
	lastExitIndex := -1
	for i, ev := range events {
		switch ev.Type {
		case EventRunStarted:
			var p RunStartedPayload
			if err := json.Unmarshal(ev.Payload, &p); err == nil {
				plan.Started = &p
			}
		case EventNodeExit:
			lastExitIndex = i
		case EventApprovalRequest:
			var p ApprovalRequestPayload
			if err := json.Unmarshal(ev.Payload, &p); err == nil {
				// An unresolved approval is the resume target by
				// default — surface it so the CLI can route to
				// `runs approve` instead.
				plan.PendingApproval = p.ApprovalID
			}
		case EventApprovalResponse:
			plan.PendingApproval = ""
		}
	}

	cutoff := len(events)
	if fromNodeID != "" {
		cutoff = -1
		for i, ev := range events {
			if ev.Type != EventNodeEnter {
				continue
			}
			var p NodeEnterPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.NodeID == fromNodeID {
				cutoff = i
				break
			}
		}
		if cutoff < 0 {
			return ResumePlan{}, fmt.Errorf("persist: node %q not found in run %q", fromNodeID, runID)
		}
	} else if lastExitIndex >= 0 {
		cutoff = lastExitIndex + 1
	}
	plan.Replay = events[:cutoff]
	if len(plan.Replay) > 0 {
		plan.LastSeq = plan.Replay[len(plan.Replay)-1].Seq
	}
	return plan, nil
}
