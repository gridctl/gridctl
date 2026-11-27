import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { GitCompareArrows, AlertCircle, CheckCircle2, AlertTriangle, FlaskConical, Info, RefreshCw } from 'lucide-react';
import { useSpecStore } from '../../stores/useSpecStore';
import { useStackStore } from '../../stores/useStackStore';
import { fetchStackSpec, fetchStackHealth, validateStackSpec, fetchStackExport } from '../../lib/api';
import type { IssueSeverity, ValidationIssue } from '../../types';
import {
  getLoadedYaml,
  groupIssuesByLine,
  highestSeverity,
  loadYaml,
  needsFieldResolution,
  resolveIssueLines,
  sortIssues,
  type ResolvedIssue,
  type YamlModule,
} from '../../lib/lineIssues';
import { cn } from '../../lib/cn';
import { useInterval } from '../../hooks/useInterval';

// YAML syntax highlighting tokens
interface HighlightedToken {
  text: string;
  className: string;
}

function highlightYAMLLine(line: string): HighlightedToken[] {
  const tokens: HighlightedToken[] = [];

  // Comment lines
  if (line.trimStart().startsWith('#')) {
    return [{ text: line, className: 'text-text-muted italic' }];
  }

  // Key-value pairs
  const kvMatch = line.match(/^(\s*)([\w.-]+)(:)(.*)/);
  if (kvMatch) {
    const [, indent, key, colon, rest] = kvMatch;
    if (indent) tokens.push({ text: indent, className: '' });
    tokens.push({ text: key, className: 'text-secondary-light' });
    tokens.push({ text: colon, className: 'text-text-muted' });

    if (rest.trim()) {
      const value = rest;
      // String values in quotes
      if (value.trim().startsWith('"') || value.trim().startsWith("'")) {
        tokens.push({ text: value, className: 'text-primary-light' });
      }
      // Boolean/null
      else if (/^\s*(true|false|null|~)$/i.test(value)) {
        tokens.push({ text: value, className: 'text-tertiary-light' });
      }
      // Numbers
      else if (/^\s*-?\d+(\.\d+)?$/.test(value.trim())) {
        tokens.push({ text: value, className: 'text-primary' });
      }
      // Variable store references (canonical ${var:KEY}; ${vault:KEY} still
      // accepted while the deprecation alias is in effect).
      else if (value.includes('${vault:') || value.includes('${var:')) {
        tokens.push({ text: value, className: 'text-status-pending' });
      }
      // Regular values
      else {
        tokens.push({ text: value, className: 'text-text-primary' });
      }
    }
    return tokens;
  }

  // List items
  const listMatch = line.match(/^(\s*)(- )(.*)/);
  if (listMatch) {
    const [, indent, dash, rest] = listMatch;
    if (indent) tokens.push({ text: indent, className: '' });
    tokens.push({ text: dash, className: 'text-text-muted' });
    tokens.push({ text: rest, className: 'text-text-primary' });
    return tokens;
  }

  return [{ text: line, className: 'text-text-primary' }];
}

const SEVERITY_LABEL: Record<IssueSeverity, string> = { error: 'Error', warning: 'Warning', info: 'Info' };
const SEVERITY_COLOR: Record<IssueSeverity, string> = {
  error: 'text-status-error',
  warning: 'text-status-pending',
  info: 'text-status-running',
};

function SeverityIcon({ severity, size = 12 }: { severity: IssueSeverity; size?: number }) {
  const Icon = severity === 'error' ? AlertCircle : severity === 'warning' ? AlertTriangle : Info;
  return <Icon size={size} aria-hidden="true" className={cn('flex-shrink-0', SEVERITY_COLOR[severity])} />;
}

function plural(count: number, word: string): string {
  return `${count} ${word}${count === 1 ? '' : 's'}`;
}

// "2 issues on line 14: 1 error, 1 warning"
function gutterLabel(lineNum: number, lineIssues: ResolvedIssue[]): string {
  const counts = (['error', 'warning', 'info'] as const)
    .map((sev) => [sev, lineIssues.filter((i) => i.severity === sev).length] as const)
    .filter(([, n]) => n > 0)
    .map(([sev, n]) => plural(n, sev));
  return `${plural(lineIssues.length, 'issue')} on line ${lineNum}: ${counts.join(', ')}`;
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

function withoutTrailingPeriod(message: string): string {
  return message.replace(/\.+$/, '');
}

const NO_ISSUES: ValidationIssue[] = [];

// Background refresh failures keep the last loaded copy on screen.
function RefreshBanner({ message, loading, onRetry }: { message: string; loading: boolean; onRetry: () => void }) {
  return (
    <div role="alert" className="mx-4 mt-2 flex items-start gap-2 rounded-md border border-status-pending/40 bg-status-pending/10 px-3 py-2 text-xs text-text-secondary flex-shrink-0">
      <AlertTriangle size={12} aria-hidden="true" className="mt-0.5 flex-shrink-0 text-status-pending" />
      <span className="flex-1 min-w-0 break-words">{message}</span>
      <button
        type="button"
        onClick={onRetry}
        disabled={loading}
        className="flex items-center gap-1 rounded-md px-2 py-0.5 font-medium text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40 disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-primary"
      >
        <RefreshCw size={11} aria-hidden="true" className={cn(loading && 'animate-spin')} />
        Retry
      </button>
    </div>
  );
}

export function SpecTab() {
  const featureDetails = useStackStore((s) => s.featureDetails);
  const spec = useSpecStore((s) => s.spec);
  const specLoading = useSpecStore((s) => s.specLoading);
  const specError = useSpecStore((s) => s.specError);
  const validation = useSpecStore((s) => s.validation);
  const validationError = useSpecStore((s) => s.validationError);
  const openCompareModal = useSpecStore((s) => s.openCompareModal);
  const exportPending = useRef(false);
  const loadPending = useRef(false);
  const yamlRef = useRef<HTMLDivElement>(null);
  const issuesHeadingId = useId();
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState('');
  const [exportNotice, setExportNotice] = useState('Export preserves authored references without resolving values. Review authored literals, including mixed reference/literal strings, before sharing.');
  const [yamlModule, setYamlModule] = useState<YamlModule | null>(getLoadedYaml);
  const [activeLine, setActiveLine] = useState<number | null>(null);

  const content = spec?.content ?? '';
  const issues = validation?.issues ?? NO_ISSUES;
  const needsYaml = useMemo(() => needsFieldResolution(issues), [issues]);

  // Field paths are located only once yaml is loaded; until then (or if the
  // fetch fails) server and message lines are shown.
  useEffect(() => {
    if (!needsYaml || yamlModule) return;
    let cancelled = false;
    loadYaml().then(
      (mod) => {
        if (!cancelled) setYamlModule(mod);
      },
      () => {},
    );
    return () => {
      cancelled = true;
    };
  }, [needsYaml, yamlModule]);

  const resolved = useMemo(
    () => resolveIssueLines(content, issues, yamlModule),
    [content, issues, yamlModule],
  );
  const sortedIssues = useMemo(() => sortIssues(resolved), [resolved]);
  const issuesByLine = useMemo(() => groupIssuesByLine(resolved), [resolved]);

  async function exportSpec() {
    if (exportPending.current) return;
    exportPending.current = true;
    setExporting(true);
    setExportError('');
    try {
      const result = await fetchStackExport();
      setExportNotice(result.notice);
      const url = URL.createObjectURL(new Blob([result.content], { type: 'application/yaml' }));
      const link = document.createElement('a');
      link.href = url;
      link.download = 'stack.yaml';
      document.body.appendChild(link);
      try {
        link.click();
      } finally {
        link.remove();
        URL.revokeObjectURL(url);
      }
    } catch (err) {
      setExportError(err instanceof Error ? err.message : 'Export failed; no download created.');
    } finally {
      exportPending.current = false;
      setExporting(false);
    }
  }

  // One load at a time, whether the poll or Retry asks. A spec or health
  // failure sets specError; a validation failure sets validationError and
  // keeps the previous result.
  const loadSpec = useCallback(async () => {
    if (loadPending.current) return;
    loadPending.current = true;
    const store = useSpecStore.getState();
    store.setSpecLoading(true);
    try {
      let content: string;
      try {
        const [specData, healthData] = await Promise.all([
          fetchStackSpec(),
          fetchStackHealth(),
        ]);
        store.setSpec(specData);
        store.setHealth(healthData);
        content = specData.content;
      } catch (err) {
        store.setSpecError(errorMessage(err, 'Failed to load spec'));
        return;
      }
      try {
        store.setValidation(await validateStackSpec(content));
      } catch (err) {
        store.setValidationError(errorMessage(err, 'Validation failed'));
      }
    } finally {
      loadPending.current = false;
      store.setSpecLoading(false);
    }
  }, []);

  useInterval(loadSpec, 10000);

  const retry = useCallback(() => {
    void loadSpec();
  }, [loadSpec]);

  const jumpToLine = useCallback((line: number) => {
    setActiveLine(line);
    const row = yamlRef.current?.querySelector(`[data-line="${line}"]`);
    const reduce = typeof window.matchMedia === 'function'
      && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    row?.scrollIntoView?.({ block: 'center', behavior: reduce ? 'auto' : 'smooth' });
  }, []);

  if (specLoading && !spec) {
    return (
      <div className="flex items-center justify-center h-full text-text-muted text-xs gap-2">
        <RefreshCw size={12} className="animate-spin" />
        Loading spec...
      </div>
    );
  }

  if (specError && !spec) {
    return (
      <div className="flex items-center justify-center h-full text-text-muted text-xs gap-2">
        <AlertCircle size={12} className="text-status-error" />
        {specError}
      </div>
    );
  }

  if (!spec) {
    return (
      <div className="flex items-center justify-center h-full text-text-muted text-xs">
        No stack spec available
      </div>
    );
  }

  const lines = spec.content.split('\n');
  // The highlight survives polls and ends when its line leaves the content.
  const currentLine = activeLine !== null && activeLine <= lines.length ? activeLine : null;
  const banner = specError
    ? { kind: 'spec', text: `Could not refresh the spec: ${withoutTrailingPeriod(specError)}. Showing the last loaded copy.` }
    : validationError
      ? { kind: 'validation', text: `Could not refresh validation: ${withoutTrailingPeriod(validationError)}. Issues below may be out of date.` }
      : null;

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center justify-between gap-2 px-4 py-2 border-b border-border/30 flex-shrink-0">
        <div className="flex flex-wrap min-w-0 items-center gap-3">
          <span className="text-xs text-text-muted font-mono break-all">{spec.path}</span>
          {validation && (
            <div className="flex items-center gap-1.5">
              {validation.valid ? (
                <span className="flex items-center gap-1 text-xs text-status-running">
                  <CheckCircle2 size={11} />
                  Valid
                </span>
              ) : (
                <>
                  {validation.errorCount > 0 && (
                    <span className="flex items-center gap-1 text-xs text-status-error">
                      <AlertCircle size={11} />
                      {validation.errorCount} error{validation.errorCount !== 1 ? 's' : ''}
                    </span>
                  )}
                  {validation.warningCount > 0 && (
                    <span className="flex items-center gap-1 text-xs text-status-pending">
                      <AlertTriangle size={11} />
                      {validation.warningCount} warning{validation.warningCount !== 1 ? 's' : ''}
                    </span>
                  )}
                </>
              )}
            </div>
          )}
        </div>

        <button type="button" onClick={exportSpec} disabled={exporting} aria-describedby="stack-export-notice"
          className="px-2.5 py-1 rounded-md text-xs font-medium text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40 disabled:opacity-50">
          {exporting ? 'Exporting...' : 'Export YAML'}
        </button>
        <button
          onClick={openCompareModal}
          className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-all duration-200 text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40"
        >
          <GitCompareArrows size={12} />
          Compare to running
        </button>
      </div>
      <p id="stack-export-notice" role="status" className="px-4 py-2 text-xs text-text-muted">{exportNotice} The raw spec below may contain authored credentials.</p>
      {exportError && <p role="alert" className="px-4 py-2 text-xs text-status-error break-words">{exportError}</p>}

      {banner && <RefreshBanner key={banner.kind} message={banner.text} loading={specLoading} onRetry={retry} />}

      {/* Enabled experimental flags — read-only rows (flags are configured
          in stack.yaml, or via GRIDCTL_EXPERIMENTAL_* env vars, and cannot
          be toggled here). Stage is conveyed as text plus the flask icon,
          never color alone. */}
      {featureDetails.length > 0 && (
        <div role="status" aria-label="Enabled experimental flags" className="px-4 py-2 border-b border-border/30 flex-shrink-0 space-y-1">
          {featureDetails.map((f) => (
            <div key={f.name} className="flex items-start gap-2 text-xs">
              <FlaskConical size={11} className="text-text-muted mt-0.5 flex-shrink-0" />
              <span className="font-mono text-text-secondary">{f.name}</span>
              <span className="px-1.5 rounded-full border border-border/50 text-text-muted uppercase tracking-wide text-3xs">
                {f.stage}
              </span>
              {f.description && <span className="text-text-muted">{f.description}</span>}
            </div>
          ))}
        </div>
      )}

      {sortedIssues.length > 0 && (
        <section aria-labelledby={issuesHeadingId} className="mt-2 border-y border-border/30 flex-shrink-0">
          <h3 id={issuesHeadingId} className="px-4 pt-2 pb-1 text-xs font-medium text-text-secondary">
            Issues <span className="text-text-muted font-normal">({sortedIssues.length})</span>
          </h3>
          <ul className="max-h-48 overflow-y-auto scrollbar-dark pb-1">
            {sortedIssues.map((issue, idx) => {
              const line = issue.resolvedLine;
              const rowContent = (
                <>
                  <span className={cn('flex items-center gap-1 w-20 flex-shrink-0 font-medium', SEVERITY_COLOR[issue.severity])}>
                    <SeverityIcon severity={issue.severity} />
                    {SEVERITY_LABEL[issue.severity]}
                  </span>
                  <span className="flex-1 min-w-0 space-x-2">
                    <span className="font-mono text-text-secondary break-all">{issue.field}</span>
                    <span className="text-text-primary break-words">{issue.message}</span>
                  </span>
                  {line !== null && (
                    <span className="flex-shrink-0 font-mono text-text-muted">line {line}</span>
                  )}
                </>
              );
              return (
                <li key={`${idx}-${issue.field}`}>
                  {line !== null ? (
                    <button
                      type="button"
                      onClick={() => jumpToLine(line)}
                      aria-current={line === currentLine ? 'true' : undefined}
                      className={cn(
                        'flex w-full items-start gap-2 px-4 py-1 text-left text-xs hover:bg-surface-highlight/40 focus-visible:outline-2 focus-visible:outline-primary',
                        line === currentLine && 'bg-primary/10',
                      )}
                    >
                      {rowContent}
                    </button>
                  ) : (
                    <div className="flex items-start gap-2 px-4 py-1 text-xs">{rowContent}</div>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      )}

      {/* Spec content */}
      <div ref={yamlRef} className="flex-1 overflow-auto scrollbar-dark font-mono text-xs leading-relaxed">
        <table className="w-full border-collapse">
          <tbody>
            {lines.map((line, idx) => {
              const lineNum = idx + 1;
              const lineIssues = issuesByLine.get(lineNum);
              const severity = lineIssues ? highestSeverity(lineIssues) : null;
              const active = lineNum === currentLine;

              return (
                <tr
                  key={lineNum}
                  data-line={lineNum}
                  aria-current={active ? 'true' : undefined}
                  className={cn('group', active && 'bg-primary/10')}
                >
                  {/* Line number */}
                  <td className="w-12 pr-3 text-right text-text-muted select-none align-top py-px">
                    {lineNum}
                  </td>

                  {/* Code */}
                  <td className="pr-8 py-px whitespace-pre">
                    {highlightYAMLLine(line).map((token, i) => (
                      <span key={i} className={token.className}>{token.text}</span>
                    ))}
                  </td>

                  {/* Annotation */}
                  <td className="w-8 text-center align-top py-px">
                    {lineIssues && severity && (
                      <span
                        role="img"
                        aria-label={gutterLabel(lineNum, lineIssues)}
                        title={lineIssues.map((i) => `${i.severity}: ${i.message}`).join('\n')}
                        className="inline-flex items-center justify-center cursor-help align-middle"
                      >
                        <SeverityIcon severity={severity} />
                      </span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
