import { useMemo } from 'react';
import { createPortal } from 'react-dom';
import { Equal, Minus, Plus, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { diffLines, prettySchema, MAX_DIFF_LINES, type DiffToken } from '../../lib/diff';
import { useFocusTrap } from '../../hooks/useFocusTrap';

// SchemaDiffModal is the full-viewport reading room for a schema delta: the
// inline card panel stays the always-visible review surface (approve must
// never depend on opening anything), this adds width, line numbers, and
// soft-wrapped long lines. That wrap is the point: on real stacks over half
// of pinned schemas carry lines wider than the inline column (property
// descriptions, the favorite hiding place for tool poisoning), and text a
// reviewer must drag a scrollbar to read is text that goes unread.
//
// With no oldSchema (a pin recorded before schema capture) there is nothing
// to diff against, so the modal renders a single-pane viewer of the new
// schema instead of a two-pane comparison against an empty document.
interface SchemaDiffModalProps {
  title: string;
  oldSchema?: string;
  newSchema: string;
  onClose: () => void;
}

// Mirrors SchemaDiffBlock's balanced truncation: a head slice of the
// oversize fallback (all removals, then all additions) must not hide the
// entire added side.
function truncateBalanced(lines: DiffToken[]): { visible: DiffToken[]; hidden: number } {
  if (lines.length <= MAX_DIFF_LINES) return { visible: lines, hidden: 0 };
  const firstAdded = lines.findIndex((l) => l.kind === 'added');
  const visible =
    firstAdded > MAX_DIFF_LINES / 2
      ? [
          ...lines.slice(0, MAX_DIFF_LINES / 2),
          ...lines.slice(firstAdded, firstAdded + MAX_DIFF_LINES / 2),
        ]
      : lines.slice(0, MAX_DIFF_LINES);
  return { visible, hidden: lines.length - visible.length };
}

export function SchemaDiffModal({ title, oldSchema, newSchema, onClose }: SchemaDiffModalProps) {
  const isDiff = Boolean(oldSchema);

  const { visible, hidden, addedCount, removedCount, unchangedCount } = useMemo(() => {
    const lines = isDiff
      ? diffLines(prettySchema(oldSchema!), prettySchema(newSchema))
      : prettySchema(newSchema)
          .split('\n')
          .map((text, i) => ({ kind: 'same' as const, text, newLine: i + 1 }));
    const { visible, hidden } = truncateBalanced(lines);
    return {
      visible,
      hidden,
      addedCount: lines.filter((l) => l.kind === 'added').length,
      removedCount: lines.filter((l) => l.kind === 'removed').length,
      unchangedCount: lines.filter((l) => l.kind === 'same').length,
    };
  }, [isDiff, oldSchema, newSchema]);

  const panelRef = useFocusTrap<HTMLDivElement>({ active: true });

  return createPortal(
    <div
      ref={panelRef}
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
      }}
      className="fixed inset-0 z-[100] flex flex-col bg-background"
    >
      {/* Header - pinned */}
      <div className="flex-shrink-0 flex items-center justify-between px-6 py-3 border-b border-border/50 bg-surface-elevated">
        <div className="flex items-center gap-4 min-w-0">
          <h2 className="text-sm font-semibold text-text-primary truncate">{title}</h2>
          {isDiff ? (
            <div className="flex items-center gap-3 text-xs flex-shrink-0">
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
          ) : (
            <span className="text-xs text-status-pending flex-shrink-0">
              pinned before schema capture; old schema unavailable
            </span>
          )}
        </div>
        <button
          onClick={onClose}
          aria-label="Close"
          className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors text-text-muted hover:text-text-primary"
        >
          <X size={16} />
        </button>
      </div>

      {/* Body - full width, long lines soft-wrap so nothing hides behind a
          horizontal scrollbar */}
      <div className="flex-1 min-h-0 overflow-auto scrollbar-dark">
        <table className="w-full font-mono text-xs border-collapse">
          <thead>
            <tr className="border-b border-border/30 sticky top-0 bg-surface-elevated z-10">
              {isDiff && (
                <th className="text-right px-3 py-2 text-text-muted font-medium w-14">Old</th>
              )}
              <th className="text-right px-3 py-2 text-text-muted font-medium w-14">New</th>
              <th className="text-left px-4 py-2 text-text-muted font-medium">Content</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((line, idx) => (
              <tr
                key={idx}
                className={cn(
                  'border-b border-border/10',
                  line.kind === 'added' && 'bg-status-running/[0.08]',
                  line.kind === 'removed' && 'bg-status-error/[0.08]',
                )}
              >
                {isDiff && (
                  <td
                    className={cn(
                      'px-3 py-0.5 text-right w-14 select-none align-top',
                      line.kind === 'removed' ? 'text-status-error/60' : 'text-text-muted/40',
                    )}
                  >
                    {line.oldLine ?? ''}
                  </td>
                )}
                <td
                  className={cn(
                    'px-3 py-0.5 text-right w-14 select-none align-top',
                    line.kind === 'added' ? 'text-status-running/60' : 'text-text-muted/40',
                  )}
                >
                  {line.newLine ?? ''}
                </td>
                <td
                  className={cn(
                    'px-4 py-0.5 whitespace-pre-wrap break-words',
                    line.kind === 'added' && 'text-status-running',
                    line.kind === 'removed' && 'text-status-error',
                    line.kind === 'same' && 'text-text-secondary',
                  )}
                >
                  {line.kind === 'removed' ? '- ' : line.kind === 'added' ? '+ ' : '  '}
                  {escapeNonPrintable(line.text)}
                </td>
              </tr>
            ))}
            {hidden > 0 && (
              <tr>
                <td
                  colSpan={isDiff ? 3 : 2}
                  className="px-4 py-2 text-text-muted italic"
                >
                  … {hidden} more lines; run `gridctl pins diff` for the full schemas
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>,
    document.body,
  );
}
