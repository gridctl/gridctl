import { cn } from '../../lib/cn';
import { MAX_DIFF_LINES } from '../../lib/diff';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { classifyUnifiedDiff, type UnifiedLine } from '../../lib/unifiedDiff';

interface UnifiedDiffViewProps {
  diff: string;
  ariaLabel: string;
  maxHeightClass?: string;
}

// UnifiedDiffView renders a unified diff verbatim, one classified line at a
// time. Each line is escaped after splitting so hidden characters show, and
// rendering stops at MAX_DIFF_LINES so a huge diff cannot hang the page.
export function UnifiedDiffView({ diff, ariaLabel, maxHeightClass = 'max-h-96' }: UnifiedDiffViewProps) {
  const lines = classifyUnifiedDiff(diff);
  const visible = lines.length > MAX_DIFF_LINES ? lines.slice(0, MAX_DIFF_LINES) : lines;
  const omitted = lines.length - visible.length;
  return (
    <div
      className={cn(
        'rounded border border-border/40 bg-background/60 overflow-x-auto overflow-y-auto scrollbar-dark',
        maxHeightClass,
      )}
    >
      <pre className="text-2xs font-mono leading-relaxed p-2 whitespace-pre-wrap break-words" aria-label={ariaLabel}>
        {visible.map((line, i) => (
          <UnifiedDiffLine key={i} line={line} />
        ))}
      </pre>
      {omitted > 0 && (
        <p className="px-3 pb-2 text-3xs text-text-muted">
          {omitted} more line{omitted === 1 ? '' : 's'} not shown.
        </p>
      )}
    </div>
  );
}

function UnifiedDiffLine({ line }: { line: UnifiedLine }) {
  return (
    <span
      data-kind={line.kind}
      className={cn(
        'block px-1',
        line.kind === 'added' && 'bg-status-running/10 text-status-running',
        line.kind === 'removed' && 'bg-status-error/10 text-status-error',
        line.kind === 'context' && 'text-text-secondary',
        line.kind === 'hunk' && 'text-primary',
        (line.kind === 'header' || line.kind === 'note') && 'text-text-muted',
      )}
    >
      {escapeNonPrintable(line.text) || ' '}
    </span>
  );
}
