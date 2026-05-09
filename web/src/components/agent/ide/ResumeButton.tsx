import { useState } from 'react';
import { resumeRun } from '../../../lib/agent-api';
import { cn } from '../../../lib/cn';

interface ResumeButtonProps {
  runID: string;
  fromStep: string;
}

/**
 * ResumeButton drives the "Resume from here" affordance — every
 * completed step gets one in trace mode. The button is intentionally
 * understated: time-travel is a power feature and the affordance
 * shouldn't shout at developers scanning the canvas casually.
 */
export function ResumeButton({ runID, fromStep }: ResumeButtonProps) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  async function handleClick() {
    setBusy(true);
    setError(null);
    try {
      await resumeRun(runID, { from_step: fromStep });
      setDone(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <button
        type="button"
        disabled={busy || done}
        onClick={() => void handleClick()}
        className={cn(
          'inline-flex items-center gap-2 px-3 py-1.5 rounded',
          'font-mono text-xs uppercase tracking-[0.16em]',
          'border border-primary/40 text-primary-light bg-primary/5',
          'hover:bg-primary/15 hover:border-primary/60 transition-colors',
          'disabled:opacity-50 disabled:cursor-not-allowed',
        )}
      >
        {done ? '✓ resume queued' : busy ? 'resuming…' : '↻ resume from here'}
      </button>
      {error && (
        <p className="mt-2 text-xs text-status-error font-mono">{error}</p>
      )}
    </div>
  );
}
