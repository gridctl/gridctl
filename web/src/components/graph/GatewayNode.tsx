import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Activity, Server, Wrench, Zap, Bot, Users, Radio, ListChecks, Monitor } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import type { GatewayNodeData } from '../../types';

interface GatewayNodeProps {
  data: GatewayNodeData;
  selected?: boolean;
}

const GatewayNode = memo(({ data, selected }: GatewayNodeProps) => {
  return (
    <div
      className={cn(
        'w-60 rounded-2xl',
        'bg-gradient-to-b from-surface/95 via-surface/90 to-primary/[0.03]',
        'backdrop-blur-xl border border-primary/20',
        'shadow-lg transition-all duration-300 ease-out',
        selected && 'border-primary shadow-glow-primary ring-2 ring-primary/20',
        !selected && 'hover:shadow-node-hover hover:border-primary/40'
      )}
    >
      {/* Top accent gradient */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/50 to-transparent" />

      {/* Header */}
      <div className="px-4 py-3.5 flex items-center gap-3 border-b border-primary/10 bg-primary/[0.02] relative">
        {/* Glowing logo */}
        <div className="relative">
          <div className="p-2.5 bg-gradient-to-br from-primary/20 to-primary/5 rounded-xl border border-primary/30 shadow-glow-primary">
            <Activity size={20} className="text-primary" />
          </div>
          {/* Pulse effect */}
          <div
            className="absolute inset-0 rounded-xl bg-primary/20 animate-ping"
            style={{ animationDuration: '2s', animationIterationCount: 'infinite' }}
          />
        </div>
        <div>
          <h3 className="font-bold text-sm text-text-primary tracking-tight">{data.name}</h3>
          <p className="text-[10px] text-text-muted font-mono tracking-wider">{data.version}</p>
        </div>
      </div>

      {/* Stats Grid */}
      <div className="p-4 space-y-3">
        {/* MCP Servers */}
        <div className="flex items-center justify-between group">
          <div className="flex items-center gap-2.5">
            <div className="p-1.5 rounded-lg bg-primary/10 border border-primary/20 group-hover:bg-primary/15 transition-colors">
              <Server size={12} className="text-primary" />
            </div>
            <span className="text-xs text-text-secondary font-medium">MCP Servers</span>
          </div>
          <span className="text-sm font-bold text-text-primary tabular-nums">
            {data.serverCount}
          </span>
        </div>

        {/* Agents */}
        {data.agentCount > 0 && (
          <div className="flex items-center justify-between group">
            <div className="flex items-center gap-2.5">
              <div className="p-1.5 rounded-lg bg-tertiary/10 border border-tertiary/20 group-hover:bg-tertiary/15 transition-colors">
                <Bot size={12} className="text-tertiary" />
              </div>
              <span className="text-xs text-text-secondary font-medium">Agents</span>
            </div>
            <span className="text-sm font-bold text-text-primary tabular-nums">
              {data.agentCount}
            </span>
          </div>
        )}

        {/* A2A Agents */}
        {data.a2aAgentCount > 0 && (
          <div className="flex items-center justify-between group">
            <div className="flex items-center gap-2.5">
              <div className="p-1.5 rounded-lg bg-secondary/10 border border-secondary/20 group-hover:bg-secondary/15 transition-colors">
                <Users size={12} className="text-secondary" />
              </div>
              <span className="text-xs text-text-secondary font-medium">A2A Agents</span>
            </div>
            <span className="text-sm font-bold text-text-primary tabular-nums">
              {data.a2aAgentCount}
            </span>
          </div>
        )}

        {/* Resources */}
        {data.resourceCount > 0 && (
          <div className="flex items-center justify-between group">
            <div className="flex items-center gap-2.5">
              <div className="p-1.5 rounded-lg bg-secondary/10 border border-secondary/20 group-hover:bg-secondary/15 transition-colors">
                <Server size={12} className="text-secondary" />
              </div>
              <span className="text-xs text-text-secondary font-medium">Resources</span>
            </div>
            <span className="text-sm font-bold text-text-primary tabular-nums">
              {data.resourceCount}
            </span>
          </div>
        )}

        {/* Sessions */}
        {(data.sessions ?? 0) > 0 && (
          <div className="flex items-center justify-between group">
            <div className="flex items-center gap-2.5">
              <div className="p-1.5 rounded-lg bg-primary/10 border border-primary/20 group-hover:bg-primary/15 transition-colors">
                <Radio size={12} className="text-primary" />
              </div>
              <span className="text-xs text-text-secondary font-medium">Sessions</span>
            </div>
            <span className="text-sm font-bold text-text-primary tabular-nums">
              {data.sessions}
            </span>
          </div>
        )}

        {/* Linked Clients */}
        {(data.clientCount ?? 0) > 0 && (
          <div className="flex items-center justify-between group">
            <div className="flex items-center gap-2.5">
              <div className="p-1.5 rounded-lg bg-primary/10 border border-primary/20 group-hover:bg-primary/15 transition-colors">
                <Monitor size={12} className="text-primary" />
              </div>
              <span className="text-xs text-text-secondary font-medium">Clients</span>
            </div>
            <span className="text-sm font-bold text-text-primary tabular-nums">
              {data.clientCount}
            </span>
          </div>
        )}

        {/* A2A Tasks */}
        {data.a2aTasks != null && data.a2aTasks > 0 && (
          <div className="flex items-center justify-between group">
            <div className="flex items-center gap-2.5">
              <div className="p-1.5 rounded-lg bg-secondary/10 border border-secondary/20 group-hover:bg-secondary/15 transition-colors">
                <ListChecks size={12} className="text-secondary" />
              </div>
              <span className="text-xs text-text-secondary font-medium">A2A Tasks</span>
            </div>
            <span className="text-sm font-bold text-text-primary tabular-nums">
              {data.a2aTasks}
            </span>
          </div>
        )}

        {/* Tools */}
        <div className="flex items-center justify-between group">
          <div className="flex items-center gap-2.5">
            <div className="p-1.5 rounded-lg bg-surface-highlight border border-border group-hover:border-text-muted/30 transition-colors">
              <Wrench size={12} className="text-text-secondary" />
            </div>
            <span className="text-xs text-text-secondary font-medium">Total Tools</span>
          </div>
          <span className="text-sm font-bold text-text-primary tabular-nums">
            {data.totalToolCount}
          </span>
        </div>

        {/* Status indicator */}
        <div className="flex items-center gap-2.5 pt-2 mt-1 border-t border-border/50">
          <div className="flex items-center gap-2 px-2.5 py-1.5 rounded-full bg-status-running/10 border border-status-running/20">
            <StatusDot status="running" />
            <span className="text-[11px] text-status-running font-semibold tracking-wide">
              Gateway Active
            </span>
          </div>
          <Zap size={14} className="text-primary animate-pulse" style={{ animationDuration: '2s' }} />
        </div>
      </div>

      {/* Connection Handles - Left side: separate handles for agents and clients */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-3 !h-3 !bg-tertiary !border-2 !border-background !rounded-full',
          'transition-all duration-200 hover:!scale-125 hover:!shadow-glow-tertiary'
        )}
        id="agent-input"
        style={{ top: '30%' }}
      />
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-3 !h-3 !bg-primary !border-2 !border-background !rounded-full',
          'transition-all duration-200 hover:!scale-125 hover:!shadow-glow-primary'
        )}
        id="client-input"
        style={{ top: '70%' }}
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-3.5 !h-3.5 !bg-primary !border-2 !border-background !rounded-full',
          'transition-all duration-200 hover:!scale-125 hover:!shadow-glow-primary'
        )}
        id="output"
      />
    </div>
  );
});

GatewayNode.displayName = 'GatewayNode';

export default GatewayNode;
