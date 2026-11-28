import { useId } from 'react';
import { Button } from '../ui/Button';
import { MIN_SUPPORTED_WIDTH } from '../../lib/viewport';

/**
 * Covers the workspace area when the window is narrower than the supported
 * minimum. Dismissible: a hard block would leave zoomed or split-window users
 * with no way forward.
 */
export function MinWidthState({ onContinue }: { onContinue: () => void }) {
  const headingId = useId();
  return (
    <section
      aria-labelledby={headingId}
      className="absolute inset-0 z-overlay bg-background flex flex-col items-center justify-center gap-3 p-6 text-center"
    >
      <h2 id={headingId} className="text-sm font-semibold text-text-primary">
        Window too narrow
      </h2>
      <p className="max-w-xs text-xs text-text-secondary">
        gridctl needs a window at least {MIN_SUPPORTED_WIDTH} px wide. Widen the window or reduce the browser zoom.
      </p>
      <Button variant="secondary" size="sm" onClick={onContinue}>
        Continue anyway
      </Button>
    </section>
  );
}
