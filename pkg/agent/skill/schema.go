package skill

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/invopop/jsonschema"
)

// reflectInputSchema produces the JSON Schema for a skill input type.
// It uses invopop/jsonschema with anonymous-struct expansion enabled
// (so the schema is self-contained — no $defs/$ref pointers a model
// would have to chase) and trims schema metadata that providers like
// Anthropic and OpenAI reject.
//
// Two input shapes get special treatment:
//
//   - The empty struct (struct{}) and other zero-field anonymous
//     structs reflect to a no-properties object schema, which most
//     providers accept and read as "any object".
//   - map[string]any (and json.RawMessage) reflect to "any object",
//     letting authors take loose input when they don't want a typed
//     argument shape.
//
// Anything else — primitives, slices at the top level — is rejected
// with an error: an MCP tool input MUST be an object, and surfacing
// the error at registration is far better than at call time.
func reflectInputSchema(zero any) (json.RawMessage, error) {
	t := reflect.TypeOf(zero)
	if t == nil {
		// Untyped nil: treat as "any object" — authors who want truly
		// schemaless input can declare interface{} or map[string]any.
		return json.RawMessage(`{"type":"object"}`), nil
	}

	// Allow explicit "loose object" inputs.
	switch t.Kind() {
	case reflect.Map, reflect.Interface:
		return json.RawMessage(`{"type":"object"}`), nil
	}

	// json.RawMessage and byte slices are also fine — same shape.
	if t == reflect.TypeOf(json.RawMessage(nil)) {
		return json.RawMessage(`{"type":"object"}`), nil
	}

	// Reject scalars at the top level. MCP tool inputs are objects.
	if t.Kind() != reflect.Struct && (t.Kind() != reflect.Ptr || t.Elem().Kind() != reflect.Struct) {
		return nil, fmt.Errorf("skill input must be a struct or map; got %s", t.Kind())
	}

	reflector := &jsonschema.Reflector{
		// Inline definitions: agent runtimes generally do not chase
		// $ref pointers, and the resulting schema is more readable.
		ExpandedStruct: false,
		DoNotReference: true,
		// Keep additional properties forbidden by default — typed
		// skills are picky on purpose.
		AllowAdditionalProperties: false,
	}

	schema := reflector.Reflect(zero)
	if schema == nil {
		return nil, fmt.Errorf("jsonschema reflect produced nil schema for %s", t)
	}

	// Strip schema metadata that providers reject. Anthropic in
	// particular rejects $schema, $id, additionalProperties on the
	// root, and (in some shapes) version. We keep it minimal: type,
	// properties, required.
	schema.Version = ""
	schema.ID = ""
	schema.Anchor = ""

	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshaling schema: %w", err)
	}
	return raw, nil
}
