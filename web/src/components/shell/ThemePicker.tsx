import { useEffect, useRef, useState } from 'react';
import { Sun, Moon, Monitor } from 'lucide-react';
import { useUIStore } from '../../stores/useUIStore';
import { useResolvedTheme } from '../../themes/useTheme';
import type { ThemeMode } from '../../themes/types';
import { cn } from '../../lib/cn';

const OPTIONS: { mode: ThemeMode; label: string; icon: typeof Sun }[] = [
  { mode: 'light', label: 'Light', icon: Sun },
  { mode: 'dark', label: 'Dark', icon: Moon },
  { mode: 'system', label: 'System', icon: Monitor },
];

interface ThemePickerProps {
  /** Trigger styling: a prominent bordered button (header action cluster) or a
   *  compact inline glyph (statusbar). */
  variant?: 'header' | 'statusbar';
  /** Which way the popover opens — 'down' for a top-of-screen mount (header),
   *  'up' for a bottom-of-screen mount (statusbar). */
  placement?: 'down' | 'up';
}

/**
 * Appearance control. The collapsed trigger shows the *resolved* theme glyph
 * (sun/moon) so the persistent indicator always reflects what's on screen;
 * clicking opens a popover with a Light/Dark/System segmented control (WAI-ARIA
 * radiogroup) and, while System is active, a live line announcing which concrete
 * theme it currently resolves to.
 */
export function ThemePicker({ variant = 'statusbar', placement = 'up' }: ThemePickerProps) {
  const themeMode = useUIStore((s) => s.themeMode);
  const setThemeMode = useUIStore((s) => s.setThemeMode);
  const resolved = useResolvedTheme();

  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('mousedown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open]);

  // Roving arrow-key navigation across the segmented radio group.
  const onRadioKeyDown = (e: React.KeyboardEvent, index: number) => {
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault();
      setThemeMode(OPTIONS[(index + 1) % OPTIONS.length].mode);
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault();
      setThemeMode(OPTIONS[(index - 1 + OPTIONS.length) % OPTIONS.length].mode);
    }
  };

  const TriggerIcon = resolved === 'light' ? Sun : Moon;
  const triggerLabel = `Appearance: ${
    themeMode === 'system' ? `System (${resolved === 'light' ? 'Light' : 'Dark'})` : themeMode
  }`;
  const isHeader = variant === 'header';

  return (
    <div ref={ref} className="relative flex items-center">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={triggerLabel}
        title="Appearance"
        className={
          isHeader
            ? cn(
                'rounded-lg p-2 transition-all duration-200 ease-out backdrop-blur-sm',
                'bg-surface-elevated/60 border border-border/50',
                'focus:outline-none focus:ring-2 focus:ring-primary/30 focus:ring-offset-1 focus:ring-offset-background',
                open
                  ? 'text-primary border-primary/30'
                  : 'text-text-muted hover:bg-surface-highlight hover:text-primary hover:border-primary/30',
              )
            : cn(
                'flex items-center justify-center w-5 h-5 rounded text-text-muted',
                'hover:text-text-secondary hover:bg-surface-highlight/50 transition-colors',
                'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50',
                open && 'text-text-secondary bg-surface-highlight/50',
              )
        }
      >
        <TriggerIcon size={isHeader ? 16 : 12} aria-hidden="true" />
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Appearance"
          className={cn(
            'absolute right-0 z-50 w-52 p-2',
            placement === 'down' ? 'top-full mt-2' : 'bottom-full mb-2',
            'rounded-lg border border-border bg-surface-elevated/95',
            'backdrop-blur-xl shadow-lg animate-fade-in-scale',
          )}
        >
          <div className="px-1 pb-1.5 text-[9px] uppercase tracking-[0.18em] text-text-muted/70">
            Appearance
          </div>

          <div
            role="radiogroup"
            aria-label="Theme"
            className="flex items-center gap-1 rounded-md bg-background/60 p-1"
          >
            {OPTIONS.map(({ mode, label, icon: Icon }, i) => {
              const active = themeMode === mode;
              return (
                <button
                  key={mode}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  tabIndex={active ? 0 : -1}
                  onClick={() => setThemeMode(mode)}
                  onKeyDown={(e) => onRadioKeyDown(e, i)}
                  className={cn(
                    'flex-1 flex flex-col items-center gap-1 py-1.5 rounded text-[10px] font-medium',
                    'transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50',
                    active
                      ? 'bg-surface-highlight text-text-primary shadow-bevel'
                      : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/50',
                  )}
                >
                  <Icon size={14} aria-hidden="true" className={active ? 'text-primary' : undefined} />
                  {label}
                </button>
              );
            })}
          </div>

          <div className="px-1 pt-2 min-h-[1rem] text-[10px] text-text-muted" aria-live="polite">
            {themeMode === 'system'
              ? `Following system — currently ${resolved === 'light' ? 'Light' : 'Dark'}`
              : ''}
          </div>
        </div>
      )}
    </div>
  );
}
