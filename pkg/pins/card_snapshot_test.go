package pins

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestNewCardSnapshot_Immutable(t *testing.T) {
	readOnly := true
	tools := []mcp.Tool{{Name: "send", InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: &readOnly}}}
	card := []byte(`{"name":"untrusted card content"}`)
	snapshot, err := NewCardSnapshot(7, CardIdentity{Card: "https://example.com/card"}, card, tools)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Generation() != 7 {
		t.Fatal("lost generation")
	}
	hash := snapshot.Hash()
	tools[0].Name = "other"
	tools[0].InputSchema[0] = 'x'
	tools[0].OutputSchema[0] = 'x'
	readOnly = false
	card[0] = 'x'
	records := snapshot.Records()
	if len(records) != 3 || records[0].Name != "send" || records[0].InputSchema[0] != '{' || records[0].OutputSchema[0] != '{' || !*records[0].Annotations.ReadOnlyHint {
		t.Fatal("snapshot aliases input")
	}
	for _, record := range records[1:] {
		if !strings.HasPrefix(record.Description, "sha256:") || len(record.Description) != 71 || len(record.InputSchema) != 0 || len(record.OutputSchema) != 0 {
			t.Fatal("hidden record is not digest-only")
		}
	}
	records[0].Name = "changed"
	records[0].InputSchema[0] = 'x'
	*records[0].Annotations.ReadOnlyHint = false
	if snapshot.Records()[0].Name != "send" || !*snapshot.Records()[0].Annotations.ReadOnlyHint {
		t.Fatal("snapshot aliases returned records")
	}
	actual, err := HashTools(snapshot.Records())
	if err != nil || hash != actual || snapshot.Hash() != hash {
		t.Fatal("snapshot hash changed")
	}
}

func TestNewCardSnapshot_IdentityAndByteTrust(t *testing.T) {
	base := CardIdentity{Card: "https://example.com/card"}
	snapshot, err := NewCardSnapshot(1, base, []byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	identityHash := snapshot.Records()[1].Description
	for _, equivalent := range []CardIdentity{{Card: "https://EXAMPLE.COM:443/card", Dialect: "auto"}, base} {
		got, err := NewCardSnapshot(2, equivalent, []byte(" {}"), []mcp.Tool{{Name: "skill-new"}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Records()[2].Description != identityHash {
			t.Fatal("identity changed with generation, tools, or card bytes")
		}
		if got.Hash() == snapshot.Hash() || got.Records()[1].Description == snapshot.Records()[0].Description {
			t.Fatal("byte drift not fingerprinted")
		}
	}
	for _, changed := range []CardIdentity{
		{Card: "https://example.com/other"},
		{Card: base.Card, Endpoint: "https://example.com/rpc"},
		{Card: base.Card, Endpoint: "https://example.com/rpc?agent=other"},
		{Card: base.Card, Dialect: "0.3"},
		{Card: base.Card, Profile: "bedrock"},
	} {
		got, err := NewCardSnapshot(1, changed, []byte(`{}`), nil)
		if err != nil || got.Records()[1].Description == identityHash {
			t.Fatal("configured identity change hidden")
		}
	}
}

func TestNewCardSnapshot_Invalid(t *testing.T) {
	for _, tc := range []struct {
		generation uint64
		identity   CardIdentity
		body       []byte
		tools      []mcp.Tool
	}{
		{0, CardIdentity{Card: "https://example.com"}, []byte(`{}`), nil},
		{1, CardIdentity{Card: "file:///card"}, []byte(`{}`), nil},
		{1, CardIdentity{Card: "https://example.com", Endpoint: "file:///rpc"}, []byte(`{}`), nil},
		{1, CardIdentity{Card: "https://example.com", Dialect: "2.0"}, []byte(`{}`), nil},
		{1, CardIdentity{Card: "https://example.com", Profile: "other"}, []byte(`{}`), nil},
		{1, CardIdentity{Card: "https://example.com"}, nil, nil},
		{1, CardIdentity{Card: "https://example.com"}, []byte(`{}`), []mcp.Tool{{Name: "_agent_card"}}},
		{1, CardIdentity{Card: "https://example.com"}, []byte(`{}`), []mcp.Tool{{Name: "_agent_identity"}}},
		{1, CardIdentity{Card: "https://example.com"}, []byte(`{}`), []mcp.Tool{{Name: "send"}, {Name: "send"}}},
		{1, CardIdentity{Card: "https://example.com"}, []byte(`{}`), []mcp.Tool{{Name: "send", InputSchema: json.RawMessage(`invalid`)}}},
	} {
		if _, err := NewCardSnapshot(tc.generation, tc.identity, tc.body, tc.tools); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
}
