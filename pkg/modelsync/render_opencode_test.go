package modelsync

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderOpenCode_V1Golden(t *testing.T) {
	p := testPolicy(t)
	got, err := RenderOpenCode(p, SchemaV1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Container != "provider" {
		t.Errorf("container = %q, want provider", got.Container)
	}
	data, _ := json.MarshalIndent(got.Value, "", "  ")
	want := `{
  "models": {
    "smart-router": {
      "name": "smart-router (auto)"
    }
  },
  "name": "LiteLLM",
  "npm": "@ai-sdk/openai-compatible",
  "options": {
    "apiKey": "{env:LITELLM_KEY}",
    "baseURL": "http://localhost:4000/v1"
  }
}`
	if string(data) != want {
		t.Errorf("v1 render:\n%s\nwant:\n%s", data, want)
	}
}

func TestRenderOpenCode_V2Golden(t *testing.T) {
	p := testPolicy(t)
	got, err := RenderOpenCode(p, SchemaV2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Container != "providers" {
		t.Errorf("container = %q, want providers", got.Container)
	}
	data, _ := json.MarshalIndent(got.Value, "", "  ")
	want := `{
  "env": [
    "LITELLM_KEY"
  ],
  "models": {
    "smart-router": {
      "name": "smart-router (auto)"
    }
  },
  "name": "LiteLLM",
  "package": "aisdk:@ai-sdk/openai-compatible",
  "settings": {
    "apiKey": "{env:LITELLM_KEY}",
    "baseURL": "http://localhost:4000/v1"
  }
}`
	if string(data) != want {
		t.Errorf("v2 render:\n%s\nwant:\n%s", data, want)
	}
}

func TestRenderOpenCode_NeverEmitsLiteralSecret(t *testing.T) {
	p := testPolicy(t)
	for _, schema := range []string{SchemaV1, SchemaV2} {
		got, err := RenderOpenCode(p, schema)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(got.Value)
		if schema == SchemaV1 && !strings.Contains(string(data), `"apiKey":"{env:LITELLM_KEY}"`) {
			t.Errorf("v1 must reference the key by env: %s", data)
		}
		if schema == SchemaV2 && !strings.Contains(string(data), `"env":["LITELLM_KEY"]`) {
			t.Errorf("v2 must carry the env list: %s", data)
		}
	}
}
