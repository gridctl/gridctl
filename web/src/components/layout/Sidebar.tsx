import {
  Terminal,
  Box,
  Wrench,
  FileText,
  Sparkles,
  Globe,
  Cpu,
  KeyRound,
  FileJson,
  HeartPulse,
  Gauge,
  FileOutput,
  Activity,
  Database,
  ArrowUpRight,
  ShieldCheck,
  SlidersHorizontal,
  DollarSign,
} from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { ControlBar } from '../ui/ControlBar';
import { PopoutButton } from '../ui/PopoutButton';
import { InspectorHeader, InspectorSection } from '../inspector';
import { GatewaySidebar } from '../gateway/GatewaySidebar';
import { TokenUsageSection } from '../sidebar/TokenUsageSection';
import { ToolsEditor } from '../sidebar/ToolsEditor';
import { AutoscalePanel } from '../status/AutoscalePanel';
import { SidebarTelemetrySection } from '../telemetry/SidebarTelemetrySection';
import { getTransportIcon, getTransportColorClasses } from '../../lib/transport';
import { getClientIcon } from '../../lib/clientIcons';
import { summarizeClientReach } from '../../lib/clientScope';
import { useStackStore, useSelectedNodeData } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useAccessLensStore } from '../../stores/useAccessLensStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { formatRelativeTime } from '../../lib/time';
import { EffectiveModelTag } from '../pricing/EffectiveModelTag';
import { MODEL_PRECEDENCE_HINT } from '../pricing/constants';
import type { MCPServerNodeData, ResourceNodeData, ClientNodeData } from '../../types';

export function Sidebar() {
  const selectedData = useSelectedNodeData();
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const setBottomPanelOpen = useUIStore((s) => s.setBottomPanelOpen);
  const sidebarDetached = useUIStore((s) => s.sidebarDetached);
  const selectNode = useStackStore((s) => s.selectNode);
  const autoscaleHistory = useStackStore((s) => s.autoscaleHistory);
  const autoscaleDecisions = useStackStore((s) => s.autoscaleDecisions);
  const clients = useStackStore((s) => s.clients);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const clientModels = useStackStore((s) => s.clientModels);
  const effectiveClientModels = useStackStore((s) => s.effectiveClientModels);
  const effectiveServerModels = useStackStore((s) => s.effectiveServerModels);
  const defaultModel = useStackStore((s) => s.defaultModel);
  const costAttribution = useStackStore((s) => s.costAttribution);
  const setPricingManagerOpen = useUIStore((s) => s.setPricingManagerOpen);
  const enableAccessLens = useAccessLensStore((s) => s.setEnabled);
  const openAccessLensEditor = useAccessLensStore((s) => s.openSlideOver);
  const { openDetachedWindow } = useWindowManager();
  const navigate = useNavigate();

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

  // Icon logic: per-client brand icon for clients, Globe for external, Cpu for local process, KeyRound for SSH, FileJson for OpenAPI, Terminal for container-based
  const Icon = isClient
    ? getClientIcon(clientData?.slug ?? '')
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

  // Color logic: neutral monochrome for clients (matches their topology nodes), violet for MCP servers, teal for resources
  const colorClass = isClient ? 'neutral' : isServer ? 'violet' : 'secondary';

  const handleShowLogs = () => {
    setBottomPanelOpen(true);
  };

  const handlePopout = () => {
    openDetachedWindow('sidebar', `node=${encodeURIComponent(data.name)}`);
  };

  const handleViewSecrets = () => {
    navigate(`/vault?filter=server:${encodeURIComponent(data.name)}`);
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
          colorClass === 'secondary' && 'bg-gradient-to-b from-secondary/40 via-secondary/20 to-transparent',
          colorClass === 'neutral' && 'bg-gradient-to-b from-white/20 via-white/10 to-transparent'
        )}
      />

      <InspectorHeader
        title={data.name}
        icon={Icon}
        accent={colorClass}
        subtitle={
          <>
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
          </>
        }
        onClose={handleClose}
        actions={
          <PopoutButton onClick={handlePopout} disabled={sidebarDetached} />
        }
      />

      {/* Content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {/* Status Section */}
        <InspectorSection title="Status" icon={Sparkles} defaultOpen>
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

            {isServer && serverData?.protocolVersion && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Protocol</span>
                <span className="text-xs text-text-secondary font-mono bg-background/50 px-2 py-1 rounded-md">
                  {serverData.protocolVersion}
                </span>
              </div>
            )}

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
        </InspectorSection>

        {/* Access Scope Section (clients only) */}
        {isClient && clientData && (() => {
          // Read scope from the live store client (canonical, refreshes on poll)
          // and fall back to the node snapshot.
          const liveClient = clients.find((c) => c.slug === clientData.slug);
          const scope = liveClient?.effectiveScope ?? clientData.effectiveScope;
          const reach = summarizeClientReach(
            scope,
            mcpServers.map((s) => s.name),
          );
          return (
            <InspectorSection title="Access Scope" icon={ShieldCheck} defaultOpen>
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-sm text-text-muted">Reach</span>
                  {reach.scoped ? (
                    <span className="text-xs px-2 py-0.5 rounded-md font-mono font-medium bg-primary/10 text-primary">
                      {reach.reachableCount} of {reach.totalCount} servers
                    </span>
                  ) : (
                    <span className="text-xs px-2 py-0.5 rounded-md font-mono font-medium bg-secondary/10 text-secondary">
                      Unscoped · all servers
                    </span>
                  )}
                </div>

                {reach.scoped ? (
                  reach.servers.length > 0 ? (
                    <div className="flex flex-wrap gap-1.5">
                      {reach.servers.map((name) => (
                        <span
                          key={name}
                          className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-background/60 border border-border/40 text-text-secondary"
                        >
                          {name}
                        </span>
                      ))}
                    </div>
                  ) : (
                    <p className="text-[11px] text-text-muted leading-relaxed">
                      Reaches the gateway but no servers. Unlisted under a deny default, this client
                      sees nothing.
                    </p>
                  )
                ) : (
                  <p className="text-[11px] text-text-muted leading-relaxed">
                    No <span className="font-mono text-text-secondary">clients</span> scope applies;
                    this client can reach every MCP server in the stack.
                  </p>
                )}

                <button
                  type="button"
                  onClick={() => {
                    // Enter Access Lens on this (already-selected) client and open
                    // the slide-over beside the canvas, so edits preview live.
                    enableAccessLens(true);
                    openAccessLensEditor();
                  }}
                  className={cn(
                    'w-full inline-flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg text-sm font-medium transition-all',
                    'bg-primary/10 text-primary border border-primary/30',
                    'hover:bg-primary/20 hover:border-primary/50',
                  )}
                >
                  <SlidersHorizontal size={14} />
                  Edit Scope
                </button>
              </div>
            </InspectorSection>
          );
        })()}

        {/* Pricing Section (clients and MCP servers) — which model this
            node's calls are priced as, with a path into the manager. */}
        {(isClient || isServer) && (() => {
          const declared = isClient
            ? clientModels[clientData?.slug ?? '']
            : mcpServers.find((s) => s.name === data.name)?.model;
          const effective = isClient
            ? effectiveClientModels[clientData?.slug ?? '']
            : effectiveServerModels[data.name];
          // Show the read-only Effective line only when it adds information
          // beyond the declared line: a mixed blend or unpriced traffic.
          const showEffective =
            effective && (effective.provenance === 'mixed' || effective.provenance === 'none');
          return (
            <InspectorSection title="Pricing" icon={DollarSign}>
              <div className="space-y-3">
                <div className="flex justify-between items-center gap-3">
                  <span className="text-sm text-text-muted">
                    {isClient ? 'Priced as' : 'Pricing model'}
                  </span>
                  {declared ? (
                    <span className="inline-flex items-center gap-1 rounded-full bg-surface-highlight/60 border border-border/40 px-2 py-0.5">
                      <span className="text-[10px] font-mono text-text-primary">{declared}</span>
                      <span className="text-[9px] text-text-muted/70">
                        {isClient ? '· client' : '· server'}
                      </span>
                    </span>
                  ) : !isClient && defaultModel ? (
                    <span className="text-xs font-mono text-text-muted">
                      default: {defaultModel}
                    </span>
                  ) : (
                    <span className="text-xs text-text-muted">
                      {isClient && costAttribution ? 'per-server' : 'not configured'}
                    </span>
                  )}
                </div>
                {showEffective && effective && (
                  <div className="flex justify-between items-center gap-3">
                    <span className="text-sm text-text-muted" title={MODEL_PRECEDENCE_HINT}>
                      Effective
                    </span>
                    <EffectiveModelTag effective={effective} onClick={() => setPricingManagerOpen(true)} />
                  </div>
                )}
                <button
                  type="button"
                  onClick={() => setPricingManagerOpen(true)}
                  className={cn(
                    'w-full inline-flex items-center justify-center gap-2 px-4 py-2 rounded-lg text-xs font-medium transition-all',
                    'bg-surface-elevated/60 border border-border/50',
                    'hover:bg-surface-highlight hover:border-text-muted/30',
                    'text-text-secondary hover:text-text-primary',
                  )}
                >
                  <DollarSign size={12} />
                  Edit Pricing Models
                </button>
              </div>
            </InspectorSection>
          );
        })()}

        {/* Token Usage Section (MCP servers only) */}
        {isServer && (
          <InspectorSection title="Token Usage" icon={Gauge}>
            <TokenUsageSection serverName={data.name} />
          </InspectorSection>
        )}

        {/* Scaling Section (MCP servers with autoscale only) */}
        {isServer && serverData?.autoscale && (
          <InspectorSection title="Scaling" icon={Activity} defaultOpen>
            <AutoscalePanel
              status={serverData.autoscale}
              history={autoscaleHistory[data.name] ?? []}
              decisions={autoscaleDecisions[data.name] ?? []}
            />
          </InspectorSection>
        )}

        {/* Controls Section (not for clients - they aren't containers) */}
        {!isClient && (
          <InspectorSection title="Actions" icon={Terminal} defaultOpen>
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
            {isServer && (
              <button
                onClick={handleViewSecrets}
                className={cn(
                  'mt-3 inline-flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg',
                  'bg-surface-elevated/60 border border-border/50',
                  'hover:bg-surface-highlight hover:border-text-muted/30 transition-all text-sm',
                  'text-text-secondary hover:text-text-primary'
                )}
              >
                <KeyRound size={14} />
                Secrets
              </button>
            )}
          </InspectorSection>
        )}

        {/* Telemetry Section (MCP servers only) — between Actions and Tools */}
        {isServer && (
          <InspectorSection title="Telemetry" icon={Database}>
            <SidebarTelemetrySection serverName={data.name} />
          </InspectorSection>
        )}

        {/* Tools Section (MCP servers only) */}
        {isServer && (
          <InspectorSection title="Tools" icon={Wrench} count={(data as MCPServerNodeData).toolCount}>
            <ToolsEditor
              serverName={data.name}
              savedTools={(data as MCPServerNodeData).toolWhitelist ?? []}
              serverTools={(data as MCPServerNodeData).tools ?? []}
            />
            <button
              type="button"
              onClick={() => navigate(`/tools?server=${encodeURIComponent(data.name)}`)}
              className="mt-2 inline-flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
            >
              <ArrowUpRight size={10} />
              Open in Tools workspace
            </button>
          </InspectorSection>
        )}

      </div>
    </div>
  );
}

