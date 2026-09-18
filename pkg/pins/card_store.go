package pins

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/state"
)

// VerifyCard checks only mandatory card/identity trust. Generated tools remain
// subject to the separate legacy verifier. First use is persisted atomically;
// missing identity on an existing record requires approval, not migration TOFU.
func (ps *PinStore) VerifyCard(ctx context.Context, server string, snapshot mcp.PinSnapshot) (mcp.CardPinDecision, error) {
	return ps.verifyCard(ctx, server, snapshot, true)
}

// CheckCard verifies existing evidence without allowing first use or identity
// replacement. A live registration must never re-pin after storage is removed.
func (ps *PinStore) CheckCard(ctx context.Context, server string, snapshot mcp.PinSnapshot) (mcp.CardPinDecision, error) {
	return ps.verifyCard(ctx, server, snapshot, false)
}

func (ps *PinStore) verifyCard(ctx context.Context, server string, snapshot mcp.PinSnapshot, allowTOFU bool) (mcp.CardPinDecision, error) {
	var decision mcp.CardPinDecision
	err := ps.cardTransaction(ctx, func() (bool, error) {
		records, err := validateCardSnapshot(snapshot)
		if err != nil {
			return false, err
		}
		previous := ps.data.Servers[server]
		if previous != nil {
			identity := previous.Tools["_agent_identity"]
			card := previous.Tools["_agent_card"]
			if identity == nil || card == nil {
				return false, nil
			}
			if identity.Description == records["_agent_identity"].Description {
				identityHash, identityErr := hashToolForPin(identity.Hash, records["_agent_identity"])
				cardHash, cardErr := hashToolForPin(card.Hash, records["_agent_card"])
				decision.Trusted = identityErr == nil && cardErr == nil && identityHash == identity.Hash && cardHash == card.Hash
				return false, nil
			}
			decision.NewIdentity = true
		}
		if !allowTOFU {
			return false, nil
		}
		if _, err := ps.pinServer(server, snapshot.Records()); err != nil {
			return false, err
		}
		decision.Trusted, decision.FirstUse = true, true
		return true, nil
	})
	if err != nil {
		return mcp.CardPinDecision{}, err
	}
	return decision, nil
}

// ApproveCard persists a reviewed snapshot. The owning trust service must hold
// its generation/revision lock throughout this transaction and publication.
func (ps *PinStore) ApproveCard(ctx context.Context, server string, snapshot mcp.PinSnapshot) error {
	return ps.cardTransaction(ctx, func() (bool, error) {
		if _, err := validateCardSnapshot(snapshot); err != nil {
			return false, err
		}
		_, err := ps.pinServer(server, snapshot.Records())
		return true, err
	})
}

func validateCardSnapshot(snapshot mcp.PinSnapshot) (map[string]mcp.Tool, error) {
	if snapshot == nil || snapshot.Generation() == 0 {
		return nil, errors.New("a2a: invalid pin snapshot")
	}
	tools := snapshot.Records()
	hash, err := HashTools(tools)
	if err != nil || hash != snapshot.Hash() {
		return nil, errors.New("a2a: invalid pin snapshot")
	}
	records := make(map[string]mcp.Tool, len(tools))
	for _, tool := range tools {
		if _, duplicate := records[tool.Name]; duplicate {
			return nil, errors.New("a2a: invalid pin snapshot")
		}
		records[tool.Name] = tool
	}
	for _, name := range []string{"_agent_card", "_agent_identity"} {
		tool := records[name]
		if !validCardDigest(tool.Description) || len(tool.InputSchema) != 0 || len(tool.OutputSchema) != 0 {
			return nil, errors.New("a2a: invalid pin snapshot")
		}
	}
	return records, nil
}

func validCardDigest(s string) bool {
	if len(s) != 71 || s[:7] != "sha256:" {
		return false
	}
	for _, c := range s[7:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Strictly reload under the cross-process writer lock. Legacy Load's empty-store
// fallback must never turn corrupt or unreadable card trust into first use.
func (ps *PinStore) cardTransaction(ctx context.Context, mutate func() (bool, error)) error {
	err := state.WithLockContext(ctx, "pins-"+ps.stackName, func(ctx context.Context) error {
		ps.mu.Lock()
		defer ps.mu.Unlock()
		if ps.loadFailed {
			return errors.New("a2a: card pin load failed")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ps.ensurePath(); err != nil {
			return errors.New("a2a: card pin storage unavailable")
		}
		data, err := os.ReadFile(ps.path)
		if err != nil && !os.IsNotExist(err) {
			return errors.New("a2a: card pin read failed")
		}
		if os.IsNotExist(err) && len(ps.data.Servers) != 0 {
			return errors.New("a2a: card pin file disappeared")
		}
		candidate := ps.emptyPinFile()
		if err == nil {
			if json.Unmarshal(data, candidate) != nil {
				return errors.New("a2a: card pin file invalid")
			}
			switch candidate.Version {
			case "", "1", fileVersion:
			default:
				return errors.New("a2a: card pin file version unsupported")
			}
			if candidate.Servers == nil {
				candidate.Servers = make(map[string]*ServerPins)
			}
			for _, server := range candidate.Servers {
				if server == nil {
					return errors.New("a2a: card pin file invalid")
				}
				for _, record := range server.Tools {
					if record == nil {
						return errors.New("a2a: card pin file invalid")
					}
				}
			}
		}
		previous := ps.data
		ps.data = candidate
		write, err := mutate()
		if err == nil {
			err = ctx.Err()
		}
		if err == nil && write {
			err = ps.saveLocked()
		}
		if err != nil {
			ps.data = previous
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return errors.New("a2a: card pin transaction failed")
		}
		return nil
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err != nil {
		return errors.New("a2a: card pin storage failed")
	}
	return nil
}
