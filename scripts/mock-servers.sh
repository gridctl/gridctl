#!/usr/bin/env bash
# Build and run the example mock MCP servers (examples/_mock-servers/).
# Invoked by `task mock:servers` and `task mock:clean`. The start/stop
# lifecycle lives here rather than in Taskfile.yml because Task's embedded
# shell has no job control: backgrounding with `&` and capturing `$!` need a
# real bash.
#
# Usage:
#   mock-servers.sh start [port]   Build both servers, start mock-mcp-server
#                                  in HTTP mode on <port> (default 9001) and
#                                  SSE mode on <port>+1, writing PID files.
#   mock-servers.sh stop           Kill the started servers and remove the
#                                  built binaries and PID files.
set -euo pipefail

# Always operate from the repo root so direct invocation from any cwd works
# (Task already runs from the Taskfile directory; this covers everyone else).
cd "$(dirname "$0")/.."

MOCK_DIR="examples/_mock-servers"
STDIO_DIR="$MOCK_DIR/local-stdio-server"
MCP_DIR="$MOCK_DIR/mock-mcp-server"

start() {
  local port="${1:-9001}"
  if ! [[ "$port" =~ ^[0-9]+$ ]]; then
    echo "Error: port must be numeric, got '$port'" >&2
    exit 1
  fi
  local sse_port=$((port + 1))
  command -v go >/dev/null 2>&1 || { echo "Error: Go is not installed. Please install Go first: https://go.dev/dl/"; exit 1; }
  echo "Building mock-stdio-server..."
  (cd "$STDIO_DIR" && go build -o mock-stdio-server .)
  echo "Building mock-mcp-server..."
  (cd "$MCP_DIR" && go build -o mock-mcp-server .)
  echo "Starting mock-mcp-server on port $port (HTTP mode)..."
  "$MCP_DIR/mock-mcp-server" -port "$port" > /dev/null 2>&1 &
  echo $! > "$MCP_DIR/.pid-http"
  echo "Starting mock-mcp-server on port $sse_port (SSE mode)..."
  "$MCP_DIR/mock-mcp-server" -port "$sse_port" -sse > /dev/null 2>&1 &
  echo $! > "$MCP_DIR/.pid-sse"
  echo ""
  echo "Mock servers running:"
  echo "  mock-stdio-server: built at $STDIO_DIR/mock-stdio-server"
  echo "  mock-mcp-server:   HTTP on localhost:$port, SSE on localhost:$sse_port"
  echo ""
  echo "Run 'task mock:clean' to stop and remove them."
}

stop() {
  echo "Stopping mock MCP servers..."
  local pidfile
  for pidfile in "$MCP_DIR/.pid-http" "$MCP_DIR/.pid-sse"; do
    if [ -f "$pidfile" ]; then
      kill "$(cat "$pidfile")" 2>/dev/null || true
      rm -f "$pidfile"
    fi
  done
  echo "Removing mock server binaries..."
  rm -f "$STDIO_DIR/mock-stdio-server"
  rm -f "$MCP_DIR/mock-mcp-server"
  echo "Mock servers cleaned up."
}

case "${1:-}" in
  start) start "${2:-}" ;;
  stop) stop ;;
  *)
    echo "Usage: $0 {start [port]|stop}" >&2
    exit 1
    ;;
esac
