import { useState } from 'react';
import {
  X,
  Terminal,
  Box,
  Bot,
  ChevronDown,
  ChevronRight,
  Wrench,
  FileText,
  Sparkles,
  Globe,
  Server,
  Zap,
  Cpu,
  KeyRound,
  Network,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { ToolList } from '../ui/ToolList';
import { ControlBar } from '../ui/ControlBar';
import { PopoutButton } from '../ui/PopoutButton';
import { getTransportIcon, getTransportColorClasses } from '../../lib/transport';
import { useStackStore, useSelectedNodeData } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import type { MCPServerNodeData, ResourceNodeData, AgentNodeData, ToolSelector } from '../../types';

export function Sidebar() {
  const selectedData = useSelectedNodeData();
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const setBottomPanelOpen = useUIStore((s) => s.setBottomPanelOpen);
  const sidebarDetached = useUIStore((s) => s.sidebarDetached);
  const selectNode = useStackStore((s) => s.selectNode);
  const { openDetachedWindow } = useWindowManager();

  // Don't render content if no valid selection
  if (!selectedData || selectedData.type === 'gateway') {
    return null;
  }

  const isServer = selectedData.type === 'mcp-server';
  const isAgent = selectedData.type === 'agent';
  const data = selectedData as unknown as MCPServerNodeData | ResourceNodeData | AgentNodeData;

  // For MCP servers, determine if external, local process, or SSH
  const serverData = isServer ? (data as MCPServerNodeData) : null;
  const isExternal = serverData?.external ?? false;
  const isLocalProcess = serverData?.localProcess ?? false;
  const isSSH = serverData?.ssh ?? false;

  // For agents, determine variant and A2A capability
  const agentData = isAgent ? (data as AgentNodeData) : null;
  const isRemote = agentData?.variant === 'remote';
  const hasA2A = agentData?.hasA2A ?? false;

  // Icon logic: Globe for external, Cpu for local process, KeyRound for SSH, Terminal for container-based
  const Icon = isServer
    ? isExternal
      ? Globe
      : isLocalProcess
        ? Cpu
        : isSSH
          ? KeyRound
          : Terminal
    : isAgent
      ? Bot
      : Box;

  // Color logic: violet for all MCP servers, purple for local agents, teal for remote agents/resources
  const colorClass = isServer ? 'violet' : isAgent ? (isRemote ? 'secondary' : 'tertiary') : 'secondary';

  const handleClose = () => {
    setSidebarOpen(false);
    selectNode(null);
  };

  const handleShowLogs = () => {
    setBottomPanelOpen(true);
  };

  const handlePopout = () => {
    openDetachedWindow('sidebar', `node=${encodeURIComponent(data.name)}`);
  };

  return (
    <div
      className={cn(
        'h-full w-full',
        'flex flex-col overflow-hidden'
      )}
    >
      {/* Accent line */}
      <div
        className={cn(
          'absolute top-0 left-0 bottom-0 w-px',
          colorClass === 'violet' && 'bg-gradient-to-b from-violet-500/40 via-violet-500/20 to-transparent',
          colorClass === 'tertiary' && 'bg-gradient-to-b from-tertiary/40 via-tertiary/20 to-transparent',
          colorClass === 'secondary' && 'bg-gradient-to-b from-secondary/40 via-secondary/20 to-transparent'
        )}
      />

      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30">
        <div className="flex items-center gap-3 min-w-0">
          <div
            className={cn(
              'p-2 rounded-xl flex-shrink-0 border relative',
              colorClass === 'violet' && 'bg-violet-500/10 border-violet-500/20',
              colorClass === 'tertiary' && 'bg-tertiary/10 border-tertiary/20',
              colorClass === 'secondary' && 'bg-secondary/10 border-secondary/20'
            )}
          >
            <Icon
              size={16}
              className={cn(
                colorClass === 'violet' && 'text-violet-400',
                colorClass === 'tertiary' && 'text-tertiary',
                colorClass === 'secondary' && 'text-secondary'
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
            <h2 className="font-semibold text-text-primary truncate tracking-tight">{data.name}</h2>
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
              {isServer && isLocalProcess && (
                <span className="text-[9px] px-1 py-0.5 rounded font-medium bg-surface-highlight text-text-muted flex items-center gap-0.5">
                  <Cpu size={8} />
                  Local
                </span>
              )}
              {isServer && isSSH && (
                <span className="text-[9px] px-1 py-0.5 rounded font-medium bg-surface-highlight text-text-muted flex items-center gap-0.5">
                  <KeyRound size={8} />
                  SSH
                </span>
              )}
              {isAgent && (
                <span
                  className={cn(
                    'text-[9px] px-1 py-0.5 rounded font-medium uppercase flex items-center gap-0.5',
                    isRemote ? 'bg-secondary/10 text-secondary' : 'bg-tertiary/10 text-tertiary'
                  )}
                >
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
        <div className="flex items-center gap-1">
          <PopoutButton
            onClick={handlePopout}
            tooltip="Open in new window"
            disabled={sidebarDetached}
          />
          <button onClick={handleClose} className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors group">
            <X size={16} className="text-text-muted group-hover:text-text-primary transition-colors" />
          </button>
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {/* Status Section */}
        <Section title="Status" icon={Sparkles} defaultOpen>
          <div className="space-y-3">
            <div className="flex justify-between items-center">
              <span className="text-sm text-text-muted">State</span>
              <Badge status={data.status}>{data.status}</Badge>
            </div>

            {isServer &&
              (data as MCPServerNodeData).transport &&
              (() => {
                const transport = (data as MCPServerNodeData).transport;
                const TransportIcon = getTransportIcon(transport);
                return (
                  <div className="flex justify-between items-center">
                    <span className="text-sm text-text-muted">Transport</span>
                    <span
                      className={cn(
                        'text-xs px-2 py-0.5 rounded-md font-mono font-medium uppercase tracking-wider flex items-center gap-1',
                        getTransportColorClasses(transport)
                      )}
                    >
                      <TransportIcon size={10} />
                      {transport}
                    </span>
                  </div>
                );
              })()}

            {isServer && (data as MCPServerNodeData).endpoint && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Endpoint</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={(data as MCPServerNodeData).endpoint}
                >
                  {(data as MCPServerNodeData).endpoint}
                </span>
              </div>
            )}

            {isServer && isSSH && serverData?.sshHost && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">SSH Host</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={serverData.sshHost}
                >
                  {serverData.sshHost}
                </span>
              </div>
            )}

            {/* Agent fields - Container info (local variant) */}
            {isAgent && agentData?.image && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Image</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={agentData.image}
                >
                  {agentData.image}
                </span>
              </div>
            )}

            {isAgent && agentData?.containerId && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Container</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={agentData.containerId}
                >
                  {agentData.containerId}
                </span>
              </div>
            )}

            {/* Agent fields - A2A info (when hasA2A) */}
            {isAgent && hasA2A && agentData?.role && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">A2A Role</span>
                <span
                  className={cn(
                    'text-xs px-2 py-0.5 rounded-md font-medium uppercase tracking-wider flex items-center gap-1',
                    'bg-secondary/10 text-secondary'
                  )}
                >
                  {agentData.role === 'remote' ? <Globe size={10} /> : <Server size={10} />}
                  {agentData.role}
                </span>
              </div>
            )}

            {isAgent && hasA2A && agentData?.url && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">URL</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={agentData.url}
                >
                  {agentData.url}
                </span>
              </div>
            )}

            {isAgent && hasA2A && agentData?.endpoint && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">A2A Endpoint</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={agentData.endpoint}
                >
                  {agentData.endpoint}
                </span>
              </div>
            )}

            {isAgent && hasA2A && (agentData?.skillCount ?? 0) > 0 && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Skills</span>
                <span className="text-sm text-secondary font-bold tabular-nums">{agentData?.skillCount}</span>
              </div>
            )}

            {isAgent && hasA2A && agentData?.description && (
              <div className="mt-2 pt-2 border-t border-border/30">
                <span className="text-sm text-text-muted block mb-1">Description</span>
                <p className="text-xs text-text-secondary leading-relaxed">{agentData.description}</p>
              </div>
            )}

            {/* Resource fields */}
            {!isServer && !isAgent && (data as ResourceNodeData).image && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Image</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={(data as ResourceNodeData).image}
                >
                  {(data as ResourceNodeData).image}
                </span>
              </div>
            )}

            {!isServer && !isAgent && (data as ResourceNodeData).network && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Network</span>
                <span className="text-sm text-secondary font-medium">{(data as ResourceNodeData).network}</span>
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
              'mt-3 inline-flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg',
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
          <Section title="Tools" icon={Wrench} count={(data as MCPServerNodeData).toolCount}>
            <ToolList serverName={data.name} />
          </Section>
        )}

        {/* Skills Section (Agents with A2A) */}
        {isAgent && hasA2A && (agentData?.skills?.length ?? 0) > 0 && (
          <Section title="Skills" icon={Sparkles} count={agentData?.skillCount} defaultOpen>
            <div className="space-y-2">
              {agentData?.skills?.map((skill, idx) => (
                <div key={idx} className="px-3 py-2 rounded-lg bg-surface-elevated/60 border border-border/30">
                  <span className="text-sm text-text-primary font-medium">{skill}</span>
                </div>
              ))}
            </div>
          </Section>
        )}

        {/* Access Section (Agents only - shows MCP server dependencies) */}
        {isAgent && agentData?.uses && (agentData.uses?.length ?? 0) > 0 && (
          <Section title="Access" icon={Network} count={agentData.uses?.length ?? 0} defaultOpen>
            <div className="space-y-3">
              {(agentData.uses ?? []).map((selector: ToolSelector) => (
                <AccessItem key={selector.server} selector={selector} />
              ))}
            </div>
          </Section>
        )}
      </div>
    </div>
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
      <div className={cn('overflow-hidden transition-all duration-200', isOpen ? 'max-h-[1000px] opacity-100' : 'max-h-0 opacity-0')}>
        <div className="px-4 pb-4">{children}</div>
      </div>
    </div>
  );
}

// Access item component for showing agent's MCP server dependencies
interface AccessItemProps {
  selector: ToolSelector;
}

function AccessItem({ selector }: AccessItemProps) {
  const isRestricted = selector.tools && (selector.tools?.length ?? 0) > 0;

  return (
    <div className="rounded-lg bg-surface-elevated border border-border/40 overflow-hidden">
      {/* Server Header */}
      <div className="px-3 py-2 bg-violet-500/10 flex justify-between items-center">
        <div className="flex items-center gap-2">
          <Server size={12} className="text-violet-400" />
          <span className="text-xs font-medium text-violet-100">{selector.server}</span>
        </div>
        <span
          className={cn(
            'text-[9px] px-1.5 py-0.5 rounded font-medium uppercase tracking-wider border',
            isRestricted
              ? 'border-amber-500/30 text-amber-400 bg-amber-500/10'
              : 'border-violet-500/30 text-violet-400 bg-violet-500/5'
          )}
        >
          {isRestricted ? 'Restricted' : 'Full Access'}
        </span>
      </div>

      {/* Tool List */}
      <div className="p-2">
        {isRestricted ? (
          <div className="space-y-1">
            {selector.tools?.map((toolName) => (
              <div key={toolName} className="flex items-center gap-2 px-2 py-1.5 rounded bg-background/50">
                <Wrench size={10} className="text-primary flex-shrink-0" />
                <span className="text-xs font-mono text-text-primary truncate">{toolName}</span>
              </div>
            ))}
          </div>
        ) : (
          <span className="text-xs text-text-muted italic px-2">Access to all available tools</span>
        )}
      </div>
    </div>
  );
}
