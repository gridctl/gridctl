import { Fragment, useEffect, useMemo, useState, type ReactNode } from 'react';
import { AlertTriangle, FileDiff } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { HTTPError, previewStackAppend } from '../../../lib/api';
import { diffHunks, diffLines, isLineDiffOversized, MAX_DIFF_LINES, type DiffToken } from '../../../lib/diff';
import { escapeNonPrintable } from '../../../lib/nonPrintable';
import type { StackAppendPreview } from '../../../types';

type FragmentType = 'mcp-server' | 'resource';

interface PreviewState {
  key: string;
  preview: StackAppendPreview | null;
  status: number;
  message: string;
  issues: string[];
}

function SectionCard({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="bg-surface-overlay-2 border border-border-subtle rounded-xl p-4 space-y-2 text-xs">
      <div className="flex items-center gap-2 mb-1">
        <FileDiff size={14} className="text-primary" />
        <h3 className="text-xs font-medium text-text-secondary uppercase tracking-wider">{label}</h3>
      </div>
      {children}
    </div>
  );
}

function plural(count: number, noun: string): string {
  return `${count} ${count === 1 ? noun : `${noun}s`}`;
}

function DiffLine({ token }: { token: DiffToken }) {
  const prefix = token.kind === 'added' ? '+ ' : token.kind === 'removed' ? '- ' : '  ';
  return (
    <span
      className={cn(
        'flex px-1',
        token.kind === 'added' && 'bg-status-running/10 text-status-running',
        token.kind === 'removed' && 'bg-status-error/10 text-status-error',
        token.kind === 'same' && 'text-text-secondary',
      )}
    >
      <span className="w-8 flex-shrink-0 text-right text-text-muted select-none">{token.oldLine ?? ''}</span>
      <span className="w-8 flex-shrink-0 text-right text-text-muted select-none mr-2">{token.newLine ?? ''}</span>
      <span className="break-all">{prefix}{escapeNonPrintable(token.text)}</span>
    </span>
  );
}

function StackFileDiff({ preview, resourceType }: { preview: StackAppendPreview; resourceType: FragmentType }) {
  const oversized = isLineDiffOversized(preview.before, preview.after);
  const tokens = useMemo(
    () => (oversized ? [] : diffLines(preview.before, preview.after)),
    [oversized, preview.before, preview.after],
  );

  if (oversized) {
    const block = resourceType === 'mcp-server' ? 'the block shown under Exact server YAML' : 'the block shown in the YAML preview';
    return <p className="text-text-secondary">The stack file is too large to diff here. Deploy adds {block} and re-serializes the rest of the file.</p>;
  }

  const added = tokens.filter((t) => t.kind === 'added').length;
  const removed = tokens.filter((t) => t.kind === 'removed').length;
  if (added === 0 && removed === 0) {
    return <p className="text-text-secondary">No changes to the stack file.</p>;
  }

  const hunks = diffHunks(tokens, 3);
  const total = hunks.reduce((n, h) => n + h.length, 0);
  let budget = MAX_DIFF_LINES;
  const shown: DiffToken[][] = [];
  for (const hunk of hunks) {
    if (budget <= 0) break;
    shown.push(hunk.slice(0, budget));
    budget -= hunk.length;
  }

  return (
    <div className="space-y-2">
      <p className="text-text-secondary">
        {plural(added, 'line')} added
        {removed > 0 && `, ${removed} existing ${removed === 1 ? 'line' : 'lines'} rewritten`}
      </p>
      {removed > 0 && (
        <p className="text-text-secondary">
          Deploy re-serializes the whole file, so blank lines and indentation elsewhere can change. Comments and key order are kept.
        </p>
      )}
      <div role="region" tabIndex={0} className="max-h-64 overflow-auto rounded border border-border/40 bg-background/60 scrollbar-dark focus-visible:-outline-offset-2" aria-label="Stack file diff">
        <pre className="text-2xs font-mono leading-relaxed p-2 whitespace-pre-wrap">
          {shown.map((hunk, h) => (
            <Fragment key={h}>
              {h > 0 && <span className="block px-1 text-text-muted">...</span>}
              {hunk.map((token, i) => <DiffLine key={i} token={token} />)}
            </Fragment>
          ))}
        </pre>
      </div>
      {total > MAX_DIFF_LINES && (
        <p className="text-3xs text-text-muted">Diff truncated at {MAX_DIFF_LINES} lines.</p>
      )}
    </div>
  );
}

/**
 * States what Deploy writes for a server or resource, from a server-computed
 * preview of the append. `ready` is true once the step's own validation has
 * finished with no errors. `appendedName` is set when this exact YAML was
 * already appended, so Deploy only checks status and no preview is requested.
 */
export function DeployChangesSection({ yaml, resourceType, ready, appendedName = '' }: {
  yaml: string;
  resourceType: FragmentType;
  ready: boolean;
  appendedName?: string;
}) {
  const key = `${resourceType}\n${yaml}`;
  const fetchable = ready && !appendedName && yaml.trim().length > 0;
  const [state, setState] = useState<PreviewState>({ key: '', preview: null, status: 0, message: '', issues: [] });

  useEffect(() => {
    if (!fetchable) return;
    let active = true;
    previewStackAppend(yaml, resourceType)
      .then((preview) => {
        if (!active) return;
        setState({ key, preview, status: 0, message: '', issues: [] });
      })
      .catch((err) => {
        if (!active) return;
        const status = err instanceof HTTPError ? err.status : 0;
        const issues = err instanceof HTTPError && err.errors?.length ? err.errors : [];
        const message = err instanceof Error && err.message ? err.message : 'Preview failed';
        setState({ key, preview: null, status, message, issues });
      });
    return () => { active = false; };
  }, [fetchable, key, yaml, resourceType]);

  const current = fetchable && state.key === key ? state : null;
  const pending = fetchable && !current;
  const preview = current?.preview ?? null;
  const noun = resourceType === 'mcp-server' ? 'server' : 'resource';

  if (appendedName) {
    return (
      <SectionCard label="What Deploy changes">
        <p className="text-text-secondary">
          {appendedName} is already in the stack file. Check deployment reads its status and writes nothing.
        </p>
      </SectionCard>
    );
  }

  return (
    <SectionCard label="What Deploy changes">
      {preview ? (
        <>
          <p className="text-text-primary">
            Deploy adds the {noun} {preview.resourceName} to {resourceType === 'mcp-server' ? 'mcp-servers' : 'resources'} in{' '}
            <span className="font-mono break-all">{preview.path}</span>.
          </p>
          {preview.watching ? (
            <p className="text-text-secondary">
              {resourceType === 'mcp-server'
                ? 'The gateway is watching this file. It reloads after the write and tries to start the server. Review waits for the server to connect.'
                : 'The gateway is watching this file. It reloads after the write and tries to start the resource.'}
            </p>
          ) : (
            <p className="flex items-start gap-2 text-status-pending">
              <AlertTriangle size={14} className="flex-shrink-0 mt-0.5" aria-hidden="true" />
              <span>
                The gateway is not watching this file. Nothing starts until you use Reload Config or run <code className="font-mono">gridctl reload</code>. A reload applies every unapplied edit in the file.
              </span>
            </p>
          )}
          <StackFileDiff preview={preview} resourceType={resourceType} />
        </>
      ) : (
        <>
          <p className="text-text-secondary">Deploy adds this {noun} to the active stack file.</p>
          {current && current.issues.length > 0 && (
            <div className="space-y-1 text-status-error">
              <p>
                {current.status === 422
                  ? 'The stack file would be invalid after this change:'
                  : `Could not preview the change: ${current.message.replace(/\.$/, '')}.`}
              </p>
              <ul className="list-disc pl-5 space-y-0.5">
                {current.issues.map((issue, i) => <li key={i} className="break-words">{issue}</li>)}
              </ul>
            </div>
          )}
          {current && current.issues.length === 0 && (
            <p className="text-status-error">Could not preview the change: {current.message.replace(/\.$/, '')}.</p>
          )}
        </>
      )}
      <div aria-live="polite">
        {pending && <p className="text-text-muted">Checking the stack file...</p>}
      </div>
    </SectionCard>
  );
}

/** States what Save & Load writes for a whole stack. No request. */
export function SaveLoadChangesSection({ yaml, activeStack }: { yaml: string; activeStack: string }) {
  const name = useMemo(() => {
    const match = yaml.match(/^name:\s*(.+)$/m);
    return match ? match[1].trim() : '';
  }, [yaml]);

  return (
    <SectionCard label="What Save & Load changes">
      <p className="text-text-primary">
        {name ? (
          <>
            Save & Load writes <span className="font-mono break-all">{name}.yaml</span> to the stack library and replaces a saved stack with that name.
          </>
        ) : (
          'Save & Load writes the stack to the stack library under a generated name.'
        )}
      </p>
      <p className="text-text-secondary">
        {activeStack
          ? `${activeStack} is already loaded, so this stack is saved but not loaded.`
          : 'No stack is loaded, so the gateway loads this one.'}
      </p>
    </SectionCard>
  );
}
