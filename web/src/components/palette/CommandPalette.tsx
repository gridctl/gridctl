import { useEffect, useRef, useState, useCallback } from 'react';
import { Command } from 'cmdk';
import { Search, X, ArrowRight, Clock, Layers } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useCommandRegistry } from '../../hooks/useCommandRegistry';
import { useUIStore } from '../../stores/useUIStore';
import type { PaletteCommand, PaletteSection } from '../../types/palette';

interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
}

type ScopePrefix = 't:' | 'v:' | 'r:' | '>';

const SCOPE_CONFIG: Record<ScopePrefix, { label: string; section: PaletteSection }> = {
  't:': { label: 'Traces', section: 'traces' },
  'v:': { label: 'Vault', section: 'vault' },
  'r:': { label: 'Registry', section: 'registry' },
  '>': { label: 'Actions', section: 'global' },
};

const SECTION_DISPLAY: Record<PaletteSection, { label: string; className: string }> = {
  traces: { label: 'TRACES', className: 'text-secondary bg-secondary/5 border-secondary/20' },
  vault: { label: 'VAULT', className: 'text-primary bg-primary/5 border-primary/20' },
  registry: { label: 'REGISTRY', className: 'text-tertiary bg-tertiary/5 border-tertiary/20' },
  canvas: { label: 'CANVAS', className: 'text-text-muted bg-surface-highlight border-border/30' },
  logs: { label: 'LOGS', className: 'text-text-muted bg-surface-highlight border-border/30' },
  metrics: { label: 'METRICS', className: 'text-secondary bg-secondary/5 border-secondary/20' },
  global: { label: 'GLOBAL', className: 'text-primary bg-primary/5 border-primary/20' },
};

function parsePrefixScope(input: string): {
  scope: PaletteSection | null;
  prefix: ScopePrefix | null;
  query: string;
} {
  const prefixes: ScopePrefix[] = ['t:', 'v:', 'r:', '>'];
  for (const prefix of prefixes) {
    if (input.startsWith(prefix)) {
      return { scope: SCOPE_CONFIG[prefix].section, prefix, query: input.slice(prefix.length) };
    }
  }
  return { scope: null, prefix: null, query: input };
}

function ShortcutKeys({ keys }: { keys: string[] }) {
  return (
    <span className="flex items-center gap-0.5 shrink-0 ml-2">
      {keys.map((key, i) => (
        <kbd
          key={i}
          className="inline-flex items-center justify-center px-1.5 py-0.5 rounded text-[10px] font-mono text-text-muted bg-surface border border-border/50 min-w-[18px]"
        >
          {key}
        </kbd>
      ))}
    </span>
  );
}

function SectionBadge({ section }: { section: PaletteSection }) {
  const display = SECTION_DISPLAY[section];
  return (
    <span
      className={cn(
        'shrink-0 text-[10px] font-mono font-medium px-1.5 py-0.5 rounded border',
        display.className,
      )}
    >
      {display.label}
    </span>
  );
}

interface PaletteItemProps {
  command: PaletteCommand;
  onSelect: (cmd: PaletteCommand) => void;
  showBadge?: boolean;
}

function PaletteItem({ command, onSelect, showBadge }: PaletteItemProps) {
  return (
    <Command.Item
      value={command.id}
      keywords={command.keywords}
      onSelect={() => onSelect(command)}
      className={cn(
        'flex items-center gap-3 px-4 py-2.5 cursor-pointer select-none outline-none',
        'text-text-secondary transition-colors duration-75',
        'hover:bg-surface-highlight/70',
        '[&[data-selected=true]]:bg-primary/10 [&[data-selected=true]]:text-text-primary',
      )}
    >
      {command.icon && (
        <span className="shrink-0 text-text-muted [&[data-selected=true]>&]:text-primary transition-colors">
          {command.icon}
        </span>
      )}
      <span className="flex-1 text-sm truncate">{command.label}</span>
      {showBadge && <SectionBadge section={command.section} />}
      {command.shortcut && <ShortcutKeys keys={command.shortcut} />}
    </Command.Item>
  );
}

function GroupHeading({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <div className="flex items-center gap-1.5 px-4 pt-3 pb-1.5" aria-hidden="true">
      <span className="text-text-muted opacity-50">{icon}</span>
      <span className="text-[10px] font-semibold text-text-muted tracking-widest uppercase opacity-50">
        {label}
      </span>
    </div>
  );
}

export function CommandPalette({ isOpen, onClose }: CommandPaletteProps) {
  const [inputValue, setInputValue] = useState('');
  const previousFocusRef = useRef<HTMLElement | null>(null);

  const { getSortedCommands, getRecentCommands, recordUsage } = useCommandRegistry();
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const bottomPanelTab = useUIStore((s) => s.bottomPanelTab);

  // Capture focused element before opening
  useEffect(() => {
    if (isOpen) {
      previousFocusRef.current =
        document.activeElement instanceof HTMLElement ? document.activeElement : null;
    } else {
      setInputValue('');
    }
  }, [isOpen]);

  const handleClose = useCallback(() => {
    const toRestore = previousFocusRef.current;
    onClose();
    requestAnimationFrame(() => {
      toRestore?.focus();
    });
  }, [onClose]);

  // Escape at capture phase — higher priority than other handlers
  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        handleClose();
      }
    };
    window.addEventListener('keydown', handler, true);
    return () => window.removeEventListener('keydown', handler, true);
  }, [isOpen, handleClose]);

  const { scope, prefix, query } = parsePrefixScope(inputValue);
  const isActiveSearch = query.trim().length > 0 || scope !== null;

  // Determine current section context from panel state
  const currentSection: PaletteSection = bottomPanelOpen
    ? bottomPanelTab === 'traces'
      ? 'traces'
      : bottomPanelTab === 'metrics'
        ? 'metrics'
        : 'logs'
    : 'canvas';

  const recentCommands = getRecentCommands(5);
  const contextCommands = getSortedCommands(undefined, currentSection).slice(0, 6);
  const navCommands = getSortedCommands(undefined, undefined).filter(
    (c) => c.id.startsWith('navigate:'),
  );
  const searchResults = isActiveSearch
    ? getSortedCommands(query.trim() || undefined, scope ?? undefined)
    : [];

  const resultCount = isActiveSearch ? searchResults.length : null;

  const handleSelect = useCallback(
    (command: PaletteCommand) => {
      recordUsage(command.id);
      command.onSelect();
      handleClose();
    },
    [recordUsage, handleClose],
  );

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 z-[60] flex items-start justify-center"
      style={{ paddingTop: '13vh', backgroundColor: 'rgba(8,8,10,0.75)', backdropFilter: 'blur(6px)' }}
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      onClick={(e) => {
        if (e.target === e.currentTarget) handleClose();
      }}
    >
      {/* aria-live region for screen readers */}
      <div aria-live="polite" aria-atomic="true" className="sr-only">
        {resultCount !== null &&
          (resultCount === 0
            ? 'No results'
            : `${resultCount} result${resultCount === 1 ? '' : 's'}`)}
      </div>

      {/* Panel */}
      <div
        className="glass-panel-elevated animate-fade-in-scale w-full max-w-xl mx-4 flex flex-col overflow-hidden"
        style={{ maxHeight: '64vh' }}
        onKeyDown={(e) => {
          if (e.key === 'Tab') e.preventDefault(); // focus trap
        }}
      >
        <Command shouldFilter={false} className="flex flex-col overflow-hidden flex-1 min-h-0">
          {/* Input row */}
          <div className="flex items-center gap-2.5 px-4 py-3 border-b border-border/30 shrink-0">
            <Search size={14} className="text-text-muted shrink-0" />

            {/* Scope chip */}
            {scope && prefix && (
              <div className="flex items-center gap-1 shrink-0 pl-2 pr-1.5 py-0.5 rounded-full bg-primary/10 border border-primary/25">
                <span className="text-[11px] font-medium text-primary leading-none">
                  {SCOPE_CONFIG[prefix].label}
                </span>
                <button
                  onMouseDown={(e) => {
                    e.preventDefault();
                    setInputValue('');
                  }}
                  className="text-primary/50 hover:text-primary transition-colors ml-0.5"
                  aria-label="Clear scope filter"
                >
                  <X size={10} />
                </button>
              </div>
            )}

            <Command.Input
              value={inputValue}
              onValueChange={setInputValue}
              placeholder={
                scope && prefix
                  ? `Search ${SCOPE_CONFIG[prefix].label}...`
                  : 'Search commands...'
              }
              autoFocus
              className={cn(
                'flex-1 min-w-0 bg-transparent outline-none text-sm text-text-primary',
                'placeholder:text-text-muted caret-primary',
              )}
            />

            <kbd className="shrink-0 px-1.5 py-0.5 rounded text-[10px] font-mono text-text-muted bg-surface border border-border/50">
              esc
            </kbd>
          </div>

          {/* Results list */}
          <Command.List
            className="overflow-y-auto scrollbar-dark flex-1 min-h-0 py-1"
            aria-label="Command results"
          >
            {!isActiveSearch ? (
              /* Default state: grouped */
              <>
                {recentCommands.length > 0 && (
                  <Command.Group heading="Recent" aria-label="Recent commands">
                    <GroupHeading icon={<Clock size={11} />} label="Recent" />
                    {recentCommands.map((cmd) => (
                      <PaletteItem key={cmd.id} command={cmd} onSelect={handleSelect} showBadge />
                    ))}
                  </Command.Group>
                )}

                {contextCommands.length > 0 && (
                  <Command.Group heading="Current Context" aria-label="Current context commands">
                    <GroupHeading icon={<Layers size={11} />} label="Current Context" />
                    {contextCommands.map((cmd) => (
                      <PaletteItem key={cmd.id} command={cmd} onSelect={handleSelect} />
                    ))}
                  </Command.Group>
                )}

                {navCommands.length > 0 && (
                  <Command.Group heading="Navigate To" aria-label="Navigation commands">
                    <GroupHeading icon={<ArrowRight size={11} />} label="Navigate To" />
                    {navCommands.map((cmd) => (
                      <PaletteItem key={cmd.id} command={cmd} onSelect={handleSelect} />
                    ))}
                  </Command.Group>
                )}

                {recentCommands.length === 0 &&
                  contextCommands.length === 0 &&
                  navCommands.length === 0 && (
                    <div className="flex flex-col items-center gap-2 py-10 text-center">
                      <p className="text-sm text-text-muted">Start typing to search commands</p>
                      <p className="text-xs text-text-muted opacity-60">
                        <code className="text-primary/70">t:</code> traces ·{' '}
                        <code className="text-secondary/70">v:</code> vault ·{' '}
                        <code className="text-tertiary/70">r:</code> registry ·{' '}
                        <code className="text-text-muted/70">{'>'}</code> actions
                      </p>
                    </div>
                  )}
              </>
            ) : (
              /* Active search: flat list with badges */
              <>
                <Command.Empty>
                  <div className="flex flex-col items-center gap-2 py-10 text-center">
                    <p className="text-sm text-text-secondary">
                      No results for{' '}
                      <span className="text-text-primary font-medium">
                        "{query || (prefix ? SCOPE_CONFIG[prefix].label : '')}"
                      </span>
                    </p>
                    <p className="text-xs text-text-muted opacity-70">
                      Try <code className="text-primary/80">{'>'}</code> for actions ·{' '}
                      <code className="text-secondary/80">v:</code> for Vault secrets
                    </p>
                  </div>
                </Command.Empty>
                {searchResults.map((cmd) => (
                  <PaletteItem key={cmd.id} command={cmd} onSelect={handleSelect} showBadge />
                ))}
              </>
            )}
          </Command.List>

          {/* Footer hints */}
          <div className="flex items-center gap-3 px-4 py-2 border-t border-border/20 shrink-0">
            <span className="flex items-center gap-1 text-[10px] text-text-muted">
              <kbd className="px-1 py-0.5 rounded bg-surface border border-border/50 font-mono text-[9px]">
                ↑↓
              </kbd>
              navigate
            </span>
            <span className="flex items-center gap-1 text-[10px] text-text-muted">
              <kbd className="px-1 py-0.5 rounded bg-surface border border-border/50 font-mono text-[9px]">
                ↵
              </kbd>
              select
            </span>
            <span className="ml-auto text-[10px] text-text-muted opacity-50">
              <code className="text-primary/80">t:</code>{' '}
              <code className="text-secondary/80">v:</code>{' '}
              <code className="text-tertiary/80">r:</code>{' '}
              <code>{'>'}</code>
            </span>
          </div>
        </Command>
      </div>
    </div>
  );
}
