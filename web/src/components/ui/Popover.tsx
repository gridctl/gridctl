import { useCallback, useEffect, useLayoutEffect, useRef, type ReactNode, type RefObject } from 'react';
import { cn } from '../../lib/cn';
import { useDismiss } from '../../hooks/useDismiss';

type CloseReason = 'escape' | 'outside' | null;

interface PopoverProps {
  open: boolean;
  onClose: () => void;
  /** Element to refocus when the popover closes via Escape or programmatically. */
  triggerRef: RefObject<HTMLElement | null>;
  role?: 'dialog' | 'menu';
  'aria-label': string;
  /** Positioning classes from the caller (absolute top-full right-0 mt-1 …). */
  className?: string;
  children: ReactNode;
}

// useDismiss closes on any mousedown outside its ref. The trigger sits outside
// the panel, so a trigger press must be ignored or the following click reopens
// the popover. The ignore flag always describes the current event: a trigger
// press whose bubble never reaches useDismiss must not swallow the next
// Escape or outside click. Close reason is recorded explicitly: an outside
// mousedown on a non-focusable node leaves focus inside the panel, so focus
// location cannot distinguish Escape from an outside click.
export function Popover({
  open,
  onClose,
  triggerRef,
  role = 'dialog',
  'aria-label': ariaLabel,
  className,
  children,
}: PopoverProps) {
  const reasonRef = useRef<CloseReason>(null);
  const ignoreTriggerRef = useRef(false);

  const handleDismiss = useCallback(() => {
    if (ignoreTriggerRef.current) {
      ignoreTriggerRef.current = false;
      return;
    }
    const reason = reasonRef.current;
    onClose();
    if (reason === 'escape') triggerRef.current?.focus();
  }, [onClose, triggerRef]);

  const panelRef = useDismiss<HTMLDivElement>(open, handleDismiss);

  // Registered in layout so these capture listeners run before useDismiss.
  useLayoutEffect(() => {
    if (!open) return;
    const onMouseDown = (e: MouseEvent) => {
      const target = e.target as Node | null;
      const fromTrigger = Boolean(target && triggerRef.current?.contains(target));
      ignoreTriggerRef.current = fromTrigger;
      if (!target || fromTrigger) return;
      if (panelRef.current?.contains(target)) return;
      reasonRef.current = 'outside';
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      ignoreTriggerRef.current = false;
      reasonRef.current = 'escape';
    };
    document.addEventListener('mousedown', onMouseDown, true);
    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('mousedown', onMouseDown, true);
      document.removeEventListener('keydown', onKeyDown, true);
    };
  }, [open, triggerRef, panelRef]);

  useEffect(() => {
    if (!open) return;
    const trigger = triggerRef.current;
    reasonRef.current = null;
    return () => {
      const reason = reasonRef.current;
      reasonRef.current = null;
      ignoreTriggerRef.current = false;
      // Outside click must not pull focus back to the trigger.
      if (reason === 'outside') return;
      trigger?.focus();
    };
  }, [open, triggerRef]);

  if (!open) return null;

  return (
    <div
      ref={panelRef}
      role={role}
      aria-label={ariaLabel}
      className={cn(
        'z-popover rounded-lg border border-border bg-surface-elevated/95 backdrop-blur-xl shadow-lg animate-fade-in-scale',
        className,
      )}
    >
      {children}
    </div>
  );
}
