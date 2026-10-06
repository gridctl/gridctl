# MCP Apps

`stack.yaml` fronts the public Excalidraw MCP App and declares the client extension that server checks before registering UI-enabled tools. Resource URIs, including `ui://` values named by a tool's `_meta.ui.resourceUri`, pass through unchanged. gridctl does not render the HTML.

```bash
gridctl validate examples/mcp-apps/stack.yaml
gridctl apply examples/mcp-apps/stack.yaml
gridctl destroy examples/mcp-apps/stack.yaml
```

Apply contacts `https://mcp.excalidraw.com/mcp`. A UI-capable host then reads the tool's `resourceUri` through `resources/read`. See [MCP server fields](../../docs/config-schema.md#all-mcp-server-fields) and [POST /mcp](../../docs/api-reference.md#post-mcp).
