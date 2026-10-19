// Secret-echo MCP server. It prints SECRET_VALUE to stderr once, and its
// fingerprint tool returns a sha256 of that value rather than the value.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
}

func main() {
	secret := os.Getenv("SECRET_VALUE")
	if secret != "" {
		fmt.Fprintf(os.Stderr, "secret loaded %s\n", secret)
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	for in.Scan() {
		var req request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil || req.Method == "" {
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		switch req.Method {
		case "initialize":
			write(req.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]string{"name": "secret-echo", "version": "0"},
				"capabilities":    map[string]any{"tools": map[string]any{}},
			})
		case "tools/list":
			write(req.ID, map[string]any{"tools": []any{map[string]any{
				"name":        "fingerprint",
				"description": "sha256 of SECRET_VALUE",
				"inputSchema": map[string]any{"type": "object"},
			}}})
		case "tools/call":
			sum := sha256.Sum256([]byte(secret))
			write(req.ID, map[string]any{"content": []any{map[string]string{"type": "text", "text": hex.EncodeToString(sum[:])}}})
		case "ping":
			write(req.ID, map[string]any{})
		default:
			writeError(req.ID, -32601, "method not found")
		}
	}
}

func write(id json.RawMessage, result any) {
	enc, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	fmt.Printf("%s\n", enc)
}

func writeError(id json.RawMessage, code int, message string) {
	enc, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
	fmt.Printf("%s\n", enc)
}
