import { memo, useState } from 'react';
import { Link } from 'react-router';
import { ArrowRight, ChevronDown, ChevronRight, KeyRound, Library, Lightbulb, X } from 'lucide-react';
import { MCP } from '@lobehub/icons';
import { OptimizeSection } from '../sidebar/OptimizeSection';
import { cn } from '../../lib/cn';
import { useStackStore, useSelectedNodeData } from '../../stores/useStackStore';
import { useRegistryStore } from '../../stores/useRegistryStore';
import type { GatewayNodeData } from '../../types';

interface GatewaySidebarProps {
  onClose: () => void;
}

const GatewaySidebar = memo(({ onClose }: GatewaySidebarProps) => {
  const selectedData = useSelectedNodeData() as GatewayNodeData | null;
  const skills = useRegistryStore((s) => s.skills);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const selectNode = useStackStore((s) => s.selectNode);
  // null = not loaded; render the CTA without a count badge as a skeleton.
  const skillCount = skills?.length ?? null;
  // Servers waiting on downstream OAuth authorization. Clicking the row
  // jumps to the first pending server, whose sidebar has the Authorize
  // button.
  const pendingAuthServers = mcpServers.filter((s) => s.authStatus === 'needs_auth');

  return (
    <div className="h-full w-full flex flex-col overflow-hidden">
      {/* Accent line */}
      <div className="absolute top-0 left-0 bottom-0 w-px bg-gradient-to-b from-primary/40 via-primary/20 to-transparent" />

      {/* Gateway header */}
      <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30">
        <div className="flex items-center gap-3 min-w-0">
          <div className="p-2 rounded-xl flex-shrink-0 border bg-primary/10 border-primary/20">
            <MCP size={16} className="text-primary" />
          </div>
          <div className="min-w-0">
            <h2 className="font-semibold text-text-primary truncate tracking-tight">
              {selectedData?.name ?? 'Gateway'}
            </h2>
            <p className="text-[10px] text-text-muted font-mono tracking-wider">
              {selectedData?.version ?? ''}
            </p>
          </div>
        </div>
        <button onClick={onClose} aria-label="Close gateway sidebar" className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors group">
          <X size={16} className="text-text-muted group-hover:text-text-primary transition-colors" />
        </button>
      </div>

      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {pendingAuthServers.length > 0 && (
          <div className="p-4 border-b border-border/30">
            <button
              type="button"
              onClick={() => selectNode(`mcp-${pendingAuthServers[0].name}`)}
              aria-label={`Authorization: ${pendingAuthServers.length} pending. Go to ${pendingAuthServers[0].name}`}
              className={cn(
                'group flex items-center justify-between w-full px-3 py-2.5 rounded-lg',
                'bg-status-pending/5 hover:bg-status-pending/10 border border-status-pending/25 hover:border-status-pending/40',
                'transition-colors',
              )}
            >
              <div className="flex items-center gap-2.5">
                <div className="p-1.5 rounded-md bg-status-pending/10 border border-status-pending/20">
                  <KeyRound size={12} className="text-status-pending" />
                </div>
                <span className="text-xs font-medium text-text-primary">
                  Authorization
                  <span className="ml-1.5 text-[10px] text-status-pending font-mono">
                    {pendingAuthServers.length} pending
                  </span>
                </span>
              </div>
              <ArrowRight size={12} className="text-text-muted group-hover:text-status-pending transition-colors" />
            </button>
          </div>
        )}
        <CollapsibleSection title="Optimize" icon={Lightbulb}>
          <OptimizeSection />
        </CollapsibleSection>

        <div className="p-4 border-b border-border/30">
          <Link
            to="/library"
            className={cn(
              'group flex items-center justify-between w-full px-3 py-2.5 rounded-lg',
              'bg-surface-elevated/40 hover:bg-surface-highlight/60 border border-border/40 hover:border-primary/30',
              'transition-colors',
            )}
          >
            <div className="flex items-center gap-2.5">
              <div className="p-1.5 rounded-md bg-primary/10 border border-primary/20 group-hover:bg-primary/15 transition-colors">
                <Library size={12} className="text-primary" />
              </div>
              <span className="text-xs font-medium text-text-primary">
                Manage Skills
                {skillCount !== null && (
                  <span className="ml-1.5 text-[10px] text-text-muted font-mono">
                    ({skillCount})
                  </span>
                )}
              </span>
            </div>
            <ArrowRight size={12} className="text-text-muted group-hover:text-primary transition-colors" />
          </Link>
        </div>
      </div>
    </div>
  );
});

interface CollapsibleSectionProps {
  title: string;
  icon: React.ComponentType<{ size?: number; className?: string }>;
  defaultOpen?: boolean;
  children: React.ReactNode;
}

function CollapsibleSection({ title, icon: Icon, defaultOpen = true, children }: CollapsibleSectionProps) {
  const [isOpen, setIsOpen] = useState(defaultOpen);
  return (
    <div className="border-b border-border/30">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center justify-between p-4 hover:bg-surface-highlight/50 transition-colors group"
      >
        <div className="flex items-center gap-2.5">
          <div className="p-1 rounded-md bg-surface-highlight/50 group-hover:bg-surface-highlight transition-colors">
            <Icon size={12} className="text-text-muted group-hover:text-primary transition-colors" />
          </div>
          <span className="text-sm font-medium text-text-primary">{title}</span>
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

GatewaySidebar.displayName = 'GatewaySidebar';

export { GatewaySidebar };
