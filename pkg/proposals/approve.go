package proposals

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/pinapprovals"
)

// WriteError is the ErrApplyFailed returned when a target write fails
// after the compare-and-swap. Restored lists files put back to their
// prior bytes; StillChanged lists files the restore could not put back.
type WriteError struct {
	ID           string
	File         string
	Err          error
	Restored     []string
	StillChanged []string
}

func (e *WriteError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "proposal %s failed writing %s: %v.", e.ID, e.File, e.Err)
	if len(e.Restored) > 0 {
		fmt.Fprintf(&b, " Restored %s to their prior content.", joinList(e.Restored))
	}
	if len(e.StillChanged) > 0 {
		fmt.Fprintf(&b, " Could not restore %s; inspect the target's pins diff.", joinList(e.StillChanged))
	}
	b.WriteString(" The proposal is still pending.")
	return b.String()
}

// Unwrap lets errors.Is match ErrApplyFailed.
func (e *WriteError) Unwrap() error { return ErrApplyFailed }

// RestoreFailed reports whether any file is still changed.
func (e *WriteError) RestoreFailed() bool { return len(e.StillChanged) > 0 }

func joinList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

// Approve approves a pending proposal. It refuses ErrBaseStale when the
// target's current composite hash differs from the base, ErrReasonRequired
// when findings exist and no reason is available, and, on the apply path,
// ErrTargetDrift when the target's pin shows drift. The apply path writes
// the diff, re-pins through the target's pin store with the proposal's
// reason and evidence plus proposal:<id>, and marks the proposal approved.
// The record path writes only the verdict. The proposal file is rewritten
// only after target writes and the pin store call have returned. A re-pin
// failure after a successful write returns the approved proposal together
// with an error wrapping ErrPinFailed.
func (s *Store) Approve(ctx context.Context, id string, req ApproveRequest, targets Resolver) (*Proposal, error) {
	if !idPattern.MatchString(id) {
		return nil, fmt.Errorf("proposals: %w: %q", ErrNotFound, id)
	}
	if err := pinapprovals.ValidateReason(req.Reason); err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	extra, err := pinapprovals.ValidateReferences(req.Evidence)
	if err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	by := strings.TrimSpace(req.ApprovedBy)
	if err := pinapprovals.ValidateApprover(by); err != nil {
		return nil, fmt.Errorf("proposals: %w", err)
	}
	bySource := req.ApprovedBySource
	if by == "" {
		bySource = ""
	}
	if targets == nil {
		return nil, fmt.Errorf("proposals: %w: no target resolver", ErrStoreUnavailable)
	}
	enabled, ignore := s.scanSettings()

	var out *Proposal
	var pinErr error
	err = s.withLock(ctx, func(dir string) error {
		p, err := s.load(dir, id)
		if err != nil {
			return err
		}
		if err := requirePending(p); err != nil {
			return err
		}
		target, err := targets.Resolve(ctx, p.Kind, p.Target)
		if err != nil {
			return err
		}
		current, err := target.CurrentHash(ctx)
		if err != nil {
			return err
		}
		if current != p.BaseHash {
			return staleError(p, current)
		}
		prov, err := target.Provenance(ctx)
		if err != nil {
			return err
		}
		path := decidePath(p.Kind, prov)

		reason := req.Reason
		if reason == "" {
			reason = p.Reason
		}
		evidence, err := pinapprovals.ValidateReferences(append(append([]string(nil), p.Evidence...), extra...))
		if err != nil {
			return fmt.Errorf("proposals: %w", err)
		}
		if len(p.Findings) > 0 && reason == "" {
			return fmt.Errorf("proposals: proposal %s carries %d advisory finding(s); pass --reason to approve over them: %w", p.ID, len(p.Findings), ErrReasonRequired)
		}
		verdict := &Verdict{
			By:           by,
			BySource:     bySource,
			Reason:       reason,
			Evidence:     evidence,
			ApprovedHash: p.BaseHash,
		}

		if path == PathApply {
			if err := s.applyPath(ctx, p, target, verdict, enabled, ignore); err != nil {
				if !errors.Is(err, ErrPinFailed) {
					return err
				}
				pinErr = err
			}
		}
		verdict.At = time.Now().UTC().Truncate(time.Second)
		p.Status = StatusApproved
		p.Path = path
		p.Provenance = prov
		p.Verdict = verdict
		if err := s.save(dir, p); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	slog.Info("proposals: approved proposal",
		"id", id,
		"kind", out.Kind,
		"target", out.Target,
		"path", out.Path,
		"applied", out.Verdict.Applied,
		"approver_source", bySource,
		"evidence_count", len(out.Verdict.Evidence),
		"has_reason", out.Verdict.Reason != "",
		"pin_error", out.Verdict.PinError != "",
	)
	if pinErr != nil {
		return out, pinErr
	}
	return out, nil
}

// applyPath performs the apply-path steps in order: drift refusal, the
// in-memory apply and post-apply hash, the reason check over the
// post-apply scan, the final compare-and-swap, the ordered writes with
// restore on failure, and the re-pin. It fills verdict on success.
func (s *Store) applyPath(ctx context.Context, p *Proposal, target Target, verdict *Verdict, enabled bool, ignore []string) error {
	status, err := target.PinStatus(ctx)
	if err != nil {
		return err
	}
	if status == PinStatusDrift {
		return fmt.Errorf("proposals: %s %s has unreviewed pin drift; approve or reset it first (run '%s'). The proposal and the target are unchanged: %w",
			p.Kind, p.Target, pinsDiffPlain(p.Kind, p.Target), ErrTargetDrift)
	}
	patches, err := ParseUnified(p.Diff)
	if err != nil {
		return fmt.Errorf("proposals: proposal %s: stored diff: %w", p.ID, err)
	}
	current, err := target.ReadFiles(ctx, patchPaths(patches))
	if err != nil {
		return err
	}
	applied, err := applyAll(current, patches)
	if err != nil {
		return fmt.Errorf("proposals: %s: %w. Nothing was written", p.ID, err)
	}
	newHash, err := target.PostApplyHash(ctx, applied)
	if err != nil {
		return fmt.Errorf("proposals: %s: %w. Nothing was written", p.ID, err)
	}
	findings, err := target.Scan(ctx, applied, enabled, ignore)
	if err != nil {
		return err
	}
	if len(findings) > 0 && verdict.Reason == "" {
		return fmt.Errorf("proposals: proposal %s carries %d advisory finding(s); pass --reason to approve over them: %w", p.ID, len(findings), ErrReasonRequired)
	}
	logEvidence, err := pinapprovals.ValidateReferences(append(append([]string(nil), verdict.Evidence...), "proposal:"+p.ID))
	if err != nil {
		return fmt.Errorf("proposals: approval evidence plus proposal:%s: %w", p.ID, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	again, err := target.CurrentHash(ctx)
	if err != nil {
		return err
	}
	if again != p.BaseHash {
		return staleError(p, again)
	}
	restore, err := target.Write(ctx, applied)
	if err != nil {
		var fw *fileWriteError
		if !errors.As(err, &fw) {
			return err
		}
		werr := &WriteError{ID: p.ID, File: fw.File, Err: fw.Err}
		if restore != nil {
			werr.Restored, werr.StillChanged = restore()
		}
		return werr
	}
	verdict.Applied = true
	verdict.NewHash = newHash
	verdict.Stack = target.Stack()
	if err := target.Repin(ctx, RepinRequest{
		ExpectedHash:     newHash,
		Reason:           verdict.Reason,
		Evidence:         logEvidence,
		ApprovedBy:       verdict.By,
		ApprovedBySource: verdict.BySource,
	}); err != nil {
		verdict.PinError = err.Error()
		return fmt.Errorf("proposal %s was applied to %s %s but re-pinning failed: %v. Run '%s' to clear the drift: %w",
			p.ID, p.Kind, p.Target, err, pinsApproveCommand(p.Kind, p.Target), ErrPinFailed)
	}
	return nil
}

func staleError(p *Proposal, current string) error {
	return fmt.Errorf("proposals: proposal %s is based on hash %s; %s %s is now %s (run '%s' or 'gridctl proposals base %s %s' to re-derive the base). The proposal and the target are unchanged: %w",
		p.ID, shortHash(p.BaseHash), p.Kind, p.Target, shortHash(current), pinsDiffCommand(p.Kind, p.Target), p.Kind, p.Target, ErrBaseStale)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func pinsDiffPlain(kind, name string) string {
	if kind == KindContext {
		return "gridctl ctx pins diff " + name
	}
	return "gridctl skill pins diff " + name
}

func pinsApproveCommand(kind, name string) string {
	if kind == KindContext {
		return "gridctl ctx pins approve " + name
	}
	return "gridctl skill pins approve " + name
}
