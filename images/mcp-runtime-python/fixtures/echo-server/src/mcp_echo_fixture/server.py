import json
import sys


def reply(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()


def main():
    for line in sys.stdin:
        request = json.loads(line)
        if "id" not in request:
            continue
        method = request.get("method")
        if method == "server/discover":
            reply({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "Method not found"}})
        elif method == "initialize":
            version = request.get("params", {}).get("protocolVersion", "2025-06-18")
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {"protocolVersion": version, "capabilities": {"tools": {}}, "serverInfo": {"name": "mcp-echo-fixture", "version": "1.0"}}})
        elif method == "tools/list":
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {"tools": [{"name": "echo", "description": "Echo a message", "inputSchema": {"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}}]}})
        elif method == "tools/call":
            message = request.get("params", {}).get("arguments", {}).get("message", "")
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {"content": [{"type": "text", "text": message}]}})
        elif method == "ping":
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {}})
        else:
            reply({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "Method not found"}})


if __name__ == "__main__":
    main()
