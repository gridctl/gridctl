import { memo, useState, useCallback } from 'react';
import { Handle, Position } from '@xyflow/react';
import { MoreHorizontal, Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { LAYOUT } from '../../lib/constants';
import { useDismiss } from '../../hooks/useDismiss';
import ToolDetailPopover from './ToolDetailPopover';
import type { ToolOverflowNodeData } from '../../types';

interface ToolOverflowNodeProps {
  data: ToolOverflowNodeData;
}

/**
 * The "+N more" aggregate node shown when an expanded server has more tools
 * than the fan-out cap. Clicking it opens an in-node popover listing the
 * remaining tools rather than mounting more canvas nodes, so a large server
 * stays legible. The popover lives inside this single node, anchored to it,
 * so it pans and zooms with the canvas.
 *
 * Each listed tool is itself a button that opens the same canvas-anchored
 * detail popover the visible tool pills use, so the hidden tools are just as
 * inspectable as the ones below the cap.
 */
const ToolOverflowNode = memo(({ data }: ToolOverflowNodeProps) => {
  const [open, setOpen] = useState(false);
  // The hidden tool whose detail popover is showing, or null. Distinct from
  // `open` (the list itself) so the list stays put while a detail is inspected.
  const [selectedTool, setSelectedTool] = useState<string | null>(null);

  const closeAll = useCallback(() => {
    setOpen(false);
    setSelectedTool(null);
  }, []);

  // One wrapper ref covers the trigger, the list, and any open detail popover,
  // so an outside click or Escape dismisses the whole node's overlays at once.
  const wrapperRef = useDismiss<HTMLDivElement>(open || selectedTool !== null, closeAll);

  const toggle = useCallback((event: React.MouseEvent) => {
    // Keep the click from bubbling to the canvas node-selection handler.
    event.stopPropagation();
    setOpen((prev) => !prev);
  }, []);

  return (
    <div ref={wrapperRef} className="relative nodrag" style={{ width: LAYOUT.TOOL_WIDTH }}>
      <button
        type="button"
        onClick={toggle}
        aria-expanded={open}
        aria-label={`Show ${data.overflowCount} more ${data.serverName} tools`}
        className={cn(
          'animate-slide-in-right',
          'w-full flex items-center gap-2 px-2.5 rounded-lg',
          'border border-dashed border-text-secondary/40 bg-white/[0.03]',
          'backdrop-blur-xl text-left',
          'transition-colors duration-200 hover:border-text-secondary/70 hover:bg-white/[0.06]'
        )}
        style={{ height: LAYOUT.TOOL_HEIGHT }}
      >
        <MoreHorizontal size={12} className="text-text-secondary flex-shrink-0" />
        <span className="font-mono text-[11px] text-text-secondary tracking-tight">
          +{data.overflowCount} more
        </span>
      </button>

      {open && (
        <div
          className={cn(
            'absolute left-0 top-full mt-1.5 z-50 w-full',
            'rounded-lg border border-border bg-surface-elevated/95',
            'backdrop-blur-xl shadow-bevel p-1.5',
            'max-h-48 overflow-y-auto animate-fade-in-scale'
          )}
        >
          <div className="px-1.5 py-1 text-[9px] uppercase tracking-widest text-text-muted">
            {data.serverName} · {data.overflowCount} more
          </div>
          <ul className="space-y-0.5">
            {data.hiddenTools.map((tool) => (
              <li key={tool}>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    setSelectedTool(tool);
                  }}
                  aria-label={`Show details for ${data.serverName} tool ${tool}`}
                  className={cn(
                    'w-full flex items-center gap-1.5 px-1.5 py-1 rounded-md text-left',
                    'hover:bg-white/[0.06] transition-colors',
                    selectedTool === tool && 'bg-white/[0.06]',
                  )}
                >
                  <Wrench size={10} className="text-text-secondary/80 flex-shrink-0" aria-hidden="true" />
                  <span className="tool-label min-w-0 flex-1 font-mono text-[11px] text-text-secondary truncate" title={tool}>
                    {tool}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2 !h-2 !bg-text-secondary !border-2 !border-background !rounded-full',
          'transition-all duration-200'
        )}
        id="input"
      />

      {selectedTool && (
        <ToolDetailPopover
          serverName={data.serverName}
          toolName={selectedTool}
          onClose={() => setSelectedTool(null)}
        />
      )}
    </div>
  );
});

ToolOverflowNode.displayName = 'ToolOverflowNode';

export default ToolOverflowNode;
