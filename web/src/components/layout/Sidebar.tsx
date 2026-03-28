import { useState } from 'react';
import {
  X,
  Terminal,
  Box,
  ChevronDown,
  ChevronRight,
  Wrench,
  FileText,
  Sparkles,
  Globe,
  Cpu,
  KeyRound,
  FileJson,
  HeartPulse,
  Monitor,
  Gauge,
  FileOutput,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { ToolList } from '../ui/ToolList';
import { ControlBar } from '../ui/ControlBar';
import { PopoutButton } from '../ui/PopoutButton';
import { GatewaySidebar } from '../gateway/GatewaySidebar';
import { TokenUsageSection } from '../sidebar/TokenUsageSection';
import { getTransportIcon, getTransportColorClasses } from '../../lib/transport';
import { useStackStore, useSelectedNodeData } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { formatRelativeTime } from '../../lib/time';
import type { MCPServerNodeData, ResourceNodeData, ClientNodeData } from '../../types';

export function Sidebar() {
  const selectedData = useSelectedNodeData();
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const setBottomPanelOpen = useUIStore((s) => s.setBottomPanelOpen);
  const sidebarDetached = useUIStore((s) => s.sidebarDetached);
  const selectNode = useStackStore((s) => s.selectNode);
  const { openDetachedWindow } = useWindowManager();

  const handleClose = () => {
    setSidebarOpen(false);
    selectNode(null);
  };

  // Don't render content if no valid selection
  if (!selectedData) {
    return null;
  }

  // Gateway and skill groups get the registry sidebar
  if (selectedData.type === 'gateway' || selectedData.type === 'skill-group') {
    return <GatewaySidebar onClose={handleClose} />;
  }

  const isServer = selectedData.type === 'mcp-server';
  const isClient = selectedData.type === 'client';
  const data = selectedData as unknown as MCPServerNodeData | ResourceNodeData | ClientNodeData;

  // For MCP servers, determine if external, local process, or SSH
  const serverData = isServer ? (data as MCPServerNodeData) : null;
  const isExternal = serverData?.external ?? false;
  const isLocalProcess = serverData?.localProcess ?? false;
  const isSSH = serverData?.ssh ?? false;
  const isOpenAPI = serverData?.openapi ?? false;

  // Client-specific data
  const clientData = isClient ? (data as ClientNodeData) : null;

  // Icon logic: Monitor for clients, Globe for external, Cpu for local process, KeyRound for SSH, FileJson for OpenAPI, Terminal for container-based
  const Icon = isClient
    ? Monitor
    : isServer
      ? isExternal
        ? Globe
        : isLocalProcess
          ? Cpu
          : isSSH
            ? KeyRound
            : isOpenAPI
              ? FileJson
              : Terminal
      : Box;

  // Color logic: primary (amber) for clients, violet for MCP servers, teal for resources
  const colorClass = isClient ? 'primary' : isServer ? 'violet' : 'secondary';

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
          colorClass === 'primary' && 'bg-gradient-to-b from-primary/40 via-primary/20 to-transparent',
          colorClass === 'violet' && 'bg-gradient-to-b from-violet-500/40 via-violet-500/20 to-transparent',
          colorClass === 'secondary' && 'bg-gradient-to-b from-secondary/40 via-secondary/20 to-transparent'
        )}
      />

      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30">
        <div className="flex items-center gap-3 min-w-0">
          <div
            className={cn(
              'p-2 rounded-xl flex-shrink-0 border relative',
              colorClass === 'primary' && 'bg-primary/10 border-primary/20',
              colorClass === 'violet' && 'bg-violet-500/10 border-violet-500/20',
              colorClass === 'secondary' && 'bg-secondary/10 border-secondary/20'
            )}
          >
            <Icon
              size={16}
              className={cn(
                colorClass === 'primary' && 'text-primary',
                colorClass === 'violet' && 'text-violet-400',
                colorClass === 'secondary' && 'text-secondary'
              )}
            />
          </div>
          <div className="min-w-0">
            <h2 className="font-semibold text-text-primary truncate tracking-tight">{data.name}</h2>
            <div className="flex items-center gap-1.5">
              <p className="text-[10px] text-text-muted uppercase tracking-wider">
                {isClient ? 'LLM Client' : isServer ? 'MCP Server' : 'Resource'}
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
              {isServer && isOpenAPI && (
                <span className="text-[9px] px-1 py-0.5 rounded font-medium bg-surface-highlight text-text-muted flex items-center gap-0.5">
                  <FileJson size={8} />
                  OpenAPI
                </span>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-1">
          <PopoutButton
            onClick={handlePopout}
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

            {isServer && serverData?.outputFormat && serverData.outputFormat !== 'json' && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Format</span>
                <span className="text-xs px-2 py-0.5 rounded-md font-mono font-medium uppercase tracking-wider flex items-center gap-1 bg-secondary/10 text-secondary">
                  <FileOutput size={10} />
                  {serverData.outputFormat}
                </span>
              </div>
            )}

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

            {isServer && isOpenAPI && serverData?.openapiSpec && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Spec</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={serverData.openapiSpec}
                >
                  {serverData.openapiSpec}
                </span>
              </div>
            )}

            {/* Health Check Info (MCP servers only) */}
            {isServer && serverData?.healthy !== undefined && serverData?.healthy !== null && (
              <>
                <div className="flex justify-between items-center">
                  <span className="text-sm text-text-muted">Health</span>
                  <span className={cn(
                    'text-xs px-2 py-0.5 rounded-md font-medium flex items-center gap-1',
                    serverData.healthy
                      ? 'bg-status-running/10 text-status-running'
                      : 'bg-status-error/10 text-status-error'
                  )}>
                    <HeartPulse size={10} />
                    {serverData.healthy ? 'Healthy' : 'Unhealthy'}
                  </span>
                </div>

                {serverData.lastCheck && (
                  <div className="flex justify-between items-center">
                    <span className="text-sm text-text-muted">Last Check</span>
                    <span className="text-xs text-text-secondary font-mono">
                      {formatRelativeTime(new Date(serverData.lastCheck))}
                    </span>
                  </div>
                )}

                {!serverData.healthy && serverData.healthError && (
                  <div className="mt-1 p-2 rounded-md bg-status-error/5 border border-status-error/15">
                    <span className="text-xs text-status-error font-mono break-words">
                      {serverData.healthError}
                    </span>
                  </div>
                )}
              </>
            )}

            {/* Client fields */}
            {isClient && clientData?.transport && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Transport</span>
                <span className="text-xs px-2 py-0.5 rounded-md font-mono font-medium bg-primary/10 text-primary">
                  {clientData.transport}
                </span>
              </div>
            )}

            {isClient && clientData?.configPath && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Config</span>
                <span
                  className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md"
                  title={clientData.configPath}
                >
                  {clientData.configPath}
                </span>
              </div>
            )}

            {/* Resource fields */}
            {!isServer && !isClient && (data as ResourceNodeData).image && (
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

            {!isServer && !isClient && (data as ResourceNodeData).network && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Network</span>
                <span className="text-sm text-secondary font-medium">{(data as ResourceNodeData).network}</span>
              </div>
            )}
          </div>
        </Section>

        {/* Token Usage Section (MCP servers only) */}
        {isServer && (
          <Section title="Token Usage" icon={Gauge}>
            <TokenUsageSection serverName={data.name} />
          </Section>
        )}

        {/* Controls Section (not for clients - they aren't containers) */}
        {!isClient && (
          <Section title="Actions" icon={Terminal} defaultOpen>
            <ControlBar name={data.name} variant={isServer ? 'mcp-server' : 'mcp-server'} />
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
        )}

        {/* Tools Section (MCP servers only) */}
        {isServer && (
          <Section title="Tools" icon={Wrench} count={(data as MCPServerNodeData).toolCount}>
            <ToolList serverName={data.name} />
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

