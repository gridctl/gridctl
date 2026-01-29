/**
 * Transform utility functions
 *
 * Helper functions for parsing tool names and grouping tools.
 */

import { TOOL_NAME_DELIMITER } from './constants';

/**
 * Parse prefixed tool name into agent and tool names
 * Matches the format from pkg/mcp/router.go: "agent__tool"
 */
export function parsePrefixedToolName(prefixed: string): {
  agentName: string;
  toolName: string;
} {
  const idx = prefixed.indexOf(TOOL_NAME_DELIMITER);
  if (idx === -1) {
    return { agentName: '', toolName: prefixed };
  }
  return {
    agentName: prefixed.slice(0, idx),
    toolName: prefixed.slice(idx + TOOL_NAME_DELIMITER.length),
  };
}

/**
 * Group tools by their owning MCP server
 */
export function groupToolsByServer(
  tools: { name: string }[]
): Map<string, string[]> {
  const grouped = new Map<string, string[]>();

  for (const tool of tools) {
    const { agentName, toolName } = parsePrefixedToolName(tool.name);
    const existing = grouped.get(agentName) || [];
    existing.push(toolName);
    grouped.set(agentName, existing);
  }

  return grouped;
}
