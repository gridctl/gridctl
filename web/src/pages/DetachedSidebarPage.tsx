import { useEffect, useState, useCallback, useRef, Component, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Terminal,
  Box,
  Bot,
  ChevronDown,
  ChevronRight,
  Wrench,
  Sparkles,
  Globe,
  Server,
  Zap,
  Cpu,
  KeyRound,
  Network,
  RefreshCw,
  ChevronUp,
  AlertCircle,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { Badge } from '../components/ui/Badge';
import { IconButton } from '../components/ui/IconButton';
import { ZoomControls } from '../components/log/ZoomControls';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { useLogFontSize } from '../hooks/useLogFontSize';
import { fetchStatus, fetchTools } from '../lib/api';
import { getTransportIcon, getTransportColorClasses } from '../lib/transport';
import { POLLING } from '../lib/constants';
import type {
  MCPServerStatus,
  ResourceStatus,
  AgentStatus,
  Tool,
  ToolSelector,
} from '../types';

// Error boundary for detached window
interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class DetachedErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="h-screen w-screen bg-background flex items-center justify-center">
          <div className="text-center p-8 max-w-md">
            <div className="p-4 rounded-xl bg-status-error/10 border border-status-error/20 inline-block mb-4">
              <AlertCircle size={32} className="text-status-error" />
            </div>
            <h1 className="text-lg text-status-error mb-2">Something went wrong</h1>
            <pre className="text-xs text-text-muted bg-surface p-4 rounded-lg overflow-auto max-h-32 mb-4">
              {this.state.error?.message}
            </pre>
            <button
              onClick={() => window.location.reload()}
              className="px-4 py-2 bg-primary text-background rounded-lg font-medium hover:bg-primary-light transition-colors"
            >
              Reload Window
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

interface NodeOption {
  name: string;
  type: 'mcp-server' | 'agent' | 'resource';
  data: MCPServerStatus | AgentStatus | ResourceStatus;
}

function DetachedSidebarPageContent() {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialNode = searchParams.get('node');

  const [nodes, setNodes] = useState<NodeOption[]>([]);
  const [tools, setTools] = useState<Tool[]>([]);
  const [selectedNode, setSelectedNode] = useState<string | null>(initialNode);
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [isLoading, setIsLoading] = useState(true);

  // Text zoom
  const contentRef = useRef<HTMLElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } =
    useLogFontSize(contentRef);

  // Register with main window
  useDetachedWindowSync('sidebar');

  // Fetch data
  const fetchData = useCallback(async () => {
    try {
      const [status, toolsResult] = await Promise.all([fetchStatus(), fetchTools()]);

      const nodeList: NodeOption[] = [
        ...(status['mcp-servers'] ?? []).map((s) => ({
          name: s.name,
          type: 'mcp-server' as const,
          data: s,
        })),
        ...(status.agents ?? []).map((a) => ({
          name: a.name,
          type: 'agent' as const,
          data: a,
        })),
        ...(status.resources ?? []).map((r) => ({
          name: r.name,
          type: 'resource' as const,
          data: r,
        })),
      ];

      setNodes(nodeList);
      setTools(toolsResult.tools ?? []);
      setIsLoading(false);
    } catch {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    // Async wrapper to avoid synchronous setState
    const initFetch = async () => {
      await fetchData();
    };
    initFetch();
    const interval = window.setInterval(fetchData, POLLING.STATUS);
    return () => clearInterval(interval);
  }, [fetchData]);

  // Update URL when selection changes
  useEffect(() => {
    if (selectedNode) {
      setSearchParams({ node: selectedNode });
    } else {
      setSearchParams({});
    }
  }, [selectedNode, setSearchParams]);

  const selectedData = (nodes ?? []).find((n) => n.name === selectedNode);

  const handleSelectNode = (name: string) => {
    setSelectedNode(name);
    setDropdownOpen(false);
  };

  return (
    <div className="h-screen w-screen bg-background flex flex-col overflow-hidden">
      {/* Background grain */}
      <div
        className="fixed inset-0 pointer-events-none z-0 opacity-[0.015]"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E")`,
        }}
      />

      {/* Header */}
      <header className="h-12 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-b border-border/50 flex items-center justify-between px-4 z-10 relative">
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-tertiary/30 to-transparent" />

        <div className="flex items-center gap-3">
          <div className="p-1.5 rounded-lg bg-tertiary/10 border border-tertiary/20">
            <Bot size={14} className="text-tertiary" />
          </div>

          {/* Node selector dropdown */}
          <div className="relative">
            <button
              onClick={() => setDropdownOpen(!dropdownOpen)}
              className={cn(
                'flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-all',
                'bg-surface-elevated/60 border border-border/50',
                'hover:bg-surface-highlight hover:border-text-muted/30',
                dropdownOpen && 'bg-surface-highlight border-text-muted/30'
              )}
            >
              <span className={cn(selectedNode ? 'text-text-primary' : 'text-text-muted')}>
                {selectedNode ?? 'Select node...'}
              </span>
              <ChevronDown
                size={14}
                className={cn(
                  'text-text-muted transition-transform duration-200',
                  dropdownOpen && 'rotate-180'
                )}
              />
            </button>

            {dropdownOpen && (
              <div className="absolute top-full left-0 mt-1 w-64 py-1 bg-surface-elevated/95 backdrop-blur-xl border border-border/50 rounded-lg shadow-lg z-50 max-h-80 overflow-y-auto scrollbar-dark animate-fade-in-scale">
                {(nodes ?? []).length === 0 ? (
                  <div className="px-3 py-2 text-xs text-text-muted">No nodes available</div>
                ) : (
                  (nodes ?? []).map((node) => (
                    <button
                      key={node.name}
                      onClick={() => handleSelectNode(node.name)}
                      className={cn(
                        'w-full flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors',
                        'hover:bg-surface-highlight',
                        selectedNode === node.name && 'bg-tertiary/10 text-tertiary'
                      )}
                    >
                      <span
                        className={cn(
                          'w-1.5 h-1.5 rounded-full',
                          node.type === 'mcp-server' && 'bg-violet-400',
                          node.type === 'agent' && 'bg-tertiary',
                          node.type === 'resource' && 'bg-secondary'
                        )}
                      />
                      <span className="truncate">{node.name}</span>
                      <span className="ml-auto text-[10px] text-text-muted uppercase">
                        {node.type === 'mcp-server' ? 'server' : node.type}
                      </span>
                    </button>
                  ))
                )}
              </div>
            )}
          </div>
        </div>

        <div className="flex items-center gap-2">
          <ZoomControls
            fontSize={fontSize}
            onZoomIn={zoomIn}
            onZoomOut={zoomOut}
            onReset={resetZoom}
            isMin={isMin}
            isMax={isMax}
            isDefault={isDefault}
          />
          <IconButton icon={RefreshCw} onClick={fetchData} tooltip="Refresh" size="sm" variant="ghost" />
        </div>
      </header>

      {/* Content */}
      <main
        ref={contentRef}
        className="flex-1 overflow-y-auto scrollbar-dark"
        style={{ '--log-font-size': `${fontSize}px` } as React.CSSProperties}
      >
        {isLoading && (
          <div className="h-full flex items-center justify-center">
            <div className="w-6 h-6 border-2 border-tertiary border-t-transparent rounded-full animate-spin" />
          </div>
        )}

        {!isLoading && !selectedData && (
          <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale">
            <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
              <Bot size={32} className="text-text-muted/50" />
            </div>
            <span className="text-sm">Select a node to view details</span>
          </div>
        )}

        {!isLoading && selectedData && (
          <NodeDetails node={selectedData} tools={tools} />
        )}
      </main>

      {/* Footer */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted">
        <span>
          {selectedData?.type === 'mcp-server' ? 'MCP Server' : selectedData?.type === 'agent' ? 'Agent' : selectedData?.type === 'resource' ? 'Resource' : ''}
        </span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
          Detached Window
        </span>
      </footer>
    </div>
  );
}

// Export with error boundary wrapper
export function DetachedSidebarPage() {
  return (
    <DetachedErrorBoundary>
      <DetachedSidebarPageContent />
    </DetachedErrorBoundary>
  );
}

// Node details component
function NodeDetails({ node, tools }: { node: NodeOption; tools: Tool[] }) {
  const isServer = node.type === 'mcp-server';
  const isAgent = node.type === 'agent';

  const serverData = isServer ? (node.data as MCPServerStatus) : null;
  const agentData = isAgent ? (node.data as AgentStatus) : null;
  const resourceData = !isServer && !isAgent ? (node.data as ResourceStatus) : null;

  const isExternal = serverData?.external ?? false;
  const isLocalProcess = serverData?.localProcess ?? false;
  const isSSH = serverData?.ssh ?? false;
  const isRemote = agentData?.variant === 'remote';
  const hasA2A = agentData?.hasA2A ?? false;

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

  const colorClass = isServer ? 'violet' : isAgent ? (isRemote ? 'secondary' : 'tertiary') : 'secondary';

  const status = isServer
    ? serverData?.initialized
      ? 'running'
      : 'stopped'
    : isAgent
      ? agentData?.status === 'running'
        ? 'running'
        : agentData?.status
      : resourceData?.status;

  // Filter tools for this server
  const serverTools = isServer
    ? (tools ?? []).filter((t) => t.name.startsWith(`${node.name}__`))
    : [];

  return (
    <div className="animate-fade-in-up">
      {/* Header */}
      <div className="flex items-center gap-3 p-4 border-b border-border/50 bg-surface-elevated/30">
        <div
          className={cn(
            'p-2.5 rounded-xl flex-shrink-0 border relative',
            colorClass === 'violet' && 'bg-violet-500/10 border-violet-500/20',
            colorClass === 'tertiary' && 'bg-tertiary/10 border-tertiary/20',
            colorClass === 'secondary' && 'bg-secondary/10 border-secondary/20'
          )}
        >
          <Icon
            size={18}
            className={cn(
              colorClass === 'violet' && 'text-violet-400',
              colorClass === 'tertiary' && 'text-tertiary',
              colorClass === 'secondary' && 'text-secondary'
            )}
          />
          {hasA2A && !isRemote && (
            <div className="absolute -bottom-1 -right-1 p-0.5 rounded-full bg-secondary/20 border border-secondary/40">
              <Zap size={8} className="text-secondary" />
            </div>
          )}
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="font-semibold text-text-primary truncate text-lg">{node.name}</h2>
          <div className="flex items-center gap-1.5 mt-0.5">
            <p className="text-[10px] text-text-muted uppercase tracking-wider">
              {isServer ? 'MCP Server' : isAgent ? 'Agent' : 'Resource'}
            </p>
            {hasA2A && (
              <span className="text-[9px] px-1 py-0.5 rounded font-medium bg-secondary/10 text-secondary flex items-center gap-0.5">
                <Zap size={8} />
                A2A
              </span>
            )}
          </div>
        </div>
      </div>

      {/* Status Section */}
      <Section title="Status" icon={Sparkles} defaultOpen>
        <div className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="log-text text-text-muted">State</span>
            <Badge status={status as 'running' | 'stopped' | 'error'}>{status}</Badge>
          </div>

          {isServer && serverData?.transport && (() => {
            const TransportIcon = getTransportIcon(serverData.transport);
            return (
              <div className="flex justify-between items-center">
                <span className="log-text text-text-muted">Transport</span>
                <span
                  className={cn(
                    'text-xs px-2 py-0.5 rounded-md font-mono font-medium uppercase tracking-wider flex items-center gap-1',
                    getTransportColorClasses(serverData.transport)
                  )}
                >
                  <TransportIcon size={10} />
                  {serverData.transport}
                </span>
              </div>
            );
          })()}

          {isServer && serverData?.endpoint && (
            <div className="flex justify-between items-center gap-4">
              <span className="log-text text-text-muted">Endpoint</span>
              <span
                className="text-xs text-text-secondary font-mono truncate max-w-[200px] bg-background/50 px-2 py-1 rounded-md"
                title={serverData.endpoint}
              >
                {serverData.endpoint}
              </span>
            </div>
          )}

          {isAgent && agentData?.image && (
            <div className="flex justify-between items-center gap-4">
              <span className="log-text text-text-muted">Image</span>
              <span
                className="text-xs text-text-secondary font-mono truncate max-w-[200px] bg-background/50 px-2 py-1 rounded-md"
                title={agentData.image}
              >
                {agentData.image}
              </span>
            </div>
          )}

          {isAgent && agentData?.containerId && (
            <div className="flex justify-between items-center gap-4">
              <span className="log-text text-text-muted">Container</span>
              <span
                className="text-xs text-text-secondary font-mono truncate max-w-[200px] bg-background/50 px-2 py-1 rounded-md"
                title={agentData.containerId}
              >
                {agentData.containerId}
              </span>
            </div>
          )}

          {resourceData?.image && (
            <div className="flex justify-between items-center gap-4">
              <span className="log-text text-text-muted">Image</span>
              <span
                className="text-xs text-text-secondary font-mono truncate max-w-[200px] bg-background/50 px-2 py-1 rounded-md"
                title={resourceData.image}
              >
                {resourceData.image}
              </span>
            </div>
          )}

          {resourceData?.network && (
            <div className="flex justify-between items-center">
              <span className="log-text text-text-muted">Network</span>
              <span className="log-text text-secondary font-medium">{resourceData.network}</span>
            </div>
          )}
        </div>
      </Section>

      {/* Tools Section (MCP servers only) */}
      {isServer && (
        <Section title="Tools" icon={Wrench} count={serverTools.length}>
          {serverTools.length === 0 ? (
            <p className="log-text text-text-muted italic">No tools registered</p>
          ) : (
            <div className="space-y-2">
              {serverTools.map((tool) => (
                <ToolItem key={tool.name} tool={tool} serverName={node.name} />
              ))}
            </div>
          )}
        </Section>
      )}

      {/* Skills Section (Agents with A2A) */}
      {isAgent && hasA2A && (agentData?.skills?.length ?? 0) > 0 && (
        <Section title="Skills" icon={Sparkles} count={agentData?.skillCount} defaultOpen>
          <div className="space-y-2">
            {agentData?.skills?.map((skill, idx) => (
              <div key={idx} className="px-3 py-2 rounded-lg bg-surface-elevated/60 border border-border/30">
                <span className="log-text text-text-primary font-medium">{skill}</span>
              </div>
            ))}
          </div>
        </Section>
      )}

      {/* Access Section (Agents only) */}
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
  );
}

// Collapsible section
function Section({
  title,
  icon: Icon,
  count,
  defaultOpen = false,
  children,
}: {
  title: string;
  icon?: React.ComponentType<{ size?: number; className?: string }>;
  count?: number;
  defaultOpen?: boolean;
  children: React.ReactNode;
}) {
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
          <span className="log-text font-medium text-text-primary">{title}</span>
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
      <div
        className={cn(
          'overflow-hidden transition-all duration-200',
          isOpen ? 'max-h-[1000px] opacity-100' : 'max-h-0 opacity-0'
        )}
      >
        <div className="px-4 pb-4">{children}</div>
      </div>
    </div>
  );
}

// Tool item component
function ToolItem({ tool, serverName }: { tool: Tool; serverName: string }) {
  const [isExpanded, setIsExpanded] = useState(false);
  const displayName = tool.name.replace(`${serverName}__`, '');

  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full flex items-center gap-2 px-3 py-2 hover:bg-surface-highlight/50 transition-colors"
      >
        <Wrench size={12} className="text-primary flex-shrink-0" />
        <span className="log-text font-mono text-text-primary truncate flex-1 text-left">{displayName}</span>
        {isExpanded ? (
          <ChevronUp size={12} className="text-text-muted" />
        ) : (
          <ChevronDown size={12} className="text-text-muted" />
        )}
      </button>
      {isExpanded && tool.description && (
        <div className="px-3 pb-2 log-text-detail text-text-muted">{tool.description}</div>
      )}
    </div>
  );
}

// Access item component
function AccessItem({ selector }: { selector: ToolSelector }) {
  const isRestricted = selector.tools && (selector.tools?.length ?? 0) > 0;

  return (
    <div className="rounded-lg bg-surface-elevated border border-border/40 overflow-hidden">
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
          <span className="log-text-detail text-text-muted italic px-2">Access to all available tools</span>
        )}
      </div>
    </div>
  );
}
