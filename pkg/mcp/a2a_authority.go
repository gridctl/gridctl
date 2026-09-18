package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	errCapabilityInProgress = &capabilityError{code: "operation_in_progress", retryable: true}
	errCapabilitySuperseded = &capabilityError{code: "operation_superseded"}
	errCapabilityUncertain  = &capabilityError{code: "operation_uncertain"}
	errCapabilityConflict   = &capabilityError{code: "protocol_conflict"}
	errCapabilityRandom     = &capabilityError{code: "capability_random_unavailable"}
)

// Identity is supplied by trusted construction, never by tool arguments. A
// generation cannot change its identity or approved card in place.
type capabilityIdentity struct {
	server, card, endpoint, dialect, profile, cardDigest string
}

type capabilityRoot struct {
	generation    *capabilityGeneration
	session       string
	expires       time.Time
	lease         time.Time
	contextID     string
	contextDigest [32]byte
	delivered     bool
	unknownWork   bool
	expired       bool
	charges       []*capabilityReservation
	tasks         map[*capabilityTask]struct{}
	send, control *capabilityOperation
}

type capabilityTask struct {
	root            *capabilityRoot
	digest          [32]byte
	remoteID, state string
	parent          bool
	revision        uint64
	uncertain       bool
}

// An empty session deliberately gives generic agents one collision namespace.
// Invented local root IDs cannot provide isolation that the remote lacks.
type capabilityRemoteKey struct{ session, kind, id string }

type capabilityOperationKind uint8

const (
	capabilitySend capabilityOperationKind = iota
	capabilityGet
	capabilityCancel
)

type capabilityOperation struct {
	root                      *capabilityRoot
	task                      *capabilityTask
	kind                      capabilityOperationKind
	reservation               *capabilityReservation
	ctx                       context.Context
	revision                  uint64
	dispatched, done, fresh   bool
	contextHandle, taskHandle string
}

// Only routing fields are accepted here, after the transport has validated the
// complete response. Application content never enters this store.
type capabilityResponse struct {
	contextID, taskID, state string
	references               []capabilityReference
}

type capabilityReference struct{ contextID, taskID string }

type capabilityDelivery struct {
	contextHandle, taskHandle   string
	contextExpires, taskExpires time.Time
}

type capabilityRouting struct{ contextID, taskID, session string }

func (s *CapabilityStore) boundGeneration(identity capabilityIdentity) (*capabilityGeneration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if identity.card == "" || identity.endpoint == "" || identity.cardDigest == "" ||
		(identity.dialect != "1.0" && identity.dialect != "0.3") ||
		(identity.profile != "" && identity.profile != "bedrock") {
		return nil, errCapabilityUnavailable
	}
	return s.newGenerationLocked(identity)
}

func (s *CapabilityStore) mintLocked(kind string) (string, error) {
	var entropy [32]byte
	if _, err := io.ReadFull(s.random, entropy[:]); err != nil {
		return "", errCapabilityRandom
	}
	return "gca2a_" + kind + "1_" + base64.RawURLEncoding.EncodeToString(entropy[:]), nil
}

func capabilityDigest(handle, kind string) ([32]byte, bool) {
	if len(handle) != 52 || !strings.HasPrefix(handle, "gca2a_"+kind+"1_") {
		return [32]byte{}, false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(handle[9:])
	if err != nil || len(decoded) != 32 {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(handle)), true
}

func (g *capabilityGeneration) unavailableLocked() error {
	if g.failedTokens >= 1 {
		g.failedTokens--
	}
	return errCapabilityUnavailable
}

func (g *capabilityGeneration) admit(ctx context.Context, kind capabilityOperationKind, contextHandle, taskHandle string) (*capabilityOperation, error) {
	s := g.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g.retired || s.closed || g.identity.cardDigest == "" {
		return nil, errCapabilityUnavailable
	}
	now := s.now()
	g.failedTokens = min(100, g.failedTokens+max(0, now.Sub(g.failedAt).Seconds())*100)
	g.failedAt = now
	if g.failedTokens < 1 {
		return nil, errCapabilityUnavailable
	}
	s.sweepLocked()
	if kind > capabilityCancel || (kind != capabilitySend && contextHandle != "") || (kind != capabilitySend && taskHandle == "") || (kind == capabilitySend && taskHandle != "" && contextHandle == "") {
		return nil, g.unavailableLocked()
	}
	var root *capabilityRoot
	var task *capabilityTask
	if contextHandle != "" {
		digest, valid := capabilityDigest(contextHandle, "c")
		if !valid {
			return nil, g.unavailableLocked()
		}
		root = g.contexts[digest]
		if root == nil || root.expired {
			return nil, g.unavailableLocked()
		}
	}
	if taskHandle != "" {
		digest, valid := capabilityDigest(taskHandle, "t")
		if !valid {
			return nil, g.unavailableLocked()
		}
		task = g.tasks[digest]
		if task == nil || task.root.expired || (root != nil && (task.root != root || !task.parent)) {
			return nil, g.unavailableLocked()
		}
		root = task.root
	}
	if root != nil {
		if kind == capabilityCancel {
			if root.control != nil || (root.send != nil && (root.send.kind != capabilitySend || root.send.task != task)) {
				return nil, errCapabilityInProgress
			}
		} else if root.send != nil || root.control != nil {
			return nil, errCapabilityInProgress
		}
		if kind == capabilitySend {
			if root.unknownWork || root.hasUncertainLocked() {
				return nil, errCapabilityUncertain
			}
			if task != nil && task.state != "input-required" && task.state != "auth-required" {
				return nil, g.unavailableLocked()
			}
		}
	}
	counts := capabilityCounts{}
	fresh := root == nil
	if fresh {
		counts.roots = 1
		counts.tombstones = 1
	}
	if kind == capabilitySend && task == nil {
		counts.tasks = 1
		counts.tombstones++
	}
	callCtx, reservation, err := g.reserveLocked(ctx, counts)
	if err != nil {
		return nil, err
	}
	rollback := func(err error) (*capabilityOperation, error) {
		_ = reservation.finishLocked(capabilityCounts{})
		return nil, err
	}
	if fresh {
		contextHandle, err = s.mintLocked("c")
		if err != nil {
			return rollback(err)
		}
		var session [16]byte
		if _, err = io.ReadFull(s.random, session[:]); err != nil {
			return rollback(errCapabilityRandom)
		}
		session[6] = (session[6] & 0x0f) | 0x40
		session[8] = (session[8] & 0x3f) | 0x80
		encoded := hex.EncodeToString(session[:])
		root = &capabilityRoot{generation: g, session: encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], expires: now.Add(24 * time.Hour), tasks: make(map[*capabilityTask]struct{})}
	}
	if kind == capabilitySend && task == nil {
		taskHandle, err = s.mintLocked("t")
		if err != nil {
			return rollback(err)
		}
	}
	op := &capabilityOperation{root: root, task: task, kind: kind, reservation: reservation, ctx: callCtx, fresh: fresh, contextHandle: contextHandle, taskHandle: taskHandle}
	if fresh {
		g.roots[root] = struct{}{}
	}
	if kind == capabilityCancel {
		root.control = op
		if root.send != nil {
			task.revision++
			task.uncertain = true
		}
	} else {
		root.send = op
	}
	if task != nil {
		op.revision = task.revision
	}
	return op, nil
}

func (r *capabilityRoot) hasUncertainLocked() bool {
	for task := range r.tasks {
		if task.uncertain {
			return true
		}
	}
	return false
}

// dispatch is called after freshness checks, immediately before network I/O.
// Retirement cancels ctx; a dispatched request may already have reached remote.
func (op *capabilityOperation) dispatch() (capabilityRouting, error) {
	s := op.root.generation.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := op.validLocked(); err != nil {
		return capabilityRouting{}, err
	}
	if op.dispatched {
		return capabilityRouting{}, errCapabilityUnavailable
	}
	if op.task != nil && op.revision != op.task.revision {
		return capabilityRouting{}, errCapabilitySuperseded
	}
	op.dispatched = true
	routing := capabilityRouting{contextID: op.root.contextID, session: op.root.session}
	if op.task != nil {
		routing.taskID = op.task.remoteID
	}
	return routing, nil
}

func (op *capabilityOperation) validLocked() error {
	if op.done || op.root.generation.retired || op.root.generation.store.closed || !op.root.generation.store.now().Before(op.root.expires) {
		return errCapabilityUnavailable
	}
	return op.ctx.Err()
}

func validRemoteID(id string) bool { return id != "" && len(id) <= 1024 && utf8.ValidString(id) }

func validCapabilityState(state string) bool {
	switch state {
	case "submitted", "working", "input-required", "auth-required", "completed", "failed", "canceled", "rejected":
		return true
	default:
		return false
	}
}

func interruptedCapabilityState(state string) bool {
	return validCapabilityState(state) && state != "working" && state != "submitted"
}
func terminalCapabilityState(state string) bool {
	return interruptedCapabilityState(state) && state != "input-required" && state != "auth-required"
}

func (r *capabilityRoot) remoteKey(kind, id string) capabilityRemoteKey {
	session := ""
	if r.generation.identity.profile == "bedrock" {
		session = r.session
	}
	return capabilityRemoteKey{session: session, kind: kind, id: id}
}

func (op *capabilityOperation) validateResponseLocked(response capabilityResponse) error {
	r := op.root
	if (response.contextID != "" && !validRemoteID(response.contextID)) ||
		(response.taskID != "" && (!validRemoteID(response.taskID) || !validCapabilityState(response.state))) ||
		(response.taskID == "" && response.state != "") {
		return errCapabilityConflict
	}
	if op.task != nil {
		if response.taskID != op.task.remoteID || response.contextID != r.contextID {
			return errCapabilityConflict
		}
	} else {
		if !op.fresh && response.contextID != r.contextID {
			return errCapabilityConflict
		}
		if op.fresh && response.contextID != "" {
			if _, exists := r.generation.remoteIDs[r.remoteKey("c", response.contextID)]; exists {
				return errCapabilityConflict
			}
		}
		if response.taskID != "" {
			if _, exists := r.generation.remoteIDs[r.remoteKey("t", response.taskID)]; exists {
				return errCapabilityConflict
			}
		}
	}
	for _, ref := range response.references {
		if (ref.contextID != "" && ref.contextID != response.contextID) || (ref.taskID != "" && ref.taskID != response.taskID) {
			return errCapabilityConflict
		}
	}
	return nil
}

func (op *capabilityOperation) commit(response capabilityResponse) (capabilityDelivery, error) {
	s := op.root.generation.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := op.validLocked(); err != nil {
		op.failLocked(op.dispatched)
		return capabilityDelivery{}, err
	}
	if !op.dispatched {
		op.failLocked(false)
		return capabilityDelivery{}, errCapabilityUnavailable
	}
	if err := op.validateResponseLocked(response); err != nil {
		op.failLocked(true)
		return capabilityDelivery{}, err
	}
	if op.task != nil && op.revision != op.task.revision {
		op.finishLocked(capabilityCounts{})
		return capabilityDelivery{}, errCapabilitySuperseded
	}
	r := op.root
	g := r.generation
	keep := capabilityCounts{}
	delivery := capabilityDelivery{}
	if op.fresh && response.contextID != "" {
		r.contextID = response.contextID
		r.contextDigest, _ = capabilityDigest(op.contextHandle, "c")
		g.contexts[r.contextDigest] = r
		g.remoteIDs[r.remoteKey("c", response.contextID)] = struct{}{}
		keep.tombstones++
	}
	if op.kind == capabilitySend && response.contextID != "" {
		delivery.contextHandle = op.contextHandle
		delivery.contextExpires = r.expires.UTC()
	}
	if op.task == nil && response.taskID != "" {
		digest, _ := capabilityDigest(op.taskHandle, "t")
		op.task = &capabilityTask{root: r, digest: digest, remoteID: response.taskID, state: response.state, parent: r.contextID != ""}
		g.tasks[digest] = op.task
		r.tasks[op.task] = struct{}{}
		g.remoteIDs[r.remoteKey("t", response.taskID)] = struct{}{}
		keep.tasks++
		keep.tombstones++
	} else if op.task != nil {
		// A later remote observation cannot revive terminal work. Deliver the
		// remote observation separately, but preserve terminal mutation denial.
		if !terminalCapabilityState(op.task.state) {
			op.task.state = response.state
		}
		if op.kind == capabilityGet && interruptedCapabilityState(response.state) {
			op.task.uncertain = false
		}
	}
	if response.taskID != "" {
		delivery.taskHandle = op.taskHandle
		delivery.taskExpires = r.expires.UTC()
	}
	if op.fresh && (response.contextID != "" || response.taskID != "") {
		keep.roots = 1
		r.delivered = true
	}
	op.finishLocked(keep)
	return delivery, nil
}

// fail marks ambiguous mutations only when dispatch may have reached remote.
// Safe remote errors and pre-dispatch failures pass unknown=false.
func (op *capabilityOperation) fail(unknown bool) {
	op.root.generation.store.mu.Lock()
	defer op.root.generation.store.mu.Unlock()
	op.failLocked(unknown)
}

func (op *capabilityOperation) failLocked(unknown bool) {
	if op.done {
		return
	}
	keep := capabilityCounts{}
	if unknown && op.dispatched && op.kind != capabilityGet {
		if op.task != nil {
			op.task.uncertain = true
		} else {
			op.root.unknownWork = true
			keep = op.reservation.counts
			if op.fresh {
				op.root.lease = minTime(op.root.expires, op.root.generation.store.now().Add(10*time.Minute))
			}
		}
	}
	op.finishLocked(keep)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (op *capabilityOperation) finishLocked(keep capabilityCounts) {
	if op.done {
		return
	}
	op.done = true
	r := op.root
	if r.send == op {
		r.send = nil
	}
	if r.control == op {
		r.control = nil
	}
	op.contextHandle = ""
	op.taskHandle = ""
	if err := op.reservation.finishLocked(keep); err == nil && keep != (capabilityCounts{}) {
		r.charges = append(r.charges, op.reservation)
	}
	if op.fresh && keep == (capabilityCounts{}) {
		delete(r.generation.roots, r)
	}
	r.generation.store.sweepLocked()
}

func (s *CapabilityStore) sweep() { s.mu.Lock(); defer s.mu.Unlock(); s.sweepLocked() }

func (s *CapabilityStore) sweepLocked() {
	now := s.now()
	for g := range s.generations {
		if g.retired {
			continue
		}
		for root := range g.roots {
			if now.Before(root.expires) && (root.lease.IsZero() || now.Before(root.lease)) {
				continue
			}
			root.expired = true
			delete(g.contexts, root.contextDigest)
			for task := range root.tasks {
				delete(g.tasks, task.digest)
			}
			if root.send != nil {
				root.send.reservation.cancel()
			}
			if root.control != nil {
				root.control.reservation.cancel()
			}
			if root.send != nil || root.control != nil {
				continue
			}
			for _, charge := range root.charges {
				charge.releaseLocked(capabilityCounts{roots: charge.counts.roots, tasks: charge.counts.tasks})
			}
			// Undelivered uncertainty has no admitted IDs to protect.
			if !root.delivered {
				for _, charge := range root.charges {
					charge.releaseLocked(charge.counts)
				}
			}
			delete(g.roots, root)
		}
	}
}
