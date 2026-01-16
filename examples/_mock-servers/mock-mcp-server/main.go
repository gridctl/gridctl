// Mock MCP Server for testing external HTTP/SSE MCP server support.
// This simple server responds to MCP protocol requests with dummy data.
//
// Usage:
//
//	go run main.go                   # HTTP mode on :8080
//	go run main.go -port 9000        # Custom port
//	go run main.go -sse              # Enable SSE response format
//
// Supports:
//   - initialize - MCP handshake
//   - tools/list - Returns sample tools
//   - tools/call - Echo tool that returns arguments
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
)

var (
	port    int
	sseMode bool
)

func init() {
	flag.IntVar(&port, "port", 8080, "Port to listen on")
	flag.BoolVar(&sseMode, "sse", false, "Enable SSE response format")
}

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

func handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, nil, -32700, "Parse error")
		return
	}

	log.Printf("Received request: method=%s", req.Method)

	var result any
	var rpcErr *Error

	switch req.Method {
	case "initialize":
		result = InitializeResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo: ServerInfo{
				Name:    "mock-mcp-server",
				Version: "1.0.0",
			},
			Capabilities: Capabilities{
				Tools: &ToolsCapability{ListChanged: false},
			},
		}

	case "notifications/initialized":
		// Notification, no response needed
		w.WriteHeader(http.StatusOK)
		return

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

	if rpcErr != nil {
		sendError(w, req.ID, rpcErr.Code, rpcErr.Message)
		return
	}

	sendResult(w, req.ID, result)
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
			Content: []Content{{Type: "text", Text: "Current time: 2024-01-15T10:30:00Z (mock)"}},
		}

	default:
		return ToolCallResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", params.Name)}},
			IsError: true,
		}
	}
}

func sendResult(w http.ResponseWriter, id json.RawMessage, result any) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	if sseMode {
		sendSSE(w, resp)
	} else {
		sendJSON(w, resp)
	}
}

func sendError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	}

	if sseMode {
		sendSSE(w, resp)
	} else {
		sendJSON(w, resp)
	}
}

func sendJSON(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func sendSSE(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	data, _ := json.Marshal(resp)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func main() {
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", handleMCP)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", handleHealth) // Root returns health for ping

	mode := "HTTP"
	if sseMode {
		mode = "SSE"
	}

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Mock MCP Server starting on %s (%s mode)", addr, mode)
	log.Printf("Endpoints:")
	log.Printf("  POST /mcp    - MCP JSON-RPC endpoint")
	log.Printf("  GET  /health - Health check")
	log.Printf("Tools available: %s", strings.Join(toolNames(), ", "))

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func toolNames() []string {
	names := make([]string, len(sampleTools))
	for i, t := range sampleTools {
		names[i] = t.Name
	}
	return names
}
