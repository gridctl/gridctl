import type { KeyboardEvent } from 'react';
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

/**
 * Light/Dark/System radiogroup shared by the header ThemePicker and Preferences.
 * Arrow keys rove the selection. While System is active, a live line names the
 * concrete theme the OS currently resolves to.
 */
export function ThemeModeRadioGroup() {
  const themeMode = useUIStore((s) => s.themeMode);
  const setThemeMode = useUIStore((s) => s.setThemeMode);
  const resolved = useResolvedTheme();

  const onRadioKeyDown = (e: KeyboardEvent, index: number) => {
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault();
      setThemeMode(OPTIONS[(index + 1) % OPTIONS.length].mode);
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault();
      setThemeMode(OPTIONS[(index - 1 + OPTIONS.length) % OPTIONS.length].mode);
    }
  };

  return (
    <div>
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
                'flex-1 flex flex-col items-center gap-1 py-1.5 rounded text-3xs font-medium',
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
      <div className="px-1 pt-2 min-h-[1rem] text-3xs text-text-muted" aria-live="polite">
        {themeMode === 'system'
          ? `Following system — currently ${resolved === 'light' ? 'Light' : 'Dark'}`
          : ''}
      </div>
    </div>
  );
}
