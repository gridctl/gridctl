import type { RefObject } from 'react';
import { Search, X } from 'lucide-react';
import { cn } from '../../lib/cn';

// Filter chrome for the Per-Tool breakdown: search over tool/server names,
// one server facet, and a priced/unpriced facet. All state lives in the URL
// (the host owns the params); this component is presentation only. The
// result-count line is a live region so screen readers hear the filter take
// effect, matching the workspace's polite-announcement pattern.
export function ToolsFilterBar({
  query,
  onQuery,
  servers,
  activeServer,
  onServer,
  priced,
  onPriced,
  onClearAll,
  matchCount,
  totalCount,
  searchInputRef,
}: {
  query: string;
  onQuery: (q: string) => void;
  servers: string[];
  activeServer: string | null;
  onServer: (server: string | null) => void;
  priced: 'yes' | 'no' | null;
  onPriced: (priced: 'yes' | 'no' | null) => void;
  onClearAll: () => void;
  matchCount: number;
  totalCount: number;
  searchInputRef: RefObject<HTMLInputElement | null>;
}) {
  const activeFilterCount = (query ? 1 : 0) + (activeServer ? 1 : 0) + (priced ? 1 : 0);

  return (
    <div className="px-3 py-2 space-y-1.5 border-b border-border/30">
      <div className="flex items-center gap-2">
        <div className="relative flex-1 max-w-xs">
          <Search size={11} className="absolute left-2 top-1/2 -translate-y-1/2 text-text-muted/60" aria-hidden="true" />
          <input
            ref={searchInputRef}
            type="text"
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            placeholder="Filter tools ( / )"
            aria-label="Filter tools by name or server"
            className="w-full rounded-md border border-border/40 bg-background/60 pl-7 pr-6 py-1 text-[11px] text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50"
          />
          {query && (
            <button
              type="button"
              onClick={() => onQuery('')}
              aria-label="Clear search"
              className="absolute right-1.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-surface-highlight text-text-muted"
            >
              <X size={10} />
            </button>
          )}
        </div>
        <span role="status" className="text-[10px] font-mono tabular-nums text-text-muted whitespace-nowrap">
          {matchCount} of {totalCount} tools
        </span>
        {activeFilterCount >= 2 && (
          <button type="button" onClick={onClearAll} className="text-[10px] text-primary hover:underline whitespace-nowrap">
            Clear all
          </button>
        )}
      </div>

      <div className="flex items-center gap-1 flex-wrap">
        {servers.map((server) => {
          const active = activeServer === server;
          return (
            <button
              key={server}
              type="button"
              onClick={() => onServer(active ? null : server)}
              aria-pressed={active}
              className={cn(
                'px-2 py-0.5 rounded-full border text-[10px] font-mono transition-colors',
                active
                  ? 'border-primary/40 bg-primary/15 text-primary'
                  : 'border-border/40 text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40',
              )}
            >
              {server}
            </button>
          );
        })}
        <span aria-hidden="true" className="w-px h-3 bg-border/50 mx-1" />
        {(['yes', 'no'] as const).map((value) => {
          const active = priced === value;
          return (
            <button
              key={value}
              type="button"
              onClick={() => onPriced(active ? null : value)}
              aria-pressed={active}
              className={cn(
                'px-2 py-0.5 rounded-full border text-[10px] transition-colors',
                active
                  ? 'border-primary/40 bg-primary/15 text-primary'
                  : 'border-border/40 text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40',
              )}
            >
              {value === 'yes' ? 'Priced' : 'Unpriced'}
            </button>
          );
        })}
      </div>
    </div>
  );
}
