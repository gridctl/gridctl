package a2aclient

import (
	"encoding/json"
	"strings"
)

// Compatibility is the supported media intersection for one message operation.
type Compatibility struct {
	AcceptsData bool
	OutputModes []string
}

// CheckCompatibility evaluates agent defaults or a skill's explicit overrides.
// Security alternatives are OR; schemes within an alternative are AND. URLs in
// security declarations are descriptive only and are never fetched here.
func CheckCompatibility(card *Card, skill *Skill, version string, bearer, bedrock bool) (Compatibility, error) {
	if card == nil || version != "1.0" && version != "0.3" {
		return Compatibility{}, codecError("invalid_compatibility_request")
	}
	input, output := card.DefaultInputModes, card.DefaultOutputModes
	requirements := card.SecurityRequirements
	if version == "0.3" {
		requirements = card.Security
	}
	if skill != nil {
		if skill.InputModes != nil {
			input = skill.InputModes
		}
		if skill.OutputModes != nil {
			output = skill.OutputModes
		}
		override := skill.SecurityRequirements
		if version == "0.3" {
			override = skill.Security
		}
		if len(override) != 0 {
			requirements = override
		}
	}
	result := Compatibility{}
	text := false
	for _, mode := range input {
		text = text || mode == "text/plain" || bedrock && mode == "text"
		result.AcceptsData = result.AcceptsData || mode == "application/json"
	}
	if !text {
		return Compatibility{}, codecError("text_input_unsupported")
	}
	seen := make(map[string]bool)
	for _, mode := range output {
		if (mode == "text/plain" || mode == "application/json" || bedrock && mode == "text") && !seen[mode] {
			result.OutputModes = append(result.OutputModes, mode)
			seen[mode] = true
		}
	}
	if len(result.OutputModes) == 0 {
		return Compatibility{}, codecError("output_modes_unsupported")
	}
	if !securityCompatible(requirements, card.SecuritySchemes, version, bearer) {
		return Compatibility{}, codecError("authentication_requirements_unsupported")
	}
	return result, nil
}

func securityCompatible(requirements, schemes json.RawMessage, version string, bearer bool) bool {
	if len(requirements) == 0 {
		return true
	}
	var alternatives []json.RawMessage
	if decodeJSON(requirements, &alternatives) != nil || alternatives == nil {
		return false
	}
	if len(alternatives) == 0 {
		return true
	}
	var definitions map[string]json.RawMessage
	if len(schemes) != 0 && decodeJSON(schemes, &definitions) != nil {
		return false
	}
	for _, alternative := range alternatives {
		var required map[string][]string
		if version == "1.0" {
			var wrapped map[string]json.RawMessage
			if json.Unmarshal(alternative, &wrapped) != nil || len(wrapped) != 1 || wrapped["schemes"] == nil {
				continue
			}
			alternative = wrapped["schemes"]
		}
		if json.Unmarshal(alternative, &required) != nil || required == nil {
			continue
		}
		satisfied := true
		for name, scopes := range required {
			// This subset has no scope attestation or token acquisition. A supplied
			// bearer can satisfy a bearer scheme, not an unverified scope claim.
			if len(scopes) != 0 || !bearer || !bearerScheme(definitions[name], version) {
				satisfied = false
				break
			}
		}
		if satisfied {
			return true
		}
	}
	return false
}

func bearerScheme(raw json.RawMessage, version string) bool {
	if version == "0.3" {
		var scheme struct {
			Type   string `json:"type"`
			Scheme string `json:"scheme"`
		}
		if json.Unmarshal(raw, &scheme) != nil {
			return false
		}
		return scheme.Type == "http" && strings.EqualFold(scheme.Scheme, "bearer") || scheme.Type == "oauth2" || scheme.Type == "openIdConnect"
	}
	var scheme map[string]json.RawMessage
	if json.Unmarshal(raw, &scheme) != nil || len(scheme) != 1 {
		return false
	}
	for name, definition := range scheme {
		var body map[string]json.RawMessage
		if json.Unmarshal(definition, &body) != nil || body == nil {
			return false
		}
		switch name {
		case "httpAuthSecurityScheme":
			var method string
			return json.Unmarshal(body["scheme"], &method) == nil && strings.EqualFold(method, "bearer")
		case "oauth2SecurityScheme", "openIdConnectSecurityScheme":
			return true
		}
	}
	return false
}
