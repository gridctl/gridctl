import { useState } from 'react';
import { X, Terminal, Box, Bot, ChevronDown, ChevronRight, Wrench, FileText, Sparkles } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { ToolList } from '../ui/ToolList';
import { ControlBar } from '../ui/ControlBar';
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
  const Icon = isServer ? Terminal : isAgent ? Bot : Box;
  const colorClass = isServer ? 'primary' : isAgent ? 'tertiary' : 'secondary';

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
        colorClass === 'secondary' && 'bg-gradient-to-b from-secondary/40 via-secondary/20 to-transparent'
      )} />

      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30">
        <div className="flex items-center gap-3 min-w-0">
          <div className={cn(
            'p-2 rounded-xl flex-shrink-0 border',
            colorClass === 'primary' && 'bg-primary/10 border-primary/20',
            colorClass === 'tertiary' && 'bg-tertiary/10 border-tertiary/20',
            colorClass === 'secondary' && 'bg-secondary/10 border-secondary/20'
          )}>
            <Icon
              size={16}
              className={cn(
                colorClass === 'primary' && 'text-primary',
                colorClass === 'tertiary' && 'text-tertiary',
                colorClass === 'secondary' && 'text-secondary'
              )}
            />
          </div>
          <div className="min-w-0">
            <h2 className="font-semibold text-text-primary truncate tracking-tight">
              {data.name}
            </h2>
            <p className="text-[10px] text-text-muted uppercase tracking-wider">
              {isServer ? 'MCP Server' : isAgent ? 'Agent' : 'Resource'}
            </p>
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

            {isServer && (data as MCPServerNodeData).transport && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Transport</span>
                <span className={cn(
                  'text-xs px-2 py-0.5 rounded-md font-mono font-medium uppercase tracking-wider',
                  (data as MCPServerNodeData).transport === 'stdio'
                    ? 'bg-primary/10 text-primary'
                    : 'bg-secondary/10 text-secondary'
                )}>
                  {(data as MCPServerNodeData).transport}
                </span>
              </div>
            )}

            {isServer && (data as MCPServerNodeData).endpoint && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Endpoint</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={(data as MCPServerNodeData).endpoint}>
                  {(data as MCPServerNodeData).endpoint}
                </span>
              </div>
            )}

            {/* Agent fields */}
            {isAgent && (data as AgentNodeData).image && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Image</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={(data as AgentNodeData).image}>
                  {(data as AgentNodeData).image}
                </span>
              </div>
            )}

            {isAgent && (data as AgentNodeData).containerId && (
              <div className="flex justify-between items-center gap-4">
                <span className="text-sm text-text-muted">Container</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px] bg-background/50 px-2 py-1 rounded-md" title={(data as AgentNodeData).containerId}>
                  {(data as AgentNodeData).containerId}
                </span>
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
