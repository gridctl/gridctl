package mcp

import (
	"strconv"

	"github.com/gridctl/gridctl/pkg/runs"
)

// JSON-RPC codes the MCP convention does not treat as errors. They still
// record rpc.response.status_code and do not set error.type.
const jsonRPCNotErrorCode = -32002

// ErrorTypeInput is the input to SelectErrorType. One function owns the
// three-way rule so the metrics observer never recomputes it.
type ErrorTypeInput struct {
	// RPCCode is set when the downstream returned a JSON-RPC error.
	RPCCode *int
	// ToolIsError is true only for a downstream tool result with isError.
	// Gateway-synthesized denials leave this false and set Disposition.
	ToolIsError bool
	Disposition string
	Reason      string
}

// SelectErrorType chooses error.type.
//
// Order: a classifying JSON-RPC code's decimal string; empty when the code
// is one the convention excludes; runs.DispositionToolError when the
// downstream result has isError; otherwise the failure disposition when one
// was recorded, else the failure reason. Cancellation uses
// runs.DispositionCancelled ("cancelled"), never "canceled". A reason such
// as context_canceled or deadline_exceeded is used only when no failure
// disposition was set.
func SelectErrorType(in ErrorTypeInput) string {
	if in.RPCCode != nil {
		if jsonRPCCodeIsError(*in.RPCCode) {
			return strconv.Itoa(*in.RPCCode)
		}
		return ""
	}
	if in.ToolIsError {
		return runs.DispositionToolError
	}
	if in.Disposition != "" && in.Disposition != runs.DispositionCompleted {
		return in.Disposition
	}
	if in.Reason != "" && in.Reason != runs.ReasonOK {
		return in.Reason
	}
	return ""
}

func jsonRPCCodeIsError(code int) bool {
	switch code {
	case -32700, -32600, -32601, -32602, jsonRPCNotErrorCode:
		return false
	default:
		return true
	}
}

// jsonRPCCode returns the downstream JSON-RPC code when err carries one.
func jsonRPCCode(err error) *int {
	rpc := rpcErrorFrom(err)
	if rpc == nil {
		return nil
	}
	code := rpc.Code
	return &code
}
