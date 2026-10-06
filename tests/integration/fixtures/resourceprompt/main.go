package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

var (
	protocol      string
	enablePrompts bool
	enableRes     bool
	failResources bool
	sharedURI     bool
	extraURI      bool
	enableUI      bool
	readError     int
	cacheTTL      int
	cacheScope    string
	uiDeclared    bool
)

func main() {
	flag.StringVar(&protocol, "protocol", "", "empty for handshake, 2026-07-28 for stateless")
	flag.BoolVar(&enablePrompts, "prompts", false, "declare prompts")
	flag.BoolVar(&enableRes, "resources", false, "declare resources")
	flag.BoolVar(&failResources, "fail-resources", false, "resources/list returns an error")
	flag.BoolVar(&sharedURI, "shared", false, "publish file:///shared")
	flag.BoolVar(&extraURI, "extra", false, "publish file:///extra")
	flag.BoolVar(&enableUI, "ui", false, "serve a UI resource when the client declares the extension")
	flag.IntVar(&readError, "read-error", 0, "resources/read of file:///error returns this JSON-RPC code")
	flag.IntVar(&cacheTTL, "cache-ttl", 30000, "stateless ttlMs")
	flag.StringVar(&cacheScope, "cache-scope", "public", "stateless cacheScope")
	flag.Parse()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := bufio.NewWriter(os.Stdout)
	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil {
			write(out, map[string]any{"jsonrpc": "2.0", "error": map[string]any{"code": -32700, "message": "Parse error"}})
			continue
		}
		if resp := handle(req); resp != nil {
			write(out, resp)
		}
	}
}

func write(out *bufio.Writer, v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintln(out, string(data))
	_ = out.Flush()
}

func modern() bool { return protocol == "2026-07-28" }

func handle(req map[string]any) map[string]any {
	method, _ := req["method"].(string)
	id := req["id"]
	if id == nil {
		return nil
	}
	noteExtension(req["params"])
	switch method {
	case "initialize":
		if modern() {
			return errResp(id, -32601, "Method not found")
		}
		noteExtension(req["params"])
		return ok(id, map[string]any{
			"protocolVersion": "2025-06-18",
			"serverInfo":      map[string]any{"name": "resourceprompt", "version": "1.0.0"},
			"capabilities":    capabilities(),
		})
	case "notifications/initialized":
		return nil
	case "server/discover":
		if !modern() {
			return errResp(id, -32601, "Method not found")
		}
		return ok(id, map[string]any{
			"resultType":        "complete",
			"supportedVersions": []string{"2026-07-28"},
			"capabilities":      capabilities(),
			"ttlMs":             cacheTTL,
			"cacheScope":        cacheScope,
			"_meta":             map[string]any{"io.modelcontextprotocol/serverInfo": map[string]any{"name": "resourceprompt", "version": "1.0.0"}},
		})
	case "tools/list":
		return ok(id, cacheWrap(map[string]any{"tools": tools()}))
	case "tools/call":
		return ok(id, cacheWrap(map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}))
	case "ping":
		if modern() {
			return errResp(id, -32601, "Method not found")
		}
		return ok(id, map[string]any{})
	case "prompts/list":
		if !enablePrompts {
			return errResp(id, -32601, "Method not found")
		}
		return ok(id, cacheWrap(map[string]any{"prompts": prompts()}))
	case "prompts/get":
		if !enablePrompts {
			return errResp(id, -32601, "Method not found")
		}
		return ok(id, cacheWrap(promptGet(params(req))))
	case "resources/list":
		if !enableRes {
			return errResp(id, -32601, "Method not found")
		}
		if failResources {
			return errResp(id, -32000, "list failed")
		}
		return ok(id, cacheWrap(map[string]any{"resources": resources()}))
	case "resources/templates/list":
		if !enableRes {
			return errResp(id, -32601, "Method not found")
		}
		return ok(id, cacheWrap(map[string]any{"resourceTemplates": templates()}))
	case "resources/read":
		if !enableRes && !enableUI {
			return errResp(id, -32601, "Method not found")
		}
		return readResource(id, params(req))
	default:
		return errResp(id, -32601, "Method not found")
	}
}

func params(req map[string]any) map[string]any {
	p, _ := req["params"].(map[string]any)
	return p
}

func noteExtension(params any) {
	obj, _ := params.(map[string]any)
	if obj == nil {
		return
	}
	if caps, ok := obj["capabilities"].(map[string]any); ok && hasUI(caps) {
		uiDeclared = true
	}
	if meta, ok := obj["_meta"].(map[string]any); ok {
		if caps, ok := meta["io.modelcontextprotocol/clientCapabilities"].(map[string]any); ok && hasUI(caps) {
			uiDeclared = true
		}
	}
}

func hasUI(caps map[string]any) bool {
	ext, _ := caps["extensions"].(map[string]any)
	if ext == nil {
		return false
	}
	_, ok := ext["io.modelcontextprotocol/ui"]
	return ok
}

func capabilities() map[string]any {
	caps := map[string]any{"tools": map[string]any{}}
	if enablePrompts {
		caps["prompts"] = map[string]any{}
	}
	if enableRes || enableUI {
		caps["resources"] = map[string]any{}
	}
	return caps
}

func tools() []any {
	list := []any{map[string]any{
		"name":        "echo",
		"description": "echo",
		"inputSchema": map[string]any{"type": "object"},
	}}
	if enableUI && uiDeclared {
		list = append(list, map[string]any{
			"name":        "draw",
			"description": "draw",
			"inputSchema": map[string]any{"type": "object"},
			"_meta":       map[string]any{"ui": map[string]any{"resourceUri": "ui://app/view"}},
		})
	}
	return list
}

func prompts() []any {
	return []any{
		map[string]any{"name": "review", "description": "Review"},
		map[string]any{
			"name":        "brief",
			"description": "Brief",
			"arguments":   []any{map[string]any{"name": "topic", "description": "Topic", "required": true}},
		},
	}
}

func promptGet(p map[string]any) map[string]any {
	name, _ := p["name"].(string)
	if name == "brief" {
		topic := ""
		if args, ok := p["arguments"].(map[string]any); ok {
			topic, _ = args["topic"].(string)
		}
		return map[string]any{
			"description": "Brief",
			"messages": []any{map[string]any{
				"role": "user",
				"content": map[string]any{
					"type": "resource",
					"resource": map[string]any{
						"uri":      "file:///brief",
						"mimeType": "text/plain",
						"text":     "topic=" + topic,
						"blob":     "AAEC",
						"_meta":    map[string]any{"kept": true},
					},
				},
			}},
		}
	}
	return map[string]any{
		"description": "Review",
		"messages": []any{map[string]any{
			"role":    "user",
			"content": map[string]any{"type": "text", "text": "review-body"},
		}},
	}
}

func resources() []any {
	list := []any{
		map[string]any{"uri": "file:///readme", "name": "readme", "mimeType": "text/plain"},
		map[string]any{"uri": "file:///blob", "name": "blob", "mimeType": "application/octet-stream"},
	}
	if sharedURI {
		list = append(list, map[string]any{"uri": "file:///shared", "name": "shared", "mimeType": "text/plain"})
	}
	if extraURI {
		list = append(list, map[string]any{"uri": "file:///extra", "name": "extra", "mimeType": "text/plain"})
	}
	return list
}

func templates() []any {
	if !enableRes {
		return []any{}
	}
	return []any{map[string]any{"uriTemplate": "file:///tmpl/{name}", "name": "tmpl", "mimeType": "text/plain"}}
}

func readResource(id any, p map[string]any) map[string]any {
	uri, _ := p["uri"].(string)
	if readError != 0 && uri == "file:///error" {
		return errResp(id, readError, "downstream rejected read")
	}
	if uri == "ui://app/view" {
		if !uiDeclared {
			return errResp(id, -32002, "not found")
		}
		return ok(id, cacheWrap(map[string]any{"contents": []any{map[string]any{
			"uri": uri, "mimeType": "text/html;profile=mcp-app", "text": "<html>app</html>",
		}}}))
	}
	if strings.HasPrefix(uri, "file:///tmpl/") {
		return ok(id, cacheWrap(map[string]any{"contents": []any{map[string]any{
			"uri": uri, "mimeType": "text/plain", "text": "template",
		}}}))
	}
	switch uri {
	case "file:///readme", "file:///shared", "file:///extra":
		return ok(id, cacheWrap(map[string]any{"contents": []any{map[string]any{
			"uri": uri, "mimeType": "text/plain", "text": "hello",
		}}}))
	case "file:///blob":
		return ok(id, cacheWrap(map[string]any{"contents": []any{map[string]any{
			"uri": uri, "mimeType": "application/octet-stream", "blob": "AAEC",
		}}}))
	default:
		return errResp(id, -32002, "not found")
	}
}

func cacheWrap(result map[string]any) map[string]any {
	if !modern() {
		return result
	}
	result["resultType"] = "complete"
	result["ttlMs"] = cacheTTL
	result["cacheScope"] = cacheScope
	return result
}

func ok(id any, result any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
}

func errResp(id any, code int, message string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}}
}
