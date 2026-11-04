package skills

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
	"gopkg.in/yaml.v3"
)

func TestRequiredHooksFromSkill(t *testing.T) {
	got := RequiredHooksFromSkill(registry.SkillMetadata{
		"requires": `{"hooks":["block-force-push","other"]}`,
	})
	if len(got) != 2 || got[0] != "block-force-push" || got[1] != "other" {
		t.Fatalf("got %v", got)
	}
	if RequiredHooksFromSkill(registry.SkillMetadata{"requires": "not json"}) != nil {
		t.Fatal("non-JSON requires was not ignored")
	}
	if RequiredHooksFromSkill(registry.SkillMetadata{"requires": `{"hooks":[1]}`}) != nil {
		t.Fatal("non-string hooks array was not ignored")
	}
	if RequiredHooksFromSkill(registry.SkillMetadata{"hooks": `["nope"]`}) != nil {
		t.Fatal("top-level hooks passthrough was read")
	}
}

func TestRequiredHooksFromAgent(t *testing.T) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte("requires:\n  hooks: [block-force-push]\n"), &node); err != nil {
		t.Fatal(err)
	}
	def := &AgentDefinition{Extra: []AgentExtraField{{Key: "metadata", Value: node.Content[0]}}}
	got := RequiredHooksFromAgent(def)
	if len(got) != 1 || got[0] != "block-force-push" {
		t.Fatalf("got %v", got)
	}
	var bad yaml.Node
	if err := yaml.Unmarshal([]byte("requires:\n  hooks: [1]\n"), &bad); err != nil {
		t.Fatal(err)
	}
	if RequiredHooksFromAgent(&AgentDefinition{Extra: []AgentExtraField{{Key: "metadata", Value: bad.Content[0]}}}) != nil {
		t.Fatal("non-string agent hooks array was not ignored")
	}
}
