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
// Supports (legacy handshake mode, the default):
//   - initialize - MCP handshake
//   - notifications/initialized - Notification acknowledgment
//   - tools/list - Returns sample tools
//   - tools/call - Execute tools (echo, add, get_time)
//   - ping - Health check
//
// With -protocol 2026-07-28 the server speaks the stateless generation
// instead: server/discover replaces the handshake and ping, results
// carry resultType and cache metadata, and the removed legacy methods
// answer -32601 (mirroring mock-mcp-server's -protocol flag).
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

var protocol string

func modernMode() bool { return protocol == "2026-07-28" }

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

// DiscoverResult is the server/discover response (2026-07-28).
type DiscoverResult struct {
	ResultType        string         `json:"resultType"`
	SupportedVersions []string       `json:"supportedVersions"`
	Capabilities      Capabilities   `json:"capabilities"`
	TTLMs             int64          `json:"ttlMs"`
	CacheScope        string         `json:"cacheScope"`
	Meta              map[string]any `json:"_meta,omitempty"`
}

// cacheableToolsListResult is ToolsListResult plus the stateless-era
// required result fields.
type cacheableToolsListResult struct {
	ResultType string `json:"resultType"`
	TTLMs      int64  `json:"ttlMs"`
	CacheScope string `json:"cacheScope"`
	Tools      []Tool `json:"tools"`
}

// modernToolCallResult is ToolCallResult plus resultType.
type modernToolCallResult struct {
	ToolCallResult
	ResultType string `json:"resultType"`
}

// handleModernRequest serves the stateless 2026-07-28 generation over
// stdio: no handshake, server/discover doubles as the health method.
func handleModernRequest(req Request) *Response {
	// Notifications are accepted and dropped.
	if len(req.ID) == 0 {
		return nil
	}

	resp := &Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "server/discover":
		resp.Result = DiscoverResult{
			ResultType:        "complete",
			SupportedVersions: []string{"2026-07-28"},
			Capabilities:      Capabilities{Tools: &ToolsCapability{}},
			TTLMs:             60000,
			CacheScope:        "public",
			Meta: map[string]any{
				"io.modelcontextprotocol/serverInfo": ServerInfo{Name: "mock-stdio-server", Version: "2.0.0"},
			},
		}
	case "tools/list":
		resp.Result = cacheableToolsListResult{
			ResultType: "complete",
			TTLMs:      60000,
			CacheScope: "public",
			Tools:      sampleTools,
		}
	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			resp.Error = &Error{Code: -32602, Message: "Invalid params"}
			return resp
		}
		resp.Result = modernToolCallResult{ToolCallResult: handleToolCall(params), ResultType: "complete"}
	case "initialize":
		resp.Error = &Error{Code: -32601, Message: "initialize is not supported; this server speaks protocol versions: 2026-07-28"}
	default:
		// The modern generation removed ping and logging/setLevel.
		resp.Error = &Error{Code: -32601, Message: "Method not found"}
	}
	return resp
}

func handleRequest(req Request) *Response {
	if modernMode() {
		return handleModernRequest(req)
	}

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
	flag.StringVar(&protocol, "protocol", "", "Protocol generation: empty for legacy handshake, 2026-07-28 for stateless")
	flag.Parse()

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
