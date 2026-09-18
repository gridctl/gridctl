package a2aclient

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// Interface describes an advertised protocol binding. It is not authorization.
type Interface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
	Tenant          string `json:"tenant"`
	Transport       string `json:"transport"`
}

// Card retains the discovery fields needed by the bounded adapter subset.
// Text and raw compatibility declarations are untrusted, not diagnostic values.
type Card struct {
	SupportedInterfaces  []Interface `json:"supportedInterfaces"`
	ProtocolVersion      string      `json:"protocolVersion"`
	URL                  string      `json:"url"`
	PreferredTransport   string      `json:"preferredTransport"`
	AdditionalInterfaces []Interface `json:"additionalInterfaces"`
	Capabilities         struct {
		Extensions []struct {
			Required bool `json:"required"`
		} `json:"extensions"`
	} `json:"capabilities"`
	DefaultInputModes    []string        `json:"defaultInputModes"`
	DefaultOutputModes   []string        `json:"defaultOutputModes"`
	Security             json.RawMessage `json:"security"`
	SecurityRequirements json.RawMessage `json:"securityRequirements"`
	SecuritySchemes      json.RawMessage `json:"securitySchemes"`
	Skills               []Skill         `json:"skills"`
}

// Skill preserves authored descriptions and mode/security overrides.
type Skill struct {
	ID                   string          `json:"id"`
	Description          string          `json:"description"`
	InputModes           []string        `json:"inputModes"`
	OutputModes          []string        `json:"outputModes"`
	Security             json.RawMessage `json:"security"`
	SecurityRequirements json.RawMessage `json:"securityRequirements"`
}

// ParseCard selects version evidence without allowing a dialect override to invent it.
// Every advertised endpoint is validated, including unselected bindings.
func ParseCard(body []byte, dialect string) (*Card, Interface, error) {
	if dialect == "" {
		dialect = "auto"
	}
	if dialect != "auto" && dialect != "1.0" && dialect != "0.3" {
		return nil, Interface{}, errors.New("a2a: invalid configured dialect")
	}
	var card Card
	if len(body) > maxCardBytes || len(body) == 0 || decodeJSON(body, &card) != nil {
		return nil, Interface{}, errors.New("a2a: invalid agent card")
	}
	for _, extension := range card.Capabilities.Extensions {
		if extension.Required {
			return nil, Interface{}, errors.New("a2a: required extension unsupported")
		}
	}
	all := append(append([]Interface(nil), card.SupportedInterfaces...), card.AdditionalInterfaces...)
	if card.URL != "" {
		all = append(all, Interface{URL: card.URL})
	}
	for _, iface := range all {
		if _, err := ParseURL(iface.URL); err != nil {
			return nil, Interface{}, err
		}
	}
	if dialect != "0.3" {
		for _, iface := range card.SupportedInterfaces {
			if iface.ProtocolBinding == "JSONRPC" && versionMinor(iface.ProtocolVersion, "1.0") {
				if iface.Tenant != "" {
					return nil, Interface{}, errors.New("a2a: required tenant unsupported")
				}
				iface.ProtocolVersion = "1.0"
				return &card, iface, nil
			}
		}
	}
	if dialect != "1.0" && versionMinor(card.ProtocolVersion, "0.3") {
		if card.URL != "" && (card.PreferredTransport == "" || card.PreferredTransport == "JSONRPC") {
			return &card, Interface{URL: card.URL, ProtocolBinding: "JSONRPC", ProtocolVersion: "0.3"}, nil
		}
		for _, iface := range card.AdditionalInterfaces {
			if iface.Transport == "JSONRPC" {
				iface.ProtocolBinding, iface.ProtocolVersion = "JSONRPC", "0.3"
				return &card, iface, nil
			}
		}
	}
	// Fixed vocabulary avoids reflecting arbitrary remote version strings.
	return nil, Interface{}, errors.New("a2a: dialect mismatch; configured " + dialect + ", card has no matching 1.0/0.3 JSONRPC interface")
}

func versionMinor(version, minor string) bool {
	if version == minor {
		return true
	}
	patch, ok := strings.CutPrefix(version, minor+".")
	if !ok || patch == "" {
		return false
	}
	for _, c := range patch {
		if c < '0' || c > '9' {
			return false
		}
	}
	_, err := strconv.ParseUint(patch, 10, 32)
	return err == nil
}
