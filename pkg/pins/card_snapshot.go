package pins

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"

	"github.com/gridctl/gridctl/pkg/a2aclient"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// CardIdentity contains only configured agent identity, never credentials or
// discovery-derived endpoint changes. Timeout and skill selection are not identity.
type CardIdentity struct {
	Card, Endpoint, Dialect, Profile string
}

// BuildCardSnapshot supplies the gateway adapter with the existing pin hashing
// and immutable snapshot format, independently of legacy verifier settings.
func (s *PinStore) BuildCardSnapshot(generation uint64, cardURL, endpoint, dialect, profile string, card []byte, tools []mcp.Tool) (mcp.PinSnapshot, error) {
	return NewCardSnapshot(generation, CardIdentity{Card: cardURL, Endpoint: endpoint, Dialect: dialect, Profile: profile}, card, tools)
}

type cardSnapshot struct {
	records    []mcp.Tool
	hash       string
	generation uint64
}

func (s *cardSnapshot) Records() []mcp.Tool { return cloneSnapshotTools(s.records) }
func (s *cardSnapshot) Hash() string        { return s.hash }
func (s *cardSnapshot) Generation() uint64  { return s.generation }

// NewCardSnapshot captures complete unfiltered tools and digest-only card and
// configured-identity records. Raw card bytes are neither persisted nor retained.
func NewCardSnapshot(generation uint64, identity CardIdentity, card []byte, tools []mcp.Tool) (mcp.PinSnapshot, error) {
	if generation == 0 || len(card) == 0 || len(card) > 1<<20 {
		return nil, errors.New("a2a: invalid pin snapshot")
	}
	digest, err := identityDigest(identity)
	if err != nil {
		return nil, err
	}
	records := cloneSnapshotTools(tools)
	seen := make(map[string]bool, len(records))
	for _, tool := range records {
		if tool.Name == "" || tool.Name == "_agent_card" || tool.Name == "_agent_identity" || seen[tool.Name] {
			return nil, errors.New("a2a: invalid pin record name")
		}
		seen[tool.Name] = true
	}
	cardSum := sha256.Sum256(card)
	records = append(records,
		mcp.Tool{Name: "_agent_card", Description: "sha256:" + hex.EncodeToString(cardSum[:])},
		mcp.Tool{Name: "_agent_identity", Description: "sha256:" + digest},
	)
	hash, err := HashTools(records)
	if err != nil {
		return nil, errors.New("a2a: invalid pin schema")
	}
	return &cardSnapshot{records: records, hash: hash, generation: generation}, nil
}

func identityDigest(identity CardIdentity) (string, error) {
	card, err := a2aclient.ParseURL(identity.Card)
	if err != nil {
		return "", err
	}
	endpoint := "absent"
	if identity.Endpoint != "" {
		parsed, err := a2aclient.ParseURL(identity.Endpoint)
		if err != nil {
			return "", err
		}
		endpoint = "present:" + parsed.String()
	}
	dialect := identity.Dialect
	if dialect == "" {
		dialect = "auto"
	}
	if dialect != "auto" && dialect != "1.0" && dialect != "0.3" || identity.Profile != "" && identity.Profile != "bedrock" {
		return "", errors.New("a2a: invalid configured identity")
	}
	hash := sha256.New()
	for _, field := range []string{card.String(), endpoint, dialect, identity.Profile} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(field)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(field))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func cloneSnapshotTools(tools []mcp.Tool) []mcp.Tool {
	out := make([]mcp.Tool, len(tools))
	for i, tool := range tools {
		out[i] = tool
		out[i].InputSchema = append([]byte(nil), tool.InputSchema...)
		out[i].OutputSchema = append([]byte(nil), tool.OutputSchema...)
		if tool.Annotations != nil {
			annotations := *tool.Annotations
			annotations.ReadOnlyHint = cloneHint(annotations.ReadOnlyHint)
			annotations.DestructiveHint = cloneHint(annotations.DestructiveHint)
			annotations.IdempotentHint = cloneHint(annotations.IdempotentHint)
			annotations.OpenWorldHint = cloneHint(annotations.OpenWorldHint)
			out[i].Annotations = &annotations
		}
	}
	return out
}

func cloneHint(hint *bool) *bool {
	if hint == nil {
		return nil
	}
	value := *hint
	return &value
}
