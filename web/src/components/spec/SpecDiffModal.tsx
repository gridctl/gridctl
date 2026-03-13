import { useMemo } from 'react';
import { AlertCircle } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Modal } from '../ui/Modal';
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

  // Simple LCS-based diff
  const m = oldLines.length;
  const n = newLines.length;

  // Build LCS table
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

  // Backtrack to produce diff
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
  const closeDiffModal = useSpecStore((s) => s.closeDiffModal);
  const spec = useSpecStore((s) => s.spec);
  const pendingSpec = useSpecStore((s) => s.pendingSpec);

  const diffLines = useMemo(() => {
    if (!spec || !pendingSpec) return [];
    return computeLineDiff(spec.content, pendingSpec);
  }, [spec, pendingSpec]);

  const hasChanges = diffLines.some((l) => l.type !== 'context');
  const hasErrors = validationErrors && validationErrors.length > 0;

  return (
    <Modal
      isOpen={diffModalOpen}
      onClose={closeDiffModal}
      title="Configuration Changed"
      size="full"
    >
      <div className="flex flex-col gap-4 h-full">
        {/* Diff view */}
        <div className="flex-1 overflow-auto scrollbar-dark rounded-lg border border-border/30 bg-background/60">
          {!hasChanges ? (
            <div className="flex items-center justify-center h-full text-text-muted text-xs py-12">
              No changes detected
            </div>
          ) : (
            <table className="w-full font-mono text-xs border-collapse">
              <thead>
                <tr className="border-b border-border/30 sticky top-0 bg-surface-elevated/95 backdrop-blur-sm">
                  <th className="text-left px-3 py-2 text-text-muted font-medium w-12">Old</th>
                  <th className="text-left px-3 py-2 text-text-muted font-medium w-12">New</th>
                  <th className="text-left px-3 py-2 text-text-muted font-medium">Content</th>
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
                      'px-3 py-px text-right w-12',
                      line.type === 'removed' ? 'text-status-error/60' : 'text-text-muted/40',
                    )}>
                      {line.lineOld ?? ''}
                    </td>
                    <td className={cn(
                      'px-3 py-px text-right w-12',
                      line.type === 'added' ? 'text-status-running/60' : 'text-text-muted/40',
                    )}>
                      {line.lineNew ?? ''}
                    </td>
                    <td className="px-3 py-px whitespace-pre">
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

        {/* Validation errors */}
        {hasErrors && (
          <div className="rounded-lg border border-status-error/30 bg-status-error/[0.06] px-4 py-3">
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

        {/* Actions */}
        <div className="flex items-center justify-end gap-3 pt-2 border-t border-border/30">
          <Button variant="secondary" onClick={closeDiffModal}>
            Cancel
          </Button>
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
        </div>
      </div>
    </Modal>
  );
}
