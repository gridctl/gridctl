import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react';
import { Check, Copy, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { copyTextToClipboard } from '../../lib/clipboard';

const COPY_FEEDBACK_MS = 1500;

interface CopyButtonProps {
  value: string;
  // Accessible name, naming the field: "Copy endpoint".
  label: string;
  // Optional visible text. Without it the button is a small icon button.
  children?: ReactNode;
  className?: string;
}

type CopyState = 'idle' | 'copied' | 'failed';

/**
 * Copies `value` and shows the outcome in place for 1.5 seconds: a check
 * (and "Copied") on success, "Copy failed" when the browser refuses. The
 * result is announced through a visually hidden status element. The click
 * never propagates, so the button is safe inside canvas popovers.
 */
export function CopyButton({ value, label, children, className }: CopyButtonProps) {
  const [state, setState] = useState<CopyState>('idle');
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (timerRef.current !== null) clearTimeout(timerRef.current);
    };
  }, []);

  const show = (next: CopyState) => {
    if (!mountedRef.current) return;
    if (timerRef.current !== null) clearTimeout(timerRef.current);
    setState(next);
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      setState('idle');
    }, COPY_FEEDBACK_MS);
  };

  const handleClick = (e: MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    copyTextToClipboard(value).then(
      () => show('copied'),
      () => show('failed'),
    );
  };

  const Icon = state === 'copied' ? Check : state === 'failed' ? X : Copy;
  const iconOnly = children === undefined;
  const text =
    state === 'copied' ? (iconOnly ? null : 'Copied')
      : state === 'failed' ? 'Copy failed'
        : children;
  const announcement = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : '';

  return (
    <>
      <button
        type="button"
        onClick={handleClick}
        aria-label={label}
        className={cn(
          'inline-flex shrink-0 items-center gap-1 rounded-md transition-colors',
          iconOnly ? 'p-1 text-3xs' : 'text-3xs',
          state === 'copied' && 'text-status-running',
          state === 'failed' && 'text-status-error',
          state === 'idle' && 'text-text-muted hover:text-text-secondary',
          iconOnly && 'hover:bg-surface-highlight/60',
          className,
        )}
      >
        <Icon size={11} aria-hidden="true" />
        {text}
      </button>
      <span role="status" className="sr-only">
        {announcement}
      </span>
    </>
  );
}
