import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Terminal, Box, Wifi, Server } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { StatusDot } from '../ui/StatusDot';
import type { MCPServerNodeData, ResourceNodeData } from '../../types';

export type CustomNodeData = MCPServerNodeData | ResourceNodeData;

interface CustomNodeProps {
  data: CustomNodeData;
  selected?: boolean;
}

const CustomNode = memo(({ data, selected }: CustomNodeProps) => {
  const isServer = data.type === 'mcp-server';

  // Choose icon
  const Icon = isServer ? Terminal : Box;

  // Get transport info for MCP servers
  const transport = isServer ? (data as MCPServerNodeData).transport : null;
  const TransportIcon = transport === 'stdio' ? Server : Wifi;
  const toolCount = isServer ? (data as MCPServerNodeData).toolCount : null;

  // Get endpoint/containerId for MCP servers (filter out 'unknown')
  const endpoint = isServer ? (data as MCPServerNodeData).endpoint : null;
  const containerId = isServer ? (data as MCPServerNodeData).containerId : null;
  const hasValidEndpoint = endpoint && endpoint !== 'unknown';
  const hasValidContainerId = containerId && containerId !== 'unknown';

  // Image for resources
  const image = !isServer ? (data as ResourceNodeData).image : null;
  const network = !isServer ? (data as ResourceNodeData).network : null;

  return (
    <div
      className={cn(
        'w-64 shadow-node overflow-hidden rounded-lg',
        'bg-surface border border-border',
        'transition-all duration-200 ease-out',
        selected && 'border-primary ring-2 ring-primary/20',
        !selected && 'hover:shadow-node-hover hover:border-text-muted'
      )}
    >
      {/* Header */}
      <div className="bg-surface-highlight px-3 py-2.5 flex items-center justify-between border-b border-border">
        <div className="flex items-center gap-2 min-w-0">
          <div className={cn(
            'p-1.5 rounded-md',
            isServer ? 'bg-primary/15' : 'bg-secondary/15'
          )}>
            <Icon
              size={14}
              className={isServer ? 'text-primary' : 'text-secondary'}
            />
          </div>
          <span className="font-semibold text-sm text-text-primary truncate">
            {data.name}
          </span>
        </div>
        <StatusDot status={data.status} />
      </div>

      {/* Body */}
      <div className="p-3 space-y-2.5">
        {/* Endpoint Row (for HTTP MCP servers) */}
        {isServer && hasValidEndpoint && (
          <div className="space-y-0.5">
            <div className="flex items-center gap-1.5">
              <Wifi size={10} className="text-text-muted" />
              <span className="text-[10px] uppercase tracking-wider font-medium text-text-muted">
                Endpoint
              </span>
            </div>
            <div className="text-xs text-text-secondary font-mono truncate" title={endpoint}>
              {endpoint}
            </div>
          </div>
        )}

        {/* Container Row (for stdio MCP servers without endpoint) */}
        {isServer && !hasValidEndpoint && hasValidContainerId && (
          <div className="space-y-0.5">
            <div className="flex items-center gap-1.5">
              <Server size={10} className="text-text-muted" />
              <span className="text-[10px] uppercase tracking-wider font-medium text-text-muted">
                Container
              </span>
            </div>
            <div className="text-xs text-text-secondary font-mono truncate" title={containerId}>
              {containerId.slice(0, 12)}
            </div>
          </div>
        )}

        {/* Image Row (for resources) */}
        {!isServer && image && (
          <div className="space-y-0.5">
            <span className="text-[10px] uppercase tracking-wider font-medium text-text-muted">
              Image
            </span>
            <div className="text-xs text-text-secondary font-mono truncate" title={image}>
              {image}
            </div>
          </div>
        )}

        {/* Transport + Tool count (for MCP servers) */}
        {isServer && transport && (
          <div className="flex items-center justify-between text-xs">
            <div className="flex items-center gap-1.5 text-text-muted">
              <TransportIcon size={12} />
              <span className="uppercase text-[10px] tracking-wider">
                {transport}
              </span>
            </div>
            {toolCount !== null && toolCount !== undefined && (
              <span className="text-text-secondary">
                {toolCount} tools
              </span>
            )}
          </div>
        )}

        {/* Network (for resources) */}
        {!isServer && network && (
          <div className="flex items-center gap-1.5 text-xs text-text-muted">
            <Server size={12} />
            <span className="text-text-secondary">
              {network}
            </span>
          </div>
        )}

        {/* Status Badge */}
        <div className="pt-1">
          <Badge status={data.status}>
            <span className="capitalize">{data.status}</span>
          </Badge>
        </div>
      </div>

      {/* Connection Handles - Clean style */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-3 !h-3 !border-2 !border-background',
          isServer ? '!bg-primary' : '!bg-secondary',
          'transition-all duration-150'
        )}
        id="input"
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-3 !h-3 !border-2 !border-background',
          isServer ? '!bg-primary' : '!bg-secondary',
          'transition-all duration-150'
        )}
        id="output"
      />
    </div>
  );
});

CustomNode.displayName = 'CustomNode';

export default CustomNode;
