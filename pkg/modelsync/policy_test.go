package modelsync

import (
	"testing"
)

func TestParsePolicy_UnknownKeysLandInExtra(t *testing.T) {
	p, err := ParsePolicy([]byte(`name: x
kind: models
router: {entry_model: r, default_tier: MEDIUM}
backends: [a]
tiers: {SIMPLE: a, MEDIUM: a, COMPLEX: a, REASONING: a}
future_feature: keepme
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.Extra["future_feature"] != "keepme" {
		t.Errorf("Extra = %v", p.Extra)
	}
	if _, ok := p.Extra["router"]; ok {
		t.Error("known keys must not land in Extra")
	}
}

func TestPolicyHash_Stable(t *testing.T) {
	data := []byte("name: x\nkind: models\n")
	a, _ := ParsePolicy(data)
	b, _ := ParsePolicy(data)
	if a.Hash() != b.Hash() {
		t.Error("hash must be stable for identical bytes")
	}
	c, _ := ParsePolicy([]byte("name: y\nkind: models\n"))
	if a.Hash() == c.Hash() {
		t.Error("hash must change with content")
	}
}

func TestInitFromTemplate(t *testing.T) {
	m := NewManagerWithHome(t.TempDir())
	if err := m.InitFromTemplate("", false); err != nil {
		t.Fatal(err)
	}
	if !m.HasPolicy() {
		t.Fatal("policy not written")
	}
	if err := m.InitFromTemplate("local-only", false); err == nil {
		t.Error("must refuse to overwrite without force")
	}
	if err := m.InitFromTemplate("local-only", true); err != nil {
		t.Errorf("force overwrite failed: %v", err)
	}
	if err := m.InitFromTemplate("no-such", true); err == nil {
		t.Error("unknown template must error")
	}
}
