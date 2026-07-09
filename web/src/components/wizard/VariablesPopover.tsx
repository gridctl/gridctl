import { useState, useEffect, useLayoutEffect, useRef, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { KeyRound, Search, Plus, X, Loader2, Check } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useVaultStore } from '../../stores/useVaultStore';
import { fetchVariables, createVariable } from '../../lib/api';
import type { VariableType } from '../../lib/api';
import { VariableVisibilityIcon } from '../vault/VariableVisibilityIcon';
import { VariableTypeBadge } from '../vault/VariableTypeBadge';
import { VariableTypeSelector } from '../vault/VariableTypeSelector';
import { VariableSecretToggle } from '../vault/VariableSecretToggle';
import { SecretGenerator } from '../vault/SecretGenerator';
import { VariableValueInput } from '../vault/VariableValueInput';
import { validateVariableInput } from '../vault/variableTypeHelpers';

interface VariablesPopoverProps {
  onSelect: (reference: string) => void;
  className?: string;
}

interface PopoverPosition {
  top?: number;
  bottom?: number;
  right: number;
}

// Approximate max height of the popover (search + list + divider + button)
const POPOVER_ESTIMATED_HEIGHT = 260;

// VariablesPopover surfaces the unified variable store from the stack wizard.
// It lists both secrets (lock icon) and plaintext variables (eye icon) so a
// user picking a reference can see which kind they're inserting. Emitted
// references use the canonical ${var:KEY} syntax.
export function VariablesPopover({ onSelect, className }: VariablesPopoverProps) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState('');
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');
  const [showValue, setShowValue] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [newType, setNewType] = useState<VariableType>('string');
  const [newIsSecret, setNewIsSecret] = useState(true);
  const [valueValid, setValueValid] = useState(true);
  const [position, setPosition] = useState<PopoverPosition | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const variables = useVaultStore((s) => s.variables);
  const setVariables = useVaultStore((s) => s.setVariables);

  // Compute fixed position from trigger button rect
  const updatePosition = useCallback(() => {
    if (!triggerRef.current) return;
    const rect = triggerRef.current.getBoundingClientRect();
    const right = window.innerWidth - rect.right;
    const spaceBelow = window.innerHeight - rect.bottom;
    if (spaceBelow < POPOVER_ESTIMATED_HEIGHT + 8) {
      setPosition({ bottom: window.innerHeight - rect.top + 6, right });
    } else {
      setPosition({ top: rect.bottom + 6, right });
    }
  }, []);

  // Load variables when popover opens
  useEffect(() => {
    if (!open) return;
    fetchVariables()
      .then((s) => setVariables(s))
      .catch(() => {});
  }, [open, setVariables]);

  // Drop the stale position when the popover closes (state adjustment during
  // render; commits with the close, no extra frame).
  if (!open && position !== null) setPosition(null);

  // Compute position on open; reposition on scroll or resize. Layout effect
  // because the position comes from a DOM measurement and must land before
  // paint to avoid a misplaced first frame.
  useLayoutEffect(() => {
    if (!open) return;
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
    (key: string) => {
      onSelect(`\${var:${key}}`);
      setOpen(false);
      setFilter('');
    },
    [onSelect],
  );

  const handleCreate = useCallback(async () => {
    if (!newKey || !newValue) return;
    if (!/^[A-Z][A-Z0-9_]*$/.test(newKey)) {
      setError('Key must be uppercase alphanumeric with underscores');
      return;
    }
    const validation = validateVariableInput(newType, newValue);
    if (!validation.ok) {
      setError(validation.error);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await createVariable({
        key: newKey,
        value: validation.normalized,
        type: newType,
        isSecret: newIsSecret,
      });
      const updated = await fetchVariables();
      setVariables(updated);
      onSelect(`\${var:${newKey}}`);
      setOpen(false);
      setCreating(false);
      setNewKey('');
      setNewValue('');
      setNewType('string');
      setNewIsSecret(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create variable');
    } finally {
      setSaving(false);
    }
  }, [newKey, newValue, newType, newIsSecret, onSelect, setVariables]);

  // Switching into/out of bool clears the value — its "true"/"false" is
  // widget-managed, not user-typed, so it shouldn't bleed into other types.
  const handleTypeChange = (next: VariableType) => {
    if (newType === 'bool' || next === 'bool') {
      setNewValue('');
      setError(null);
    }
    setNewType(next);
  };

  const filtered = (variables ?? []).filter((s) =>
    s.key.toLowerCase().includes(filter.toLowerCase()),
  );

  // Rendered via portal to escape ancestor overflow containers
  const dropdown = position && (
    <div
      ref={dropdownRef}
      style={{
        position: 'fixed',
        top: position.top,
        bottom: position.bottom,
        right: position.right,
        width: '18rem',
        zIndex: 9999,
      }}
      className={cn('glass-panel-elevated rounded-xl', 'animate-fade-in-scale')}
    >
      {/* Search */}
      <div className="px-3 pt-3 pb-2">
        <div className="relative">
          <Search size={12} className="absolute left-2.5 top-1/2 -translate-y-1/2 text-text-muted" />
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter variables..."
            autoFocus
            className="w-full bg-background/60 border border-border/40 rounded-lg pl-7 pr-3 py-1.5 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
          />
        </div>
      </div>

      {/* Variable list */}
      <div className="max-h-36 overflow-y-auto scrollbar-dark">
        {filtered.length === 0 && !creating && (
          <div className="px-3 py-4 text-center text-[10px] text-text-muted">
            {variables === null ? 'Loading...' : 'No variables found'}
          </div>
        )}
        {filtered.map((variable) => (
          <button
            key={variable.key}
            onClick={() => handleSelect(variable.key)}
            className="w-full flex items-center gap-2 px-3 py-2 text-xs hover:bg-white/[0.04] transition-colors group"
          >
            <VariableVisibilityIcon isSecret={variable.is_secret} />
            <span className="font-mono text-text-primary truncate flex-1 text-left">{variable.key}</span>
            <VariableTypeBadge type={variable.type} />
            <span className="text-[10px] text-text-muted opacity-0 group-hover:opacity-100 transition-opacity flex items-center gap-1">
              <Check size={10} />
              Select
            </span>
          </button>
        ))}
      </div>

      {/* Divider */}
      <div className="border-t border-border/30 mx-3" />

      {/* Create new */}
      {!creating ? (
        <button
          onClick={() => {
            setCreating(true);
            setError(null);
          }}
          className="w-full flex items-center gap-2 px-3 py-2.5 text-xs text-secondary hover:bg-white/[0.04] transition-colors"
        >
          <Plus size={12} />
          Create New Variable
        </button>
      ) : (
        <div className="px-3 py-3 space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-[10px] text-text-muted uppercase tracking-wider font-medium">
              New Variable
            </span>
            <button
              onClick={() => {
                setCreating(false);
                setError(null);
              }}
              className="text-text-muted hover:text-text-secondary"
            >
              <X size={12} />
            </button>
          </div>
          <input
            type="text"
            value={newKey}
            onChange={(e) => setNewKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, ''))}
            placeholder="VARIABLE_KEY"
            className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
          />
          <VariableValueInput
            type={newType}
            value={newValue}
            onChange={(v) => {
              setNewValue(v);
              setError(null);
            }}
            isSecret={newIsSecret}
            revealed={showValue}
            onToggleReveal={() => setShowValue((s) => !s)}
            onValidityChange={setValueValid}
            onRequestSubmit={handleCreate}
            compact
          />
          <div className="flex flex-wrap gap-1">
            <VariableTypeSelector value={newType} onChange={handleTypeChange} />
          </div>
          <div className="flex flex-wrap items-center gap-1">
            <VariableSecretToggle isSecret={newIsSecret} onChange={setNewIsSecret} />
            {newType === 'string' && (
              <SecretGenerator
                onGenerate={setNewValue}
                onReveal={() => setShowValue(true)}
              />
            )}
          </div>
          {error && (
            <p className="text-[10px] text-status-error">{error}</p>
          )}
          <button
            onClick={handleCreate}
            disabled={!newKey || !newValue || !valueValid || saving}
            className={cn(
              'w-full flex items-center justify-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-all duration-200',
              'bg-secondary/20 text-secondary hover:bg-secondary/30',
              'disabled:opacity-40 disabled:cursor-not-allowed',
            )}
          >
            {saving ? <Loader2 size={12} className="animate-spin" /> : <Plus size={12} />}
            Create & Insert
          </button>
        </div>
      )}
    </div>
  );

  return (
    <div className={cn('relative', className)}>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen(!open)}
        className={cn(
          'p-1.5 rounded-md transition-all duration-200',
          'hover:bg-primary/10 text-text-muted hover:text-primary',
          open && 'bg-primary/10 text-primary',
        )}
        title="Insert variable"
      >
        <KeyRound size={13} />
      </button>

      {open && dropdown && createPortal(dropdown, document.body)}
    </div>
  );
}
