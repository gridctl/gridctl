package mcp

import (
	"context"
	"errors"
	"time"
)

// CardRefetch obtains a complete snapshot with an unconditional bounded card GET.
// Implementations bypass freshness reuse but retain bounded failure backoff.
type CardRefetch func(context.Context) (PinSnapshot, error)

type cardTrustEntry struct {
	approved, pending PinSnapshot
	generation        uint64
	revision          uint64
	blocked           bool
	retired           bool
	refetch           CardRefetch
	retire            func()
}

// CardTrustService serializes registration, observation, and approval publication.
// It is independent of legacy schema pinning and owns only the card block. Other
// gateway block reasons must still be checked after this service grants trust.
type CardTrustService struct {
	gate    chan struct{}
	storage CardPinStorage
	entries map[string]*cardTrustEntry
	closed  bool
}

// NewCardTrustService provisions mandatory trust even if storage is unavailable.
// A nil store fails every registration closed rather than disabling verification.
func NewCardTrustService(storage CardPinStorage) *CardTrustService {
	return &CardTrustService{gate: make(chan struct{}, 1), storage: storage, entries: make(map[string]*cardTrustEntry)}
}

func (s *CardTrustService) lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-s.gate
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *CardTrustService) unlock() { <-s.gate }

// Register replaces only an older generation. Retire must be a nonblocking local
// authority teardown callback and must not reenter this service. Replacement
// retires the old instance even if the new storage verification fails.
func (s *CardTrustService) Register(ctx context.Context, server string, snapshot PinSnapshot, refetch CardRefetch, retire func()) (CardPinDecision, error) {
	if snapshot == nil || snapshot.Generation() == 0 || refetch == nil || retire == nil {
		return CardPinDecision{}, errors.New("a2a: invalid trust registration")
	}
	if err := s.lock(ctx); err != nil {
		return CardPinDecision{}, err
	}
	defer s.unlock()
	if s.closed {
		return CardPinDecision{}, errors.New("a2a: card trust closed")
	}
	if old := s.entries[server]; old != nil {
		if snapshot.Generation() <= old.generation {
			return CardPinDecision{}, errors.New("a2a: stale trust generation")
		}
		old.retireOnce()
	}
	entry := &cardTrustEntry{pending: snapshot, generation: snapshot.Generation(), revision: 1, blocked: true, refetch: refetch, retire: retire}
	s.entries[server] = entry
	if s.storage == nil {
		return CardPinDecision{}, errors.New("a2a: card pin storage unavailable")
	}
	decision, err := s.storage.VerifyCard(ctx, server, snapshot)
	if err != nil {
		return CardPinDecision{}, errors.New("a2a: card pin verification failed")
	}
	if decision.Trusted {
		entry.approved, entry.pending, entry.blocked = snapshot, nil, false
	}
	return decision, nil
}

func (e *cardTrustEntry) retireOnce() {
	if !e.retired {
		e.retired = true
		e.retire()
	}
}

// Observe records a successfully refreshed snapshot. Card drift retires authority
// immediately and preserves approved evidence until explicit approval. Errors
// also fail the card gate closed; callers must not dispatch using stale trust.
func (s *CardTrustService) Observe(ctx context.Context, server string, snapshot PinSnapshot) error {
	if snapshot == nil {
		return errors.New("a2a: invalid pin snapshot")
	}
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	e := s.entries[server]
	if e == nil || e.refetch == nil || e.generation != snapshot.Generation() {
		return errors.New("a2a: stale trust generation")
	}
	identity := e.approved
	if identity == nil {
		identity = e.pending
	}
	if snapshotIdentity(identity) != snapshotIdentity(snapshot) {
		return errors.New("a2a: configured identity changed within registration")
	}
	if e.pending == nil || e.pending.Hash() != snapshot.Hash() {
		e.revision++
	}
	e.pending = snapshot
	if e.approved != nil && snapshotRecord(e.approved, "_agent_card") != snapshotRecord(snapshot, "_agent_card") {
		e.blocked = true
		e.retireOnce()
		return errors.New("a2a: card approval required")
	}
	if s.storage == nil {
		e.blocked = true
		return errors.New("a2a: card pin storage unavailable")
	}
	decision, err := s.storage.CheckCard(ctx, server, snapshot)
	if err != nil {
		e.blocked = true
		return errors.New("a2a: card pin verification failed")
	}
	if !decision.Trusted || e.retired {
		e.blocked = true
		e.retireOnce()
		return errors.New("a2a: card approval required")
	}
	e.approved, e.pending, e.blocked = snapshot, nil, false
	return nil
}

// Snapshot returns pending evidence for diff/approval, otherwise approved
// evidence. Neither inventory is a callable tool catalog.
func (s *CardTrustService) Snapshot(ctx context.Context, server string) (PinSnapshot, error) {
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	e := s.entries[server]
	if e == nil || e.refetch == nil {
		return nil, errors.New("a2a: trust registration unavailable")
	}
	if e.pending != nil {
		return e.pending, nil
	}
	return e.approved, nil
}

// Approved checks the card block and generation without clearing any gateway
// policy block. Admission must perform this check again after a refresh wait.
func (s *CardTrustService) Approved(ctx context.Context, server string, generation uint64) (PinSnapshot, error) {
	if err := s.lock(ctx); err != nil {
		return nil, err
	}
	defer s.unlock()
	e := s.entries[server]
	if e == nil || e.generation != generation || e.blocked || e.approved == nil {
		return nil, errors.New("a2a: card approval required")
	}
	return e.approved, nil
}

// Approve binds an unconditional fetch to both the reviewed hash and the current
// registration/revision. Network I/O never holds the service gate. Persistence
// and publication share the gate, so a replacement cannot be unblocked by an old
// fetch. Failure leaves the current block and approved evidence untouched.
func (s *CardTrustService) Approve(ctx context.Context, server, expected string) error {
	if expected == "" {
		return errors.New("a2a: expected_server_hash is required")
	}
	if err := s.lock(ctx); err != nil {
		return err
	}
	e := s.entries[server]
	if e == nil || e.refetch == nil || s.storage == nil {
		s.unlock()
		return errors.New("a2a: trust registration unavailable")
	}
	current := e.pending
	if current == nil {
		current = e.approved
	}
	if current == nil || current.Hash() != expected {
		s.unlock()
		return errors.New("a2a: stale card approval")
	}
	generation, revision, fetch := e.generation, e.revision, e.refetch
	s.unlock()
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	fresh, err := fetch(fetchCtx)
	if err == nil {
		err = fetchCtx.Err()
	}
	cancel()
	if err != nil || fresh == nil {
		return errors.New("a2a: approval card refresh failed")
	}
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	e = s.entries[server]
	if e == nil || e.generation != generation || e.revision != revision || fresh.Generation() != generation {
		return errors.New("a2a: stale card approval")
	}
	if snapshotIdentity(fresh) != snapshotIdentity(current) {
		return errors.New("a2a: configured identity changed within registration")
	}
	if fresh.Hash() != expected {
		e.pending, e.blocked = fresh, true
		e.revision++
		e.retireOnce()
		return errors.New("a2a: stale card approval")
	}
	if err := s.storage.ApproveCard(ctx, server, fresh); err != nil {
		return errors.New("a2a: card pin approval failed")
	}
	e.approved, e.pending, e.blocked = fresh, nil, false
	e.retired = false
	e.revision++
	return nil
}

func snapshotIdentity(snapshot PinSnapshot) string {
	return snapshotRecord(snapshot, "_agent_identity")
}

func snapshotRecord(snapshot PinSnapshot, name string) string {
	if snapshot != nil {
		for _, record := range snapshot.Records() {
			if record.Name == name {
				return record.Description
			}
		}
	}
	return ""
}

// Unregister retires only the requested registration. A delayed teardown cannot
// remove a replacement. The generation marker remains to reject stale reuse.
func (s *CardTrustService) Unregister(ctx context.Context, server string, generation uint64) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	if e := s.entries[server]; e != nil && e.generation == generation {
		e.unregister()
	}
	return nil
}

func (e *cardTrustEntry) unregister() {
	e.retireOnce()
	e.blocked, e.refetch, e.retire = true, nil, nil
	e.approved, e.pending = nil, nil
	e.revision++
}

func (s *CardTrustService) unregisterCurrent(ctx context.Context, server string) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	if e := s.entries[server]; e != nil {
		e.unregister()
	}
	return nil
}

func (s *CardTrustService) close(ctx context.Context) error {
	if err := s.lock(ctx); err != nil {
		return err
	}
	defer s.unlock()
	s.closed = true
	for _, e := range s.entries {
		e.unregister()
	}
	return nil
}
