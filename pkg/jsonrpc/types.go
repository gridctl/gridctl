// Package jsonrpc provides shared JSON-RPC 2.0 types used by MCP and A2A protocols.
package jsonrpc

import "encoding/json"

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// NewErrorResponse creates a JSON-RPC error response.
func NewErrorResponse(id *json.RawMessage, code int, message string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	}
}

// NewSuccessResponse creates a JSON-RPC success response.
func NewSuccessResponse(id *json.RawMessage, result any) Response {
	var resultBytes json.RawMessage
	if result != nil {
		resultBytes, _ = json.Marshal(result)
	}
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultBytes,
	}
}
