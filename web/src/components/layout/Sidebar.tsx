import { useState } from 'react';
import { X, Terminal, Box, Bot, ChevronDown, ChevronRight, Wrench, FileText, Sparkles, Globe, Server, Zap } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { ToolList } from '../ui/ToolList';
import { ControlBar } from '../ui/ControlBar';
import { getTransportIcon, getTransportColorClasses } from '../../lib/transport';
import { useTopologyStore, useSelectedNodeData } from '../../stores/useTopologyStore';
import { useUIStore } from '../../stores/useUIStore';
import type { MCPServerNodeData, ResourceNodeData, AgentNodeData } from '../../types';

export function Sidebar() {
  const selectedData = useSelectedNodeData();
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const setBottomPanelOpen = useUIStore((s) => s.setBottomPanelOpen);
  const selectNode = useTopologyStore((s) => s.selectNode);

  if (!selectedData || selectedData.type === 'gateway') {
    return null;
  }

  const isServer = selectedData.type === 'mcp-server';
  const isAgent = selectedData.type === 'agent';
  const data = selectedData as unknown as MCPServerNodeData | ResourceNodeData | AgentNodeData;

  // For MCP servers, determine if external
  const serverData = isServer ? (data as MCPServerNodeData) : null;
  const isExternal = serverData?.external ?? false;

  // For agents, determine variant and A2A capability
  const agentData = isAgent ? (data as AgentNodeData) : null;
  const isRemote = agentData?.variant === 'remote';
  const hasA2A = agentData?.hasA2A ?? false;

  // Icon logic: Globe for external MCP servers, Terminal for container-based
  const Icon = isServer ? (isExternal ? Globe : Terminal) : isAgent ? Bot : Box;

  // Color logic: violet for external, primary for container MCP, purple for local agents, teal for remote
  const colorClass = isServer
    ? (isExternal ? 'external' : 'primary')
    : isAgent
      ? (isRemote ? 'secondary' : 'tertiary')
      : 'secondary';

  const handleClose = () => {
    setSidebarOpen(false);
    selectNode(null);
  };

  const handleShowLogs = () => {
    setBottomPanelOpen(true);
  };

  return (
    <aside
      className={cn(
        'fixed top-14 right-0 h-[calc(100vh-56px-32px)] w-80',
        'bg-surface/80 backdrop-blur-xl border-l border-border/50',
        'transform transition-all duration-300 ease-out z-20',
        'flex flex-col overflow-hidden',
        sidebarOpen ? 'translate-x-0 opacity-100' : 'translate-x-full opacity-0'
      )}
    >
      {/* Accent line */}
      <div className={cn(
        'absolute top-0 left-0 bottom-0 w-px',
        colorClass === 'primary' && 'bg-gradient-to-b from-primary/40 via-primary/20 to-transparent',
        colorClass === 'tertiary' && 'bg-gradient-to-b from-tertiary/40 via-tertiary/20 to-transparent',
        colorClass === 'secondary' && 'bg-gradient-to-b from-secondary/40 via-secondary/20 to-transparent',
        colorClass === 'external' && 'bg-gradient-to-b from-violet-500/40 via-violet-500/20 to-transparent'
      )} />

      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30">
        <div className="flex items-center gap-3 min-w-0">
          <div className={cn(
            'p-2 rounded-xl flex-shrink-0 border relative',
            colorClass === 'primary' && 'bg-primary/10 border-primary/20',
            colorClass === 'tertiary' && 'bg-tertiary/10 border-tertiary/20',
            colorClass === 'secondary' && 'bg-secondary/10 border-secondary/20',
            colorClass === 'external' && 'bg-violet-500/10 border-violet-500/20'
          )}>
            <Icon
              size={16}
              className={cn(
                colorClass === 'primary' && 'text-primary',
                colorClass === 'tertiary' && 'text-tertiary',
                colorClass === 'secondary' && 'text-secondary',
                colorClass === 'external' && 'text-violet-400'
              )}
            />
            {/* A2A indicator on icon */}
            {hasA2A && !isRemote && (
              <div className="absolute -bottom-1 -right-1 p-0.5 rounded-full bg-secondary/20 border border-secondary/40">
                <Zap size={6} className="text-secondary" />
              </div>
            )}
          </div>
          <div className="min-w-0">
            <h2 className="font-semibold text-text-primary truncate tracking-tight">
              {data.name}
            </h2>
            <div className="flex items-center gap-1.5">
              <p className="text-[10px] text-text-muted uppercase tracking-wider">
                {isServer ? 'MCP Server' : isAgent ? 'Agent' : 'Resource'}
              </p>
              {isServer && isExternal && (
                <span className="text-[9px] px-1 py-0.5 rounded font-medium bg-violet-500/10 text-violet-400 flex items-center gap-0.5">
                  <Globe size={8} />
                  External
                </span>
              )}
              {isAgent && (
                <span className={cn(
                  'text-[9px] px-1 py-0.5 rounded font-medium uppercase flex items-center gap-0.5',
                  isRemote ? 'bg-secondary/10 text-secondary' : 'bg-tertiary/10 text-tertiary'
                )}>
                  {isRemote ? <Globe size={8} /> : <Server size={8} />}
                  {agentData?.variant}
                </span>
              )}
              {hasA2A && (
                <span className="text-[9px] px-1 py-0.5 rounded font-medium bg-secondary/10 text-secondary flex items-center gap-0.5">
                  <Zap size={8} />
                  A2A
                </span>
              )}
            </div>
          </div>
        </div>
        <button
          onClick={handleClose}
          className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors group"
        >
          <X size={16} className="text-text-muted group-hover:text-text-primary transition-colors" />
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {/* Status Section */}
        <Section title="Status" icon={Sparkles} defaultOpen>
          <div className="space-y-3">
            <div className="flex justify-between items-center">
              <span className="text-sm text-text-muted">State</span>
              <Badge status={data.status}>
                {data.status}
              </Badge>
            </div>

            {isServer && (data as MCPServerNodeData).transport && (() => {
              const transport = (data as MCPServerNodeData).transport;
              const TransportIcon = getTransportIcon(transport);
              return (
                <div className="flex justify-between items-center">
                  <span className="text-sm text-text-muted">Transport</span>
                  <span className={cn(
                    'text-xs px-2 py-0.5 rounded-md font-mono font-medium uppercase tracking-wider flex items-center gap-1',
                    getTransportColorClasses(transport)
                  )}>
                    <TransportIcon size={10} />
                    {transport}
                  </span>
                </div>
              );
            })()}

            {isServer && (data as MCPServerNodeData).endpoint && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Endpoint</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={(data as MCPServerNodeData).endpoint}>
                  {(data as MCPServerNodeData).endpoint}
                </span>
              </div>
            )}

            {/* Agent fields - Container info (local variant) */}
            {isAgent && agentData?.image && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Image</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={agentData.image}>
                  {agentData.image}
                </span>
              </div>
            )}

            {isAgent && agentData?.containerId && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Container</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={agentData.containerId}>
                  {agentData.containerId}
                </span>
              </div>
            )}

            {/* Agent fields - A2A info (when hasA2A) */}
            {isAgent && hasA2A && agentData?.role && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">A2A Role</span>
                <span className={cn(
                  'text-xs px-2 py-0.5 rounded-md font-medium uppercase tracking-wider flex items-center gap-1',
                  'bg-secondary/10 text-secondary'
                )}>
                  {agentData.role === 'remote' ? <Globe size={10} /> : <Server size={10} />}
                  {agentData.role}
                </span>
              </div>
            )}

            {isAgent && hasA2A && agentData?.url && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">URL</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={agentData.url}>
                  {agentData.url}
                </span>
              </div>
            )}

            {isAgent && hasA2A && agentData?.endpoint && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">A2A Endpoint</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={agentData.endpoint}>
                  {agentData.endpoint}
                </span>
              </div>
            )}

            {isAgent && hasA2A && (agentData?.skillCount ?? 0) > 0 && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Skills</span>
                <span className="text-sm text-secondary font-bold tabular-nums">
                  {agentData?.skillCount}
                </span>
              </div>
            )}

            {isAgent && hasA2A && agentData?.description && (
              <div className="mt-2 pt-2 border-t border-border/30">
                <span className="text-sm text-text-muted block mb-1">Description</span>
                <p className="text-xs text-text-secondary leading-relaxed">
                  {agentData.description}
                </p>
              </div>
            )}

            {/* Resource fields */}
            {!isServer && !isAgent && (data as ResourceNodeData).image && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Image</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={(data as ResourceNodeData).image}>
                  {(data as ResourceNodeData).image}
                </span>
              </div>
            )}

            {!isServer && !isAgent && (data as ResourceNodeData).network && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Network</span>
                <span className="text-sm text-secondary font-medium">
                  {(data as ResourceNodeData).network}
                </span>
              </div>
            )}
          </div>
        </Section>

        {/* Controls Section */}
        <Section title="Actions" icon={Terminal} defaultOpen>
          <ControlBar agentName={data.name} />
          <button
            onClick={handleShowLogs}
            className={cn(
              'w-full mt-3 flex items-center justify-center gap-2 py-2.5 rounded-lg',
              'bg-surface-elevated/60 border border-border/50',
              'hover:bg-surface-highlight hover:border-text-muted/30 transition-all text-sm',
              'text-text-secondary hover:text-text-primary'
            )}
          >
            <FileText size={14} />
            Show Logs Panel
          </button>
        </Section>

        {/* Tools Section (MCP servers only) */}
        {isServer && (
          <Section
            title="Tools"
            icon={Wrench}
            count={(data as MCPServerNodeData).toolCount}
          >
            <ToolList agentName={data.name} />
          </Section>
        )}

        {/* Skills Section (Agents with A2A) */}
        {isAgent && hasA2A && (agentData?.skills?.length ?? 0) > 0 && (
          <Section
            title="Skills"
            icon={Sparkles}
            count={agentData?.skillCount}
            defaultOpen
          >
            <div className="space-y-2">
              {agentData?.skills?.map((skill, idx) => (
                <div
                  key={idx}
                  className="px-3 py-2 rounded-lg bg-surface-elevated/60 border border-border/30"
                >
                  <span className="text-sm text-text-primary font-medium">{skill}</span>
                </div>
              ))}
            </div>
          </Section>
        )}
      </div>
    </aside>
  );
}

// Collapsible section component
interface SectionProps {
  title: string;
  icon?: React.ComponentType<{ size?: number; className?: string }>;
  count?: number;
  defaultOpen?: boolean;
  children: React.ReactNode;
}

function Section({ title, icon: Icon, count, defaultOpen = false, children }: SectionProps) {
  const [isOpen, setIsOpen] = useState(defaultOpen);

  return (
    <div className="border-b border-border/30">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center justify-between p-4 hover:bg-surface-highlight/50 transition-colors group"
      >
        <div className="flex items-center gap-2.5">
          {Icon && (
            <div className="p-1 rounded-md bg-surface-highlight/50 group-hover:bg-surface-highlight transition-colors">
              <Icon size={12} className="text-text-muted group-hover:text-primary transition-colors" />
            </div>
          )}
          <span className="text-sm font-medium text-text-primary">{title}</span>
          {count !== undefined && (
            <span className="text-[10px] text-text-muted bg-surface-elevated px-1.5 py-0.5 rounded-md font-mono">
              {count}
            </span>
          )}
        </div>
        <div className="p-1 rounded-md group-hover:bg-surface-highlight transition-colors">
          {isOpen ? (
            <ChevronDown size={14} className="text-text-muted" />
          ) : (
            <ChevronRight size={14} className="text-text-muted" />
          )}
        </div>
      </button>
      <div className={cn(
        'overflow-hidden transition-all duration-200',
        isOpen ? 'max-h-[1000px] opacity-100' : 'max-h-0 opacity-0'
      )}>
        <div className="px-4 pb-4">
          {children}
        </div>
      </div>
    </div>
  );
}
