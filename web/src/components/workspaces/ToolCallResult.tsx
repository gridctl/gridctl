import { useEffect, useMemo, useRef, useState, type MouseEvent, type ReactNode } from 'react';
import { ArrowUpRight, Copy } from 'lucide-react';
import { cn } from '../../lib/cn';
import { copyTextToClipboard } from '../../lib/clipboard';
import type { ToolCallEnvelope } from '../../lib/api';
import {
  RESULT_CHAR_CAP,
  capSections,
  copyText,
  describeOutcome,
  outcomeLine,
  resultSections,
  sectionsLength,
  type OutcomeClass,
  type ResultSection,
} from '../../lib/toolCallPresentation';

const OUTCOME_CLASS: Record<OutcomeClass, string> = {
  completed: 'border-status-running/30 text-status-running',
  tool_error: 'border-status-error/40 text-status-error',
  not_run: 'border-status-pending/40 text-status-pending',
};

const LINK_CLASS =
  'inline-flex items-center gap-1 text-3xs text-secondary hover:text-secondary-light transition-colors rounded';

// A real anchor so modified clicks open a new tab; an unmodified primary
// click is handed to onNavigate (the router, behind the unsaved-edits guard).
export function RouteLink({
  path,
  onNavigate,
  className = LINK_CLASS,
  children,
}: {
  path: string;
  onNavigate?: (path: string) => void;
  className?: string;
  children: ReactNode;
}) {
  const handleClick = (e: MouseEvent<HTMLAnchorElement>) => {
    if (!onNavigate) return;
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    onNavigate(path);
  };
  return (
    <a href={path} onClick={handleClick} className={className}>
      {children}
    </a>
  );
}

// ToolCallResult renders one tools/call envelope as plain text. Tool output
// is attacker-controlled, so content never becomes markup, links, or media,
// and oversized text is sliced before it reaches React.
export function ToolCallResult({
  envelope,
  elapsedMs,
  stale,
  serverName,
  toolName,
  fontSize,
  onNavigate,
}: {
  envelope: ToolCallEnvelope;
  elapsedMs: number;
  stale: boolean;
  serverName: string;
  toolName: string;
  fontSize: number;
  onNavigate?: (path: string) => void;
}) {
  const view = useMemo(() => describeOutcome(envelope), [envelope]);
  const sections = useMemo(() => resultSections(envelope.result), [envelope]);
  const total = useMemo(() => sectionsLength(sections), [sections]);
  const capped = useMemo(() => capSections(sections, RESULT_CHAR_CAP), [sections]);
  const [showAll, setShowAll] = useState(false);
  const [copied, setCopied] = useState<'copied' | 'failed' | null>(null);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => {
    if (copiedTimer.current) clearTimeout(copiedTimer.current);
  }, []);

  const overCap = total > RESULT_CHAR_CAP;
  const shown = showAll || !overCap ? sections : capped;
  const tracePath = envelope.trace_id
    ? `/traces?trace=${encodeURIComponent(envelope.trace_id)}`
    : `/traces?q=${encodeURIComponent(toolName)}&server=${encodeURIComponent(serverName)}`;

  const handleCopy = async () => {
    try {
      await copyTextToClipboard(copyText(envelope.result));
      setCopied('copied');
    } catch {
      setCopied('failed');
    }
    if (copiedTimer.current) clearTimeout(copiedTimer.current);
    copiedTimer.current = setTimeout(() => setCopied(null), 1500);
  };

  return (
    <div
      data-testid="tool-call-result"
      data-outcome={view.kind}
      className={cn('space-y-2', stale && 'opacity-60')}
    >
      {stale && <p className="text-3xs text-text-muted italic">Previous result (stale)</p>}
      <div className={cn('rounded-md border px-2 py-1.5 space-y-1', OUTCOME_CLASS[view.kind])}>
        <p className="text-2xs font-medium">{outcomeLine(envelope, elapsedMs)}</p>
        {view.sentences.map((sentence) => (
          <p key={sentence} className="text-2xs text-text-secondary leading-relaxed">
            {sentence}
          </p>
        ))}
        {view.pinsLink && (
          <RouteLink path={`/pins?server=${encodeURIComponent(serverName)}`} onNavigate={onNavigate}>
            <ArrowUpRight size={10} aria-hidden="true" />
            Review in Pins
          </RouteLink>
        )}
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <RouteLink path={tracePath} onNavigate={onNavigate}>
          <ArrowUpRight size={10} aria-hidden="true" />
          {envelope.trace_id ? 'View trace' : 'View traces for this tool'}
        </RouteLink>
        {envelope.result && (
          <button type="button" onClick={() => void handleCopy()} className={cn(LINK_CLASS, 'ml-auto')}>
            <Copy size={10} aria-hidden="true" />
            {copied === 'copied' ? 'Copied' : copied === 'failed' ? 'Copy failed' : 'Copy result'}
          </button>
        )}
      </div>

      {sections.length > 0 && (
        <div
          data-testid="tool-call-result-body"
          className={cn('space-y-2', showAll && overCap && 'max-h-[60vh] overflow-auto scrollbar-dark')}
        >
          {shown.map((section, i) => (
            <ResultBlock key={i} section={section} fontSize={fontSize} />
          ))}
        </div>
      )}
      {overCap && !showAll && (
        <div className="flex items-center gap-2">
          <p className="text-3xs text-text-muted">
            Showing the first {RESULT_CHAR_CAP.toLocaleString('en-US')} of {total.toLocaleString('en-US')} characters
          </p>
          <button type="button" onClick={() => setShowAll(true)} className={LINK_CLASS}>
            Show all
          </button>
        </div>
      )}
    </div>
  );
}

function ResultBlock({ section, fontSize }: { section: ResultSection; fontSize: number }) {
  if (section.kind === 'summary') {
    return <p className="text-2xs font-mono text-text-muted break-all">{section.text}</p>;
  }
  return (
    <div className="space-y-1">
      {section.label && (
        <p className="text-3xs uppercase tracking-[0.18em] text-text-muted">{section.label}</p>
      )}
      <pre
        className="rounded-md border border-border/30 bg-background/80 px-2 py-1.5 font-mono text-text-secondary whitespace-pre-wrap break-words"
        style={{ fontSize }}
      >
        {section.text}
      </pre>
    </div>
  );
}
