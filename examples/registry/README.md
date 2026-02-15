# Registry

Examples demonstrating reusable MCP prompts and skills via the registry.

## Examples

| File | Description |
|------|-------------|
| `registry-basic.yaml` | Single server — prompts and basic skill chains |
| `registry-advanced.yaml` | Two servers — cross-server skill chains |

## How the Registry Works

The registry stores prompt templates and skill chains as YAML files:

```
~/.gridctl/registry/
├── prompts/          # Prompt templates with arguments
│   └── {name}.yaml
└── skills/           # Multi-step tool chains
    └── {name}.yaml
```

Each item has a lifecycle state:
- **draft** — stored but not exposed via MCP (default)
- **active** — exposed as an MCP prompt or tool
- **disabled** — temporarily hidden without deletion

Items are managed via the REST API or Web UI — they are **not** declared in stack YAML.

## Prerequisites

Build the mock MCP servers:

```bash
make mock-servers
```

## Quick Start

### Option A: Copy Pre-Made Items

```bash
# Copy items into the registry
mkdir -p ~/.gridctl/registry/prompts ~/.gridctl/registry/skills
cp examples/registry/items/prompts/*.yaml ~/.gridctl/registry/prompts/
cp examples/registry/items/skills/echo-and-time.yaml ~/.gridctl/registry/skills/
cp examples/registry/items/skills/add-and-echo.yaml ~/.gridctl/registry/skills/

# Deploy the stack
gridctl deploy examples/registry/registry-basic.yaml

# Activate a skill and test it
curl -X POST http://localhost:8180/api/registry/skills/echo-and-time/activate
curl -X POST http://localhost:8180/api/registry/skills/echo-and-time/test \
  -H 'Content-Type: application/json' \
  -d '{"message": "hello world"}'
```

### Option B: Create via API

```bash
# Deploy the stack first
gridctl deploy examples/registry/registry-basic.yaml

# Create a prompt via API
curl -X POST http://localhost:8180/api/registry/prompts \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "greeting",
    "description": "Simple greeting prompt",
    "content": "Hello {{name}}, welcome to {{project}}!",
    "arguments": [
      {"name": "name", "description": "Person to greet", "required": true},
      {"name": "project", "description": "Project name", "required": true}
    ]
  }'

# Create a skill via API
curl -X POST http://localhost:8180/api/registry/skills \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "quick-echo",
    "description": "Echo a message",
    "input": [{"name": "message", "description": "Message", "required": true}],
    "steps": [{"tool": "local-tools__echo", "arguments": {"message": "{{input.message}}"}}]
  }'
```

## Pre-Made Items

### Prompts

| File | Arguments | Description |
|------|-----------|-------------|
| `code-review.yaml` | `language`\*, `focus_area`, `code`\* | Structured code review template |
| `explain-error.yaml` | `error_message`\*, `context` | Error explanation template |

\* = required

### Skills

| File | Inputs | Steps | For Stack |
|------|--------|-------|-----------|
| `echo-and-time.yaml` | `message`\* | echo → get_time | registry-basic |
| `add-and-echo.yaml` | `a`\*, `b`\* | add → echo (step piping) | registry-basic |
| `chained-calculation.yaml` | `a`\*, `b`\* | add → echo → get_time (cross-server) | registry-advanced |

## Concepts

### Prompts

Prompts are text templates with named arguments. When active, they appear in MCP `prompts/list` responses so LLM clients can discover and use them.

```yaml
name: code-review
content: |
  Review the following {{language}} code.
  {{code}}
arguments:
  - name: language
    description: Programming language
    required: true
  - name: code
    description: Code to review
    required: true
state: draft
```

Arguments use `{{argument_name}}` syntax in the content field.

### Skills

Skills are multi-step tool chains. When active, each skill appears as a single MCP tool. Clients call the skill by name and the gateway executes all steps sequentially.

```yaml
name: add-and-echo
input:
  - name: a
    required: true
  - name: b
    required: true
steps:
  - tool: local-tools__add
    arguments:
      a: "{{input.a}}"        # Substituted from skill input
      b: "{{input.b}}"
  - tool: local-tools__echo
    arguments:
      message: "{{step1.result}}"  # Output from step 1
state: draft
```

**Template variables:**
- `{{input.name}}` — substitutes a skill input parameter
- `{{stepN.result}}` — substitutes the output from step N (1-indexed)

### Tool Naming

Tools are prefixed with the MCP server name to avoid collisions:

```
server-name__tool-name
```

For example, a server named `local-tools` with an `echo` tool becomes `local-tools__echo`.

## Testing

### REST API

```bash
# Check registry status
curl http://localhost:8180/api/registry/status | jq

# List all prompts and skills
curl http://localhost:8180/api/registry/prompts | jq
curl http://localhost:8180/api/registry/skills | jq

# Activate a skill
curl -X POST http://localhost:8180/api/registry/skills/add-and-echo/activate

# Test a skill with arguments
curl -X POST http://localhost:8180/api/registry/skills/add-and-echo/test \
  -H 'Content-Type: application/json' \
  -d '{"a": "10", "b": "20"}'
```

### Web UI

1. Open http://localhost:8180 in a browser
2. Navigate to the Registry section
3. Activate items and run test executions from the UI

### MCP Protocol

```bash
# List active prompts via MCP
curl -X POST http://localhost:8180/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"prompts/list"}'

# List tools (active skills appear here)
curl http://localhost:8180/api/tools | jq '.tools[].name'
```
