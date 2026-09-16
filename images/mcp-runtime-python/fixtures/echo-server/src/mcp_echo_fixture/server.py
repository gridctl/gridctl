import json
import os
from pathlib import Path
import sqlite3
import sys


def reply(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()


def probe():
    sqlite3.connect("/tmp/mcp-echo-probe.db").execute("create table if not exists t(x integer)")
    Path("/tmp/mcp-echo-probe.txt").write_text("ok")
    app_readonly = False
    try:
        Path("/app/mcp-echo-probe.txt").write_text("no")
    except OSError:
        app_readonly = True
    return {
        "sqlite": True,
        "scratch": Path("/tmp/mcp-echo-probe.txt").read_text() == "ok",
        "app_readonly": app_readonly,
        "uid": os.getuid(),
        "gid": os.getgid(),
    }


def main():
    print("mcp-echo-fixture starting", file=sys.stderr, flush=True)
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
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {"tools": [
                {"name": "echo", "description": "Echo a message", "inputSchema": {"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}},
                {"name": "probe", "description": "Probe scratch, identity, and sqlite", "inputSchema": {"type": "object", "properties": {}}},
            ]}})
        elif method == "tools/call":
            name = request.get("params", {}).get("name")
            if name == "probe":
                reply({"jsonrpc": "2.0", "id": request["id"], "result": {"content": [{"type": "text", "text": json.dumps(probe())}]}})
                continue
            message = request.get("params", {}).get("arguments", {}).get("message", "")
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {"content": [{"type": "text", "text": message}]}})
        elif method == "ping":
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {}})
        else:
            reply({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "Method not found"}})


if __name__ == "__main__":
    main()
