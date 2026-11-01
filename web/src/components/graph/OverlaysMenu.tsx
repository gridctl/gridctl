import { useRef, useState } from 'react';
import { SlidersHorizontal, Flame, Eye, Check } from 'lucide-react';
import { useUIStore } from '../../stores/useUIStore';
import { cn } from '../../lib/cn';
import { Popover } from '../ui/Popover';

/**
 * Groups the canvas's graph overlays (token heat, spec mode) behind a single
 * toolbar button. The popover opens upward from the bottom-left control panel
 * and stays open across toggles so several overlays can be flipped in one pass;
 * an outside click or Escape dismisses it. The trigger keeps a ring while any
 * overlay is active so the grouped state stays visible when the menu is closed.
 */
export function OverlaysMenu() {
  const showHeatMap = useUIStore((s) => s.showHeatMap);
  const toggleHeatMap = useUIStore((s) => s.toggleHeatMap);
  const showSpecMode = useUIStore((s) => s.showSpecMode);
  const toggleSpecMode = useUIStore((s) => s.toggleSpecMode);

  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);

  const anyActive = showHeatMap || showSpecMode;

  const items = [
    {
      key: 'token-heat',
      label: 'Token heat map',
      icon: Flame,
      active: showHeatMap,
      toggle: toggleHeatMap,
      accent: 'text-primary',
    },
    {
      key: 'spec-mode',
      label: 'Spec mode',
      icon: Eye,
      active: showSpecMode,
      toggle: toggleSpecMode,
      accent: 'text-secondary',
    },
  ];

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={cn('control-button', (open || anyActive) && 'ring-1 ring-primary/30')}
        title="Overlays"
        aria-label="Overlays"
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <SlidersHorizontal className="w-4 h-4" />
      </button>

      <Popover
        open={open}
        onClose={() => setOpen(false)}
        triggerRef={triggerRef}
        role="menu"
        aria-label="Canvas overlays"
        className="absolute bottom-full left-0 mb-2 w-44 p-1"
      >
          <div className="px-2 py-1 text-3xs uppercase tracking-[0.18em] text-text-muted/70">
            Overlays
          </div>
          {items.map(({ key, label, icon: Icon, active, toggle, accent }) => (
            <button
              key={key}
              type="button"
              role="menuitemcheckbox"
              aria-checked={active}
              onClick={toggle}
              className={cn(
                'w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-2xs',
                'transition-colors hover:bg-surface-highlight',
                active ? 'text-text-primary' : 'text-text-muted',
              )}
            >
              <Icon
                size={13}
                className={cn('flex-shrink-0', active ? accent : 'text-text-muted')}
                aria-hidden="true"
              />
              <span className="flex-1 text-left">{label}</span>
              {active && <Check size={12} className={accent} aria-hidden="true" />}
            </button>
          ))}
      </Popover>
    </div>
  );
}
