.PHONY: all build build-web build-go dev clean help test test-coverage test-integration test-frontend mock-servers clean-mock-servers generate

# Version from git tags (fallback to dev)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Default target
all: build

# Build everything
build: build-web build-go

# Build the React frontend
build-web:
	@if [ -f web/tsconfig.json ]; then \
		echo "Building web frontend..."; \
		(cd web && npm run build); \
		echo "Copying dist to cmd/gridctl/web/dist..."; \
		rm -rf cmd/gridctl/web; \
		mkdir -p cmd/gridctl/web; \
		cp -r web/dist cmd/gridctl/web/; \
	else \
		echo "Skipping web build (source files not present)"; \
	fi

# Build the Go binary
build-go:
	@echo "Building Go binary ($(VERSION))..."
	@if [ -d cmd/gridctl/web/dist ]; then \
		echo "Including embedded web assets..."; \
		go build -tags embed_web -ldflags "$(LDFLAGS)" -o gridctl ./cmd/gridctl; \
	else \
		echo "Building without web assets (run make build-web first to include UI)..."; \
		go build -ldflags "$(LDFLAGS)" -o gridctl ./cmd/gridctl; \
	fi

# Development mode - run Vite dev server
dev:
	cd web && npm run dev

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf gridctl
	rm -rf cmd/gridctl/web
	rm -rf web/dist
	rm -rf web/node_modules

# Install dependencies
deps:
	@echo "Installing dependencies..."
	cd web && npm install
	go mod tidy

# Run the built binary
run: build
	./gridctl

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run frontend tests
test-frontend:
	@echo "Running frontend tests..."
	cd web && npm test

# Run integration tests (requires Docker)
test-integration:
	@echo "Running integration tests..."
	go test -v -tags=integration ./tests/integration/...

# Build and run mock MCP servers for examples
# Usage: make mock-servers [PORT=9001]
PORT ?= 9001
mock-servers:
	@echo "Building and starting mock MCP servers..."
	@command -v go >/dev/null 2>&1 || { echo "Error: Go is not installed. Please install Go first: https://go.dev/dl/"; exit 1; }
	@echo "Building mock-stdio-server..."
	@(cd examples/_mock-servers/local-stdio-server && go build -o mock-stdio-server .)
	@echo "Building mock-mcp-server..."
	@(cd examples/_mock-servers/mock-mcp-server && go build -o mock-mcp-server .)
	@echo "Starting mock-mcp-server on port $(PORT) (HTTP mode)..."
	@examples/_mock-servers/mock-mcp-server/mock-mcp-server -port $(PORT) > /dev/null 2>&1 & echo $$! > examples/_mock-servers/mock-mcp-server/.pid-http
	@echo "Starting mock-mcp-server on port $$(( $(PORT) + 1 )) (SSE mode)..."
	@examples/_mock-servers/mock-mcp-server/mock-mcp-server -port $$(( $(PORT) + 1 )) -sse > /dev/null 2>&1 & echo $$! > examples/_mock-servers/mock-mcp-server/.pid-sse
	@echo ""
	@echo "Mock servers running:"
	@echo "  mock-stdio-server: built at examples/_mock-servers/local-stdio-server/mock-stdio-server"
	@echo "  mock-mcp-server:   HTTP on localhost:$(PORT), SSE on localhost:$$(( $(PORT) + 1 ))"
	@echo ""
	@echo "Run 'make clean-mock-servers' to stop and remove them."

# Stop and remove mock MCP servers
clean-mock-servers:
	@echo "Stopping mock MCP servers..."
	@if [ -f examples/_mock-servers/mock-mcp-server/.pid-http ]; then \
		kill $$(cat examples/_mock-servers/mock-mcp-server/.pid-http) 2>/dev/null || true; \
		rm -f examples/_mock-servers/mock-mcp-server/.pid-http; \
	fi
	@if [ -f examples/_mock-servers/mock-mcp-server/.pid-sse ]; then \
		kill $$(cat examples/_mock-servers/mock-mcp-server/.pid-sse) 2>/dev/null || true; \
		rm -f examples/_mock-servers/mock-mcp-server/.pid-sse; \
	fi
	@echo "Removing mock server binaries..."
	@rm -f examples/_mock-servers/local-stdio-server/mock-stdio-server
	@rm -f examples/_mock-servers/mock-mcp-server/mock-mcp-server
	@echo "Mock servers cleaned up."

# Generate mocks (requires mockgen: go install go.uber.org/mock/mockgen@latest)
generate:
	@echo "Generating mocks..."
	go generate ./pkg/mcp/... ./pkg/runtime/...
	@echo "Done."

# Help
help:
	@echo "Gridctl Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build      - Build frontend and backend"
	@echo "  make build-web  - Build React frontend only"
	@echo "  make build-go   - Build Go binary only"
	@echo "  make dev        - Run Vite dev server"
	@echo "  make clean      - Remove build artifacts"
	@echo "  make deps       - Install all dependencies"
	@echo "  make run        - Build and run the binary"
	@echo "  make test       - Run all tests"
	@echo "  make test-coverage - Run tests with coverage report"
	@echo "  make test-frontend - Run frontend tests"
	@echo "  make test-integration - Run integration tests (requires Docker)"
	@echo "  make generate   - Regenerate mock files (requires mockgen)"
	@echo "  make mock-servers [PORT=9001] - Build and run mock MCP servers for examples"
	@echo "  make clean-mock-servers - Stop and remove mock MCP servers"
	@echo "  make help       - Show this help message"
