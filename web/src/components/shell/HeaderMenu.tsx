import { Fragment, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { Popover } from '../ui/Popover';

export interface HeaderMenuItem {
  key: string;
  label: string;
  icon: LucideIcon;
  /** Trailing hint such as a shortcut. Hidden from assistive tech so it does not join the item's name. */
  hint?: ReactNode;
  onSelect?: () => void;
  /** Renders the item as a link that opens in a new tab. */
  href?: string;
  disabled?: boolean;
  spinning?: boolean;
  separatorBefore?: boolean;
}

interface HeaderMenuProps {
  /** Trigger tooltip and accessible name. */
  tooltip: string;
  /** Accessible name of the menu panel. */
  menuLabel: string;
  icon: LucideIcon;
  spinning?: boolean;
  items: HeaderMenuItem[];
  className?: string;
}

const ITEM_SELECTOR = '[role="menuitem"]:not([disabled])';

/**
 * Menu button for the header's action cluster, following the WAI-ARIA menu
 * button pattern on top of Popover. Popover owns dismissal and the focus
 * return to the trigger; this component owns initial focus and arrow keys.
 */
export function HeaderMenu({ tooltip, menuLabel, icon, spinning, items, className }: HeaderMenuProps) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const list = listRef.current;
    if (!list) return;
    const first = list.querySelector<HTMLElement>(ITEM_SELECTOR);
    (first ?? list).focus();
  }, [open]);

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    // Tab closes the menu without preventDefault: Popover's cleanup refocuses
    // the trigger, and the browser then moves on from it.
    if (e.key === 'Tab') {
      setOpen(false);
      return;
    }
    const list = listRef.current;
    if (!list) return;
    const enabled = Array.from(list.querySelectorAll<HTMLElement>(ITEM_SELECTOR));
    if (e.key === ' ' && e.target instanceof HTMLAnchorElement) {
      e.preventDefault();
      e.target.click();
      return;
    }
    if (enabled.length === 0) return;
    const current = enabled.indexOf(document.activeElement as HTMLElement);
    let next: number;
    switch (e.key) {
      case 'ArrowDown':
        next = current < 0 ? 0 : (current + 1) % enabled.length;
        break;
      case 'ArrowUp':
        next = current < 0 ? enabled.length - 1 : (current - 1 + enabled.length) % enabled.length;
        break;
      case 'Home':
        next = 0;
        break;
      case 'End':
        next = enabled.length - 1;
        break;
      default:
        return;
    }
    e.preventDefault();
    enabled[next].focus();
  };

  // Close first, in the same handler: Popover's cleanup returns focus to the
  // trigger before a dialog opened by the action records the active element.
  const select = (item: HeaderMenuItem) => {
    setOpen(false);
    item.onSelect?.();
  };

  const itemClass = cn(
    'w-full min-h-8 flex items-center gap-2 px-2 rounded-md text-xs text-left',
    'text-text-secondary transition-colors hover:bg-surface-highlight hover:text-text-primary',
    'focus-visible:-outline-offset-2',
    'disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent',
  );

  return (
    <div className="relative flex items-center">
      <IconButton
        ref={triggerRef}
        icon={icon}
        spinning={spinning}
        onClick={() => setOpen((v) => !v)}
        tooltip={tooltip}
        hasPopup="menu"
        expanded={open}
        className={cn('hover:text-primary hover:border-primary/30', open && 'text-primary border-primary/30', className)}
      />
      <Popover
        open={open}
        onClose={() => setOpen(false)}
        triggerRef={triggerRef}
        role="menu"
        aria-label={menuLabel}
        className="absolute right-0 top-full mt-2 w-56 p-1"
      >
        <div ref={listRef} tabIndex={-1} onKeyDown={handleKeyDown}>
          {items.map((item) => {
            const Icon = item.icon;
            const content = (
              <>
                <Icon
                  size={14}
                  aria-hidden="true"
                  className={cn('shrink-0 text-text-muted', item.spinning && 'animate-spin')}
                />
                <span className="flex-1">{item.label}</span>
                {item.hint && <span aria-hidden="true" className="ml-auto">{item.hint}</span>}
              </>
            );
            return (
              <Fragment key={item.key}>
                {item.separatorBefore && <div role="separator" className="my-1 h-px bg-border/50" />}
                {item.href ? (
                  <a
                    role="menuitem"
                    href={item.href}
                    target="_blank"
                    rel="noreferrer"
                    tabIndex={-1}
                    onClick={() => select(item)}
                    className={itemClass}
                  >
                    {content}
                  </a>
                ) : (
                  <button
                    type="button"
                    role="menuitem"
                    tabIndex={-1}
                    disabled={item.disabled}
                    onClick={() => select(item)}
                    className={itemClass}
                  >
                    {content}
                  </button>
                )}
              </Fragment>
            );
          })}
        </div>
      </Popover>
    </div>
  );
}
