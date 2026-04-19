import { useMemo, useState } from 'react';
import { Command } from 'cmdk';
import Fuse from 'fuse.js';
import { ArrowLeft, Check, Edit3, Plus, Search, X } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { useStackStore } from '../../../stores/useStackStore';
import { TOOL_NAME_DELIMITER } from '../../../lib/constants';

const inputClass =
  'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors';
const labelClass = 'block text-xs text-text-secondary mb-1.5';

interface ToolsPickerProps {
  value: string[];
  onChange: (val: string[]) => void;
  serverName: string;
}

interface ToolOption {
  name: string;
  description?: string;
}

// Tri-state mode override: null = auto-select based on context,
// true = user forced manual, false = user forced search/checklist.
type ModeOverride = boolean | null;

export function ToolsPicker({ value, onChange, serverName }: ToolsPickerProps) {
  const tools = useStackStore((s) => s.tools);
  const [modeOverride, setModeOverride] = useState<ModeOverride>(null);
  const [query, setQuery] = useState('');

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

  const hasTools = topologyTools.length > 0;

  // Merge selected values that aren't in topology so existing stacks with
  // `tools: [...]` still render those entries pre-checked in the picker.
  const displayTools: ToolOption[] = useMemo(() => {
    const merged = new Map(topologyTools.map((t) => [t.name, t] as const));
    for (const name of value) {
      if (!merged.has(name)) merged.set(name, { name });
    }
    return [...merged.values()];
  }, [topologyTools, value]);

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

  if (inEmptyState) {
    return (
      <div aria-label="Tools Picker">
        <label className={labelClass}>Tools Whitelist</label>
        <div className="rounded-lg border border-dashed border-border/40 bg-background/30 px-4 py-5 text-center space-y-2.5">
          <p className="text-[11px] text-text-muted leading-relaxed">
            No tools found for{' '}
            <span className="font-mono text-text-secondary">
              {serverName || 'this server'}
            </span>{' '}
            in the current topology.
            <br />
            Enter names manually, or deploy the server first to discover its tools.
          </p>
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
