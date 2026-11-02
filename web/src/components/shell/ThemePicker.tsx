import { useRef, useState } from 'react';
import { Sun, Moon } from 'lucide-react';
import { useUIStore } from '../../stores/useUIStore';
import { useResolvedTheme } from '../../themes/useTheme';
import { cn } from '../../lib/cn';
import { Popover } from '../ui/Popover';
import { ThemeModeRadioGroup } from './ThemeModeRadioGroup';

/**
 * Header appearance control. The collapsed trigger shows the resolved theme
 * glyph (sun/moon). The popover opens downward and hosts ThemeModeRadioGroup.
 */
export function ThemePicker() {
  const themeMode = useUIStore((s) => s.themeMode);
  const resolved = useResolvedTheme();

  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);

  const TriggerIcon = resolved === 'light' ? Sun : Moon;
  const triggerLabel = `Appearance: ${
    themeMode === 'system' ? `System (${resolved === 'light' ? 'Light' : 'Dark'})` : themeMode
  }`;

  return (
    <div className="relative flex items-center">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={triggerLabel}
        title="Appearance"
        className={cn(
          'rounded-lg p-2 transition-all duration-200 ease-out backdrop-blur-sm',
          'bg-surface-elevated/60 border border-border/50',
          'focus:outline-none focus:ring-2 focus:ring-primary/30 focus:ring-offset-1 focus:ring-offset-background',
          open
            ? 'text-primary border-primary/30'
            : 'text-text-muted hover:bg-surface-highlight hover:text-primary hover:border-primary/30',
        )}
      >
        <TriggerIcon size={16} aria-hidden="true" />
      </button>

      <Popover
        open={open}
        onClose={() => setOpen(false)}
        triggerRef={triggerRef}
        aria-label="Appearance"
        className="absolute right-0 top-full mt-2 w-52 p-2"
      >
        <div className="px-1 pb-1.5 text-3xs uppercase tracking-[0.18em] text-text-muted/70">
          Appearance
        </div>
        <ThemeModeRadioGroup />
      </Popover>
    </div>
  );
}
