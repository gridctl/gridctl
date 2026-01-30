# Testing Standards

## Required for All PRs

1. **Unit Tests**: New exported functions must have tests
2. **Coverage**: Maintain existing coverage levels
3. **Naming**: Use `TestFunctionName_Scenario` pattern
4. **Table-Driven**: Preferred for multiple test cases

## Test Locations

| Package | Test Pattern |
|---------|--------------|
| pkg/config | loader_test.go |
| pkg/mcp | router_test.go, gateway_test.go, session_test.go, client_test.go, mock_test.go |
| pkg/a2a | types_test.go, gateway_test.go, handler_test.go |
| pkg/runtime | runtime_test.go (tests Orchestrator via mock interfaces) |
| pkg/runtime/docker | labels_test.go, mock_test.go |
| pkg/state | state_test.go |
| tests/integration | orchestrator_test.go, runtime_test.go (build tag: integration) |

## Mocks

- `MockWorkloadRuntime`: pkg/runtime/runtime_test.go (for testing Orchestrator)
- `MockDockerClient`: pkg/runtime/docker/mock_test.go (for testing DockerRuntime)
- `MockAgentClient`: pkg/mcp/mock_test.go
- HTTP handlers: use `net/http/httptest`

## Running Tests

```bash
make test                                    # Unit tests
make test-coverage                           # With coverage report
go test -tags=integration ./tests/integration/...  # Integration tests
```

## Integration Tests

Integration tests require Docker and use the `integration` build tag:

```go
//go:build integration

package integration
```

Run with:
```bash
make test-integration
```
