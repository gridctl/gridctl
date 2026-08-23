package modelsync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tailscale/hujson"
)

// OpenCode config generations. The v2 config renamed provider ->
// providers, npm -> package, options -> settings, and requires an env
// list; both shapes stay renderable because upstream config churn is a
// release-note event, not a runtime crash.
const (
	SchemaV1     = "v1"
	SchemaV2     = "v2"
	SchemaDetect = "detect"
)

// OpenCodeRender is one rendered provider stanza: the container key it
// lives under and the subtree value gridctl owns. The top-level model
// key is deliberately not part of the render: users change it through
// the client's own picker, and owning it would turn every switch into
// drift.
type OpenCodeRender struct {
	Schema    string
	Container string
	Value     map[string]any
}

// Pointer returns the RFC 6901 pointer to the owned subtree.
func (r OpenCodeRender) Pointer(providerID string) string {
	return "/" + r.Container + "/" + providerID
}

// RenderOpenCode builds the provider stanza for the resolved schema
// generation. The API key is always an env reference, never a literal.
func RenderOpenCode(p *Policy, schema string) (OpenCodeRender, error) {
	oc := p.Clients.OpenCode
	if oc == nil {
		return OpenCodeRender{}, fmt.Errorf("policy has no clients.opencode block")
	}
	models := map[string]any{
		p.Router.EntryModel: map[string]any{
			"name": p.Router.EntryModel + " (auto)",
		},
	}
	switch schema {
	case SchemaV1:
		return OpenCodeRender{
			Schema:    SchemaV1,
			Container: "provider",
			Value: map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "LiteLLM",
				"options": map[string]any{
					"baseURL": oc.BaseURL,
					"apiKey":  "{env:" + oc.APIKeyEnv + "}",
				},
				"models": models,
			},
		}, nil
	case SchemaV2:
		return OpenCodeRender{
			Schema:    SchemaV2,
			Container: "providers",
			Value: map[string]any{
				"package": "@ai-sdk/openai-compatible",
				"name":    "LiteLLM",
				"settings": map[string]any{
					"baseURL": oc.BaseURL,
				},
				// v2 drops custom providers that omit env entirely; the
				// list also keys the client's own key resolution.
				"env":    []any{oc.APIKeyEnv},
				"models": models,
			},
		}, nil
	}
	return OpenCodeRender{}, fmt.Errorf("unresolved OpenCode schema %q", schema)
}

// ResolveOpenCodeSchema turns the policy's schema declaration into a
// concrete generation, sniffing the target file in detect mode: an
// existing providers key means v2, an existing provider key means v1,
// and an empty or missing file defaults to v1.
func ResolveOpenCodeSchema(declared, configPath string) string {
	switch declared {
	case SchemaV1, SchemaV2:
		return declared
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return SchemaV1
	}
	data, err := parseJSONC(raw)
	if err != nil {
		return SchemaV1
	}
	if _, ok := data["providers"]; ok {
		return SchemaV2
	}
	return SchemaV1
}

// parseJSONC decodes JSON-with-comments into a plain map. Empty or
// whitespace-only input decodes to an empty map, matching how clients
// pre-create empty config files.
func parseJSONC(raw []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	ast, err := hujson.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing JSONC: %w", err)
	}
	ast.Standardize()
	var data map[string]any
	if err := json.Unmarshal(ast.Pack(), &data); err != nil {
		return nil, fmt.Errorf("decoding JSONC: %w", err)
	}
	return data, nil
}
