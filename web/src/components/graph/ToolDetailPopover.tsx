import { memo, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Wrench, X, ArrowUpRight, Copy, ChevronDown, ChevronRight } from 'lucide-react';
import { cn } from '../../lib/cn';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { formatLastUsed } from '../../lib/toolAudit';
import { fetchToolUsage } from '../../lib/api';
import { useStackStore } from '../../stores/useStackStore';
import { CodeViewer } from '../ui/CodeViewer';

interface ToolDetailPopoverProps {
  // Owning MCP server name.
  serverName: string;
  // Unprefixed tool name (as carried by fan-out nodes).
  toolName: string;
  onClose: () => void;
}

/**
 * Canvas-anchored detail card for a single fanned-out tool. Mirrors
 * ToolOverflowNode's popover mechanics (absolute, in-node, pans/zooms with the
 * graph) rather than a portal, so it stays glued to its pill. Resolves
 * description and input schema from the globally-polled tool catalog and shows
 * a best-effort "last used" line. The parent owns open/close state and the
 * outside-click/Escape dismissal (see useDismiss); this card only renders and
 * fires onClose from its own close button.
 *
 * A trimmed inline layout is used instead of sharing ToolDetailPanel's sections
 * because the popover wants a compact, collapse-by-default schema that the
 * workspace rail does not; a shared component would have a single consumer.
 */
const ToolDetailPopover = memo(({ serverName, toolName, onClose }: ToolDetailPopoverProps) => {
  const navigate = useNavigate();
  const prefixedName = `${serverName}${TOOL_NAME_DELIMITER}${toolName}`;

  // The catalog is keyed by the prefixed name and is populated app-wide by the
  // poll cycle, so it is already present on the Topology page. A missing entry
  // (e.g. first paint before the first poll) renders explicit empty states.
  // Select the array and resolve the entry in a memo so a poll that replaces an
  // unrelated tool does not re-run the lookup.
  const toolCatalog = useStackStore((s) => s.toolCatalog);
  const entry = useMemo(
    () => toolCatalog.find((t) => t.name === prefixedName),
    [toolCatalog, prefixedName],
  );

  const [schemaOpen, setSchemaOpen] = useState(false);
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
      // stopPropagation so a click inside the card never reaches the canvas
      // pane/node handlers; dismissal is the parent's job via useDismiss.
      onClick={(e) => e.stopPropagation()}
      className={cn(
        'nodrag absolute left-full top-0 ml-2 z-50 w-72',
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

      <div className="px-3 py-2.5 space-y-3 max-h-80 overflow-y-auto scrollbar-dark">
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

        <section className="space-y-1.5">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setSchemaOpen((v) => !v);
            }}
            aria-expanded={schemaOpen}
            className="flex items-center gap-1 text-[9px] uppercase tracking-[0.18em] text-text-muted/70 hover:text-text-secondary transition-colors"
          >
            {schemaOpen ? <ChevronDown size={10} aria-hidden="true" /> : <ChevronRight size={10} aria-hidden="true" />}
            Input schema
          </button>
          {schemaOpen &&
            (entry?.inputSchema ? (
              <CodeViewer
                language="json"
                content={JSON.stringify(entry.inputSchema, null, 2)}
                ariaLabel={`${prefixedName} input schema`}
                className="rounded-md border border-border/30 bg-background/80 max-h-48"
              />
            ) : (
              <p className="text-[10px] text-text-muted/70 italic">No input schema available.</p>
            ))}
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
