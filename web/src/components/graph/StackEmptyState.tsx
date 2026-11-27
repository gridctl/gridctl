import { NodeToolbar, Position } from '@xyflow/react';
import { Database, Layers, Plus, Server } from 'lucide-react';

import { useWizardStore } from '../../stores/useWizardStore';
import { cn } from '../../lib/cn';
import { GATEWAY_NODE_ID } from '../../lib/graph/edges';

interface StackEmptyStateProps {
  /** A stack is loaded; stackless mode cannot append MCP servers or resources. */
  hasStack: boolean;
}

const QUICK_ADD = [
  { type: 'mcp-server' as const, icon: Server, label: 'MCP Server', color: 'text-primary hover:text-primary-light' },
  { type: 'resource' as const, icon: Database, label: 'Resource', color: 'text-secondary hover:text-secondary/80' },
];

// Anchored beside the gateway node so it follows pans and drags at a constant
// size. The toolbar portals into React Flow's zoom surface, so the card opts
// out of pan, wheel zoom, and drag gestures that start on it.
export function StackEmptyState({ hasStack }: StackEmptyStateProps) {
  return (
    <NodeToolbar nodeId={GATEWAY_NODE_ID} isVisible position={Position.Right} offset={24}>
      <div
        data-testid="stack-empty-state"
        className="nopan nowheel nodrag pointer-events-auto glass-panel-elevated rounded-xl p-5 w-72 animate-fade-in-up"
      >
        <div className="w-10 h-10 rounded-xl bg-surface-elevated border border-border/40 flex items-center justify-center mb-3">
          <Layers size={18} className="text-primary" />
        </div>
        <h2 className="text-sm font-medium text-text-primary mb-1.5">
          {hasStack ? 'This stack is empty' : 'No stack loaded'}
        </h2>
        <p className="text-xs text-text-muted mb-4 leading-relaxed">
          {hasStack
            ? 'Add an MCP server or a resource to start building this stack.'
            : 'Create a stack to add MCP servers and resources to this gateway.'}
        </p>
        <button
          type="button"
          onClick={() => useWizardStore.getState().open()}
          className={cn(
            'inline-flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-medium',
            'bg-primary/20 text-primary hover:bg-primary/30 border border-primary/30',
            'transition-all duration-200',
          )}
        >
          <Plus size={14} />
          Create new
        </button>
        {hasStack && (
          <div className="flex items-center gap-2 mt-3">
            <span className="text-3xs text-text-muted">or add:</span>
            {QUICK_ADD.map((item) => (
              <button
                key={item.type}
                type="button"
                onClick={() => useWizardStore.getState().open(item.type)}
                className={cn(
                  'inline-flex items-center gap-1 px-2 py-1 rounded-md text-3xs font-medium',
                  'bg-surface-overlay-2 border border-border-subtle hover:border-border',
                  'transition-all duration-200',
                  item.color,
                )}
              >
                <item.icon size={10} />
                {item.label}
              </button>
            ))}
          </div>
        )}
      </div>
    </NodeToolbar>
  );
}
