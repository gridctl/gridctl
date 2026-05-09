// Package llm hosts the gridctl LLM provider abstraction. Each
// subpackage (anthropic, openai, google, gateway) implements the
// agent.ChatModel interface against a single vendor's wire format.
//
// Providers use only net/http and encoding/json; gridctl deliberately
// avoids each vendor's official SDK because the LLM-API surface is
// narrow, the cost of impedance mismatches with our typed graph
// runtime is high, and the SDK ecosystems churn quickly. The
// provider tool adapters in <provider>/tools.go translate gridctl's
// agent.ToolInfo / agent.ToolCall to the vendor's tool format —
// the compose graph never sees vendor-specific shapes.
//
// All providers honor the Article VI contract (ctx context.Context as
// first argument), the Article XIV contract (log/slog only — no
// fmt.Println / log.Printf), and the Article V contract (error
// propagation, no panics in library code).
package llm

import "github.com/gridctl/gridctl/pkg/agent"

// Provider is an alias for agent.ChatModel that carries the
// "Provider" terminology used throughout the prompt and architectural
// docs. Existing callers that already depend on agent.ChatModel
// continue to work unchanged; new code should prefer llm.Provider for
// consistency.
type Provider = agent.ChatModel
