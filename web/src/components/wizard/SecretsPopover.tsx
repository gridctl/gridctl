import { useState, useEffect, useRef, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { KeyRound, Search, Plus, Eye, EyeOff, X, Loader2, Check } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useVaultStore } from '../../stores/useVaultStore';
import { fetchVaultSecrets, createVaultSecret } from '../../lib/api';

interface SecretsPopoverProps {
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

export function SecretsPopover({ onSelect, className }: SecretsPopoverProps) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState('');
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');
  const [showValue, setShowValue] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [position, setPosition] = useState<PopoverPosition | null>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const secrets = useVaultStore((s) => s.secrets);
  const setSecrets = useVaultStore((s) => s.setSecrets);

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

  // Load secrets when popover opens
  useEffect(() => {
    if (!open) return;
    fetchVaultSecrets()
      .then((s) => setSecrets(s))
      .catch(() => {});
  }, [open, setSecrets]);

  // Compute position on open; reposition on scroll or resize
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
    (key: string) => {
      onSelect(`\${vault:${key}}`);
      setOpen(false);
      setFilter('');
    },
    [onSelect],
  );

  const handleCreate = async () => {
    if (!newKey || !newValue) return;
    if (!/^[A-Z][A-Z0-9_]*$/.test(newKey)) {
      setError('Key must be uppercase alphanumeric with underscores');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await createVaultSecret(newKey, newValue);
      const updated = await fetchVaultSecrets();
      setSecrets(updated);
      onSelect(`\${vault:${newKey}}`);
      setOpen(false);
      setCreating(false);
      setNewKey('');
      setNewValue('');
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create secret');
    } finally {
      setSaving(false);
    }
  };

  const filtered = (secrets ?? []).filter((s) =>
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
            placeholder="Filter secrets..."
            autoFocus
            className="w-full bg-background/60 border border-border/40 rounded-lg pl-7 pr-3 py-1.5 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
          />
        </div>
      </div>

      {/* Secret list — scrollable without overflow-hidden clipping */}
      <div className="max-h-36 overflow-y-auto scrollbar-dark">
        {filtered.length === 0 && !creating && (
          <div className="px-3 py-4 text-center text-[10px] text-text-muted">
            {secrets === null ? 'Loading...' : 'No secrets found'}
          </div>
        )}
        {filtered.map((secret) => (
          <button
            key={secret.key}
            onClick={() => handleSelect(secret.key)}
            className="w-full flex items-center justify-between px-3 py-2 text-xs hover:bg-white/[0.04] transition-colors group"
          >
            <span className="font-mono text-text-primary truncate">{secret.key}</span>
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
          Create New Secret
        </button>
      ) : (
        <div className="px-3 py-3 space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-[10px] text-text-muted uppercase tracking-wider font-medium">
              New Secret
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
            placeholder="SECRET_KEY"
            className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
          />
          <div className="relative">
            <input
              type={showValue ? 'text' : 'password'}
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              placeholder="Secret value"
              className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 pr-8 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
            />
            <button
              type="button"
              onClick={() => setShowValue(!showValue)}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-text-muted hover:text-text-secondary"
            >
              {showValue ? <EyeOff size={12} /> : <Eye size={12} />}
            </button>
          </div>
          {error && (
            <p className="text-[10px] text-status-error">{error}</p>
          )}
          <button
            onClick={handleCreate}
            disabled={!newKey || !newValue || saving}
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
        title="Insert vault secret"
      >
        <KeyRound size={13} />
      </button>

      {open && dropdown && createPortal(dropdown, document.body)}
    </div>
  );
}
