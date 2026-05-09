package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TypedRunner is the typed authoring signature skill authors write.
// I and O are Go structs; the SDK marshals across the boundary so the
// handler body works in typed Go and the wire form stays JSON.
type TypedRunner[I any, O any] func(ctx context.Context, input I) (O, error)

// Define wraps a typed runner as a Definition. The input schema is
// inferred from I via reflectInputSchema (jsonschema struct tags); the
// returned Definition's Invoker:
//
//  1. Re-marshals the argument map back to JSON (the gateway hands
//     skills the decoded shape; the typed boundary needs the raw
//     bytes to feed json.Unmarshal into a Go value of type I).
//  2. Decodes into a fresh I.
//  3. Invokes the runner.
//  4. Renders the typed output O back to MCP content as a single
//     JSON text block. Skills that need richer content shapes
//     (multi-part, image, etc.) should construct a Definition by hand.
//
// Decode failures and runner errors flow back to the caller; the
// runner's err is wrapped, never swallowed.
func Define[I any, O any](name, description string, run TypedRunner[I, O]) (*Definition, error) {
	if name == "" {
		return nil, errors.New("skill.Define: name is required")
	}
	if run == nil {
		return nil, fmt.Errorf("skill %q: run is nil", name)
	}

	var zeroIn I
	schema, err := reflectInputSchema(zeroIn)
	if err != nil {
		return nil, fmt.Errorf("skill %q: inferring input schema: %w", name, err)
	}

	invoker := func(ctx context.Context, arguments map[string]any) (*mcp.ToolCallResult, error) {
		var input I
		if len(arguments) > 0 {
			raw, err := json.Marshal(arguments)
			if err != nil {
				return nil, fmt.Errorf("skill %q: re-marshaling arguments: %w", name, err)
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, fmt.Errorf("skill %q: decoding input: %w", name, err)
			}
		}

		output, err := run(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", name, err)
		}

		raw, err := json.Marshal(output)
		if err != nil {
			return nil, fmt.Errorf("skill %q: marshaling output: %w", name, err)
		}
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(string(raw))},
		}, nil
	}

	return &Definition{
		Name:        name,
		Description: description,
		InputSchema: schema,
		Invoker:     invoker,
	}, nil
}

// MustDefine is the panicking variant of Define. Use it in package-init
// code where a malformed skill is a programming error and the binary
// has nothing useful to do without it. Library code should call
// Define and propagate the error.
func MustDefine[I any, O any](name, description string, run TypedRunner[I, O]) *Definition {
	def, err := Define[I, O](name, description, run)
	if err != nil {
		panic(err)
	}
	return def
}
