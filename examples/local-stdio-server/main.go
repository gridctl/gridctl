// Mock MCP Server for testing local process (stdio) MCP server support.
// This server communicates via stdin/stdout using JSON-RPC.
//
// Build:
//
//	go build -o mock-stdio-server .
//
// The server reads JSON-RPC requests from stdin (one per line) and
// writes JSON-RPC responses to stdout. Each request/response is a
// single line of JSON.
//
// Supports:
//   - initialize - MCP handshake
//   - notifications/initialized - Notification acknowledgment
//   - tools/list - Returns sample tools
//   - tools/call - Execute tools (echo, add, get_time)
//   - ping - Health check
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// JSON-RPC types
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP types
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
	Capabilities    Capabilities `json:"capabilities"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capabilities struct {
	Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type ToolCallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Sample tools provided by this mock server
var sampleTools = []Tool{
	{
		Name:        "echo",
		Description: "Echoes back the input message",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "The message to echo back",
				},
			},
			"required": []string{"message"},
		},
	},
	{
		Name:        "add",
		Description: "Adds two numbers together",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{
					"type":        "number",
					"description": "First number",
				},
				"b": map[string]any{
					"type":        "number",
					"description": "Second number",
				},
			},
			"required": []string{"a", "b"},
		},
	},
	{
		Name:        "get_time",
		Description: "Returns the current server time",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	},
}

func handleRequest(req Request) *Response {
	var result any
	var rpcErr *Error

	switch req.Method {
	case "initialize":
		result = InitializeResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo: ServerInfo{
				Name:    "mock-stdio-server",
				Version: "1.0.0",
			},
			Capabilities: Capabilities{
				Tools: &ToolsCapability{ListChanged: false},
			},
		}

	case "notifications/initialized":
		// Notification, no response needed
		return nil

	case "tools/list":
		result = ToolsListResult{Tools: sampleTools}

	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			rpcErr = &Error{Code: -32602, Message: "Invalid params"}
		} else {
			result = handleToolCall(params)
		}

	case "ping":
		result = map[string]string{"status": "ok"}

	default:
		rpcErr = &Error{Code: -32601, Message: "Method not found"}
	}

	resp := &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}

	return resp
}

func handleToolCall(params ToolCallParams) ToolCallResult {
	switch params.Name {
	case "echo":
		msg, _ := params.Arguments["message"].(string)
		return ToolCallResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Echo: %s", msg)}},
		}

	case "add":
		a, _ := params.Arguments["a"].(float64)
		b, _ := params.Arguments["b"].(float64)
		return ToolCallResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Result: %v", a+b)}},
		}

	case "get_time":
		return ToolCallResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Current time: %s", time.Now().Format(time.RFC3339))}},
		}

	default:
		return ToolCallResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", params.Name)}},
			IsError: true,
		}
	}
}

func main() {
	// Log startup to stderr (not stdout, which is for JSON-RPC)
	fmt.Fprintln(os.Stderr, "Mock stdio MCP server started")

	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer size for large requests
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			// Send parse error
			resp := Response{
				JSONRPC: "2.0",
				Error:   &Error{Code: -32700, Message: "Parse error"},
			}
			data, _ := json.Marshal(resp)
			fmt.Println(string(data))
			continue
		}

		resp := handleRequest(req)
		if resp != nil {
			data, _ := json.Marshal(resp)
			fmt.Println(string(data))
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading stdin:", err)
		os.Exit(1)
	}
}
