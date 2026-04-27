import { useMemo, useEffect, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { AlertCircle, Minus, Plus, Equal, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useSpecStore } from '../../stores/useSpecStore';
import { Button } from '../ui/Button';

interface DiffLine {
  type: 'context' | 'added' | 'removed';
  lineOld: number | null;
  lineNew: number | null;
  text: string;
}

function computeLineDiff(oldText: string, newText: string): DiffLine[] {
  const oldLines = oldText.split('\n');
  const newLines = newText.split('\n');
  const result: DiffLine[] = [];

  const m = oldLines.length;
  const n = newLines.length;

  const dp: number[][] = Array.from({ length: m + 1 }, () => Array(n + 1).fill(0));
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (oldLines[i - 1] === newLines[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1;
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
      }
    }
  }

  const diff: Array<{ type: 'context' | 'added' | 'removed'; text: string; oldIdx: number; newIdx: number }> = [];
  let i = m, j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
      diff.unshift({ type: 'context', text: oldLines[i - 1], oldIdx: i, newIdx: j });
      i--; j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      diff.unshift({ type: 'added', text: newLines[j - 1], oldIdx: 0, newIdx: j });
      j--;
    } else {
      diff.unshift({ type: 'removed', text: oldLines[i - 1], oldIdx: i, newIdx: 0 });
      i--;
    }
  }

  for (const d of diff) {
    result.push({
      type: d.type,
      lineOld: d.type !== 'added' ? d.oldIdx : null,
      lineNew: d.type !== 'removed' ? d.newIdx : null,
      text: d.text,
    });
  }

  return result;
}

interface SpecDiffModalProps {
  onApply: () => void;
  validationErrors?: string[];
}

export function SpecDiffModal({ onApply, validationErrors }: SpecDiffModalProps) {
  const diffModalOpen = useSpecStore((s) => s.diffModalOpen);
  const diffModalMode = useSpecStore((s) => s.diffModalMode);
  const closeDiffModal = useSpecStore((s) => s.closeDiffModal);
  const appliedSpec = useSpecStore((s) => s.appliedSpec);
  const pendingSpec = useSpecStore((s) => s.pendingSpec);
  const spec = useSpecStore((s) => s.spec);

  const isCompareMode = diffModalMode === 'compare';

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeDiffModal();
    },
    [closeDiffModal],
  );

  useEffect(() => {
    if (diffModalOpen) {
      document.addEventListener('keydown', handleKeyDown);
      return () => document.removeEventListener('keydown', handleKeyDown);
    }
  }, [diffModalOpen, handleKeyDown]);

  const compareSource = isCompareMode ? spec?.content ?? null : pendingSpec;

  const diffLines = useMemo(() => {
    if (!appliedSpec || compareSource === null) return [];
    return computeLineDiff(appliedSpec.content, compareSource);
  }, [appliedSpec, compareSource]);

  const missingBaseline = isCompareMode && !appliedSpec;
  const hasChanges = diffLines.some((l) => l.type !== 'context');
  const hasErrors = !isCompareMode && validationErrors && validationErrors.length > 0;

  const addedCount = diffLines.filter((l) => l.type === 'added').length;
  const removedCount = diffLines.filter((l) => l.type === 'removed').length;
  const unchangedCount = diffLines.filter((l) => l.type === 'context').length;

  if (!diffModalOpen) return null;

  return createPortal(
    <div className="fixed inset-0 z-[100] flex flex-col bg-background">
      {/* Header — pinned */}
      <div className="flex-shrink-0 flex items-center justify-between px-6 py-3 border-b border-border/50 bg-surface-elevated">
        <div className="flex items-center gap-4">
          <h2 className="text-sm font-semibold text-text-primary">
            {isCompareMode ? 'Compare to Running' : 'Configuration Changed'}
          </h2>
          {hasChanges && (
            <div className="flex items-center gap-3 text-xs">
              <span className="flex items-center gap-1 px-2 py-0.5 rounded-full bg-status-running/10 text-status-running">
                <Plus size={11} />
                {addedCount} added
              </span>
              <span className="flex items-center gap-1 px-2 py-0.5 rounded-full bg-status-error/10 text-status-error">
                <Minus size={11} />
                {removedCount} removed
              </span>
              <span className="flex items-center gap-1 px-2 py-0.5 rounded-full bg-surface-highlight text-text-muted">
                <Equal size={11} />
                {unchangedCount} unchanged
              </span>
            </div>
          )}
        </div>
        <button
          onClick={closeDiffModal}
          className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors text-text-muted hover:text-text-primary"
        >
          <X size={16} />
        </button>
      </div>

      {/* Diff body — scrollable, takes all remaining space */}
      <div className="flex-1 min-h-0 overflow-auto scrollbar-dark">
        {missingBaseline ? (
          <div className="flex items-center justify-center h-full text-text-muted text-sm">
            Waiting for the gateway baseline — reload once to capture it.
          </div>
        ) : !hasChanges ? (
          <div className="flex items-center justify-center h-full text-text-muted text-sm">
            {isCompareMode
              ? 'No drift — on-disk spec matches the running gateway.'
              : 'No changes detected'}
          </div>
        ) : (
          <table className="w-full font-mono text-xs border-collapse">
            <thead>
              <tr className="border-b border-border/30 sticky top-0 bg-surface-elevated z-10">
                <th className="text-right px-3 py-2 text-text-muted font-medium w-14">Old</th>
                <th className="text-right px-3 py-2 text-text-muted font-medium w-14">New</th>
                <th className="text-left px-4 py-2 text-text-muted font-medium">Content</th>
              </tr>
            </thead>
            <tbody>
              {diffLines.map((line, idx) => (
                <tr
                  key={idx}
                  className={cn(
                    'border-b border-border/10',
                    line.type === 'added' && 'bg-status-running/[0.08]',
                    line.type === 'removed' && 'bg-status-error/[0.08]',
                  )}
                >
                  <td className={cn(
                    'px-3 py-0.5 text-right w-14 select-none',
                    line.type === 'removed' ? 'text-status-error/60' : 'text-text-muted/40',
                  )}>
                    {line.lineOld ?? ''}
                  </td>
                  <td className={cn(
                    'px-3 py-0.5 text-right w-14 select-none',
                    line.type === 'added' ? 'text-status-running/60' : 'text-text-muted/40',
                  )}>
                    {line.lineNew ?? ''}
                  </td>
                  <td className="px-4 py-0.5 whitespace-pre">
                    <span className={cn(
                      line.type === 'added' && 'text-status-running',
                      line.type === 'removed' && 'text-status-error line-through',
                      line.type === 'context' && 'text-text-secondary',
                    )}>
                      {line.type === 'added' && '+ '}
                      {line.type === 'removed' && '- '}
                      {line.text}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Footer — pinned */}
      <div className="flex-shrink-0 border-t border-border/50 bg-surface-elevated px-6 py-3">
        {hasErrors && (
          <div className="rounded-lg border border-status-error/30 bg-status-error/[0.06] px-4 py-3 mb-3">
            <div className="flex items-center gap-2 text-status-error text-xs font-medium mb-2">
              <AlertCircle size={12} />
              Validation errors in new spec
            </div>
            <ul className="text-xs text-status-error/80 space-y-1">
              {validationErrors!.map((err, i) => (
                <li key={i} className="flex items-start gap-1.5">
                  <span className="text-status-error/50 mt-0.5">-</span>
                  {err}
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="flex items-center justify-end gap-3">
          <Button variant="secondary" onClick={closeDiffModal}>
            {isCompareMode ? 'Close' : 'Cancel'}
          </Button>
          {!isCompareMode && (
            <Button
              variant="primary"
              onClick={() => {
                onApply();
                closeDiffModal();
              }}
              disabled={!!hasErrors}
            >
              Apply Changes
            </Button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
