import { useEffect, useCallback, useRef } from 'react';
import { GitCompareArrows, AlertCircle, CheckCircle2, AlertTriangle, RefreshCw } from 'lucide-react';
import { useSpecStore } from '../../stores/useSpecStore';
import { fetchStackSpec, fetchStackHealth, validateStackSpec } from '../../lib/api';
import type { ValidationIssue, IssueSeverity } from '../../types';

// YAML syntax highlighting tokens
interface HighlightedToken {
  text: string;
  className: string;
}

function highlightYAMLLine(line: string): HighlightedToken[] {
  const tokens: HighlightedToken[] = [];

  // Comment lines
  if (line.trimStart().startsWith('#')) {
    return [{ text: line, className: 'text-text-muted/60 italic' }];
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
      // Vault references
      else if (value.includes('${vault:')) {
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

function severityIcon(severity: IssueSeverity) {
  switch (severity) {
    case 'error':
      return <span className="w-2 h-2 rounded-full bg-status-error shadow-[0_0_6px_var(--color-status-error-glow)]" />;
    case 'warning':
      return <span className="w-2 h-2 rounded-full bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]" />;
    case 'info':
      return <span className="w-2 h-2 rounded-full bg-status-running shadow-[0_0_6px_var(--color-status-running-glow)]" />;
  }
}

function getLineIssues(lineNum: number, issues: ValidationIssue[]): ValidationIssue[] {
  return issues.filter((i) => {
    // Match field names that reference line content
    // Field format: "servers[0].name" or "name" etc.
    return i.field.includes(`[${lineNum}]`) || i.field === `line:${lineNum}`;
  });
}

export function SpecTab() {
  const spec = useSpecStore((s) => s.spec);
  const specLoading = useSpecStore((s) => s.specLoading);
  const specError = useSpecStore((s) => s.specError);
  const validation = useSpecStore((s) => s.validation);
  const openCompareModal = useSpecStore((s) => s.openCompareModal);
  const pollRef = useRef<ReturnType<typeof setInterval>>(null);

  const loadSpec = useCallback(async () => {
    const store = useSpecStore.getState();
    store.setSpecLoading(true);
    try {
      const [specData, healthData] = await Promise.all([
        fetchStackSpec(),
        fetchStackHealth(),
      ]);
      store.setSpec(specData);
      store.setHealth(healthData);

      const validationResult = await validateStackSpec(specData.content);
      store.setValidation(validationResult);
    } catch (err) {
      store.setSpecError(err instanceof Error ? err.message : 'Failed to load spec');
    } finally {
      store.setSpecLoading(false);
    }
  }, []);

  useEffect(() => {
    loadSpec();
    pollRef.current = setInterval(loadSpec, 10000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [loadSpec]);

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
  const issues = validation?.issues ?? [];

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-border/30 flex-shrink-0">
        <div className="flex items-center gap-3">
          <span className="text-xs text-text-muted font-mono">{spec.path}</span>
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

        <button
          onClick={openCompareModal}
          className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-all duration-200 text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40"
        >
          <GitCompareArrows size={12} />
          Compare to running
        </button>
      </div>

      {/* Spec content */}
      <div className="flex-1 overflow-auto scrollbar-dark font-mono text-xs leading-relaxed">
        <table className="w-full border-collapse">
          <tbody>
            {lines.map((line, idx) => {
              const lineNum = idx + 1;
              const lineIssues = getLineIssues(lineNum, issues);
              const highestSeverity = lineIssues.length > 0
                ? lineIssues.some((i) => i.severity === 'error') ? 'error'
                  : lineIssues.some((i) => i.severity === 'warning') ? 'warning'
                    : 'info'
                : null;

              return (
                <tr
                  key={lineNum}
                  className="group"
                >
                  {/* Line number */}
                  <td className="w-12 pr-3 text-right text-text-muted/40 select-none align-top py-px">
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
                    {highestSeverity ? (
                      <span
                        className="inline-flex items-center justify-center cursor-help relative group/anno"
                        title={lineIssues.map((i) => `${i.severity}: ${i.message}`).join('\n')}
                      >
                        {severityIcon(highestSeverity as IssueSeverity)}
                        {/* Tooltip */}
                        <span className="absolute right-6 top-0 z-10 hidden group-hover/anno:block w-64 px-3 py-2 rounded-lg bg-surface-elevated border border-border/50 shadow-lg text-xs text-text-secondary whitespace-normal">
                          {lineIssues.map((issue, j) => (
                            <div key={j} className="flex items-start gap-1.5 mb-1 last:mb-0">
                              {severityIcon(issue.severity)}
                              <span>{issue.message}</span>
                            </div>
                          ))}
                        </span>
                      </span>
                    ) : (
                      // Valid line dot (subtle)
                      line.trim() && !line.trimStart().startsWith('#') && (
                        <span className="w-1.5 h-1.5 rounded-full bg-status-running/20 inline-block" />
                      )
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
