package pins

import (
	"encoding/json"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestHashTool_OpaqueFieldsIgnored(t *testing.T) {
	base := mcp.Tool{Name: "t", Description: "d", InputSchema: json.RawMessage(`{}`)}
	readOnly := true
	with := base
	with.Title = "Display"
	with.Icons = json.RawMessage(`[{"src":"icon.png"}]`)
	with.Execution = json.RawMessage(`{"taskSupport":"optional"}`)
	with.Meta = json.RawMessage(`{"ui":{"resourceUri":"ui://app"}}`)
	with.Extra = map[string]json.RawMessage{"future": json.RawMessage(`1`)}
	with.Annotations = &mcp.ToolAnnotations{
		ReadOnlyHint: &readOnly,
		Extra:        map[string]json.RawMessage{"x-custom": json.RawMessage(`"keep"`)},
	}

	current, err := hashTool(base)
	if err != nil {
		t.Fatal(err)
	}
	currentWith, err := hashTool(with)
	if err != nil {
		t.Fatal(err)
	}
	if current != currentWith {
		t.Fatalf("hashTool changed for opaque fields: %s vs %s", current, currentWith)
	}

	legacy, err := hashToolLegacy(base)
	if err != nil {
		t.Fatal(err)
	}
	legacyWith, err := hashToolLegacy(with)
	if err != nil {
		t.Fatal(err)
	}
	if legacy != legacyWith {
		t.Fatalf("hashToolLegacy changed for opaque fields: %s vs %s", legacy, legacyWith)
	}
}
