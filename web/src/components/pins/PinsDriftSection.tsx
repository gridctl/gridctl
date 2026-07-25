import { useEffect, useMemo, useState } from 'react';
import {
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Loader2,
  LockOpen,
  Maximize2,
  Minus,
  Plus,
  RefreshCw,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { usePinsStore } from '../../stores/usePinsStore';
import {
  approveServerPins,
  fetchPinsDiff,
  fetchServerPins,
  type PinsChangeKind,
  type PinsDiff,
  type PinsToolDiff,
} from '../../lib/api';
import { escapeNonPrintable, shortPinHash } from '../../lib/nonPrintable';
import {
  diffLines,
  diffSide,
  diffWords,
  prettySchema,
  MAX_DIFF_LINES,
  type DiffToken,
} from '../../lib/diff';
import { FindingsList } from './PinFindings';
import { SchemaDiffModal } from './SchemaDiffModal';
import { showToast } from '../ui/Toast';

// Stable DOM ids: the workspace scrolls to the section for ?view=drift and
// focuses the Approve button from the rail's Enter binding.
export const DRIFT_SECTION_ID = 'pins-drift-section';
export const APPROVE_BUTTON_ID = 'pins-approve-button';

// Human labels for the wire change kinds.
const CHANGE_KIND_LABELS: Record<PinsChangeKind, string> = {
  description: 'description',
  input_schema: 'input schema',
  output_schema: 'output schema',
  schema_uncaptured: 'schema (old uncaptured)',
};

// ---------------------------------------------------------------------------
// Drift diff + informed approve
// ---------------------------------------------------------------------------

export function DriftSection({ serverName }: { serverName: string }) {
  const [diff, setDiff] = useState<PinsDiff | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isApproving, setIsApproving] = useState(false);
  // Bumped by Retry and by a failed approve so the effect refetches.
  const [attempt, setAttempt] = useState(0);

  // No reset needed on server change: the detail pane is keyed by server
  // name, so this section remounts (with null state) whenever the selection
  // moves. Retry resets state in its click handler before bumping `attempt`.
  useEffect(() => {
    let cancelled = false;
    fetchPinsDiff(serverName)
      .then((d) => {
        if (!cancelled) setDiff(d);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load diff');
      });
    return () => {
      cancelled = true;
    };
  }, [serverName, attempt]);

  const reloadDiff = () => {
    setDiff(null);
    setError(null);
    setAttempt((n) => n + 1);
  };

  const handleApprove = async () => {
    if (!diff) return;
    setIsApproving(true);
    try {
      // Bind the approval to the reviewed snapshot: the gateway rejects with
      // 409 if the live definitions no longer hash to what was rendered here.
      await approveServerPins(serverName, diff.live_server_hash);
      const updated = await fetchServerPins();
      usePinsStore.getState().setPins(updated);
      showToast('success', `Pins approved for ${serverName}`);
    } catch (err) {
      showToast('error', `Failed to approve: ${err instanceof Error ? err.message : 'Unknown error'}`);
      // The definitions may have changed again since review; reload the diff
      // so the user re-reviews the current state instead of a stale one.
      reloadDiff();
    } finally {
      setIsApproving(false);
    }
  };

  const changeCount =
    (diff?.modified_tools.length ?? 0) +
    (diff?.new_tools.length ?? 0) +
    (diff?.removed_tools.length ?? 0);

  return (
    <section
      id={DRIFT_SECTION_ID}
      className="rounded-lg border border-status-pending/30 bg-status-pending/[0.04] px-4 py-3 space-y-3"
      aria-label={`Schema drift for ${serverName}`}
    >
      <div className="flex items-center gap-2">
        <LockOpen size={12} className="text-status-pending flex-shrink-0" />
        <h3 className="text-xs font-medium text-status-pending">Schema drift</h3>
        <span className="text-[10px] text-text-muted">
          {diff ? `${changeCount} ${changeCount === 1 ? 'change' : 'changes'}` : ''}
        </span>
        <button
          id={APPROVE_BUTTON_ID}
          onClick={handleApprove}
          disabled={isApproving || diff === null}
          title={
            diff === null
              ? 'Review the changes below before approving'
              : 'Re-pin the live definitions shown below'
          }
          className={cn(
            'ml-auto flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-all duration-200',
            isApproving || diff === null
              ? 'text-text-muted bg-surface-highlight/30 cursor-not-allowed'
              : 'text-status-running bg-status-running/10 border border-status-running/20 hover:bg-status-running/20',
          )}
        >
          {isApproving ? (
            <>
              <RefreshCw size={10} className="animate-spin" />
              Approving…
            </>
          ) : (
            <>
              <CheckCircle2 size={10} />
              Approve {diff ? `${changeCount} ${changeCount === 1 ? 'change' : 'changes'}` : ''}
            </>
          )}
        </button>
      </div>

      {error && (
        <div role="alert" className="flex items-center gap-2 text-[11px] text-status-error">
          <span className="min-w-0">{error}</span>
          <button
            onClick={reloadDiff}
            className="flex-shrink-0 inline-flex items-center gap-1 rounded-md border border-border/40 bg-background/40 px-2 py-0.5 text-[10px] text-text-secondary hover:text-text-primary hover:border-border transition-colors"
          >
            <RefreshCw size={10} />
            Retry
          </button>
        </div>
      )}

      {!diff && !error && (
        <p className="flex items-center gap-2 text-[11px] text-text-muted">
          <Loader2 size={11} className="animate-spin" />
          Comparing pinned definitions against live tools…
        </p>
      )}

      {diff && (
        <div className="space-y-3">
          {diff.modified_tools.map((d) => (
            <ModifiedToolCard
              key={d.name}
              diff={d}
              // The per-pair DP guard does not bound the tool-count dimension
              // a hostile server controls; past this many modified tools the
              // description rows render plain instead of word-diffed so the
              // review panel cannot be made to freeze the tab on mount.
              enableWordDiff={diff.modified_tools.length <= 25}
            />
          ))}

          {diff.new_tools.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-text-muted">
              <Plus size={11} className="text-status-running" />
              <span>New tools (pinned on approve):</span>
              {diff.new_tools.map((n) => (
                <span key={n} className="font-mono text-text-secondary">
                  {escapeNonPrintable(n)}
                </span>
              ))}
            </div>
          )}

          {diff.removed_tools.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-text-muted">
              <Minus size={11} className="text-status-error" />
              <span>Removed from server:</span>
              {diff.removed_tools.map((n) => (
                <span key={n} className="font-mono text-text-secondary">
                  {escapeNonPrintable(n)}
                </span>
              ))}
            </div>
          )}

          {changeCount === 0 && (
            <p className="text-[11px] text-text-muted">
              The live definitions match the pins again; approving will simply re-verify.
            </p>
          )}
        </div>
      )}
    </section>
  );
}

// ModifiedToolCard renders one drifted tool: change-kind chips, the
// description delta (word-level highlight, or an explicit "description
// unchanged" line so a schema-only drift never shows two identical prose
// rows), per-kind schema diffs, the group-override advisory, and scan
// findings. A diff from an older daemon carries no change_kinds and degrades
// to the description-only view.
function ModifiedToolCard({
  diff: d,
  enableWordDiff,
}: {
  diff: PinsToolDiff;
  enableWordDiff: boolean;
}) {
  const kinds = d.change_kinds ?? [];
  const descriptionUnchanged = kinds.length > 0 && !kinds.includes('description');
  const hasSchemaPanels =
    kinds.includes('input_schema') ||
    kinds.includes('output_schema') ||
    kinds.includes('schema_uncaptured');

  // Word-level diff of the descriptions; null when either side is empty,
  // equal, or the pair is too large to diff (plain rows render instead).
  const wordDiff = useMemo(() => {
    if (!enableWordDiff) return null;
    if (!d.old_description || !d.new_description) return null;
    if (d.old_description === d.new_description) return null;
    return diffWords(d.old_description, d.new_description);
  }, [enableWordDiff, d.old_description, d.new_description]);

  return (
    // On wide centers the card splits per tool: review prose left, schema
    // panels right, so the JSON walls stop interrupting the reading flow.
    // The split is per card (never a global column) so the schema-to-tool
    // association stays unambiguous; below the container threshold it stacks
    // exactly as before. Container query, not viewport: the rail is
    // resizable, so only the card's own width is meaningful.
    <div className="rounded-md border border-border/40 bg-background/60 px-3 py-2 @container">
      <div
        className={cn(
          'space-y-1.5',
          hasSchemaPanels &&
            '@3xl:grid @3xl:grid-cols-[minmax(0,5fr)_minmax(0,6fr)] @3xl:gap-x-4 @3xl:space-y-0',
        )}
      >
        <div className="space-y-1.5 @3xl:sticky @3xl:top-2 @3xl:self-start min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <div className="text-xs font-mono text-text-primary">{escapeNonPrintable(d.name)}</div>
            {kinds.map((k) => (
              <span
                key={k}
                className="text-[10px] px-1.5 py-0.5 rounded bg-status-pending/10 text-status-pending border border-status-pending/20"
              >
                {CHANGE_KIND_LABELS[k] ?? k}
              </span>
            ))}
          </div>

          {descriptionUnchanged ? (
            <div className="flex items-start gap-2 text-[11px]">
              <span className="flex-shrink-0 font-mono text-text-muted">
                {shortPinHash(d.old_hash)} → {shortPinHash(d.new_hash)}
              </span>
              <span className="text-text-muted italic">description unchanged</span>
            </div>
          ) : (
            <>
              <DiffRow
                kind="old"
                hash={d.old_hash}
                description={d.old_description}
                tokens={wordDiff ? diffSide(wordDiff, 'old') : undefined}
              />
              <DiffRow
                kind="new"
                hash={d.new_hash}
                description={d.new_description}
                tokens={wordDiff ? diffSide(wordDiff, 'new') : undefined}
              />
            </>
          )}

          {kinds.includes('schema_uncaptured') && (
            <p className="text-[11px] text-status-pending">
              Pinned before schema capture; the old schema is unavailable. Review the new schema
              before approving.
            </p>
          )}

          {(d.groups_rewriting?.length ?? 0) > 0 && (
            <p className="text-[11px] text-text-muted">
              Also rewritten by groups:{' '}
              {d.groups_rewriting!.map((g, i) => (
                <span key={g}>
                  {i > 0 && ', '}
                  <span className="font-mono text-text-secondary">{escapeNonPrintable(g)}</span>
                </span>
              ))}{' '}
              – review group overrides against the new definition.
            </p>
          )}

          <FindingsList findings={d.findings} />
        </div>

        {hasSchemaPanels && (
          <div className="space-y-1.5 min-w-0 mt-1.5 @3xl:mt-0">
            {kinds.includes('input_schema') && (
              <SchemaDiffBlock
                label="Input schema changed"
                toolName={d.name}
                oldSchema={d.old_input_schema ?? ''}
                newSchema={d.new_input_schema ?? ''}
              />
            )}
            {kinds.includes('output_schema') && (
              <SchemaDiffBlock
                label="Output schema changed"
                toolName={d.name}
                oldSchema={d.old_output_schema ?? ''}
                newSchema={d.new_output_schema ?? ''}
              />
            )}
            {kinds.includes('schema_uncaptured') && (
              <>
                <SchemaBlock
                  label="New input schema"
                  toolName={d.name}
                  schema={d.new_input_schema ?? ''}
                />
                <SchemaBlock
                  label="New output schema"
                  toolName={d.name}
                  schema={d.new_output_schema ?? ''}
                />
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// One side of a before/after pair. Descriptions render as plain text with
// control characters escaped - they can carry prompt-injection payloads and
// must never be interpreted as markup. When a word diff is available the
// changed tokens are marked with del/ins semantics; every token is still
// escaped individually.
function DiffRow({
  kind,
  hash,
  description,
  tokens,
}: {
  kind: 'old' | 'new';
  hash: string;
  description: string;
  tokens?: DiffToken[];
}) {
  return (
    <div className="flex items-start gap-2 text-[11px]">
      <span
        className={cn(
          'flex-shrink-0 w-8 font-mono uppercase',
          kind === 'old' ? 'text-status-error/80' : 'text-status-running/80',
        )}
      >
        {kind}
      </span>
      <span className="flex-shrink-0 font-mono text-text-muted">{shortPinHash(hash)}</span>
      <span className="min-w-0 text-text-secondary whitespace-pre-wrap break-words">
        {tokens ? (
          tokens.map((t, idx) =>
            t.kind === 'same' ? (
              <span key={idx}>{escapeNonPrintable(t.text)}</span>
            ) : t.kind === 'removed' ? (
              <del key={idx} className="no-underline rounded-sm bg-status-error/15 text-status-error">
                {escapeNonPrintable(t.text)}
              </del>
            ) : (
              <ins key={idx} className="no-underline rounded-sm bg-status-running/15 text-status-running">
                {escapeNonPrintable(t.text)}
              </ins>
            ),
          )
        ) : description ? (
          escapeNonPrintable(description)
        ) : (
          <em className="text-text-muted/60">no description</em>
        )}
      </span>
    </div>
  );
}

// SchemaDiffBlock renders the old/new canonical schemas as a collapsible
// line diff. Open by default: a schema-only drift's delta must be visible
// without a click, or the approve is blind again. Schema text is
// attacker-controlled and every line passes through escapeNonPrintable.
function SchemaDiffBlock({
  label,
  toolName,
  oldSchema,
  newSchema,
}: {
  label: string;
  toolName: string;
  oldSchema: string;
  newSchema: string;
}) {
  const [open, setOpen] = useState(true);
  const [expanded, setExpanded] = useState(false);
  const lines = useMemo(
    () => diffLines(prettySchema(oldSchema), prettySchema(newSchema)),
    [oldSchema, newSchema],
  );
  const truncated = lines.length > MAX_DIFF_LINES;
  // A plain head slice would hide the entire added side when the oversize
  // fallback (all removals, then all additions) exceeds the cap - a red-only
  // wall with the new schema invisible is a blind approve again. Split the
  // budget across the boundary in that shape; interleaved LCS output keeps
  // the simple slice.
  const visible = useMemo(() => {
    if (!truncated) return lines;
    const firstAdded = lines.findIndex((l) => l.kind === 'added');
    if (firstAdded > MAX_DIFF_LINES / 2) {
      return [
        ...lines.slice(0, MAX_DIFF_LINES / 2),
        ...lines.slice(firstAdded, firstAdded + MAX_DIFF_LINES / 2),
      ];
    }
    return lines.slice(0, MAX_DIFF_LINES);
  }, [lines, truncated]);

  return (
    <div className="rounded-md border border-border/30 bg-surface/40">
      <div className="flex items-center">
        <button
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="flex-1 min-w-0 flex items-center gap-1.5 px-2 py-1 text-[11px] text-text-secondary hover:text-text-primary transition-colors"
        >
          {open ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
          {label}
        </button>
        <button
          onClick={() => setExpanded(true)}
          title="Open the full-width schema diff"
          aria-label={`Expand ${label.toLowerCase()} for ${toolName}`}
          className="flex-shrink-0 p-1 mr-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
        >
          <Maximize2 size={11} />
        </button>
      </div>
      {open && (
        <pre className="px-2 pb-2 text-[10px] font-mono leading-relaxed overflow-x-auto scrollbar-dark">
          {visible.map((line, idx) => (
            <div
              key={idx}
              className={cn(
                'whitespace-pre',
                line.kind === 'removed' && 'bg-status-error/10 text-status-error',
                line.kind === 'added' && 'bg-status-running/10 text-status-running',
                line.kind === 'same' && 'text-text-muted',
              )}
            >
              {line.kind === 'removed' ? '- ' : line.kind === 'added' ? '+ ' : '  '}
              {escapeNonPrintable(line.text)}
            </div>
          ))}
          {truncated && (
            <div className="text-text-muted italic">
              … {lines.length - MAX_DIFF_LINES} more lines; run `gridctl pins diff` for the full
              schemas
            </div>
          )}
        </pre>
      )}
      {expanded && (
        <SchemaDiffModal
          title={`${toolName} - ${label.toLowerCase()}`}
          oldSchema={oldSchema}
          newSchema={newSchema}
          onClose={() => setExpanded(false)}
        />
      )}
    </div>
  );
}

// SchemaBlock renders a single schema (the uncaptured-old case, where there
// is nothing to diff against). Escaping runs per line, after the pretty-print
// split: escaping the whole blob would turn its own newlines into literal
// \n sequences and collapse the schema onto one line.
function SchemaBlock({
  label,
  toolName,
  schema,
}: {
  label: string;
  toolName: string;
  schema: string;
}) {
  const [open, setOpen] = useState(true);
  const [expanded, setExpanded] = useState(false);
  const lines = useMemo(() => prettySchema(schema).split('\n'), [schema]);
  const truncated = lines.length > MAX_DIFF_LINES;
  const visible = truncated ? lines.slice(0, MAX_DIFF_LINES) : lines;

  return (
    <div className="rounded-md border border-border/30 bg-surface/40">
      <div className="flex items-center">
        <button
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="flex-1 min-w-0 flex items-center gap-1.5 px-2 py-1 text-[11px] text-text-secondary hover:text-text-primary transition-colors"
        >
          {open ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
          {label}
        </button>
        <button
          onClick={() => setExpanded(true)}
          title="Open the full-width schema view"
          aria-label={`Expand ${label.toLowerCase()} for ${toolName}`}
          className="flex-shrink-0 p-1 mr-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
        >
          <Maximize2 size={11} />
        </button>
      </div>
      {open && (
        <pre className="px-2 pb-2 text-[10px] font-mono leading-relaxed text-text-muted overflow-x-auto scrollbar-dark">
          {visible.map((line, idx) => (
            <div key={idx} className="whitespace-pre">
              {escapeNonPrintable(line)}
            </div>
          ))}
          {truncated && (
            <div className="text-text-muted italic">
              … {lines.length - MAX_DIFF_LINES} more lines; run `gridctl pins diff` for the full
              schema
            </div>
          )}
        </pre>
      )}
      {expanded && (
        <SchemaDiffModal
          title={`${toolName} - ${label.toLowerCase()}`}
          newSchema={schema}
          onClose={() => setExpanded(false)}
        />
      )}
    </div>
  );
}
