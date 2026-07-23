import { memo, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useReactFlow } from '@xyflow/react';
import { Wrench, X, ArrowUpRight, Copy } from 'lucide-react';
import { cn } from '../../lib/cn';
import { horizontalOverflow } from '../../lib/graph/popoverPlacement';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { formatLastUsed } from '../../lib/toolAudit';
import { fetchToolUsage } from '../../lib/api';
import { useStackStore } from '../../stores/useStackStore';

interface ToolDetailPopoverProps {
  // Owning MCP server name.
  serverName: string;
  // Unprefixed tool name (as carried by fan-out nodes).
  toolName: string;
  // Positioning override for the card root. Tool pills use the default anchor
  // (just right of the pill); the overflow node positions the card past the
  // right edge of its tool-list panel, whose width the popover cannot know.
  positionStyle?: React.CSSProperties;
  onClose: () => void;
}

/**
 * Canvas-anchored detail card for a single fanned-out tool. Mirrors
 * ToolOverflowNode's popover mechanics (absolute, in-node, pans/zooms with the
 * graph) rather than a portal, so it stays glued to its pill. Resolves the
 * description from the globally-polled tool catalog and shows a best-effort
 * "last used" line. The parent owns open/close state and the outside-click/
 * Escape dismissal (see useDismiss); this card only renders and fires onClose
 * from its own close button.
 *
 * The input schema is intentionally left out: it does not render legibly in a
 * compact canvas popover. "Open in Tools" deep-links to the workspace rail,
 * which shows the full schema with room to read it.
 */
const ToolDetailPopover = memo(({ serverName, toolName, positionStyle, onClose }: ToolDetailPopoverProps) => {
  const navigate = useNavigate();
  const prefixedName = `${serverName}${TOOL_NAME_DELIMITER}${toolName}`;

  // Tool pills are the rightmost nodes in the graph and the auto-refit frames
  // them flush against the canvas edge, so the right-opening card routinely
  // lands past it. Mirroring the card to the anchor's left would cover the
  // graph instead, so pan the viewport left by the overrun once per open (the
  // card unmounts on close, so mount is open), before paint: the card is
  // glued to its anchor, so the pan carries it fully into view. Rects and the
  // viewport translation are both screen-space, so the canvas zoom is already
  // applied; a missing container falls back to the document and an
  // unmeasurable rect (jsdom) pans nothing.
  const { getViewport, setViewport } = useReactFlow();
  const cardRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = cardRef.current;
    if (!el) return;
    const container = el.closest('.react-flow') ?? document.documentElement;
    const overflow = horizontalOverflow(
      el.getBoundingClientRect(),
      container.getBoundingClientRect(),
    );
    if (overflow === 0) return;
    const viewport = getViewport();
    void setViewport({ ...viewport, x: viewport.x - overflow }, { duration: 200 });
  }, [getViewport, setViewport]);

  // The catalog is keyed by the prefixed name and is populated app-wide by the
  // poll cycle, so it is already present on the Stack page. A missing entry
  // (e.g. first paint before the first poll) renders an explicit empty state.
  // Select the array and resolve the entry in a memo so a poll that replaces an
  // unrelated tool does not re-run the lookup.
  const toolCatalog = useStackStore((s) => s.toolCatalog);
  const entry = useMemo(
    () => toolCatalog.find((t) => t.name === prefixedName),
    [toolCatalog, prefixedName],
  );

  const [lastCalledAt, setLastCalledAt] = useState<string | undefined>(undefined);

  // Usage is not globally polled (the hook only runs under Audit Mode), so we
  // fetch it best-effort when the popover opens. Failures and absences leave
  // the usage line hidden rather than surfacing noise.
  useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const usage = await fetchToolUsage();
        if (!active) return;
        setLastCalledAt(usage.servers?.[serverName]?.[toolName]?.lastCalledAt);
      } catch {
        /* best-effort: leave the usage line hidden */
      }
    })();
    return () => {
      active = false;
    };
  }, [serverName, toolName]);

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation();
    void navigator.clipboard?.writeText(prefixedName);
  };

  const handleOpenInTools = (e: React.MouseEvent) => {
    e.stopPropagation();
    onClose();
    navigate(`/tools?server=${encodeURIComponent(serverName)}&q=${encodeURIComponent(toolName)}`);
  };

  return (
    <div
      ref={cardRef}
      // stopPropagation so a click inside the card never reaches the canvas
      // pane/node handlers; dismissal is the parent's job via useDismiss.
      onClick={(e) => e.stopPropagation()}
      style={positionStyle}
      className={cn(
        'nodrag absolute z-50 w-72 frost-surface',
        !positionStyle && 'left-full top-0 ml-2',
        'rounded-lg border border-border bg-surface-elevated/95',
        'backdrop-blur-xl shadow-bevel animate-fade-in-scale',
      )}
    >
      <div className="flex items-start gap-2 px-3 py-2 border-b border-border/40">
        <Wrench size={12} className="text-primary/80 flex-shrink-0 mt-0.5" aria-hidden="true" />
        <span
          className="flex-1 min-w-0 font-mono text-[11px] text-text-primary break-all"
          title={prefixedName}
        >
          {prefixedName}
        </span>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onClose();
          }}
          aria-label="Close tool details"
          className="flex-shrink-0 p-0.5 rounded hover:bg-surface-highlight transition-colors"
        >
          <X size={12} className="text-text-muted" />
        </button>
      </div>

      <div className="px-3 py-2.5 space-y-3 max-h-80 overflow-y-auto scrollbar-dark node-scroll nowheel">
        <section className="space-y-1">
          <h4 className="text-[9px] uppercase tracking-[0.18em] text-text-muted/70">Description</h4>
          {entry?.description ? (
            <p className="text-[11px] text-text-secondary leading-relaxed break-words whitespace-pre-wrap">
              {entry.description}
            </p>
          ) : (
            <p className="text-[10px] text-text-muted/70 italic">No description available.</p>
          )}
        </section>

        {lastCalledAt && (
          <p className="text-[10px] text-text-muted">Last used {formatLastUsed(lastCalledAt)}</p>
        )}
      </div>

      <div className="flex items-center gap-2 px-3 py-2 border-t border-border/40">
        <button
          type="button"
          onClick={handleOpenInTools}
          className="inline-flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <ArrowUpRight size={11} aria-hidden="true" />
          Open in Tools
        </button>
        <span className="text-border" aria-hidden="true">
          ·
        </span>
        <button
          type="button"
          onClick={handleCopy}
          className="inline-flex items-center gap-1 text-[10px] text-text-muted hover:text-text-secondary transition-colors"
        >
          <Copy size={11} aria-hidden="true" />
          Copy name
        </button>
      </div>
    </div>
  );
});

ToolDetailPopover.displayName = 'ToolDetailPopover';

export default ToolDetailPopover;
