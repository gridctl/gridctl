import { useEffect, useState } from 'react';
import { AlertCircle, GitCompareArrows, Loader2 } from 'lucide-react';
import { Modal } from '../ui/Modal';
import { showToast } from '../ui/Toast';
import { cn } from '../../lib/cn';
import { fetchSkillDiff, resetSkill } from '../../lib/api';
import type { SkillDiffResponse } from '../../types';

interface SkillCompareDialogProps {
  isOpen: boolean;
  /** Source (lock) name the skill is tracked under. */
  sourceName: string;
  skillName: string;
  onClose: () => void;
  /** Called after "Take upstream" overwrites the local copy, so the caller can
   *  refresh and (typically) close the editor. */
  onTookUpstream: () => void;
}

/**
 * Compare a tracked skill's local SKILL.md against the latest upstream content.
 * Read-only by default; "Take upstream" force-restores the skill (the server
 * writes a backup first) behind an inline confirm. "Keep mine" simply closes.
 * The diff fetch failing surfaces inline rather than blocking.
 */
export function SkillCompareDialog({
  isOpen,
  sourceName,
  skillName,
  onClose,
  onTookUpstream,
}: SkillCompareDialogProps) {
  const [diff, setDiff] = useState<SkillDiffResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);

  // Reset the result on (re)open or target change during render rather than in
  // an effect, so the effect only performs the fetch and its async callbacks.
  const target = `${sourceName}/${skillName}`;
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  if (isOpen && loadedFor !== target) {
    setLoadedFor(target);
    setDiff(null);
    setError(null);
    setConfirming(false);
  }
  // Loading is derived: open, fetched for this target, no result or error yet.
  const loading = isOpen && diff === null && error === null;

  useEffect(() => {
    if (!isOpen) return;
    let cancelled = false;
    fetchSkillDiff(sourceName, skillName)
      .then((d) => {
        if (!cancelled) {
          setError(null);
          setDiff(d);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setDiff(null);
          setError(err instanceof Error ? err.message : 'Failed to load diff');
        }
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen, sourceName, skillName]);

  const handleTakeUpstream = async () => {
    setBusy(true);
    try {
      await resetSkill(sourceName, skillName);
      showToast('success', `"${skillName}" reset to upstream`);
      onTookUpstream();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Reset failed');
    } finally {
      setBusy(false);
      setConfirming(false);
    }
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={`Compare: ${skillName}`} size="wide">
      <div className="flex flex-col gap-3 min-h-[40vh]">
        <p className="text-xs text-text-muted flex items-center gap-1.5">
          <GitCompareArrows size={13} className="text-primary" />
          Your local SKILL.md vs the latest upstream content.
        </p>

        {loading && (
          <div className="flex-1 flex items-center justify-center text-text-muted text-xs gap-2 py-10">
            <Loader2 size={14} className="animate-spin" /> Fetching upstream…
          </div>
        )}

        {error && (
          <div className="flex items-start gap-2 rounded-lg border border-status-error/30 bg-status-error/10 px-3 py-2.5 text-xs text-status-error">
            <AlertCircle size={14} className="flex-shrink-0 mt-0.5" />
            <span>{error}</span>
          </div>
        )}

        {!loading && !error && diff && <DiffBody diff={diff} />}

        <div className="flex flex-wrap items-center justify-end gap-2 border-t border-border/30 pt-3">
          {confirming ? (
            <>
              <span className="text-xs text-text-muted mr-auto">
                Overwrite your local edits with upstream? A backup is kept.
              </span>
              <button
                type="button"
                onClick={() => setConfirming(false)}
                disabled={busy}
                className="px-3 py-1.5 text-xs rounded-lg text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight transition-colors"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void handleTakeUpstream()}
                disabled={busy}
                className={cn(
                  'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors',
                  'text-status-error bg-status-error/10 hover:bg-status-error/20 border border-status-error/30',
                  busy && 'opacity-50 cursor-not-allowed',
                )}
              >
                {busy ? 'Overwriting…' : 'Confirm overwrite'}
              </button>
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={onClose}
                className="px-3 py-1.5 text-xs font-medium rounded-lg bg-primary text-background hover:bg-primary-light transition-colors"
              >
                Keep mine
              </button>
              <button
                type="button"
                onClick={() => setConfirming(true)}
                disabled={loading || !!error}
                className={cn(
                  'px-3 py-1.5 text-xs rounded-lg transition-colors',
                  'text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight',
                  (loading || !!error) && 'opacity-50 cursor-not-allowed',
                )}
              >
                Take upstream
              </button>
            </>
          )}
        </div>
      </div>
    </Modal>
  );
}

function DiffBody({ diff }: { diff: SkillDiffResponse }) {
  if (diff.unifiedDiff) {
    return (
      <div className="flex-1 overflow-auto scrollbar-dark rounded-lg border border-border/30 bg-background/40">
        <pre className="text-[11px] font-mono leading-relaxed p-3 whitespace-pre">
          {diff.unifiedDiff.split('\n').map((line, i) => (
            <div key={i} className={cn('px-1 -mx-1', diffLineClass(line))}>
              {line || ' '}
            </div>
          ))}
        </pre>
      </div>
    );
  }
  // No textual diff: either identical, or only whitespace changes.
  return (
    <div className="flex-1 flex items-center justify-center text-xs text-text-muted py-10">
      {diff.drifted
        ? 'No line-level differences against upstream.'
        : 'This skill matches upstream; no local edits.'}
    </div>
  );
}

function diffLineClass(line: string): string {
  if (line.startsWith('@@')) return 'text-primary/80 bg-primary/5';
  if (line.startsWith('+')) return 'text-emerald-300 bg-emerald-500/5';
  if (line.startsWith('-')) return 'text-status-error bg-status-error/5';
  return 'text-text-secondary';
}
