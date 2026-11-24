import { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import { useSearchParams } from 'react-router';
import {
  Terminal,
  Box,
  Wrench,
  Sparkles,
  Globe,
  Cpu,
  KeyRound,
  RefreshCw,
  Server,
  Layers,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { Badge } from '../components/ui/Badge';
import { IconButton } from '../components/ui/IconButton';
import { ZoomControls } from '../components/ui/ZoomControls';
import { useSharedStatus } from '../hooks/useSharedStatus';
import { useInterval } from '../hooks/useInterval';
import { useTextZoom } from '../hooks/useTextZoom';
import { INSPECTOR_TEXT_ZOOM } from '../lib/textZoomDefaults';
import { fetchTools, fetchToolCatalog } from '../lib/api';
import { getTransportIcon, getTransportColorClasses } from '../lib/transport';
import { POLLING } from '../lib/constants';
import { useStackStore } from '../stores/useStackStore';
import { ToolsEditor } from '../components/sidebar/ToolsEditor';
import type {
  MCPServerStatus,
  ResourceStatus,
} from '../types';
import { InspectorSection } from '../components/inspector';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';
import { EmptyState, FailedState } from '../components/ui/EmptyState';
import { DetachedWindowFrame } from '../components/detached/DetachedWindowFrame';
import { SelectMenu } from '../components/detached/SelectMenu';
import { SourceProvenance } from '../components/sidebar/SourceProvenance';
import { ExecutionDetails } from '../components/sidebar/ExecutionDetails';
import { SecurityEvidence } from '../components/sidebar/SecurityEvidence';
import { CatalogSummary } from '../components/sidebar/CatalogSummary';
import { useAuthStore } from '../stores/useAuthStore';

interface NodeOption {
  name: string;
  type: 'mcp-server' | 'resource';
  data: MCPServerStatus | ResourceStatus;
}

function DetachedSidebarPageContent() {
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  const [searchParams, setSearchParams] = useSearchParams();
  const initialNode = searchParams.get('node');

  const [selectedNode, setSelectedNode] = useState<string | null>(initialNode);
  const { status, settled, error, refresh } = useSharedStatus({ enabled: !authRequired });
  const nodes = useMemo<NodeOption[]>(() => {
    if (!status || useAuthStore.getState().generation !== generation || useAuthStore.getState().authRequired) {
      return [];
    }
    return [
      ...(status['mcp-servers'] ?? []).map((s) => ({
        name: s.name,
        type: 'mcp-server' as const,
        data: s,
      })),
      ...(status.resources ?? []).map((r) => ({
        name: r.name,
        type: 'resource' as const,
        data: r,
      })),
    ];
  }, [status, generation]);
  const isLoading = status == null && !authRequired && !settled;

  // Text zoom
  const contentRef = useRef<HTMLElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: INSPECTOR_TEXT_ZOOM.name,
    defaultSize: INSPECTOR_TEXT_ZOOM.defaultSize,
    minSize: INSPECTOR_TEXT_ZOOM.minSize,
    maxSize: INSPECTOR_TEXT_ZOOM.maxSize,
    step: INSPECTOR_TEXT_ZOOM.step,
    containerRef: contentRef,
  });

  useEffect(() => {
    if (!status) return;
    if (useAuthStore.getState().generation !== generation || useAuthStore.getState().authRequired) return;
    useStackStore.getState().setGatewayStatus(status);
  }, [status, generation]);

  const loadInventory = useCallback(async () => {
    const current = () => useAuthStore.getState().generation === generation && !useAuthStore.getState().authRequired;
    if (!current()) return;
    try {
      const [toolsResult, catalogResult] = await Promise.all([
        fetchTools(),
        fetchToolCatalog(),
      ]);
      if (!current()) return;
      useStackStore.getState().setTools(toolsResult.tools ?? []);
      useStackStore.getState().setToolCatalog(catalogResult.tools ?? []);
    } catch {
      // Tools stay on the last good snapshot.
    }
  }, [generation]);

  // Status goes through the shared hook so the node list this page renders
  // updates; inventory goes to the store that ToolsEditor reads.
  const refreshAll = useCallback(async () => {
    await Promise.all([refresh(), loadInventory()]);
  }, [refresh, loadInventory]);

  useInterval(loadInventory, POLLING.INVENTORY, { enabled: !authRequired });

  // Update URL when selection changes
  useEffect(() => {
    if (selectedNode) {
      setSearchParams({ node: selectedNode });
    } else {
      setSearchParams({});
    }
  }, [selectedNode, setSearchParams]);

  const selectedData = (nodes ?? []).find((n) => n.name === selectedNode);
  // Under a credential prompt the page stays mounted but inert with no nodes,
  // so it keeps the neutral prompt instead of reporting a failure.
  const contentState: 'unavailable' | 'not-found' | 'select' =
    !authRequired && status == null && settled
      ? 'unavailable'
      : !authRequired && status != null && selectedNode
        ? 'not-found'
        : 'select';

  // The unavailable state already reports the outage in the content area.
  const lost = (error != null || (settled && status == null)) && contentState !== 'unavailable';

  return (
    <DetachedWindowFrame
      windowType="sidebar"
      titleDetail={selectedNode}
      header={{
        accent: 'tertiary',
        icon: (
          <div className="p-1.5 rounded-lg bg-tertiary/10 border border-tertiary/20">
            <Server size={14} className="text-tertiary" />
          </div>
        ),
        start: (
          <SelectMenu
            label="Node"
            accent="tertiary"
            value={
              <span className={cn(selectedNode ? 'text-text-primary' : 'text-text-muted')}>
                {selectedNode ?? 'Select node...'}
              </span>
            }
            emptyText="No nodes available"
            panelClassName="max-h-80 overflow-y-auto scrollbar-dark"
            items={nodes.map((node) => ({
              key: `${node.type}:${node.name}`,
              label: node.name,
              dotClass: node.type === 'mcp-server' ? 'bg-tertiary-light' : 'bg-secondary',
              tag: node.type === 'mcp-server' ? 'MCP server' : 'resource',
              selected: selectedNode === node.name,
              onSelect: () => setSelectedNode(node.name),
            }))}
          />
        ),
        actions: (
          <>
            <ZoomControls
              fontSize={fontSize}
              onZoomIn={zoomIn}
              onZoomOut={zoomOut}
              onReset={resetZoom}
              isMin={isMin}
              isMax={isMax}
              isDefault={isDefault}
            />
            <IconButton icon={RefreshCw} onClick={() => void refreshAll()} tooltip="Refresh" size="sm" variant="ghost" />
          </>
        ),
      }}
      connection={{ lost, message: error ?? undefined, onRetry: () => void refresh() }}
      footerStart={selectedData?.type === 'mcp-server' ? 'MCP Server' : selectedData?.type === 'resource' ? 'Resource' : ''}
    >
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

        {!isLoading && selectedData && (
          <NodeDetails node={selectedData} />
        )}

        {!isLoading && !selectedData && contentState === 'unavailable' && (
          <FailedState feature="Gateway" message={error ?? undefined} onRetry={() => void refreshAll()} />
        )}

        {!isLoading && !selectedData && contentState === 'not-found' && (
          <EmptyState
            icon={Layers}
            title={`Node "${selectedNode}" not found`}
            description="It is not in the running stack. Choose another node from the list."
          />
        )}

        {!isLoading && !selectedData && contentState === 'select' && (
          <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale">
            <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
              <Layers size={32} className="text-text-muted/50" />
            </div>
            <span className="text-sm">Select a node to view details</span>
          </div>
        )}
      </main>
    </DetachedWindowFrame>
  );
}

// Export with error boundary wrapper.
export function DetachedSidebarPage() {
  return (
    <ErrorBoundary variant="window">
      <DetachedSidebarPageContent />
    </ErrorBoundary>
  );
}

// Node details component
function NodeDetails({ node }: { node: NodeOption }) {
  const isServer = node.type === 'mcp-server';

  const serverData = isServer ? (node.data as MCPServerStatus) : null;
  const resourceData = !isServer ? (node.data as ResourceStatus) : null;

  const isExternal = serverData?.external ?? false;
  const isLocalProcess = serverData?.localProcess ?? false;
  const isSSH = serverData?.ssh ?? false;

  const Icon = isServer
    ? isExternal
      ? Globe
      : isLocalProcess
        ? Cpu
        : isSSH
          ? KeyRound
          : Terminal
    : Box;

  const colorClass = isServer ? 'violet' : 'secondary';

  const status = isServer
    ? serverData?.initialized
      ? 'running'
      : 'stopped'
    : resourceData?.status;

  const advertisedTools = isServer ? (serverData?.tools ?? []) : [];

  return (
    <div className="animate-fade-in-up">
      {/* Header */}
      <div className="flex items-center gap-3 p-4 border-b border-border/50 bg-surface-elevated/30">
        <div
          className={cn(
            'p-2.5 rounded-xl flex-shrink-0 border',
            colorClass === 'violet' && 'bg-tertiary/10 border-tertiary/20',
            colorClass === 'secondary' && 'bg-secondary/10 border-secondary/20'
          )}
        >
          <Icon
            size={18}
            className={cn(
              colorClass === 'violet' && 'text-tertiary-light',
              colorClass === 'secondary' && 'text-secondary'
            )}
          />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="font-semibold text-text-primary truncate text-lg">{node.name}</h2>
          <div className="flex items-center gap-1.5 mt-0.5">
            <p className="text-3xs text-text-muted uppercase tracking-wider">
              {isServer ? 'MCP Server' : 'Resource'}
            </p>
          </div>
        </div>
      </div>

      {/* Status Section */}
      <InspectorSection title="Status" icon={Sparkles} defaultOpen>
        <div className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="log-text text-text-muted">State</span>
            <Badge status={status as 'running' | 'stopped' | 'error'}>{status}</Badge>
          </div>

          <ExecutionDetails server={serverData ?? undefined} resource={!isServer} />
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

          {isServer && (
            <SourceProvenance
              kind={serverData?.kind}
              image={serverData?.image}
              source={serverData?.source}
              labelClassName="log-text"
            />
          )}
          {isServer && serverData && <CatalogSummary data={serverData} />}
          {isServer && serverData?.name && <SecurityEvidence scope={{ server: serverData.name }} />}

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
      </InspectorSection>

      {/* Tools Section (MCP servers only) */}
      {isServer && (
        <InspectorSection title="Tools" icon={Wrench} count={advertisedTools.length}>
          {advertisedTools.length === 0 ? (
            <p className="log-text text-text-muted italic">No tools registered</p>
          ) : (
            <ToolsEditor
              serverName={node.name}
              savedTools={serverData?.toolWhitelist ?? []}
              serverTools={advertisedTools}
            />
          )}
        </InspectorSection>
      )}
    </div>
  );
}
