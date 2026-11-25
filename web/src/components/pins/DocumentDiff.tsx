import { useEffect, useMemo } from 'react';
import { cn } from '../../lib/cn';
import {
  diffLines,
  lineDiffUnaligned,
  MAX_DIFF_LINES,
  truncateBalanced,
  type DiffToken,
} from '../../lib/diff';
import { escapeNonPrintable } from '../../lib/nonPrintable';

// DocumentDiff renders a pinned document delta as a unified line-level view
// (prose, not JSON schemas: diffLines, not the schema panels). Each line is
// escaped individually AFTER splitting: escaping first would turn real
// newlines into literal \n and collapse the document onto one line.
//
// degraded reports that the view is not a complete aligned diff (the inputs
// were too large to align, or the lines were cut at the render cap), so the
// caller can offer the full document. It is reported from an effect because
// the caller holds it as state.
//
// tokens lets a caller that already diffed the pair (to judge the approval
// risk) pass its result instead of diffing twice; it must be
// diffLines(oldDocument, newDocument). fullDiffCommand names the CLI command
// that prints the untruncated diff.
export function DocumentDiff({
  label,
  oldDocument,
  newDocument,
  tokens: givenTokens,
  fullDiffCommand,
  onDegraded,
}: {
  label: string;
  oldDocument: string;
  newDocument: string;
  tokens?: DiffToken[];
  fullDiffCommand?: string;
  onDegraded?: (degraded: boolean) => void;
}) {
  const { tokens, unaligned } = useMemo(
    () => ({
      tokens: givenTokens ?? diffLines(oldDocument, newDocument),
      unaligned: lineDiffUnaligned(oldDocument, newDocument),
    }),
    [givenTokens, oldDocument, newDocument],
  );
  const truncated = tokens.length > MAX_DIFF_LINES;
  const degraded = unaligned || truncated;
  useEffect(() => {
    onDegraded?.(degraded);
  }, [onDegraded, degraded]);

  const changed = tokens.filter((t) => t.kind !== 'same').length;
  // Balanced: the oversize fallback lists every removal before any addition,
  // and a head slice would hide the new document entirely.
  const { visible: shown, hidden } = useMemo(() => truncateBalanced(tokens), [tokens]);
  const heading = escapeNonPrintable(label);
  return (
    <div className="space-y-1">
      <h4 className="text-3xs uppercase tracking-[0.18em] text-text-muted">
        {unaligned
          ? `${heading} (too large to align)`
          : `${heading} (${changed} ${changed === 1 ? 'line' : 'lines'} changed)`}
      </h4>
      {unaligned && (
        <p className="text-3xs text-text-muted">
          These documents are too large to compare line by line. The pinned text is shown as
          removed, followed by the current text as added.
        </p>
      )}
      <div className="rounded border border-border/40 bg-background/60 overflow-x-auto max-h-96 overflow-y-auto scrollbar-dark">
        <pre className="text-2xs font-mono leading-relaxed p-2 whitespace-pre-wrap break-words">
          {shown.map((t, i) => (
            <DiffLine key={i} token={t} />
          ))}
        </pre>
      </div>
      {hidden > 0 && (
        <p className="text-3xs text-text-muted">
          {hidden} more {hidden === 1 ? 'line' : 'lines'} not shown
          {fullDiffCommand ? (
            <>
              ; run <code className="font-mono">{escapeNonPrintable(fullDiffCommand)}</code> for
              the full diff.
            </>
          ) : (
            '.'
          )}
        </p>
      )}
    </div>
  );
}

function DiffLine({ token }: { token: DiffToken }) {
  const prefix = token.kind === 'added' ? '+ ' : token.kind === 'removed' ? '- ' : '  ';
  return (
    <span
      className={cn(
        'block px-1',
        token.kind === 'added' && 'bg-status-running/10 text-status-running',
        token.kind === 'removed' && 'bg-status-error/10 text-status-error',
        token.kind === 'same' && 'text-text-secondary',
      )}
    >
      {prefix}
      {escapeNonPrintable(token.text)}
    </span>
  );
}
