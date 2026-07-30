# Transitional shim: gridctl now uses Task (https://taskfile.dev) as its task
# runner. Tasks live in Taskfile.yml; run `task --list` for the catalog. This
# shim forwards the old make targets to their task equivalents so existing
# muscle memory and older instructions keep working. It will be removed after
# a two-release sunset.
#
# Install task:
#   brew install go-task/tap/go-task
#   npm install -g @go-task/cli
#   go install github.com/go-task/task/v3/cmd/task@latest

.PHONY: all build build-web build-go dev clean deps run test test-coverage test-frontend test-integration lint mock-servers clean-mock-servers generate update-pricing validate-pricing help

# $(1) is the task name; command-line variable overrides (PORT=, FILE=) pass
# through via MAKEOVERRIDES, both as Task variables and as environment (make
# exported overrides into recipe environments, so e.g. `make build-go
# GOOS=linux` keeps working). Values containing spaces are not supported by
# the shim; call task directly for those.
define forward
	@command -v task >/dev/null 2>&1 || { \
		echo "This project now uses Task (https://taskfile.dev)."; \
		echo "Install it with: brew install go-task/tap/go-task"; \
		exit 1; }
	@echo "make is deprecated here; forwarding to: task $(1)"
	@env $(MAKEOVERRIDES) task $(1) $(MAKEOVERRIDES)
endef

all:
	$(call forward,build)

build:
	$(call forward,build)

build-web:
	$(call forward,build:web)

build-go:
	$(call forward,build:go)

dev:
	$(call forward,dev)

clean:
	$(call forward,clean)

deps:
	$(call forward,deps)

run:
	$(call forward,run)

test:
	$(call forward,test)

test-coverage:
	$(call forward,test:coverage)

test-frontend:
	$(call forward,test:frontend)

test-integration:
	$(call forward,test:integration)

lint:
	$(call forward,lint)

mock-servers:
	$(call forward,mock:servers)

clean-mock-servers:
	$(call forward,mock:clean)

generate:
	$(call forward,generate)

update-pricing:
	$(call forward,pricing:update)

validate-pricing:
	$(call forward,pricing:validate)

help:
	$(call forward,--list)
