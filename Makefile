.PHONY: all build build-web build-go dev clean help test test-coverage test-integration

# Default target
all: build

# Build everything
build: build-web build-go

# Build the React frontend
build-web:
	@if [ -f web/tsconfig.json ]; then \
		echo "Building web frontend..."; \
		(cd web && npm run build); \
		echo "Copying dist to cmd/agentlab/web/dist..."; \
		rm -rf cmd/agentlab/web; \
		mkdir -p cmd/agentlab/web; \
		cp -r web/dist cmd/agentlab/web/; \
	else \
		echo "Skipping web build (source files not present)"; \
	fi

# Build the Go binary
build-go:
	@echo "Building Go binary..."
	@if [ -d cmd/agentlab/web/dist ]; then \
		echo "Including embedded web assets..."; \
		go build -tags embed_web -o agentlab ./cmd/agentlab; \
	else \
		echo "Building without web assets (run make build-web first to include UI)..."; \
		go build -o agentlab ./cmd/agentlab; \
	fi

# Development mode - run Vite dev server
dev:
	cd web && npm run dev

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf agentlab
	rm -rf cmd/agentlab/web
	rm -rf web/dist
	rm -rf web/node_modules

# Install dependencies
deps:
	@echo "Installing dependencies..."
	cd web && npm install
	go mod tidy

# Run the built binary
run: build
	./agentlab

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

# Run integration tests (requires Docker)
test-integration:
	@echo "Running integration tests..."
	go test -v -tags=integration ./tests/integration/...

# Help
help:
	@echo "Agentlab Makefile"
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
	@echo "  make test-integration - Run integration tests (requires Docker)"
	@echo "  make help       - Show this help message"
