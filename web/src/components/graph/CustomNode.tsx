import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Terminal, Box, Hash, Globe, Wifi, Server, Cpu, KeyRound, HeartPulse } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { StatusDot } from '../ui/StatusDot';
import { getTransportIcon, getTransportColorClasses } from '../../lib/transport';
import type { MCPServerNodeData, ResourceNodeData } from '../../types';

export type CustomNodeData = MCPServerNodeData | ResourceNodeData;

interface CustomNodeProps {
  data: CustomNodeData;
  selected?: boolean;
}

const CustomNode = memo(({ data, selected }: CustomNodeProps) => {
  const isServer = data.type === 'mcp-server';
  const isExternal = isServer && (data as MCPServerNodeData).external;
  const isLocalProcess = isServer && (data as MCPServerNodeData).localProcess;
  const isSSH = isServer && (data as MCPServerNodeData).ssh;

  // Choose icon - Globe for external, Cpu for local process, KeyRound for SSH, Terminal for container-based
  const Icon = isServer ? (isExternal ? Globe : isLocalProcess ? Cpu : isSSH ? KeyRound : Terminal) : Box;

  // Get transport info for MCP servers
  const transport = isServer ? (data as MCPServerNodeData).transport : null;
  const TransportIcon = getTransportIcon(transport);
  const toolCount = isServer ? (data as MCPServerNodeData).toolCount : null;

  // Get endpoint/containerId for MCP servers
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
        'w-64 rounded-xl relative',
        'backdrop-blur-xl border transition-all duration-300 ease-out',
        isServer
          ? 'bg-gradient-to-br from-surface/95 to-violet-500/[0.03] border-violet-500/30'
          : 'bg-gradient-to-br from-surface/95 to-secondary/[0.02] border-border/50',
        selected && isServer && 'border-violet-500 shadow-[0_0_15px_rgba(139,92,246,0.3)] ring-1 ring-violet-500/30',
        selected && !isServer && 'border-secondary shadow-glow-secondary ring-1 ring-secondary/30',
        !selected && 'hover:shadow-node-hover hover:border-text-muted/30'
      )}
    >

      {/* Header with gradient accent */}
      <div className={cn(
        'px-3 py-2.5 flex items-center justify-between border-b relative',
        isServer
          ? 'border-violet-500/10 bg-violet-500/[0.03]'
          : 'border-secondary/10 bg-secondary/[0.03]'
      )}>
        {/* Accent line */}
        <div className={cn(
          'absolute top-0 left-0 right-0 h-px',
          isServer
            ? 'bg-gradient-to-r from-transparent via-violet-500/40 to-transparent'
            : 'bg-gradient-to-r from-transparent via-secondary/40 to-transparent'
        )} />

        <div className="flex items-center gap-2.5 min-w-0">
          <div className={cn(
            'p-1.5 rounded-lg border',
            isServer
              ? 'bg-violet-500/10 border-violet-500/20'
              : 'bg-secondary/10 border-secondary/20'
          )}>
            <Icon
              size={14}
              className={cn(
                isServer ? 'text-violet-400' : 'text-secondary'
              )}
            />
          </div>
          <span className="font-semibold text-sm text-text-primary truncate tracking-tight">
            {data.name}
          </span>
        </div>
        <StatusDot status={data.status} />
      </div>

      {/* Body */}
      <div className="p-3 space-y-2.5">
        {/* Endpoint Row (for HTTP MCP servers) */}
        {isServer && hasValidEndpoint && (
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <Wifi size={10} className="text-secondary" />
              <span className="text-[10px] uppercase tracking-widest font-medium text-text-muted">
                Endpoint
              </span>
            </div>
            <div className="text-xs text-text-secondary font-mono truncate bg-background/50 px-2 py-1 rounded-md" title={endpoint}>
              {endpoint}
            </div>
          </div>
        )}

        {/* Container Row (for stdio MCP servers) */}
        {isServer && !hasValidEndpoint && hasValidContainerId && (
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <Hash size={10} className="text-violet-400" />
              <span className="text-[10px] uppercase tracking-widest font-medium text-text-muted">
                Container
              </span>
            </div>
            <div className="text-xs text-text-secondary font-mono truncate bg-background/50 px-2 py-1 rounded-md" title={containerId}>
              {containerId.slice(0, 12)}
            </div>
          </div>
        )}

        {/* Image Row (for resources) */}
        {!isServer && image && (
          <div className="space-y-1">
            <span className="text-[10px] uppercase tracking-widest font-medium text-text-muted">
              Image
            </span>
            <div className="text-xs text-text-secondary font-mono truncate bg-background/50 px-2 py-1 rounded-md" title={image}>
              {image}
            </div>
          </div>
        )}

        {/* Transport + Tool count (for MCP servers) */}
        {isServer && transport && (
          <div className="flex items-center justify-between text-xs pt-1">
            <div className={cn(
              'flex items-center gap-1.5 px-2 py-1 rounded-md',
              getTransportColorClasses(transport)
            )}>
              <TransportIcon size={11} />
              <span className="uppercase text-[10px] tracking-wider font-medium">
                {transport}
              </span>
            </div>
            {toolCount !== null && toolCount !== undefined && (
              <span className="text-text-secondary font-mono text-[11px]">
                {toolCount} tools
              </span>
            )}
          </div>
        )}

        {/* Network (for resources) */}
        {!isServer && network && (
          <div className="flex items-center gap-1.5 text-xs text-secondary bg-secondary/10 px-2 py-1 rounded-md w-fit">
            <Server size={11} />
            <span className="font-medium">{network}</span>
          </div>
        )}

        {/* Health indicator (MCP servers only) */}
        {isServer && (data as MCPServerNodeData).healthy === false && (
          <div className="flex items-center gap-1.5 px-2 py-1.5 rounded-md bg-status-error/5 border border-status-error/15">
            <HeartPulse size={11} className="text-status-error flex-shrink-0" />
            <span className="text-xs text-status-error/80 font-mono truncate" title={(data as MCPServerNodeData).healthError}>
              {(data as MCPServerNodeData).healthError || 'Health check failed'}
            </span>
          </div>
        )}
        {isServer && (data as MCPServerNodeData).healthy === true && (
          <div className="flex items-center gap-1.5 text-xs text-text-muted">
            <HeartPulse size={10} className="text-status-running" />
            <span>Healthy</span>
          </div>
        )}

        {/* Status Badge + Type indicator */}
        <div className="pt-1 flex items-center gap-2">
          <Badge status={data.status}>
            <span className="capitalize">{data.status}</span>
          </Badge>
          {isServer && !isExternal && !isLocalProcess && !isSSH && (
            <div className={cn(
              'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg border',
              'text-[10px] font-semibold tracking-wide',
              'text-text-muted border-border/50'
            )}>
              <Terminal size={10} />
              Container
            </div>
          )}
          {isExternal && (
            <div className={cn(
              'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg border',
              'text-[10px] font-semibold tracking-wide',
              'text-text-muted border-border/50'
            )}>
              <Globe size={10} />
              External
            </div>
          )}
          {isLocalProcess && (
            <div className={cn(
              'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg border',
              'text-[10px] font-semibold tracking-wide',
              'text-text-muted border-border/50'
            )}>
              <Cpu size={10} />
              Local
            </div>
          )}
          {isSSH && (
            <div className={cn(
              'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg border',
              'text-[10px] font-semibold tracking-wide',
              'text-text-muted border-border/50'
            )}>
              <KeyRound size={10} />
              SSH
            </div>
          )}
        </div>
      </div>

      {/* Connection Handles - match node color */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          isServer ? '!bg-violet-500' : '!bg-secondary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="input"
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          isServer ? '!bg-violet-500' : '!bg-secondary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="output"
      />
    </div>
  );
});

CustomNode.displayName = 'CustomNode';

export default CustomNode;
