import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Terminal, Box, Settings, Wifi, Server, GitBranch } from 'lucide-react';
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
  const isRunning = data.status === 'running';
  const isProcessing = data.status === 'initializing';

  // Determine glass class based on type (Cyan for MCP servers, Purple for resources)
  const glassClass = isServer ? 'node-glass-cyan' : 'node-glass-purple';
  const glowClass = isRunning ? (isServer ? 'shadow-glow-primary' : 'shadow-glow-secondary') : '';

  // Choose icon
  const Icon = isServer ? Terminal : Box;

  // Get transport info for MCP servers
  const transport = isServer ? (data as MCPServerNodeData).transport : null;
  const TransportIcon = transport === 'stdio' ? Server : Wifi;
  const toolCount = isServer ? (data as MCPServerNodeData).toolCount : null;

  // Format source display
  const sourceDisplay = isServer
    ? (data as MCPServerNodeData).endpoint || (data as MCPServerNodeData).containerId || 'unknown'
    : (data as ResourceNodeData).image;

  return (
    <div
      className={cn(
        'w-64 shadow-node overflow-hidden node-rim-light',
        'transition-all duration-200 ease-out',
        glassClass,
        glowClass,
        isProcessing && 'node-border-animated',
        selected && 'node-active-glow ring-2 ring-primary/50 ring-offset-2 ring-offset-background',
        !selected && 'hover:shadow-node-hover'
      )}
    >
      {/* Header */}
      <div className="bg-white/5 px-3 py-2.5 flex items-center justify-between border-b border-white/10">
        <div className="flex items-center gap-2 min-w-0">
          <div className={cn(
            'p-1.5 rounded-md',
            isServer ? 'bg-primary/20' : 'bg-secondary/20'
          )}>
            <Icon
              size={14}
              className={cn(
                isServer ? 'text-primary' : 'text-secondary',
                isRunning && 'drop-shadow-[0_0_4px_currentColor]'
              )}
            />
          </div>
          <span className="font-semibold text-sm text-text-primary truncate">
            {data.name}
          </span>
        </div>
        <button
          className="p-1 rounded hover:bg-white/10 transition-colors"
          onClick={(e) => {
            e.stopPropagation();
          }}
        >
          <Settings size={14} className="text-text-muted hover:text-text-primary" />
        </button>
      </div>

      {/* Body */}
      <div className="p-3 space-y-3">
        {/* Image/Endpoint Row */}
        <div className="space-y-1">
          <div className="flex items-center gap-1.5">
            {isServer && (data as MCPServerNodeData).endpoint ? (
              <Wifi size={10} className="text-text-muted" />
            ) : (
              <GitBranch size={10} className="text-text-muted" />
            )}
            <span className="text-[10px] uppercase tracking-wider font-bold text-text-muted">
              {isServer ? (transport === 'http' ? 'Endpoint' : 'Container') : 'Image'}
            </span>
          </div>
          <div className="text-xs text-text-secondary font-mono truncate" title={sourceDisplay}>
            {sourceDisplay}
          </div>
        </div>

        {/* Transport (for MCP servers) */}
        {isServer && transport && (
          <div className="flex items-center gap-2 text-xs text-text-muted">
            <TransportIcon size={12} />
            <span className="uppercase text-[10px] tracking-wider">
              {transport}
            </span>
            {toolCount !== null && toolCount !== undefined && (
              <span className="ml-auto text-text-secondary">
                {toolCount} tools
              </span>
            )}
          </div>
        )}

        {/* Network (for resources) */}
        {!isServer && (data as ResourceNodeData).network && (
          <div className="flex items-center gap-2 text-xs text-text-muted">
            <Server size={12} />
            <span className="text-text-secondary">
              {(data as ResourceNodeData).network}
            </span>
          </div>
        )}

        {/* Footer Row: Status */}
        <div className="flex justify-between items-center pt-1">
          <Badge status={data.status}>
            <StatusDot status={data.status} />
            <span>{data.status}</span>
          </Badge>
        </div>
      </div>

      {/* Connection Handles - Neon style */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-3 !h-3 !border-2 !border-background',
          isServer ? '!bg-primary/60' : '!bg-secondary/60',
          'hover:!bg-primary hover:!shadow-[0_0_8px_rgba(0,202,255,0.6)]',
          'transition-all duration-150'
        )}
        id="input"
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-3 !h-3 !border-2 !border-background',
          isServer ? '!bg-primary/60' : '!bg-secondary/60',
          'hover:!bg-primary hover:!shadow-[0_0_8px_rgba(0,202,255,0.6)]',
          'transition-all duration-150'
        )}
        id="output"
      />
    </div>
  );
});

CustomNode.displayName = 'CustomNode';

export default CustomNode;
