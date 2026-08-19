# ⚡ Code Mode

Reduce context window consumption by replacing individual tool definitions with two meta-tools.

## 📄 Examples

| File | Description |
|------|-------------|
| `code-mode-basic.yaml` | Gateway code mode with search + execute meta-tools |

## 💡 Concepts

### Why Code Mode

When a stack exposes dozens of tools, each tool definition consumes context window tokens. Code mode replaces all tool definitions with two meta-tools - `search` and `execute` - cutting context overhead by 99%+.

### Meta-Tools

| Tool | Purpose |
|------|---------|
| `search` | Discover tools by name, description, or parameter names |
| `execute` | Run JavaScript code that calls tools via `mcp.callTool()` |

### Agent Workflow

1. Agent calls `search` with a query (or empty string to list all tools)
2. Agent reviews matching tool signatures and input schemas
3. Agent writes JavaScript using `mcp.callTool(serverName, toolName, args)`
4. Agent calls `execute` with the code
5. Sandbox runs the code and returns the result + console output

### Sandbox

Code runs in a [goja](https://github.com/dop251/goja) JavaScript runtime (ES5.1 compatible). Modern syntax (arrow functions, destructuring, template literals) is transpiled via esbuild.

**Bindings:**
- `mcp.callTool(serverName, toolName, args)` - synchronous, returns parsed objects
- `console.log()`, `console.warn()`, `console.error()` - captured in response

**Limits:**
- Max code size: 64 KB
- Default timeout: 30 seconds (configurable via `code_mode_timeout`)

### ACL Enforcement

Tool access is enforced inside the sandbox using the connecting client's allowed-tool set. A client restricted via `clients:` scoping or connected through a group endpoint can only reach its permitted tools, even through `mcp.callTool()`. See [access-control/](../access-control/) for scoping examples.

## ⚙️ Configuration

Enable in stack YAML:

```yaml
gateway:
  code_mode: "on"
  code_mode_timeout: 30
```

Or via CLI flag on any stack:

```bash
gridctl apply stack.yaml --code-mode
```

## 💻 Usage

```bash
gridctl apply examples/code-mode/code-mode-basic.yaml
```
