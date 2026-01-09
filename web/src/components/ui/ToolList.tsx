import { useState } from 'react';
import { ChevronDown, ChevronRight, Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useTopologyStore } from '../../stores/useTopologyStore';
import { parsePrefixedToolName } from '../../lib/transform';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import type { Tool } from '../../types';

// Helper type for accessing common JSON Schema properties
interface SchemaProperty {
  type?: string;
  description?: string;
}

// Safe accessor for inputSchema properties
function getSchemaProperties(schema: Record<string, unknown>): Record<string, SchemaProperty> | undefined {
  const props = schema.properties;
  if (props && typeof props === 'object') {
    return props as Record<string, SchemaProperty>;
  }
  return undefined;
}

function getSchemaRequired(schema: Record<string, unknown>): string[] | undefined {
  const required = schema.required;
  if (Array.isArray(required)) {
    return required as string[];
  }
  return undefined;
}

interface ToolListProps {
  agentName: string;
}

export function ToolList({ agentName }: ToolListProps) {
  const tools = useTopologyStore((s) => s.tools);

  // Filter tools for this agent (prefixed with agentName::)
  const agentTools = tools.filter((t) =>
    t.name.startsWith(`${agentName}${TOOL_NAME_DELIMITER}`)
  );

  if (agentTools.length === 0) {
    return (
      <p className="text-sm text-text-muted italic px-4 py-2">
        No tools available
      </p>
    );
  }

  return (
    <div className="space-y-1 px-2">
      {agentTools.map((tool) => (
        <ToolItem key={tool.name} tool={tool} />
      ))}
    </div>
  );
}

interface ToolItemProps {
  tool: Tool;
}

function ToolItem({ tool }: ToolItemProps) {
  const [expanded, setExpanded] = useState(false);

  // Remove agent prefix for display
  const { toolName } = parsePrefixedToolName(tool.name);

  const properties = getSchemaProperties(tool.inputSchema);
  const required = getSchemaRequired(tool.inputSchema);
  const hasParams = properties && Object.keys(properties).length > 0;

  return (
    <div className="rounded bg-background/50">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 p-2 hover:bg-surfaceHighlight/50 rounded text-left"
      >
        {hasParams ? (
          expanded ? (
            <ChevronDown size={12} className="text-text-muted flex-shrink-0" />
          ) : (
            <ChevronRight size={12} className="text-text-muted flex-shrink-0" />
          )
        ) : (
          <div className="w-3" />
        )}
        <Wrench size={12} className="text-primary flex-shrink-0" />
        <span className="text-sm font-mono text-text-primary truncate">
          {toolName}
        </span>
      </button>

      {expanded && (
        <div className="px-4 pb-3 space-y-2">
          {tool.description && (
            <p className="text-xs text-text-muted">{tool.description}</p>
          )}

          {properties && (
            <div className="space-y-1.5">
              <span className="text-[10px] text-text-muted uppercase tracking-wider font-bold">
                Parameters
              </span>
              {Object.entries(properties).map(([name, prop]) => (
                <div key={name} className="flex flex-col gap-0.5">
                  <div className="flex items-center gap-2 text-xs">
                    <span className={cn(
                      'font-mono',
                      required?.includes(name)
                        ? 'text-status-pending'
                        : 'text-text-secondary'
                    )}>
                      {name}
                    </span>
                    <span className="text-text-muted">({prop.type ?? 'any'})</span>
                    {required?.includes(name) && (
                      <span className="text-[10px] text-status-pending">required</span>
                    )}
                  </div>
                  {prop.description && (
                    <span className="text-[11px] text-text-muted pl-2">
                      {prop.description}
                    </span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
