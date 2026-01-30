# Example Stack Conventions

## Directory Structure

Examples are organized by category:

```
examples/
├── getting-started/      # Basic examples for new users
│   ├── agent-basic.yaml  # Minimal agent example
│   └── multi-agent.yaml  # Multiple agents example
├── transports/           # Transport-specific examples
│   ├── stdio.yaml        # Stdio transport
│   ├── http.yaml         # HTTP transport
│   ├── ssh.yaml          # SSH transport
│   └── external.yaml     # External URL transport
├── access-control/       # Tool filtering and security
│   └── restricted.yaml   # Agent-level tool restrictions
└── _mock-servers/        # Mock MCP servers for testing
    └── ...               # Test server implementations
```

## Stack File Naming

- Use descriptive kebab-case names: `agent-basic.yaml`, `multi-network.yaml`
- Prefix with underscore for internal/test files: `_mock-servers/`

## Example Requirements

Each example should:
1. Be self-contained (runnable with just `gridctl deploy`)
2. Include comments explaining non-obvious configuration
3. Use realistic but safe defaults (no real API keys)
4. Demonstrate a single concept clearly

## Environment Variables

For examples requiring secrets:
- Use placeholder environment variables: `${API_KEY}`
- Document required variables in comments at top of file

```yaml
# Required environment variables:
#   API_KEY - Your service API key
version: "1"
name: example
# ...
```

## Testing Examples

Mock servers in `_mock-servers/` provide test doubles:
- Build with: `make mock-servers`
- Clean with: `make clean-mock-servers`
