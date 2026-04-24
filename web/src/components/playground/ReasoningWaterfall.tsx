import { useState } from 'react';
import { ChevronDown, ChevronRight, Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { usePlaygroundStore, type WaterfallEntry } from '../../stores/usePlaygroundStore';

// Color palette for server-keyed entries — matches TraceWaterfall palette
const SERVER_COLORS = [
  '#0d9488', // teal
  '#8b5cf6', // violet
  '#3b82f6', // blue
  '#ec4899', // pink
  '#10b981', // emerald
  '#f97316', // orange
  '#06b6d4', // cyan
  '#a855f7', // purple
];

function stableColor(key: string): string {
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = ((hash << 5) - hash) + key.charCodeAt(i);
    hash |= 0;
  }
  return SERVER_COLORS[Math.abs(hash) % SERVER_COLORS.length];
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function WaterfallRow({ entry }: { entry: WaterfallEntry }) {
  const [expanded, setExpanded] = useState(false);
  const isPending = entry.status === 'pending';
  const color = stableColor(entry.serverName);

  return (
    <div className={cn('border-b border-border/10 last:border-0')}>
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 px-3 py-1.5 text-left hover:bg-surface-highlight/20 transition-colors group"
      >
        {/* Status dot */}
        <div className="flex-shrink-0 w-3 h-3 flex items-center justify-center">
          <span
            className={cn('w-2 h-2 rounded-full', isPending && 'animate-pulse')}
            style={{ backgroundColor: color }}
          />
        </div>

        {/* Left-edge accent bar */}
        <div
          className="w-0.5 h-4 rounded-full flex-shrink-0 opacity-50"
          style={{ backgroundColor: color }}
        />

        {/* Tool name */}
        <span className="font-mono text-[10px] text-text-primary truncate flex-1">
          {entry.toolName}
        </span>

        {/* Server name */}
        <span className="text-[9px] text-text-muted/55 font-mono truncate max-w-[90px] flex-shrink-0">
          {entry.serverName}
        </span>

        {/* Duration / spinner */}
        <div className="flex-shrink-0 w-14 text-right">
          {isPending ? (
            <span className="inline-flex items-center justify-end gap-1 text-[9px] text-text-muted/40">
              <span
                className="w-2 h-2 rounded-full border border-current border-t-transparent animate-spin"
                style={{ borderColor: `${color}80`, borderTopColor: 'transparent' }}
              />
            </span>
          ) : entry.durationMs != null ? (
            <span className="text-[9px] font-mono text-text-muted tabular-nums">
              {formatDuration(entry.durationMs)}
            </span>
          ) : null}
        </div>

        {/* Chevron — always reserve space, fade in on hover */}
        <div className="flex-shrink-0 w-3 opacity-0 group-hover:opacity-60 transition-opacity">
          {expanded
            ? <ChevronDown size={10} className="text-text-muted" />
            : <ChevronRight size={10} className="text-text-muted" />}
        </div>
      </button>

      {/* Expanded detail: input + output JSON */}
      {expanded && (
        <div className="px-3 pb-2 space-y-1.5">
          {entry.input !== undefined && (
            <div>
              <p className="text-[9px] text-text-muted/40 uppercase tracking-wider mb-0.5 font-medium">
                Input
              </p>
              <pre className="text-[9px] font-mono text-text-secondary bg-background/60 border border-border/20 rounded p-2 overflow-x-auto whitespace-pre-wrap break-words max-h-28 scrollbar-dark">
                {JSON.stringify(entry.input, null, 2)}
              </pre>
            </div>
          )}
          {entry.output !== undefined ? (
            <div>
              <p className="text-[9px] text-text-muted/40 uppercase tracking-wider mb-0.5 font-medium">
                Output
              </p>
              <pre className="text-[9px] font-mono text-text-secondary bg-background/60 border border-border/20 rounded p-2 overflow-x-auto whitespace-pre-wrap break-words max-h-32 scrollbar-dark">
                {JSON.stringify(entry.output, null, 2)}
              </pre>
            </div>
          ) : !isPending ? (
            <p className="text-[9px] text-text-muted/30 italic">No output returned</p>
          ) : (
            <p className="text-[9px] text-text-muted/30 italic">Waiting for response…</p>
          )}
        </div>
      )}
    </div>
  );
}

export function ReasoningWaterfall() {
  const waterfallEntries = usePlaygroundStore((s) => s.waterfallEntries);

  if (waterfallEntries.length === 0) return null;

  const pendingCount = waterfallEntries.filter((e) => e.status === 'pending').length;

  return (
    <div className="flex-shrink-0 border-t border-border/20 bg-surface-elevated/10">
      {/* Section header */}
      <div className="flex items-center gap-2 px-3 h-7 border-b border-border/15">
        <Wrench size={9} className="text-text-muted/40 flex-shrink-0" />
        <span className="text-[9px] font-medium text-text-muted/50 uppercase tracking-wider">
          Reasoning
        </span>
        {pendingCount > 0 && (
          <span className="inline-flex items-center gap-1 text-[9px] text-secondary/60">
            <span className="w-1.5 h-1.5 rounded-full bg-secondary/50 animate-pulse" />
            {pendingCount} active
          </span>
        )}
        <span className="text-[9px] text-text-muted/25 ml-auto tabular-nums">
          {waterfallEntries.length} {waterfallEntries.length === 1 ? 'call' : 'calls'}
        </span>
      </div>

      {/* Tool call rows */}
      <div className="max-h-[160px] overflow-y-auto scrollbar-dark">
        {waterfallEntries.map((entry) => (
          <WaterfallRow key={entry.id} entry={entry} />
        ))}
      </div>
    </div>
  );
}
