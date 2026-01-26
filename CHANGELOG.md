# Changelog

All notable changes to gridctl will be documented in this file.

## [0.1.0-alpha.2] - 2026-01-23

### Refactoring

- Update import paths and branding in Go packages
- Rename cmd/agentlab to cmd/gridctl
- Update module path to github.com/gridctl/gridctl
- Update web UI branding to Gridctl

## [0.1.0-alpha.1] - 2026-01-22

### Features

- Add version command with ldflags
- Add GoReleaser configuration
- Update release workflow for GoReleaser
- Add configurable PORT param to mock-servers target
- Add mock-servers and clean-mock-servers make targets
- Add --base-port flag for MCP server ports

### Bug Fixes

- Correct tool name delimiter to match backend
- Start HTTP server before MCP registration
- Add liveness health check and readiness endpoint
- Return friendly message for nodes without container logs
- Skip SSE notifications when parsing tool call responses
- Change tool name delimiter from :: to __ for client compatibility

### Refactoring

- Change default gateway port to 8180
- Remove unused A2A capability fields
