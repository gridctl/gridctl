import { useMemo, useState } from 'react';
import { Command } from 'cmdk';
import Fuse from 'fuse.js';
import { AlertCircle, ArrowLeft, Check, Edit3, Loader2, Plus, Radar, Search, X } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { useStackStore } from '../../../stores/useStackStore';
import { TOOL_NAME_DELIMITER } from '../../../lib/constants';
import { showToast } from '../../ui/Toast';
import { useProbeServer } from '../../../hooks/useProbeServer';
import { ProbeError, type ProbeServerConfig } from '../../../lib/api';

const inputClass =
  'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors';
const labelClass = 'block text-xs text-text-secondary mb-1.5';

interface ToolsPickerProps {
  value: string[];
  onChange: (val: string[]) => void;
  serverName: string;
  // Optional probe config. When provided and the transport is supported, a
  // "Discover tools" button appears in the empty state and calls the backend
  // probe endpoint. The picker treats an absent probeConfig as Phase 1
  // behavior — topology lookups + manual-entry fallback only.
  probeConfig?: ProbeServerConfig | null;
}

interface ToolOption {
  name: string;
  description?: string;
}

// Tri-state mode override: null = auto-select based on context,
// true = user forced manual, false = user forced search/checklist.
type ModeOverride = boolean | null;

export function ToolsPicker({ value, onChange, serverName, probeConfig }: ToolsPickerProps) {
  const tools = useStackStore((s) => s.tools);
  const [modeOverride, setModeOverride] = useState<ModeOverride>(null);
  const [query, setQuery] = useState('');
  const probeState = useProbeServer();

  const topologyTools: ToolOption[] = useMemo(() => {
    if (!serverName) return [];
    const prefix = `${serverName}${TOOL_NAME_DELIMITER}`;
    return (tools ?? [])
      .filter((t) => t.name.startsWith(prefix))
      .map((t) => ({
        name: t.name.slice(prefix.length),
        description: t.description,
      }));
  }, [tools, serverName]);

  // Discovered tools come from the ephemeral probe endpoint for servers that
  // are not yet in the topology. They are merged into the picker checklist
  // on equal footing with topology tools; topology takes precedence when both
  // exist because a running server is authoritative.
  const discoveredTools: ToolOption[] = useMemo(() => {
    if (!probeState.tools) return [];
    return probeState.tools.map((t) => ({ name: t.name, description: t.description }));
  }, [probeState.tools]);

  const hasTools = topologyTools.length > 0 || discoveredTools.length > 0;

  // Merge selected values that aren't in topology so existing stacks with
  // `tools: [...]` still render those entries pre-checked in the picker.
  const displayTools: ToolOption[] = useMemo(() => {
    const merged = new Map(topologyTools.map((t) => [t.name, t] as const));
    for (const d of discoveredTools) {
      if (!merged.has(d.name)) merged.set(d.name, d);
    }
    for (const name of value) {
      if (!merged.has(name)) merged.set(name, { name });
    }
    return [...merged.values()];
  }, [topologyTools, discoveredTools, value]);

  // When !hasTools AND there are already selected values (loaded from stack
  // YAML for a server not in the topology), default to manual mode so the
  // user can see and edit their entries.
  const autoManual = !hasTools && value.length > 0;
  const inManualMode = modeOverride ?? autoManual;
  const inEmptyState = !hasTools && !inManualMode;

  const fuse = useMemo(
    () => new Fuse(displayTools, { keys: ['name', 'description'], threshold: 0.4 }),
    [displayTools],
  );

  const visible = useMemo(() => {
    if (!query.trim()) return displayTools;
    return fuse.search(query).map((r) => r.item);
  }, [fuse, query, displayTools]);

  const selected = useMemo(() => new Set(value), [value]);

  const toggle = (name: string) => {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    onChange([...next]);
  };

  const selectAllVisible = () => {
    const next = new Set(selected);
    visible.forEach((o) => next.add(o.name));
    onChange([...next]);
  };

  const clearAll = () => onChange([]);

  const canProbe = !!probeConfig && isProbeSupported(probeConfig);
  const handleProbe = async () => {
    if (!probeConfig) return;
    const result = await probeState.probe(probeConfig);
    if (!result) {
      // probe() already stored the error in state; surface as a toast so the
      // failure is visible even if the error region is off-screen.
      const err = probeState.error;
      const msg = err instanceof Error ? err.message : 'Probe failed';
      showToast('error', msg);
    }
  };

  if (inEmptyState) {
    return (
      <div aria-label="Tools Picker" aria-busy={probeState.loading}>
        <label className={labelClass}>Tools Whitelist</label>
        <div className="rounded-lg border border-dashed border-border/40 bg-background/30 px-4 py-5 text-center space-y-3">
          <p className="text-[11px] text-text-muted leading-relaxed">
            No tools found for{' '}
            <span className="font-mono text-text-secondary">
              {serverName || 'this server'}
            </span>{' '}
            in the current topology.
            {canProbe ? (
              <>
                <br />
                Discover tools by probing the server, or enter names manually.
              </>
            ) : (
              <>
                <br />
                Enter names manually, or deploy the server first to discover its tools.
              </>
            )}
          </p>

          {canProbe && (
            <div className="flex flex-col items-center gap-1.5">
              <button
                type="button"
                onClick={handleProbe}
                disabled={probeState.loading}
                aria-label="Discover tools by probing the server"
                className={cn(
                  'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium transition-colors',
                  'bg-primary/20 text-primary border border-primary/30 hover:bg-primary/30',
                  'disabled:opacity-60 disabled:cursor-not-allowed',
                )}
              >
                {probeState.loading ? (
                  <>
                    <Loader2 size={11} className="animate-spin" />
                    Discovering…
                  </>
                ) : (
                  <>
                    <Radar size={11} />
                    Discover tools
                  </>
                )}
              </button>
              {probeState.probedAt && (
                <span className="text-[10px] text-text-muted">
                  Last discovered: {formatRelativeTime(probeState.probedAt)}
                </span>
              )}
            </div>
          )}

          {probeState.error && (
            <ProbeErrorPanel
              error={probeState.error}
              onRetry={canProbe ? handleProbe : undefined}
            />
          )}

          <button
            type="button"
            onClick={() => setModeOverride(true)}
            className="inline-flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
          >
            <Edit3 size={10} />
            Enter tool names manually
          </button>
        </div>
      </div>
    );
  }

  if (inManualMode) {
    return (
      <div aria-label="Tools Picker">
        <div className="flex items-center justify-between mb-1.5">
          <label className={cn(labelClass, 'mb-0')}>Tools Whitelist</label>
          {hasTools && (
            <button
              type="button"
              onClick={() => setModeOverride(false)}
              className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
            >
              <ArrowLeft size={10} />
              Back to search
            </button>
          )}
        </div>
        <ManualEntry value={value} onChange={onChange} />
      </div>
    );
  }

  // Checklist mode
  return (
    <div aria-label="Tools Picker">
      <div className="flex items-center justify-between mb-1.5">
        <label className={labelClass + ' mb-0'}>Tools Whitelist</label>
        <button
          type="button"
          onClick={() => setModeOverride(true)}
          className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <Edit3 size={10} />
          Enter tool names manually
        </button>
      </div>

      <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
        {/* Count + quick actions */}
        <div className="flex items-center gap-2 px-3 py-2 text-[10px] text-text-muted border-b border-border/30">
          <span>
            <span className="text-text-secondary font-medium">{selected.size}</span> of{' '}
            <span className="text-text-secondary font-medium">{displayTools.length}</span>{' '}
            selected — empty means all tools exposed
          </span>
          <div className="ml-auto flex items-center gap-2">
            <button
              type="button"
              onClick={selectAllVisible}
              aria-label="Select all visible tools"
              className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
            >
              Select all
            </button>
            <span className="text-border" aria-hidden="true">
              ·
            </span>
            <button
              type="button"
              onClick={clearAll}
              aria-label="Clear all selected tools"
              className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
            >
              Clear
            </button>
          </div>
        </div>

        <Command shouldFilter={false} label="Tools Picker" className="flex flex-col">
          <div className="flex items-center gap-2 px-3 py-2 border-b border-border/30">
            <Search size={12} className="text-text-muted flex-shrink-0" aria-hidden="true" />
            <Command.Input
              value={query}
              onValueChange={setQuery}
              placeholder="Search tools..."
              aria-label="Search tools"
              className="flex-1 bg-transparent outline-none text-xs text-text-primary placeholder:text-text-muted/60"
            />
          </div>

          <Command.List
            className="max-h-60 overflow-y-auto"
            aria-label="Available tools"
          >
            <Command.Empty>
              <p className="text-[11px] text-text-muted/60 italic py-4 px-3 text-center">
                No tools match "{query}"
              </p>
            </Command.Empty>
            {visible.map((opt) => {
              const isSelected = selected.has(opt.name);
              return (
                <Command.Item
                  key={opt.name}
                  value={opt.name}
                  onSelect={() => toggle(opt.name)}
                  aria-checked={isSelected}
                  aria-label={opt.name}
                  className={cn(
                    'flex items-start gap-2.5 px-3 py-2 cursor-pointer select-none outline-none transition-colors',
                    'hover:bg-surface-highlight/50',
                    '[&[data-selected=true]]:bg-primary/[0.06]',
                    isSelected && 'bg-primary/[0.03]',
                  )}
                >
                  <div
                    className={cn(
                      'mt-0.5 w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors',
                      isSelected
                        ? 'bg-primary/20 border-primary/60'
                        : 'border-border/60 bg-background/50',
                    )}
                    aria-hidden="true"
                  >
                    {isSelected && <Check size={10} className="text-primary" />}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div
                      className={cn(
                        'text-xs font-mono truncate',
                        isSelected ? 'text-text-primary' : 'text-text-secondary',
                      )}
                    >
                      {opt.name}
                    </div>
                    {opt.description && (
                      <div className="text-[10px] text-text-muted truncate">
                        {opt.description}
                      </div>
                    )}
                  </div>
                </Command.Item>
              );
            })}
          </Command.List>
        </Command>
      </div>
    </div>
  );
}

function ProbeErrorPanel({
  error,
  onRetry,
}: {
  error: ProbeError | Error;
  onRetry?: () => void;
}) {
  const isProbeErr = error instanceof ProbeError;
  const code = isProbeErr ? error.code : 'error';
  const showRetry = onRetry && code !== 'invalid_config';
  return (
    <div
      role="alert"
      className="flex items-start gap-2 rounded-md border border-status-error/40 bg-status-error/[0.05] px-3 py-2 text-left"
    >
      <AlertCircle size={12} className="text-status-error flex-shrink-0 mt-0.5" />
      <div className="flex-1 min-w-0 space-y-1">
        <p className="text-[11px] text-status-error font-medium">{error.message}</p>
        {isProbeErr && error.hint && (
          <p className="text-[10px] text-text-muted">{error.hint}</p>
        )}
        {showRetry && (
          <button
            type="button"
            onClick={onRetry}
            aria-label="Retry probing the server"
            className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
          >
            Retry
          </button>
        )}
      </div>
    </div>
  );
}

// isProbeSupported filters out configs the backend will refuse. The probe
// endpoint only handles external URL servers — every other transport is
// curated post-deploy from the topology sidebar, so the wizard hides the
// button rather than offering a click that will 422.
function isProbeSupported(cfg: ProbeServerConfig): boolean {
  if (cfg.ssh) return false;
  if (cfg.openapi) return false;
  if (cfg.image) return false;
  if (cfg.command && cfg.command.length > 0) return false;
  return !!cfg.url;
}

function formatRelativeTime(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const deltaSec = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (deltaSec < 5) return 'just now';
  if (deltaSec < 60) return `${deltaSec}s ago`;
  if (deltaSec < 3600) return `${Math.floor(deltaSec / 60)}m ago`;
  return `${Math.floor(deltaSec / 3600)}h ago`;
}

function ManualEntry({
  value,
  onChange,
}: {
  value: string[];
  onChange: (val: string[]) => void;
}) {
  const addItem = () => onChange([...value, '']);
  const updateItem = (idx: number, val: string) => {
    const next = [...value];
    next[idx] = val;
    onChange(next);
  };
  const removeItem = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx));
  };

  return (
    <div>
      <div className="flex justify-end mb-1.5">
        <button
          type="button"
          onClick={addItem}
          className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <Plus size={10} />
          Add tool
        </button>
      </div>
      {value.length === 0 && (
        <p className="text-[10px] text-text-muted/60 italic py-2">
          All tools exposed (no whitelist)
        </p>
      )}
      <div className="space-y-1.5">
        {value.map((item, i) => (
          <div key={i} className="flex items-center gap-1.5">
            <input
              type="text"
              value={item}
              onChange={(e) => updateItem(i, e.target.value)}
              placeholder="tool-name"
              aria-label={`Tool ${i + 1}`}
              className={cn(inputClass, 'flex-1 font-mono')}
            />
            <button
              type="button"
              onClick={() => removeItem(i)}
              aria-label={`Remove tool ${i + 1}`}
              className="p-1 text-text-muted hover:text-status-error transition-colors flex-shrink-0"
            >
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
