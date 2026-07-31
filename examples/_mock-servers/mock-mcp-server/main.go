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
	"os"
	"strings"
)

var (
	port     int
	sseMode  bool
	protocol string
)

// modernMode reports whether the mock speaks the stateless 2026-07-28
// generation instead of the legacy handshake generation.
func modernMode() bool { return protocol == "2026-07-28" }

func init() {
	flag.IntVar(&port, "port", 8080, "Port to listen on")
	flag.BoolVar(&sseMode, "sse", false, "Enable SSE response format")
	flag.StringVar(&protocol, "protocol", "", "Protocol generation: empty for legacy handshake, 2026-07-28 for stateless")
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
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
}

type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

type ToolCallParams struct {
	Name           string          `json:"name"`
	Arguments      map[string]any  `json:"arguments,omitempty"`
	RequestState   string          `json:"requestState,omitempty"`
	InputResponses json.RawMessage `json:"inputResponses,omitempty"`
}

type ToolCallResult struct {
	Content       []Content       `json:"content"`
	IsError       bool            `json:"isError,omitempty"`
	ResultType    string          `json:"resultType,omitempty"`
	InputRequests json.RawMessage `json:"inputRequests,omitempty"`
	RequestState  string          `json:"requestState,omitempty"`
}

// DiscoverResult is the 2026-07-28 server/discover response.
type DiscoverResult struct {
	ResultType        string         `json:"resultType"`
	SupportedVersions []string       `json:"supportedVersions"`
	Capabilities      Capabilities   `json:"capabilities"`
	TTLMs             int64          `json:"ttlMs"`
	CacheScope        string         `json:"cacheScope"`
	Meta              map[string]any `json:"_meta,omitempty"`
}

// cacheableToolsListResult is ToolsListResult plus the CacheableResult
// fields the stateless generation requires on list results.
type cacheableToolsListResult struct {
	ResultType string `json:"resultType"`
	TTLMs      int64  `json:"ttlMs"`
	CacheScope string `json:"cacheScope"`
	Tools      []Tool `json:"tools"`
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

	if modernMode() {
		handleModernMCP(w, req)
		return
	}

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

// handleModernMCP serves the stateless 2026-07-28 generation: no
// handshake, server/discover, resultType on every result, cache
// metadata on list results, and an MRTR tool that verifies the
// requestState round trip byte-exact.
func handleModernMCP(w http.ResponseWriter, req Request) {
	// Notifications are acknowledged with 202 and no body.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "server/discover":
		sendResult(w, req.ID, DiscoverResult{
			ResultType:        "complete",
			SupportedVersions: []string{"2026-07-28"},
			Capabilities:      Capabilities{Tools: &ToolsCapability{}},
			TTLMs:             60000,
			CacheScope:        "public",
			Meta: map[string]any{
				"io.modelcontextprotocol/serverInfo": ServerInfo{Name: "mock-mcp-server", Version: "2.0.0"},
			},
		})

	case "tools/list":
		sendResult(w, req.ID, cacheableToolsListResult{
			ResultType: "complete",
			TTLMs:      60000,
			CacheScope: "public",
			Tools:      sampleTools,
		})

	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(w, req.ID, -32602, "Invalid params")
			return
		}
		if params.Name == "ask_secret" {
			sendResult(w, req.ID, handleAskSecret(params))
			return
		}
		result := handleToolCall(params)
		result.ResultType = "complete"
		sendResult(w, req.ID, result)

	case "initialize":
		// A modern-only server SHOULD name its supported versions in
		// any error it returns to initialize; this may be the only
		// diagnostic a legacy client can surface.
		sendError(w, req.ID, -32601, "initialize is not supported; this server speaks protocol versions: 2026-07-28")

	default:
		// The modern generation removed ping and logging/setLevel;
		// unknown methods get 404 with -32601.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &Error{Code: -32601, Message: "Method not found"},
		})
	}
}

// askSecretState is the exact requestState the ask_secret tool issues;
// the retry must echo it byte-for-byte or the tool reports tampering.
// The value deliberately looks sentinel-shaped to catch double-encoding.
const askSecretState = "mock-ask-secret-state:=?base64?not-actually?=:é世"

// handleAskSecret implements the MRTR round trip: the first call
// returns input_required with an elicitation request and opaque state;
// the retry is verified byte-exact.
func handleAskSecret(params ToolCallParams) ToolCallResult {
	if params.RequestState == "" {
		return ToolCallResult{
			ResultType: "input_required",
			InputRequests: json.RawMessage(`{
				"secret_word": {"method": "elicitation/create", "params": {"mode": "form", "message": "What is the secret word?",
					"requestedSchema": {"type": "object", "properties": {"word": {"type": "string"}}, "required": ["word"]}}}
			}`),
			RequestState: askSecretState,
		}
	}
	if params.RequestState != askSecretState {
		return ToolCallResult{
			ResultType: "complete",
			Content:    []Content{{Type: "text", Text: fmt.Sprintf("requestState corrupted in transit: got %q", params.RequestState)}},
			IsError:    true,
		}
	}
	if len(params.InputResponses) == 0 {
		return ToolCallResult{
			ResultType: "complete",
			Content:    []Content{{Type: "text", Text: "retry carried no inputResponses"}},
			IsError:    true,
		}
	}
	return ToolCallResult{
		ResultType: "complete",
		Content:    []Content{{Type: "text", Text: "secret accepted"}},
	}
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

	// MOCK_ECHO_DESC overrides the echo tool's description so tests can simulate
	// a downstream server silently changing its tool schema between connects
	// (a "rug pull"), exercising schema-pinning drift detection.
	if desc := os.Getenv("MOCK_ECHO_DESC"); desc != "" {
		sampleTools[0].Description = desc
	}

	// MOCK_ECHO_OUTPUT_SCHEMA sets the echo tool's outputSchema from raw JSON so
	// tests can simulate a server changing its output contract between connects.
	if raw := os.Getenv("MOCK_ECHO_OUTPUT_SCHEMA"); raw != "" {
		var schema map[string]any
		if err := json.Unmarshal([]byte(raw), &schema); err != nil {
			log.Fatalf("invalid MOCK_ECHO_OUTPUT_SCHEMA: %v", err)
		}
		sampleTools[0].OutputSchema = schema
	}

	// The modern generation carries an MRTR-exercising tool so tests
	// can verify the requestState round trip through the gateway.
	if modernMode() {
		sampleTools = append(sampleTools, Tool{
			Name:        "ask_secret",
			Description: "Requires additional input via MRTR before answering",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}

	if oauthBaseURL == "" {
		oauthBaseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", requireBearer(handleMCP))
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", requireBearer(handleHealth)) // Root returns health for ping
	if oauthMode {
		registerOAuthRoutes(mux)
	}
	maybeLogOAuthMode()

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
