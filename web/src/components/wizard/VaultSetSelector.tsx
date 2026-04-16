import { useState, useEffect, useRef, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { Search, Plus, X, Loader2, Check } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useVaultStore } from '../../stores/useVaultStore';
import { fetchVaultSets } from '../../lib/api';

interface VaultSetSelectorProps {
  value: string[];
  onChange: (val: string[]) => void;
}

interface PopoverPosition {
  top?: number;
  bottom?: number;
  left: number;
}

const POPOVER_ESTIMATED_HEIGHT = 220;

export function VaultSetSelector({ value, onChange }: VaultSetSelectorProps) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState('');
  const [position, setPosition] = useState<PopoverPosition | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const sets = useVaultStore((s) => s.sets);
  const setSets = useVaultStore((s) => s.setSets);

  // Fetch sets when popover opens
  useEffect(() => {
    if (!open) return;
    fetchVaultSets()
      .then((s) => setSets(s))
      .catch(() => {});
  }, [open, setSets]);

  // Compute fixed position from trigger button rect
  const updatePosition = useCallback(() => {
    if (!triggerRef.current) return;
    const rect = triggerRef.current.getBoundingClientRect();
    const left = rect.left;
    const spaceBelow = window.innerHeight - rect.bottom;
    if (spaceBelow < POPOVER_ESTIMATED_HEIGHT + 8) {
      setPosition({ bottom: window.innerHeight - rect.top + 6, left });
    } else {
      setPosition({ top: rect.bottom + 6, left });
    }
  }, []);

  // Reposition on scroll or resize while open
  useEffect(() => {
    if (!open) {
      setPosition(null);
      return;
    }
    updatePosition();
    window.addEventListener('scroll', updatePosition, true);
    window.addEventListener('resize', updatePosition);
    return () => {
      window.removeEventListener('scroll', updatePosition, true);
      window.removeEventListener('resize', updatePosition);
    };
  }, [open, updatePosition]);

  // Close on outside click — checks both trigger and portal dropdown
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        !triggerRef.current?.contains(target) &&
        !dropdownRef.current?.contains(target)
      ) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open]);

  const handleSelect = useCallback(
    (name: string) => {
      onChange([...value, name]);
      setOpen(false);
      setFilter('');
    },
    [value, onChange],
  );

  const handleRemove = (name: string) => {
    onChange(value.filter((s) => s !== name));
  };

  // Exclude already-selected sets and apply filter
  const available = (sets ?? []).filter(
    (s) =>
      !value.includes(s.name) &&
      s.name.toLowerCase().includes(filter.toLowerCase()),
  );

  const allSelected =
    sets !== null && sets.length > 0 && sets.every((s) => value.includes(s.name));

  const emptyMessage =
    sets === null ? null : allSelected ? 'All sets selected' : 'No sets found';

  const dropdown = position && (
    <div
      ref={dropdownRef}
      style={{
        position: 'fixed',
        top: position.top,
        bottom: position.bottom,
        left: position.left,
        width: '16rem',
        zIndex: 9999,
      }}
      className={cn('glass-panel-elevated rounded-xl', 'animate-fade-in-scale')}
    >
      {/* Search */}
      <div className="px-3 pt-3 pb-2">
        <div className="relative">
          <Search
            size={12}
            className="absolute left-2.5 top-1/2 -translate-y-1/2 text-text-muted"
          />
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter sets..."
            autoFocus
            className="w-full bg-background/60 border border-border/40 rounded-lg pl-7 pr-3 py-1.5 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
          />
        </div>
      </div>

      {/* Set list — scrollable without overflow-hidden clipping */}
      <div className="max-h-40 overflow-y-auto scrollbar-dark">
        {available.length === 0 && (
          <div className="px-3 py-4 text-center text-[10px] text-text-muted">
            {sets === null ? (
              <span className="flex items-center justify-center gap-1.5">
                <Loader2 size={10} className="animate-spin" />
                Loading...
              </span>
            ) : (
              emptyMessage
            )}
          </div>
        )}
        {available.map((set) => (
          <button
            key={set.name}
            onClick={() => handleSelect(set.name)}
            className="w-full flex items-center justify-between px-3 py-2 text-xs hover:bg-white/[0.04] transition-colors group"
          >
            <span className="font-mono text-text-primary truncate">{set.name}</span>
            <span className="text-[10px] text-text-muted opacity-0 group-hover:opacity-100 transition-opacity flex items-center gap-1">
              <Check size={10} />
              Select
            </span>
          </button>
        ))}
      </div>
    </div>
  );

  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <label className="text-xs text-text-secondary">Variable Sets</label>
        <button
          ref={triggerRef}
          type="button"
          onClick={() => setOpen(!open)}
          className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <Plus size={10} />
          Add set
        </button>
      </div>
      {value.length === 0 && (
        <p className="text-[10px] text-text-muted/60 italic py-2">
          No secret sets referenced
        </p>
      )}
      <div className="space-y-1.5">
        {value.map((name) => (
          <div key={name} className="flex items-center gap-1.5">
            <span
              className={cn(
                'flex-1 font-mono text-xs px-3 py-2 rounded-lg',
                'bg-background/60 border border-border/40 text-text-primary',
              )}
            >
              {name}
            </span>
            <button
              type="button"
              onClick={() => handleRemove(name)}
              className="p-1 text-text-muted hover:text-status-error transition-colors flex-shrink-0"
            >
              <X size={12} />
            </button>
          </div>
        ))}
      </div>

      {open && dropdown && createPortal(dropdown, document.body)}
    </div>
  );
}
