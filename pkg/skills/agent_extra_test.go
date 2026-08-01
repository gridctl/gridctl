package skills

import "testing"

func TestAgentDefinitionExtraByKey(t *testing.T) {
	def, err := ParseAgentMD([]byte("---\nname: a\ndescription: d\ntools: Read\nmodel: sonnet\n---\n\nBody.\n"))
	if err != nil {
		t.Fatal(err)
	}
	node, ok := def.ExtraByKey("tools")
	if !ok {
		t.Fatal("tools key not found")
	}
	var v string
	if err := node.Decode(&v); err != nil || v != "Read" {
		t.Errorf("tools = %q, err %v", v, err)
	}
	if _, ok := def.ExtraByKey("name"); ok {
		t.Error("typed keys must not appear in Extra")
	}
	if _, ok := def.ExtraByKey("missing"); ok {
		t.Error("absent key reported present")
	}
}
