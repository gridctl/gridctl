import { useState, useEffect, useRef, useCallback } from 'react';
import { AlertCircle, AlertTriangle, CheckCircle2, Loader2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { validateStackSpec } from '../../lib/api';
import type { ValidationIssue } from '../../types';

interface YAMLPreviewProps {
  yaml: string;
  className?: string;
}

// Syntax highlighting for YAML
function highlightYAML(yaml: string): Array<{ lineNum: number; html: string }> {
  return yaml.split('\n').map((line, i) => {
    let html = line
      // Vault references (most specific — process before anything else)
      .replace(/(\$\{vault:[^}]+\})/, '<span class="text-tertiary font-medium">$1</span>')
      // Quoted strings (must run before key regex to avoid matching HTML class names)
      .replace(/"([^"]*)"/, '<span class="text-status-running">"$1"</span>')
      // Comments
      .replace(/(#.*)$/, '<span class="text-text-muted/50 italic">$1</span>')
      // Keys (word followed by colon)
      .replace(/^(\s*)([\w][\w.-]*)(:)/, '$1<span class="text-secondary">$2</span><span class="text-text-muted">$3</span>')
      // Array items
      .replace(/^(\s*)(-)(\s)/, '$1<span class="text-primary">$2</span>$3')
      // Numbers
      .replace(/:\s+(\d+)$/gm, ': <span class="text-primary-light">$1</span>')
      // Booleans
      .replace(/:\s+(true|false)$/gm, ': <span class="text-primary-light">$1</span>');
    return { lineNum: i + 1, html };
  });
}

export function YAMLPreview({ yaml, className }: YAMLPreviewProps) {
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [validating, setValidating] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null);
  const abortRef = useRef<AbortController | null>(null);

  const validate = useCallback(async (content: string) => {
    if (!content.trim()) {
      setIssues([]);
      return;
    }

    abortRef.current?.abort();
    abortRef.current = new AbortController();

    setValidating(true);
    try {
      const result = await validateStackSpec(content);
      setIssues(result.issues || []);
    } catch {
      // Validation failed silently — network error or abort
    } finally {
      setValidating(false);
    }
  }, []);

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => validate(yaml), 500);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [yaml, validate]);

  const lines = highlightYAML(yaml);
  const errorCount = issues.filter((i) => i.severity === 'error').length;
  const warningCount = issues.filter((i) => i.severity === 'warning').length;

  // Build a map of field → issues for inline annotations
  const issuesByField = new Map<string, ValidationIssue[]>();
  issues.forEach((issue) => {
    const existing = issuesByField.get(issue.field) || [];
    existing.push(issue);
    issuesByField.set(issue.field, existing);
  });

  return (
    <div className={cn('flex flex-col h-full', className)}>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border/30 flex-shrink-0">
        <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
          YAML Preview
        </span>
        <div className="flex items-center gap-2">
          {validating && (
            <Loader2 size={12} className="text-text-muted animate-spin" />
          )}
          {!validating && errorCount === 0 && warningCount === 0 && yaml.trim() && (
            <div className="flex items-center gap-1 text-status-running">
              <CheckCircle2 size={12} />
              <span className="text-[10px] font-medium">Valid</span>
            </div>
          )}
          {errorCount > 0 && (
            <div className="flex items-center gap-1 text-status-error">
              <AlertCircle size={12} />
              <span className="text-[10px] font-medium">{errorCount}</span>
            </div>
          )}
          {warningCount > 0 && (
            <div className="flex items-center gap-1 text-status-pending">
              <AlertTriangle size={12} />
              <span className="text-[10px] font-medium">{warningCount}</span>
            </div>
          )}
        </div>
      </div>

      {/* YAML Content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {yaml.trim() ? (
          <div className="font-mono text-[11px] leading-[1.7]">
            {lines.map(({ lineNum, html }) => (
              <div key={lineNum} className="flex group hover:bg-white/[0.02]">
                <span className="w-8 flex-shrink-0 text-right pr-3 text-text-muted/40 select-none text-[10px] leading-[1.87]">
                  {lineNum}
                </span>
                <span
                  className="flex-1 px-2 whitespace-pre"
                  dangerouslySetInnerHTML={{ __html: html || '&nbsp;' }}
                />
              </div>
            ))}
          </div>
        ) : (
          <div className="flex items-center justify-center h-full text-text-muted/50 text-xs">
            Fill out the form to see a live YAML preview
          </div>
        )}
      </div>

      {/* Validation Issues */}
      {issues.length > 0 && (
        <div className="border-t border-border/30 max-h-32 overflow-y-auto scrollbar-dark">
          {issues.map((issue, i) => (
            <div
              key={i}
              className={cn(
                'flex items-start gap-2 px-4 py-1.5 text-[10px]',
                issue.severity === 'error'
                  ? 'text-status-error bg-status-error/5'
                  : 'text-status-pending bg-status-pending/5',
              )}
            >
              {issue.severity === 'error' ? (
                <AlertCircle size={10} className="flex-shrink-0 mt-0.5" />
              ) : (
                <AlertTriangle size={10} className="flex-shrink-0 mt-0.5" />
              )}
              <span>
                <span className="font-medium">{issue.field}</span>
                <span className="text-text-muted mx-1">—</span>
                {issue.message}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
