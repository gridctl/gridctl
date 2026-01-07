import { useState } from 'react';
import { X, Terminal, Box, ChevronDown, ChevronRight, Wrench, FileText } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { ToolList } from '../ui/ToolList';
import { ControlBar } from '../ui/ControlBar';
import { useTopologyStore, useSelectedNodeData } from '../../stores/useTopologyStore';
import { useUIStore } from '../../stores/useUIStore';
import type { MCPServerNodeData, ResourceNodeData } from '../../types';

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
  const data = selectedData as unknown as MCPServerNodeData | ResourceNodeData;
  const Icon = isServer ? Terminal : Box;

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
        'fixed top-14 right-0 h-[calc(100vh-56px-32px)] w-80 bg-surface border-l border-border',
        'transform transition-transform duration-300 ease-out z-20',
        'flex flex-col overflow-hidden',
        sidebarOpen ? 'translate-x-0' : 'translate-x-full'
      )}
    >
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b border-border">
        <div className="flex items-center gap-2 min-w-0">
          <div className={cn(
            'p-1.5 rounded flex-shrink-0',
            isServer ? 'bg-primary/20' : 'bg-secondary/20'
          )}>
            <Icon
              size={16}
              className={isServer ? 'text-primary' : 'text-secondary'}
            />
          </div>
          <h2 className="font-semibold text-text-primary truncate">
            {data.name}
          </h2>
        </div>
        <button
          onClick={handleClose}
          className="p-1 rounded hover:bg-surfaceHighlight transition-colors flex-shrink-0"
        >
          <X size={16} className="text-text-muted" />
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark relative">
        {/* Status Section */}
        <Section title="Status" defaultOpen>
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
                <span className="text-sm text-text-secondary uppercase font-mono">
                  {(data as MCPServerNodeData).transport}
                </span>
              </div>
            )}

            {isServer && (data as MCPServerNodeData).endpoint && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Endpoint</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px]" title={(data as MCPServerNodeData).endpoint}>
                  {(data as MCPServerNodeData).endpoint}
                </span>
              </div>
            )}

            {!isServer && (data as ResourceNodeData).image && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Image</span>
                <span className="text-xs text-text-secondary font-mono truncate max-w-[180px]" title={(data as ResourceNodeData).image}>
                  {(data as ResourceNodeData).image}
                </span>
              </div>
            )}

            {!isServer && (data as ResourceNodeData).network && (
              <div className="flex justify-between items-center">
                <span className="text-sm text-text-muted">Network</span>
                <span className="text-sm text-text-secondary">
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
              'w-full mt-2 flex items-center justify-center gap-2 py-2 rounded',
              'bg-background hover:bg-surface-highlight transition-colors text-sm'
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
    <div className="border-b border-border">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center justify-between p-4 hover:bg-surfaceHighlight transition-colors"
      >
        <div className="flex items-center gap-2">
          {Icon && <Icon size={14} className="text-text-muted" />}
          <span className="text-sm font-medium text-text-primary">{title}</span>
          {count !== undefined && (
            <span className="text-xs text-text-muted bg-background px-1.5 py-0.5 rounded">
              {count}
            </span>
          )}
        </div>
        {isOpen ? (
          <ChevronDown size={16} className="text-text-muted" />
        ) : (
          <ChevronRight size={16} className="text-text-muted" />
        )}
      </button>
      {isOpen && (
        <div className="px-4 pb-4">
          {children}
        </div>
      )}
    </div>
  );
}
