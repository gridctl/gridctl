import { useMemo, useState } from 'react';
import { cn } from '../../lib/cn';
import type { AgentRunEvent } from '../../lib/agent-runs';
import { formatDurationMicros } from './format';

interface RunWaterfallProps {
  events: AgentRunEvent[];
  /** Optional filter — only spans matching this substring (against
   *  node_id / node_name / type) are rendered. */
  filter?: string;
}

interface Span {
  key: string;
  nodeID: string;
  nodeName: string;
  startMs: number;
  durationMs: number;
  status: 'running' | 'ok' | 'error';
  depth: number;
}

interface NodeEnterPayload {
  node_id?: string;
  node_name?: string;
}
interface NodeExitPayload {
  node_id?: string;
  duration_micros?: number;
  success?: boolean;
}

/**
 * RunWaterfall renders a Honeycomb-style timeline of node spans. The
 * input is the raw event timeline; we reconstruct spans by pairing
 * `node_enter` with the matching `node_exit` (when present) or
 * leaving the span open-ended (currently in flight).
 *
 * The visual is intentionally lo-fi — single-color bars sized by
 * duration, with the depth axis carried through indentation. It is
 * meant to be readable on a 1200px screen at a glance; richer drill
 * downs route through the per-span row tooltip and the output panel.
 */
export function RunWaterfall({ events, filter }: RunWaterfallProps) {
  const { spans, totalMs } = useMemo(() => buildSpans(events), [events]);
  const [hoverKey, setHoverKey] = useState<string | null>(null);

  const filtered = useMemo(() => {
    if (!filter) return spans;
    const q = filter.toLowerCase();
    return spans.filter(
      (s) =>
        s.nodeID.toLowerCase().includes(q) ||
        s.nodeName.toLowerCase().includes(q),
    );
  }, [spans, filter]);

  if (filtered.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-[12px] text-text-muted">
        No spans recorded for this run yet.
      </div>
    );
  }

  return (
    <div className="text-[11px] font-mono">
      <ol className="space-y-0.5">
        {filtered.map((span) => {
          const leftPct = totalMs > 0 ? (span.startMs / totalMs) * 100 : 0;
          const widthPct =
            totalMs > 0 ? Math.max((span.durationMs / totalMs) * 100, 0.5) : 100;
          const isHover = hoverKey === span.key;
          return (
            <li
              key={span.key}
              onMouseEnter={() => setHoverKey(span.key)}
              onMouseLeave={() => setHoverKey(null)}
              className={cn(
                'group grid grid-cols-[220px_1fr_80px] items-center gap-3 px-3 py-1 rounded-sm',
                isHover && 'bg-surface-highlight/30',
              )}
              style={{ paddingLeft: `${12 + span.depth * 12}px` }}
            >
              <div className="truncate text-text-secondary" title={span.nodeID}>
                <span className="text-text-muted mr-1.5">{span.nodeName}</span>
                <span className="text-text-muted/60">{span.nodeID}</span>
              </div>
              <div className="relative h-3.5 bg-background/40 rounded-sm">
                <div
                  className={cn(
                    'absolute top-0 bottom-0 rounded-sm',
                    span.status === 'error' && 'bg-status-error/80',
                    span.status === 'running' && 'bg-status-running/60 animate-pulse',
                    span.status === 'ok' && 'bg-primary/60',
                  )}
                  style={{ left: `${leftPct}%`, width: `${widthPct}%` }}
                  aria-label={`${span.nodeName} ${formatDurationMicros(span.durationMs * 1000)}`}
                />
              </div>
              <div className="text-right text-text-muted tabular-nums">
                {span.status === 'running'
                  ? 'live'
                  : formatDurationMicros(span.durationMs * 1000)}
              </div>
            </li>
          );
        })}
      </ol>
    </div>
  );
}

function buildSpans(events: AgentRunEvent[]): { spans: Span[]; totalMs: number } {
  if (events.length === 0) return { spans: [], totalMs: 0 };
  const runStart = Date.parse(events[0]?.time ?? '');
  const baseMs = Number.isNaN(runStart) ? 0 : runStart;

  const open: Map<string, { enterMs: number; depth: number; nodeName: string; key: string }> = new Map();
  const depthStack: string[] = [];
  const spans: Span[] = [];
  let counter = 0;

  for (const ev of events) {
    const tMs = Date.parse(ev.time);
    if (Number.isNaN(tMs)) continue;
    const offsetMs = tMs - baseMs;

    if (ev.type === 'node_enter') {
      const p = (ev.payload ?? {}) as NodeEnterPayload;
      const nodeID = p.node_id ?? `node-${counter++}`;
      const nodeName = p.node_name ?? '';
      const depth = depthStack.length;
      const key = `${nodeID}@${ev.seq}`;
      open.set(nodeID, { enterMs: offsetMs, depth, nodeName, key });
      depthStack.push(nodeID);
    } else if (ev.type === 'node_exit') {
      const p = (ev.payload ?? {}) as NodeExitPayload;
      const nodeID = p.node_id ?? depthStack[depthStack.length - 1];
      const opened = nodeID ? open.get(nodeID) : undefined;
      if (opened && nodeID) {
        spans.push({
          key: opened.key,
          nodeID,
          nodeName: opened.nodeName,
          startMs: opened.enterMs,
          durationMs:
            p.duration_micros != null
              ? p.duration_micros / 1000
              : offsetMs - opened.enterMs,
          status: p.success ? 'ok' : 'error',
          depth: opened.depth,
        });
        open.delete(nodeID);
        // Pop matching frame off the depth stack.
        const idx = depthStack.lastIndexOf(nodeID);
        if (idx !== -1) depthStack.splice(idx, 1);
      }
    }
  }

  // Append any still-open spans as in-flight markers anchored to the
  // last event we've seen.
  const lastTimeMs =
    Date.parse(events[events.length - 1]?.time ?? '') - baseMs || 0;
  for (const [nodeID, opened] of open.entries()) {
    spans.push({
      key: opened.key,
      nodeID,
      nodeName: opened.nodeName,
      startMs: opened.enterMs,
      durationMs: Math.max(lastTimeMs - opened.enterMs, 1),
      status: 'running',
      depth: opened.depth,
    });
  }

  spans.sort((a, b) => a.startMs - b.startMs);
  const totalMs = spans.reduce(
    (max, s) => Math.max(max, s.startMs + s.durationMs),
    1,
  );
  return { spans, totalMs };
}
