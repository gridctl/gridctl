package mcp

import "encoding/json"

// MetaToolSearch is the name of the search meta-tool.
const MetaToolSearch = "search"

// MetaToolExecute is the name of the execute meta-tool.
const MetaToolExecute = "execute"

// SearchTool returns the Tool definition for the search meta-tool.
func SearchTool() Tool {
	schema := InputSchemaObject{
		Type: "object",
		Properties: map[string]Property{
			"query": {
				Type:        "string",
				Description: "Search query to find tools by name, description, or parameter names. Leave empty to list all available tools.",
			},
		},
		Required: []string{"query"},
	}
	schemaJSON, _ := json.Marshal(schema)

	return Tool{
		Name:        MetaToolSearch,
		Description: "Search available MCP tools by name, description, or parameter names. Returns matching tool signatures with their input schemas. Use this to discover what tools are available before writing code to call them.",
		InputSchema: schemaJSON,
	}
}

// ExecuteTool returns the Tool definition for the execute meta-tool.
func ExecuteTool() Tool {
	schema := InputSchemaObject{
		Type: "object",
		Properties: map[string]Property{
			"code": {
				Type:        "string",
				Description: "JavaScript code to execute. Use mcp.callTool(serverName, toolName, args) to call MCP tools. The binding is synchronous and returns parsed objects directly. Console output (console.log/warn/error) is captured and included in the response.",
			},
		},
		Required: []string{"code"},
	}
	schemaJSON, _ := json.Marshal(schema)

	return Tool{
		Name:        MetaToolExecute,
		Description: "Execute JavaScript code in a sandboxed environment with access to MCP tools via mcp.callTool(serverName, toolName, args). The code can call multiple tools, process results, and return computed values. Console output is captured. Modern JavaScript syntax (arrow functions, destructuring, template literals) is supported.",
		InputSchema: schemaJSON,
	}
}
