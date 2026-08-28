package config

import "testing"

func TestDiagnoseDeclarations_ReportsDeterministicAdvisories(t *testing.T) {
	required := true
	plaintext := false
	stack := &Stack{
		Variables: map[string]VariableDeclaration{
			"MISSING": {Required: &required},
			"TOKEN":   {Secret: &plaintext, Type: "json"},
		},
		References: ReferenceIndex{"MISSING": {{Kind: RefKindStack, Field: "name"}}},
	}
	diagnostics := DiagnoseDeclarations(stack, map[string]VariableMetadata{
		"TOKEN": {Type: "string", Secret: true, Deprecated: "use NEW_TOKEN"},
	}, false)
	want := []string{"required_unset", "declared_unused", "deprecated", "sensitivity_mismatch", "type_mismatch"}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for i, code := range want {
		if diagnostics[i].Code != code {
			t.Fatalf("diagnostics[%d].Code = %q, want %q", i, diagnostics[i].Code, code)
		}
	}
}
