package a2aclient

import (
	"encoding/json"
	"testing"
)

func TestCheckCompatibility_Modes(t *testing.T) {
	card := &Card{DefaultInputModes: []string{"image/png", "text/plain", "application/json"}, DefaultOutputModes: []string{"audio/wav", "application/json", "text/plain", "text/plain"}}
	got, err := CheckCompatibility(card, nil, "1.0", false, false)
	if err != nil || !got.AcceptsData || len(got.OutputModes) != 2 || got.OutputModes[0] != "application/json" {
		t.Fatalf("intersection: %+v %v", got, err)
	}
	skill := &Skill{InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}}
	got, err = CheckCompatibility(card, skill, "1.0", false, false)
	if err != nil || got.AcceptsData || len(got.OutputModes) != 1 {
		t.Fatalf("override: %+v %v", got, err)
	}
	for _, skill := range []*Skill{{InputModes: []string{}}, {InputModes: []string{"application/json"}}, {OutputModes: []string{"image/png"}}} {
		if _, err := CheckCompatibility(card, skill, "1.0", false, false); err == nil {
			t.Fatal("incompatible modes accepted")
		}
	}
	skill = &Skill{InputModes: []string{"text"}, OutputModes: []string{"text"}}
	if _, err := CheckCompatibility(card, skill, "0.3", false, false); err == nil {
		t.Fatal("generic text alias accepted")
	}
	if _, err := CheckCompatibility(card, skill, "0.3", false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckCompatibility(nil, nil, "1.0", false, false); err == nil {
		t.Fatal("nil card accepted")
	}
	if _, err := CheckCompatibility(card, nil, "2.0", false, false); err == nil {
		t.Fatal("unknown dialect accepted")
	}
}

func TestCheckCompatibility_SecurityAlternatives(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		definitions := `{"bearer":{"type":"http","scheme":"Bearer"},"oauth":{"type":"oauth2"},"oidc":{"type":"openIdConnect"},"key":{"type":"apiKey"}}`
		if version == "1.0" {
			definitions = `{"bearer":{"httpAuthSecurityScheme":{"scheme":"Bearer"}},"oauth":{"oauth2SecurityScheme":{}},"oidc":{"openIdConnectSecurityScheme":{}},"key":{"apiKeySecurityScheme":{}}}`
		}
		for _, tc := range []struct {
			requirement  string
			bearer, want bool
		}{
			{`[]`, false, true}, {`[{}]`, false, true},
			{`[{"bearer":[]}]`, true, true}, {`[{"bearer":[]}]`, false, false},
			{`[{"oauth":[]}]`, true, true}, {`[{"oidc":[]}]`, true, true},
			{`[{"unknown":[]}]`, true, false}, {`[{"key":[]}]`, true, false},
			{`[{"oauth":["unknown-scope"]}]`, true, false},
			{`[{"key":[]},{"bearer":[]}]`, true, true},
			{`[{"key":[],"bearer":[]}]`, true, false},
			{`[{"key":[]},{}]`, false, true},
		} {
			requirements := json.RawMessage(tc.requirement)
			if version == "1.0" {
				var alternatives []map[string][]string
				if err := json.Unmarshal(requirements, &alternatives); err != nil {
					t.Fatal(err)
				}
				wrapped := make([]map[string]any, 0, len(alternatives))
				for _, alternative := range alternatives {
					wrapped = append(wrapped, map[string]any{"schemes": alternative})
				}
				requirements, _ = json.Marshal(wrapped)
			}
			card := &Card{DefaultInputModes: []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"}, SecuritySchemes: json.RawMessage(definitions), Security: requirements, SecurityRequirements: requirements}
			_, err := CheckCompatibility(card, nil, version, tc.bearer, false)
			if (err == nil) != tc.want {
				t.Fatalf("%s %s bearer=%v: %v", version, requirements, tc.bearer, err)
			}
			// An explicit empty skill requirement overrides agent requirements.
			if _, err := CheckCompatibility(card, &Skill{Security: json.RawMessage(`[]`), SecurityRequirements: json.RawMessage(`[]`)}, version, false, false); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCheckCompatibility_MalformedSecurity(t *testing.T) {
	for _, requirements := range []string{`null`, ` null `, `{}`, `[null]`, `[{"schemes":null}]`, `[{"schemes":{"bearer":[]},"schemes":{}}]`} {
		card := &Card{DefaultInputModes: []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"}, SecurityRequirements: json.RawMessage(requirements)}
		if _, err := CheckCompatibility(card, nil, "1.0", true, false); err == nil {
			t.Fatalf("malformed requirements accepted: %s", requirements)
		}
	}
}
