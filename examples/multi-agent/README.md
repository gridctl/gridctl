# 🤖 Multi-Agent

Advanced examples demonstrating agent orchestration and the A2A protocol.

## 📄 Examples

| File | Description |
|------|-------------|
| `multi-agent-skills.yaml` | Agents using other agents as tools via unified abstraction |
| `basic-a2a.yaml` | Agent-to-Agent protocol with skills advertisement |

## 💡 Concepts

### Unified Tool Abstraction

Agents can "equip" other agents as skills through the MCP interface:

```
orchestrator-agent
    +-- uses: [filesystem-server]     (MCP server)
    +-- uses: [research-agent]        (Agent as skill)
```

The orchestrator sees all tools with prefixes:
- `filesystem-server__read_file`
- `research-agent__web-research`

### A2A Protocol

Agents expose skills via the Agent-to-Agent protocol:

```yaml
a2a:
  enabled: true
  skills:
    - id: code-review
      name: Code Review
      description: "Review code for bugs and best practices"
```

## 💻 Usage

```bash
agentlab deploy examples/multi-agent/multi-agent-skills.yaml
agentlab deploy examples/multi-agent/basic-a2a.yaml
```

## 🧪 Testing

```bash
# List available tools
curl http://localhost:8080/api/tools | jq '.tools[].name'

# Fetch agent card
curl http://localhost:5555/.well-known/agent.json | jq
```
