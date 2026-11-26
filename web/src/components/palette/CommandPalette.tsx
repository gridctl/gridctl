import { useEffect, useRef, useState, useCallback } from 'react';
import { Command } from 'cmdk';
import { Search, X, ArrowRight, Clock, Layers } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useCommandRegistry } from '../../hooks/useCommandRegistry';
import { useUIStore } from '../../stores/useUIStore';
import { showToast } from '../ui/Toast';
import { enterModal } from '../../lib/modalPresence';
import { WORKSPACE_PALETTE_SECTION } from '../../lib/paletteWorkspaceCommands';
import type { PaletteCommand, PaletteSection } from '../../types/palette';
import { ShortcutKeys } from '../ui/ShortcutKeys';

interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
}

// `r:` is the older Library prefix; it still works but is not advertised.
type ScopePrefix = 't:' | 'v:' | 'l:' | 'r:' | '>';

const SCOPE_PREFIXES: ScopePrefix[] = ['t:', 'v:', 'l:', 'r:', '>'];

const SCOPE_CONFIG: Record<ScopePrefix, { label: string; section: PaletteSection }> = {
  't:': { label: 'Traces', section: 'traces' },
  'v:': { label: 'Variables', section: 'vault' },
  'l:': { label: 'Library', section: 'registry' },
  'r:': { label: 'Library', section: 'registry' },
  '>': { label: 'Actions', section: 'global' },
};

// Why a scoped search over a store that has not loaded is empty.
const UNLOADED_SCOPE_MESSAGE: Partial<Record<PaletteSection, string>> = {
  vault: 'No variables loaded yet. Open the Variables workspace to load them.',
  traces: 'No traces loaded yet. Open the Traces workspace to load the buffer.',
};

const RESULT_WINDOW = 50;

const SECTION_DISPLAY: Record<PaletteSection, { label: string; className: string }> = {
  traces: { label: 'TRACES', className: 'text-secondary bg-secondary/5 border-secondary/20' },
  vault: { label: 'VARIABLES', className: 'text-primary bg-primary/5 border-primary/20' },
  registry: { label: 'LIBRARY', className: 'text-tertiary bg-tertiary/5 border-tertiary/20' },
  canvas: { label: 'CANVAS', className: 'text-text-muted bg-surface-highlight border-border/30' },
  logs: { label: 'LOGS', className: 'text-text-muted bg-surface-highlight border-border/30' },
  metrics: { label: 'METRICS', className: 'text-secondary bg-secondary/5 border-secondary/20' },
  pins: { label: 'PINS', className: 'text-status-pending bg-status-pending/5 border-status-pending/20' },
  tools: { label: 'TOOLS', className: 'text-tertiary bg-tertiary/5 border-tertiary/20' },
  connections: { label: 'CONNECTIONS', className: 'text-text-muted bg-surface-highlight border-border/30' },
  global: { label: 'GLOBAL', className: 'text-primary bg-primary/5 border-primary/20' },
};

function parsePrefixScope(input: string): {
  scope: PaletteSection | null;
  prefix: ScopePrefix | null;
  query: string;
} {
  for (const prefix of SCOPE_PREFIXES) {
    if (input.startsWith(prefix)) {
      return { scope: SCOPE_CONFIG[prefix].section, prefix, query: input.slice(prefix.length) };
    }
  }
  return { scope: null, prefix: null, query: input };
}

function ScopeHints({ className }: { className?: string }) {
  return (
    <span className={className}>
      <code className="text-primary">t:</code> traces ·{' '}
      <code className="text-secondary">v:</code> variables ·{' '}
      <code className="text-tertiary">l:</code> library ·{' '}
      <code className="text-text-secondary">{'>'}</code> actions
    </span>
  );
}

function SectionBadge({ section }: { section: PaletteSection }) {
  const display = SECTION_DISPLAY[section];
  return (
    <span
      className={cn(
        'shrink-0 text-3xs font-mono font-medium px-1.5 py-0.5 rounded border',
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
        'flex items-center gap-3 px-4 py-2.5 cursor-pointer select-none',
        'text-text-secondary transition-colors duration-75',
        'hover:bg-surface-highlight/70',
        '[&[data-selected=true]]:bg-primary/10 [&[data-selected=true]]:text-text-primary',
        'group-focus-within/cmd:data-[selected=true]:outline-2 group-focus-within/cmd:data-[selected=true]:-outline-offset-2 group-focus-within/cmd:data-[selected=true]:outline-primary',
        command.unavailable && 'opacity-40',
      )}
    >
      {command.icon && (
        <span className="shrink-0 text-text-muted [&[data-selected=true]>&]:text-primary transition-colors">
          {command.icon}
        </span>
      )}
      <span className="flex-1 text-sm truncate">{command.label}</span>
      {command.unavailable ? (
        <span className="shrink-0 text-3xs text-text-muted font-mono">unavailable</span>
      ) : (
        <>
          {showBadge && <SectionBadge section={command.section} />}
          {command.shortcut && <span className="ml-2"><ShortcutKeys keys={command.shortcut} /></span>}
        </>
      )}
    </Command.Item>
  );
}

function GroupHeading({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <div className="flex items-center gap-1.5 px-4 pt-3 pb-1.5">
      <span className="text-text-muted opacity-50">{icon}</span>
      <span className="text-3xs font-semibold text-text-muted tracking-widest uppercase opacity-50">
        {label}
      </span>
    </div>
  );
}

export function CommandPalette({ isOpen, onClose }: CommandPaletteProps) {
  const [inputValue, setInputValue] = useState('');
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const clearScopeRef = useRef<HTMLButtonElement>(null);

  const { getSortedCommands, getRecentCommands, recordUsage } = useCommandRegistry();
  const activeWorkspace = useUIStore((s) => s.activeWorkspace);

  // Clear the query when the palette closes (state adjustment during render,
  // so the reset commits with the close instead of one render later).
  const [wasOpen, setWasOpen] = useState(isOpen);
  if (wasOpen !== isOpen) {
    setWasOpen(isOpen);
    if (!isOpen) setInputValue('');
  }

  // The palette traps Tab itself, so it registers as an open modal here.
  useEffect(() => {
    if (!isOpen) return;
    return enterModal();
  }, [isOpen]);

  // Capture focused element before opening
  useEffect(() => {
    if (isOpen) {
      previousFocusRef.current =
        document.activeElement instanceof HTMLElement ? document.activeElement : null;
    }
  }, [isOpen]);

  const handleClose = useCallback((restoreFocus = true) => {
    const toRestore = previousFocusRef.current;
    onClose();
    if (!restoreFocus) return;
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

  const handleSelect = useCallback(
    (command: PaletteCommand) => {
      if (command.unavailable) {
        showToast('error', `${command.label} is currently unavailable`);
        handleClose();
        return;
      }
      recordUsage(command.id);
      command.onSelect();
      // A command that opened the Stack inspector hands focus to its heading;
      // restoring the prior element afterwards would pull focus back out.
      handleClose(useUIStore.getState().inspectorFocusRequest === null);
    },
    [recordUsage, handleClose],
  );

  const clearScope = useCallback(() => {
    setInputValue('');
    inputRef.current?.focus();
  }, []);

  if (!isOpen) return null;

  const { scope, prefix, query } = parsePrefixScope(inputValue);
  const isActiveSearch = query.trim().length > 0 || scope !== null;
  const currentSection = WORKSPACE_PALETTE_SECTION[activeWorkspace];

  const recentCommands = isActiveSearch ? [] : getRecentCommands(5, activeWorkspace);
  const contextCommands = isActiveSearch
    ? []
    : getSortedCommands(undefined, currentSection, activeWorkspace).slice(0, 6);
  const navCommands = isActiveSearch
    ? []
    : getSortedCommands(undefined, undefined, activeWorkspace).filter((c) =>
        c.id.startsWith('navigate:'),
      );
  const searchResults = isActiveSearch
    ? getSortedCommands(query.trim() || undefined, scope ?? undefined, activeWorkspace)
    : [];
  const visibleResults = searchResults.slice(0, RESULT_WINDOW);
  const unloadedMessage =
    scope && searchResults.length === 0 && getSortedCommands(undefined, scope).length === 0
      ? UNLOADED_SCOPE_MESSAGE[scope]
      : undefined;

  const resultCount = isActiveSearch ? searchResults.length : null;

  // Focus stays in the dialog: Tab moves between the input and the scope
  // chip's clear button, or stays on the input when there is no chip.
  const handleTab = (e: React.KeyboardEvent) => {
    if (e.key !== 'Tab') return;
    e.preventDefault();
    const clear = clearScopeRef.current;
    if (clear && document.activeElement !== clear) clear.focus();
    else inputRef.current?.focus();
  };

  return (
    <div
      className="fixed inset-0 z-modal flex items-start justify-center bg-background/80 backdrop-blur-sm"
      style={{ paddingTop: '13vh' }}
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
        onKeyDown={handleTab}
      >
        <Command shouldFilter={false} className="group/cmd flex flex-col overflow-hidden flex-1 min-h-0">
          {/* Input row */}
          <div className="flex items-center gap-2.5 px-4 py-3 border-b border-border/30 shrink-0">
            <Search size={14} className="text-text-muted shrink-0" />

            {/* Scope chip */}
            {scope && prefix && (
              <div className="flex items-center gap-1 shrink-0 pl-2 pr-1.5 py-0.5 rounded-full bg-primary/10 border border-primary/25">
                <span className="text-2xs font-medium text-primary leading-none">
                  {SCOPE_CONFIG[prefix].label}
                </span>
                <button
                  ref={clearScopeRef}
                  type="button"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={clearScope}
                  onKeyDown={(e) => {
                    // cmdk's root runs the selected command on Enter; keep
                    // Enter and Space on this button.
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      e.stopPropagation();
                      clearScope();
                    }
                  }}
                  className={cn(
                    'inline-flex items-center justify-center w-6 h-6 -my-1 -mr-1 rounded-full',
                    'text-primary hover:text-text-primary transition-colors',
                    'focus-visible:outline-2 focus-visible:outline-offset-0 focus-visible:outline-primary',
                  )}
                  aria-label="Clear scope filter"
                >
                  <X size={10} />
                </button>
              </div>
            )}

            <Command.Input
              ref={inputRef}
              value={inputValue}
              onValueChange={setInputValue}
              onKeyDown={(e) => {
                // One Backspace removes a bare scope prefix.
                if (e.key === 'Backspace' && (SCOPE_PREFIXES as string[]).includes(inputValue)) {
                  e.preventDefault();
                  setInputValue('');
                }
              }}
              placeholder={
                scope && prefix
                  ? `Search ${SCOPE_CONFIG[prefix].label}...`
                  : 'Search commands...'
              }
              autoFocus
              className={cn(
                'flex-1 min-w-0 bg-transparent text-sm text-text-primary rounded',
                'placeholder:text-text-muted caret-primary',
              )}
            />

            <kbd className="shrink-0 px-1.5 py-0.5 rounded text-3xs font-mono text-text-muted bg-surface border border-border/50">
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
                  <Command.Group heading={<GroupHeading icon={<Clock size={11} />} label="Recent" />}>
                    {recentCommands.map((cmd) => (
                      <PaletteItem key={cmd.id} command={cmd} onSelect={handleSelect} showBadge />
                    ))}
                  </Command.Group>
                )}

                {contextCommands.length > 0 && (
                  <Command.Group
                    heading={<GroupHeading icon={<Layers size={11} />} label="Current Context" />}
                  >
                    {contextCommands.map((cmd) => (
                      <PaletteItem key={cmd.id} command={cmd} onSelect={handleSelect} />
                    ))}
                  </Command.Group>
                )}

                {navCommands.length > 0 && (
                  <Command.Group
                    heading={<GroupHeading icon={<ArrowRight size={11} />} label="Navigate To" />}
                  >
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
                      <ScopeHints className="text-xs text-text-muted" />
                    </div>
                  )}
              </>
            ) : (
              /* Active search: flat list with badges */
              <>
                <Command.Empty>
                  <div className="flex flex-col items-center gap-2 py-10 text-center">
                    <p className="text-sm text-text-secondary">
                      {unloadedMessage ?? (
                        <>
                          No results for{' '}
                          <span className="text-text-primary font-medium">
                            "{query || (prefix ? SCOPE_CONFIG[prefix].label : '')}"
                          </span>
                        </>
                      )}
                    </p>
                    <ScopeHints className="text-xs text-text-muted leading-relaxed" />
                  </div>
                </Command.Empty>
                {visibleResults.map((cmd) => (
                  <PaletteItem key={cmd.id} command={cmd} onSelect={handleSelect} showBadge />
                ))}
                {searchResults.length > visibleResults.length && (
                  <div className="px-4 py-2 text-xs text-text-muted">
                    Showing {visibleResults.length} of {searchResults.length}. Keep typing to narrow.
                  </div>
                )}
              </>
            )}
          </Command.List>

          {/* Footer hints */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 border-t border-border/20 shrink-0">
            <span className="flex items-center gap-1 text-3xs text-text-muted">
              <kbd className="px-1 py-0.5 rounded bg-surface border border-border/50 font-mono text-3xs">
                ↑↓
              </kbd>
              navigate
            </span>
            <span className="flex items-center gap-1 text-3xs text-text-muted">
              <kbd className="px-1 py-0.5 rounded bg-surface border border-border/50 font-mono text-3xs">
                ↵
              </kbd>
              select
            </span>
            <ScopeHints className="ml-auto text-3xs text-text-muted" />
          </div>
        </Command>
      </div>
    </div>
  );
}
