import { useState, useEffect, useRef } from 'react';
import {
  Search,
  Package,
  Globe,
  KeyRound,
  ExternalLink,
  AlertTriangle,
  ChevronRight,
  Container,
  TerminalSquare,
  Link2,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { fetchCatalog, type CatalogEntry } from '../../lib/api';
import { catalogInstallLabel, catalogServerName } from '../../lib/catalog';

interface CatalogPickerProps {
  onSelect: (entry: CatalogEntry) => void;
}

const INSTALL_ICONS: Record<string, LucideIcon> = {
  image: Container,
  command: TerminalSquare,
  url: Link2,
};

/** Source-tier badge: curated entries are vetted by hand, registry entries
 * are community publications and must not read as vetted. */
function TierBadge({ entry }: { entry: CatalogEntry }) {
  const curated = entry.tier === 'curated';
  return (
    <span
      className={cn(
        'px-1.5 py-0.5 rounded text-[9px] uppercase tracking-wider font-medium',
        curated
          ? 'bg-primary/15 text-primary'
          : 'bg-surface-highlight text-text-muted',
      )}
    >
      {curated ? 'curated' : 'registry'}
    </span>
  );
}

/**
 * Catalog picker — searches the server catalog (embedded curated set plus
 * the MCP Registry) and hands the chosen entry to the wizard, which
 * pre-fills the mcp-server form from it. Shell cloned from RecipePicker.
 */
export function CatalogPicker({ onSelect }: CatalogPickerProps) {
  const [query, setQuery] = useState('');
  const [entries, setEntries] = useState<CatalogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [stale, setStale] = useState(false);
  const [registryError, setRegistryError] = useState('');
  const [failed, setFailed] = useState(false);
  const [selectedName, setSelectedName] = useState<string | null>(null);
  // Generation counter so a slow earlier response never overwrites the
  // results of a newer query.
  const generation = useRef(0);

  useEffect(() => {
    const gen = ++generation.current;
    const timer = setTimeout(() => {
      setLoading(true);
      fetchCatalog(query)
        .then((resp) => {
          if (generation.current !== gen) return;
          setEntries(resp.servers);
          setStale(resp.stale ?? false);
          setRegistryError(resp.registry_error ?? '');
          setFailed(false);
          setLoading(false);
        })
        .catch(() => {
          if (generation.current !== gen) return;
          setEntries([]);
          setFailed(true);
          setLoading(false);
        });
    }, query ? 300 : 0);
    return () => clearTimeout(timer);
  }, [query]);

  const selected = entries.find((e) => e.name === selectedName);

  return (
    <div className="flex flex-col h-full">
      {/* Search */}
      <div className="flex items-center gap-2 px-4 py-3 border-b border-border/20">
        <Search size={14} className="text-text-muted flex-shrink-0" />
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search servers, e.g. postgres..."
          autoFocus
          className={cn(
            'flex-1 bg-transparent text-xs text-text-primary',
            'focus:outline-none placeholder:text-text-muted/50',
          )}
        />
        <span className="text-[10px] text-text-muted">
          {loading ? 'Searching...' : `${entries.length} servers`}
        </span>
      </div>

      {/* Degraded-registry notice */}
      {(stale || registryError) && (
        <div className="flex items-center gap-2 px-4 py-1.5 border-b border-border/10 bg-status-pending/5">
          <AlertTriangle size={10} className="text-status-pending flex-shrink-0" />
          <span className="text-[10px] text-text-muted">
            {registryError
              ? 'MCP Registry unavailable; showing curated results only'
              : 'MCP Registry unavailable; showing cached results (may be stale)'}
          </span>
        </div>
      )}

      <div className="flex flex-1 min-h-0">
        {/* Entry list */}
        <div className="w-1/2 border-r border-border/10 overflow-y-auto scrollbar-dark">
          {failed ? (
            <div className="flex items-center justify-center py-16">
              <span className="text-[10px] text-text-muted">Failed to load the catalog</span>
            </div>
          ) : entries.length === 0 && !loading ? (
            <div className="flex items-center justify-center py-16">
              <span className="text-[10px] text-text-muted">
                No servers matched &ldquo;{query}&rdquo;
              </span>
            </div>
          ) : (
            entries.map((entry) => {
              const isSelected = selectedName === entry.name;
              const Icon = entry.tier === 'curated' ? Package : Globe;
              return (
                <button
                  key={entry.name}
                  onClick={() => setSelectedName(entry.name)}
                  className={cn(
                    'w-full text-left px-4 py-3 border-b border-border/10 transition-all',
                    isSelected ? 'bg-primary/5 border-l-2 border-l-primary' : 'hover:bg-white/[0.02]',
                  )}
                >
                  <div className="flex items-center gap-2">
                    <Icon size={12} className={entry.tier === 'curated' ? 'text-primary' : 'text-text-muted'} />
                    <span className="text-xs font-medium text-text-primary truncate">
                      {entry.title || entry.name}
                    </span>
                    <ChevronRight size={10} className="text-text-muted ml-auto flex-shrink-0" />
                  </div>
                  <div className="flex items-center gap-1.5 mt-1.5">
                    <TierBadge entry={entry} />
                    {entry.status === 'deprecated' && (
                      <span className="px-1.5 py-0.5 rounded text-[9px] uppercase tracking-wider font-medium bg-status-pending/15 text-status-pending">
                        deprecated
                      </span>
                    )}
                    {entry.unsupported && (
                      <span className="px-1.5 py-0.5 rounded text-[9px] uppercase tracking-wider font-medium bg-surface-highlight text-text-muted">
                        unsupported
                      </span>
                    )}
                  </div>
                  <p className="text-[10px] text-text-muted mt-1 leading-relaxed line-clamp-2">
                    {entry.description}
                  </p>
                </button>
              );
            })
          )}
        </div>

        {/* Preview pane */}
        <div className="w-1/2 flex flex-col">
          {selected ? (
            <>
              <div className="flex-1 overflow-y-auto scrollbar-dark p-4 space-y-4">
                <div>
                  <div className="flex items-center gap-2 mb-1">
                    <span className="text-xs font-medium text-text-primary">
                      {selected.title || selected.name}
                    </span>
                    <TierBadge entry={selected} />
                  </div>
                  <p className="text-[10px] text-text-muted leading-relaxed">
                    {selected.description}
                  </p>
                </div>

                {/* Install summary */}
                <div className="rounded-lg bg-background/40 border border-white/[0.04] px-3 py-2.5 space-y-1.5">
                  <div className="flex items-center gap-2">
                    {(() => {
                      const Icon = INSTALL_ICONS[selected.install.type] ?? Container;
                      return <Icon size={11} className="text-text-secondary" />;
                    })()}
                    <span className="text-[10px] text-text-secondary">{catalogInstallLabel(selected)}</span>
                  </div>
                  <div className="text-[10px] font-mono text-text-muted break-all">
                    {selected.install.image || selected.install.url || selected.install.command?.join(' ')}
                  </div>
                  <div className="text-[10px] text-text-muted">
                    Adds server <span className="font-mono text-text-secondary">{catalogServerName(selected)}</span>
                  </div>
                </div>

                {/* Inputs */}
                {(selected.inputs?.length ?? 0) > 0 && (
                  <div>
                    <div className="text-[10px] text-text-muted uppercase tracking-wider font-medium mb-1.5">
                      Configuration
                    </div>
                    <div className="space-y-1">
                      {selected.inputs?.map((input) => (
                        <div key={input.name} className="flex items-center gap-2 text-[10px]">
                          {input.secret || input.auth ? (
                            <KeyRound size={10} className="text-tertiary flex-shrink-0" />
                          ) : (
                            <span className="w-2.5 flex-shrink-0" />
                          )}
                          <span className="font-mono text-text-secondary">{input.name}</span>
                          {input.required && <span className="text-status-pending">*</span>}
                          <span className="text-text-muted truncate">{input.description}</span>
                        </div>
                      ))}
                    </div>
                    {selected.inputs?.some((i) => i.secret || i.auth) && (
                      <p className="text-[10px] text-text-muted mt-2 leading-relaxed">
                        Secrets pre-fill as <span className="font-mono text-tertiary">{'${var:KEY}'}</span>{' '}
                        references; store the values in the vault.
                      </p>
                    )}
                  </div>
                )}

                {/* Registry trust note */}
                {selected.tier === 'registry' && (
                  <p className="text-[10px] text-text-muted leading-relaxed">
                    Registry entries are community publications, not vetted by gridctl.
                  </p>
                )}

                {selected.homepage && (
                  <a
                    href={selected.homepage}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-[10px] text-text-muted hover:text-text-primary transition-colors"
                  >
                    <ExternalLink size={10} />
                    {selected.homepage}
                  </a>
                )}
              </div>

              <div className="px-4 py-3 border-t border-border/20">
                {selected.unsupported ? (
                  <p className="text-[10px] text-text-muted text-center">
                    Unsupported package type ({selected.unsupported}); supported: container images,
                    npm and pypi packages, and remote URLs.
                  </p>
                ) : (
                  <button
                    onClick={() => onSelect(selected)}
                    className={cn(
                      'w-full px-3 py-1.5 rounded-lg text-xs font-medium',
                      'bg-primary/20 text-primary hover:bg-primary/30 border border-primary/30',
                      'transition-all duration-200',
                    )}
                  >
                    Use this server
                  </button>
                )}
              </div>
            </>
          ) : (
            <div className="flex items-center justify-center h-full">
              <div className="text-center">
                <Package size={24} className="text-text-muted/30 mx-auto mb-2" />
                <p className="text-[10px] text-text-muted">Select a server to preview</p>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
