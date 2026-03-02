# Registry

Examples demonstrating the Agent Skills registry following the [agentskills.io](https://agentskills.io) specification.

## Examples

| File | Description |
|------|-------------|
| `registry-basic.yaml` | Single server with basic Agent Skills |
| `registry-advanced.yaml` | Two servers with cross-server skills |
| `items/workflow-basic/` | Executable workflow: sequential steps with inputs and output |
| `items/workflow-parallel/` | Executable workflow: fan-out parallel execution |
| `items/workflow-conditional/` | Executable workflow: retry policies and error handling |

## How the Registry Works

The registry stores Agent Skills as SKILL.md files — markdown documents with YAML frontmatter:

```
~/.gridctl/registry/
└── skills/
    └── {name}/
        ├── SKILL.md          # Required: frontmatter + markdown body
        ├── scripts/          # Optional: executable scripts
        ├── references/       # Optional: reference materials
        └── assets/           # Optional: images, data files
```

Each skill has a lifecycle state:
- **draft** — stored but not exposed via MCP (default)
- **active** — exposed as an MCP prompt and resource
- **disabled** — temporarily hidden without deletion

Skills are managed via the REST API or Web UI — they are **not** declared in stack YAML.

## Prerequisites

Build the mock MCP servers:

```bash
make mock-servers
```

## Quick Start

### Option A: Copy Pre-Made Skills

```bash
# Copy skill directories into the registry
cp -r examples/registry/items/* ~/.gridctl/registry/skills/

# Deploy the stack
gridctl deploy examples/registry/registry-basic.yaml

# Activate a skill
curl -X POST http://localhost:8180/api/registry/skills/echo-and-time/activate
```

### Option B: Create via API

```bash
# Deploy the stack first
gridctl deploy examples/registry/registry-basic.yaml

# Create a skill via API
curl -X POST http://localhost:8180/api/registry/skills \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "my-skill",
    "description": "A helpful skill",
    "body": "# My Skill\n\nInstructions for the LLM..."
  }'

# Activate it
curl -X POST http://localhost:8180/api/registry/skills/my-skill/activate
```

## Pre-Made Skills

| Skill | Tags | For Stack |
|-------|------|-----------|
| `code-review` | development, review | any |
| `explain-error` | debugging, errors | any |
| `echo-and-time` | basic, demo | registry-basic |
| `add-and-echo` | chaining, demo | registry-basic |
| `chained-calculation` | cross-server, chaining | registry-advanced |
| `workflow-basic` | workflow, demo | registry-basic |
| `workflow-parallel` | workflow, parallel | registry-basic |
| `workflow-conditional` | workflow, error-handling | registry-basic |

## SKILL.md Format

Skills use YAML frontmatter followed by a markdown body:

```markdown
---
name: my-skill
description: What this skill does
tags:
  - category
allowed-tools: server__tool1, server__tool2
state: draft
---

# My Skill

Instructions, context, and workflow steps for the LLM.
```

**Required fields:** `name`, `description`

**Optional fields:** `tags`, `allowed-tools`, `license`, `compatibility`, `metadata`, `state`

### Executable Workflows

Add `inputs`, `workflow`, and `output` blocks to make a skill executable. Executable skills are exposed as MCP **tools** (not prompts) and run multi-step tool orchestration.

```markdown
---
name: add-and-format
description: Add two numbers and format the result
allowed-tools: local-tools__add, local-tools__echo
state: active

inputs:
  a:
    type: number
    description: First number
    required: true
  b:
    type: number
    description: Second number
    required: true

workflow:
  - id: add
    tool: local-tools__add
    args:
      a: "{{ inputs.a }}"
      b: "{{ inputs.b }}"

  - id: format
    tool: local-tools__echo
    args:
      message: "Result: {{ steps.add.result }}"
    depends_on: add

output:
  format: last
---

# Add and Format

Adds two numbers and echoes a formatted result.
```

**Workflow fields:**

| Field | Description |
|-------|-------------|
| `inputs` | Parameter definitions (`type`, `description`, `required`, `default`, `enum`) |
| `workflow[].id` | Unique step identifier |
| `workflow[].tool` | Tool to call (`server__tool` format) |
| `workflow[].args` | Arguments with template expressions (`{{ inputs.x }}`, `{{ steps.id.result }}`) |
| `workflow[].depends_on` | Step ID(s) that must complete first |
| `workflow[].on_error` | `fail` (default), `skip`, or `continue` |
| `workflow[].timeout` | Per-step timeout (e.g., `"30s"`) |
| `workflow[].retry` | `max_attempts` and `backoff` for retries |
| `workflow[].condition` | Boolean expression to control execution |
| `output.format` | `merged` (default), `last`, or `custom` |

Steps without `depends_on` run in parallel. The executor builds a DAG and groups steps into levels for concurrent execution.

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

# List all skills
curl http://localhost:8180/api/registry/skills | jq

# Activate a skill
curl -X POST http://localhost:8180/api/registry/skills/echo-and-time/activate

# Validate a SKILL.md without saving
curl -X POST http://localhost:8180/api/registry/skills/validate \
  -H 'Content-Type: text/markdown' \
  -d '---
name: test
description: Test skill
---
# Test'

# Manage supporting files
curl http://localhost:8180/api/registry/skills/echo-and-time/files | jq
```

### Workflow Execution

```bash
# Get workflow definition and DAG
curl http://localhost:8180/api/registry/skills/workflow-basic/workflow | jq

# Dry-run validation (no execution)
curl -X POST http://localhost:8180/api/registry/skills/workflow-basic/validate-workflow \
  -H 'Content-Type: application/json' \
  -d '{"arguments": {"a": 5, "b": 3}}'

# Execute a workflow
curl -X POST http://localhost:8180/api/registry/skills/workflow-basic/execute \
  -H 'Content-Type: application/json' \
  -d '{"arguments": {"a": 5, "b": 3}}'
```

### Web UI

1. Open http://localhost:8180 in a browser
2. Navigate to the Registry section
3. Create and edit skills with the split-pane markdown editor
4. Use the workflow designer (Code / Visual / Test modes) for executable skills
5. Activate skills and browse supporting files

### MCP Protocol

```bash
# List active skills as MCP prompts
curl -X POST http://localhost:8180/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"prompts/list"}'

# Get a specific skill prompt
curl -X POST http://localhost:8180/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"prompts/get","params":{"name":"code-review"}}'
```
