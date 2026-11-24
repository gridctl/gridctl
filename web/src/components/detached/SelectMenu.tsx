import { useRef, useState, type ReactNode } from 'react';
import { ChevronDown } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Popover } from '../ui/Popover';

export interface SelectMenuItem {
  key: string;
  label: string;
  /** Color class for the leading dot. */
  dotClass: string;
  /** Short type tag on the right, shown uppercase. */
  tag: string;
  selected: boolean;
  onSelect: () => void;
}

interface SelectMenuProps {
  /** Accessible name of the menu panel. */
  label: string;
  /** Trigger content shown before the chevron. */
  value: ReactNode;
  items: SelectMenuItem[];
  /** Row shown in place of items when the list is empty. */
  emptyText?: string;
  accent?: 'primary' | 'tertiary';
  /** Extra classes for the panel, such as a scroll limit. */
  panelClassName?: string;
}

// Header selector for the detached windows. Popover owns dismissal: Escape and
// a selection return focus to the trigger, an outside click does not.
export function SelectMenu({
  label,
  value,
  items,
  emptyText,
  accent = 'primary',
  panelClassName,
}: SelectMenuProps) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={cn(
          'flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-all',
          'bg-surface-elevated/60 border border-border/50',
          'hover:bg-surface-highlight hover:border-text-muted/30',
          open && 'bg-surface-highlight border-text-muted/30',
        )}
      >
        {value}
        <ChevronDown
          size={14}
          aria-hidden="true"
          className={cn('text-text-muted transition-transform duration-200', open && 'rotate-180')}
        />
      </button>

      <Popover
        open={open}
        onClose={() => setOpen(false)}
        triggerRef={triggerRef}
        role="menu"
        aria-label={label}
        className={cn('absolute top-full left-0 mt-1 w-64 py-1 border-border/50', panelClassName)}
      >
        {items.length === 0 && emptyText ? (
          <div className="px-3 py-2 text-xs text-text-muted">{emptyText}</div>
        ) : (
          items.map((item) => (
            <button
              key={item.key}
              type="button"
              role="menuitemradio"
              aria-checked={item.selected}
              onClick={() => {
                item.onSelect();
                setOpen(false);
              }}
              className={cn(
                'w-full flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors',
                'hover:bg-surface-highlight',
                item.selected && (accent === 'tertiary' ? 'bg-tertiary/10 text-tertiary' : 'bg-primary/10 text-primary'),
              )}
            >
              <span aria-hidden="true" className={cn('w-1.5 h-1.5 rounded-full flex-shrink-0', item.dotClass)} />
              <span className="truncate">{item.label}</span>
              <span className="ml-auto flex-shrink-0 text-3xs text-text-muted uppercase">{item.tag}</span>
            </button>
          ))
        )}
      </Popover>
    </div>
  );
}
