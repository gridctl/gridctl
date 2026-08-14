import { useEffect, useMemo, useState } from 'react';
import { Command } from 'cmdk';
import Fuse from 'fuse.js';
import {
  AlertCircle,
  AlertTriangle,
  ArrowLeft,
  Check,
  Edit3,
  Info,
  ListChecks,
  Loader2,
  Search,
} from 'lucide-react';
import { cn } from '../../../lib/cn';
import { ProbeError, type OpenAPIOperation } from '../../../lib/api';
import { useOpenAPIOperations } from '../../../hooks/useOpenAPIOperations';
import { useWizardStore } from '../../../stores/useWizardStore';
import {
  buildOperationsFilter,
  collectMethods,
  collectTags,
  deriveOperationsMode,
  describeSkipReason,
  formatOperationsCount,
  methodColorClass,
  operationRowLabel,
  selectableOperations,
  selectedOperationIds,
  type OperationsFilter,
  type OperationsMode,
} from '../../../lib/openapiOperations';

const labelClass = 'block text-xs text-text-secondary mb-1.5';
const inputClass =
  'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors';

// Above this many operations, exposing the whole spec is worth a nudge. Cursor
// drops tools past roughly 40 and VS Code hard-caps at 128, so a spec this size
// degrades clients well before it fails outright.
const LARGE_SPEC_ADVISORY_THRESHOLD = 50;

interface OperationsTLS {
  certFile?: string;
  keyFile?: string;
  caFile?: string;
  insecureSkipVerify?: boolean;
}

interface OperationsPickerProps {
  spec: string;
  tls?: OperationsTLS;
  operations: OperationsFilter | undefined;
  onChange: (operations: OperationsFilter | undefined) => void;
}

const MODE_OPTIONS: { mode: OperationsMode; label: string; help: string }[] = [
  {
    mode: 'all',
    label: 'All operations',
    help: 'Every operation becomes a tool. Operations added to the spec later are exposed automatically.',
  },
  {
    mode: 'include',
    label: 'Only selected',
    help: 'Only the operations you pick become tools. Operations added to the spec later are not exposed.',
  },
  {
    mode: 'exclude',
    label: 'All except selected',
    help: 'Everything except the operations you pick becomes a tool. Operations added to the spec later are exposed unless excluded.',
  },
];

/**
 * Pre-deploy picker for openapi.operations. Parses the spec on demand and lets
 * the operator choose which operations become MCP tools.
 *
 * The selection persists raw operationId values, never the sanitized tool
 * names: the backend filter matches the raw value, so a list of sanitized names
 * would silently match nothing for any spec whose IDs contain dots or spaces.
 * Both are shown per row for exactly that reason.
 */
export function OperationsPicker({ spec, tls, operations, onChange }: OperationsPickerProps) {
  const preview = useOpenAPIOperations();
  const setTotal = useWizardStore((s) => s.setOpenAPIOperationTotal);

  const [modeOverride, setModeOverride] = useState<OperationsMode | null>(null);
  const [manualMode, setManualMode] = useState(false);
  const [query, setQuery] = useState('');
  const [methodFilter, setMethodFilter] = useState<string | null>(null);
  const [tagFilter, setTagFilter] = useState<string | null>(null);
  // Set when a selection collapses to nothing, so the operator learns the
  // filter was dropped rather than silently reverting to "all".
  const [clearedNotice, setClearedNotice] = useState(false);

  const mode = modeOverride ?? deriveOperationsMode(operations);
  const selectedIds = useMemo(() => selectedOperationIds(operations), [operations]);
  const selected = useMemo(() => new Set(selectedIds), [selectedIds]);

  const allOperations = preview.operations;
  const usable = useMemo(
    () => selectableOperations(allOperations ?? []),
    [allOperations],
  );
  const total = usable.length;
  const loaded = allOperations !== null;

  // Publish the total for the Review step's summary row. Cleared on unmount so
  // a stale count cannot outlive the OpenAPI form.
  useEffect(() => {
    if (loaded) setTotal(total);
  }, [loaded, total, setTotal]);
  useEffect(() => () => setTotal(null), [setTotal]);

  const specChanged = loaded && preview.loadedSpec !== spec;

  const methods = useMemo(() => collectMethods(usable), [usable]);
  const tags = useMemo(() => collectTags(usable), [usable]);

  // A local Fuse instance rather than useFuzzySearch: that hook hardcodes
  // name/description keys, and this list must match on operationId, path,
  // summary, and tag.
  const fuse = useMemo(
    () =>
      new Fuse(usable, {
        keys: ['operation_id', 'tool_name', 'path', 'summary', 'tags'],
        threshold: 0.4,
      }),
    [usable],
  );

  const visible = useMemo(() => {
    const searched = query.trim() ? fuse.search(query).map((r) => r.item) : usable;
    return searched.filter((op) => {
      if (methodFilter && op.method.toUpperCase() !== methodFilter) return false;
      if (tagFilter && !(op.tags ?? []).includes(tagFilter)) return false;
      return true;
    });
  }, [fuse, query, usable, methodFilter, tagFilter]);

  const commit = (nextMode: OperationsMode, ids: string[]) => {
    onChange(buildOperationsFilter(nextMode, ids));
  };

  const changeMode = (nextMode: OperationsMode) => {
    setModeOverride(nextMode);
    setClearedNotice(false);
    // Carry the current selection across include/exclude so switching does not
    // silently discard a list the operator just built.
    commit(nextMode, nextMode === 'all' ? [] : selectedIds);
  };

  const toggle = (operationId: string) => {
    const next = new Set(selected);
    if (next.has(operationId)) next.delete(operationId);
    else next.add(operationId);
    const ids = [...next];

    // Requirement: deselecting to nothing in Only-selected mode drops the
    // filter and returns to All, rather than writing include: [] — which means
    // "expose everything" to the backend while reading as a whitelist.
    if (ids.length === 0 && mode === 'include') {
      setModeOverride('all');
      setClearedNotice(true);
      onChange(undefined);
      return;
    }
    setClearedNotice(false);
    commit(mode, ids);
  };

  const selectAllVisible = () => {
    if (mode === 'all') return;
    const next = new Set(selected);
    visible.forEach((op) => next.add(op.operation_id));
    setClearedNotice(false);
    commit(mode, [...next]);
  };

  const clearSelection = () => {
    if (mode === 'include') {
      setModeOverride('all');
      setClearedNotice(true);
    }
    onChange(undefined);
  };

  const handleLoad = () => {
    void preview.load({ spec, tls: hasTLS(tls) ? tls : undefined });
  };

  const deleteSelected = useMemo(() => {
    if (mode !== 'include') return [];
    return usable.filter((op) => selected.has(op.operation_id) && op.method.toUpperCase() === 'DELETE');
  }, [mode, usable, selected]);

  const everythingSelected = mode === 'include' && total > 0 && selectedIds.length === total;

  if (manualMode) {
    return (
      <div aria-label="Operations Picker">
        <div className="flex items-center justify-between mb-1.5">
          <label className={cn(labelClass, 'mb-0')}>Operations Filter</label>
          <button
            type="button"
            onClick={() => setManualMode(false)}
            className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
          >
            <ArrowLeft size={10} />
            Back to picker
          </button>
        </div>
        <ModeControl mode={mode} onChange={changeMode} />
        {mode !== 'all' && (
          <ManualEntry
            mode={mode}
            value={selectedIds}
            onChange={(ids) => commit(mode, ids)}
          />
        )}
        <StaticCaption />
      </div>
    );
  }

  return (
    <div aria-label="Operations Picker" aria-busy={preview.loading}>
      <div className="flex items-center justify-between mb-1.5">
        <label className={cn(labelClass, 'mb-0')}>Operations Filter</label>
        <button
          type="button"
          onClick={() => setManualMode(true)}
          className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <Edit3 size={10} />
          Enter operation IDs manually
        </button>
      </div>

      <ModeControl mode={mode} onChange={changeMode} />

      {clearedNotice && (
        <p role="status" className="mb-2 text-[10px] text-text-muted">
          Selection cleared, so the operations filter was removed — all operations in the spec will
          become tools. Pick operations again to filter.
        </p>
      )}

      {!loaded ? (
        <div className="rounded-lg border border-dashed border-border/40 bg-background/30 px-4 py-5 text-center space-y-3">
          <p className="text-[11px] text-text-muted leading-relaxed">
            {spec
              ? 'Load the spec to search and select operations by name, path, method, or tag.'
              : 'Enter a spec URL or file path above, then load it to pick operations.'}
          </p>
          <div className="flex flex-col items-center gap-1.5">
            <button
              type="button"
              onClick={handleLoad}
              disabled={!spec || preview.loading}
              aria-label="Load operations from the OpenAPI spec"
              className={cn(
                'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium transition-colors',
                'bg-primary/20 text-primary border border-primary/30 hover:bg-primary/30',
                'disabled:opacity-60 disabled:cursor-not-allowed',
              )}
            >
              {preview.loading ? (
                <>
                  <Loader2 size={11} className="animate-spin" />
                  Loading…
                </>
              ) : (
                <>
                  <ListChecks size={11} />
                  Load operations
                </>
              )}
            </button>
          </div>
          {preview.error && <PreviewErrorPanel error={preview.error} onRetry={spec ? handleLoad : undefined} />}
          <button
            type="button"
            onClick={() => setManualMode(true)}
            className="inline-flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
          >
            <Edit3 size={10} />
            Enter operation IDs manually
          </button>
        </div>
      ) : (
        <div className="space-y-2">
          {specChanged && (
            <p role="status" className="text-[10px] text-status-pending">
              Spec changed since this list was loaded. Reload to refresh.
            </p>
          )}

          {preview.error && <PreviewErrorPanel error={preview.error} onRetry={handleLoad} />}

          {mode === 'all' && total > LARGE_SPEC_ADVISORY_THRESHOLD && (
            <div
              role="status"
              className="flex items-start gap-2 rounded-md border border-status-pending/40 bg-status-pending/[0.05] px-3 py-2"
            >
              <Info size={12} className="text-status-pending flex-shrink-0 mt-0.5" />
              <p className="text-[10px] text-text-muted">
                {total} operations is a large tool surface. Many clients degrade well before this —
                consider selecting the subset this server actually needs.
              </p>
            </div>
          )}

          {deleteSelected.length > 0 && (
            <div
              role="status"
              className="flex items-start gap-2 rounded-md border border-status-pending/40 bg-status-pending/[0.05] px-3 py-2"
            >
              <AlertTriangle size={12} className="text-status-pending flex-shrink-0 mt-0.5" />
              <p className="text-[10px] text-text-muted">
                {deleteSelected.length} selected operation{deleteSelected.length > 1 ? 's use' : ' uses'}{' '}
                DELETE. Those tools can destroy data once exposed.
              </p>
            </div>
          )}

          {everythingSelected && (
            <div
              role="status"
              className="flex items-start gap-2 rounded-md border border-border/40 bg-background/40 px-3 py-2"
            >
              <Info size={12} className="text-secondary flex-shrink-0 mt-0.5" />
              <div className="flex-1 min-w-0">
                <p className="text-[10px] text-text-muted">
                  Every operation is selected. An enumerated list pins this server to today's spec —
                  operations added later will not be exposed. Using no filter tracks the spec instead.
                </p>
                <button
                  type="button"
                  onClick={() => changeMode('all')}
                  className="mt-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
                >
                  Switch to All operations
                </button>
              </div>
            </div>
          )}

          <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
            <div className="flex items-center gap-2 px-3 py-2 text-[10px] text-text-muted border-b border-border/30">
              <span className="text-text-secondary font-medium">
                {formatOperationsCount(mode, selectedIds.length, total)}
              </span>
              {mode !== 'all' && (
                <div className="ml-auto flex items-center gap-2">
                  <button
                    type="button"
                    onClick={selectAllVisible}
                    aria-label="Select all visible operations"
                    className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
                  >
                    Select all
                  </button>
                  <span className="text-border" aria-hidden="true">
                    ·
                  </span>
                  <button
                    type="button"
                    onClick={clearSelection}
                    aria-label="Clear selected operations"
                    className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
                  >
                    Clear
                  </button>
                </div>
              )}
            </div>

            {/* Filter chips precede the search input in DOM order so keyboard
                and screen-reader users meet the coarse filters first. */}
            {(methods.length > 1 || tags.length > 0) && (
              <div className="flex flex-wrap items-center gap-1 px-3 py-2 border-b border-border/30">
                {methods.length > 1 &&
                  methods.map((m) => (
                    <FilterChip
                      key={`method-${m}`}
                      label={m}
                      pressed={methodFilter === m}
                      onClick={() => setMethodFilter(methodFilter === m ? null : m)}
                      className={methodColorClass(m)}
                    />
                  ))}
                {tags.map((t) => (
                  <FilterChip
                    key={`tag-${t}`}
                    label={t}
                    pressed={tagFilter === t}
                    onClick={() => setTagFilter(tagFilter === t ? null : t)}
                  />
                ))}
              </div>
            )}

            <Command shouldFilter={false} label="Operations Picker" className="flex flex-col">
              <div className="flex items-center gap-2 px-3 py-2 border-b border-border/30">
                <Search size={12} className="text-text-muted flex-shrink-0" aria-hidden="true" />
                <Command.Input
                  value={query}
                  onValueChange={setQuery}
                  placeholder="Search operations..."
                  aria-label="Search operations"
                  className="flex-1 bg-transparent outline-none text-xs text-text-primary placeholder:text-text-muted/60"
                />
              </div>

              <Command.List className="max-h-72 overflow-y-auto" aria-label="Available operations">
                <Command.Empty>
                  <p className="text-[11px] text-text-muted/60 italic py-4 px-3 text-center">
                    No operations match the current search and filters
                  </p>
                </Command.Empty>
                {visible.map((op) => (
                  <OperationRow
                    key={`${op.method}-${op.path}-${op.operation_id}`}
                    op={op}
                    selectable={mode !== 'all'}
                    checked={selected.has(op.operation_id)}
                    onToggle={() => toggle(op.operation_id)}
                  />
                ))}
              </Command.List>
            </Command>

            <div className="px-3 py-2 border-t border-border/30 space-y-1">
              <p aria-live="polite" className="sr-only">
                {visible.length} of {total} operations shown
              </p>
              <p className="text-[10px] text-text-muted">
                Showing {visible.length} of {total}
                {preview.loadedAt && ` · loaded ${formatRelativeTime(preview.loadedAt)}`}
              </p>
              {preview.skippedCount > 0 && (
                <p className="text-[10px] text-status-pending">
                  {preview.skippedCount} operation{preview.skippedCount > 1 ? 's' : ''} in this spec
                  cannot become tools and {preview.skippedCount > 1 ? 'are' : 'is'} not listed
                  {firstSkipReason(allOperations) ? ` (${firstSkipReason(allOperations)})` : ''}.
                </p>
              )}
              <button
                type="button"
                onClick={handleLoad}
                disabled={preview.loading}
                className="text-[10px] text-secondary hover:text-secondary-light transition-colors disabled:opacity-60"
              >
                {preview.loading ? 'Reloading…' : 'Reload spec'}
              </button>
            </div>
          </div>
        </div>
      )}

      <StaticCaption />
    </div>
  );
}

// The three-way mode control. Each option states what happens to operations
// added to the spec later, which is the difference operators most often miss.
function ModeControl({
  mode,
  onChange,
}: {
  mode: OperationsMode;
  onChange: (mode: OperationsMode) => void;
}) {
  const active = MODE_OPTIONS.find((o) => o.mode === mode);
  return (
    <div className="mb-2">
      <div className="flex flex-wrap gap-2" role="group" aria-label="Operations filter mode">
        {MODE_OPTIONS.map((opt) => (
          <button
            key={opt.mode}
            type="button"
            onClick={() => onChange(opt.mode)}
            aria-pressed={mode === opt.mode}
            className={cn(
              'px-3 py-1 rounded-lg text-[10px] font-medium transition-all border',
              mode === opt.mode
                ? 'bg-primary/10 border-primary/30 text-primary'
                : 'bg-white/[0.02] border-white/[0.06] text-text-muted hover:text-text-secondary',
            )}
          >
            {opt.label}
          </button>
        ))}
      </div>
      {active && <p className="mt-1.5 text-[10px] text-text-muted">{active.help}</p>}
    </div>
  );
}

function FilterChip({
  label,
  pressed,
  onClick,
  className,
}: {
  label: string;
  pressed: boolean;
  onClick: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={pressed}
      className={cn(
        'px-1.5 py-0.5 rounded border text-[9px] font-mono uppercase transition-colors',
        pressed
          ? 'bg-primary/15 border-primary/40 text-primary'
          : 'bg-white/[0.02] border-white/[0.06] hover:border-border',
        !pressed && className,
      )}
    >
      {label}
    </button>
  );
}

function OperationRow({
  op,
  selectable,
  checked,
  onToggle,
}: {
  op: OpenAPIOperation;
  selectable: boolean;
  checked: boolean;
  onToggle: () => void;
}) {
  const differs = op.tool_name && op.tool_name !== op.operation_id;
  return (
    <Command.Item
      value={`${op.operation_id} ${op.path}`}
      onSelect={selectable ? onToggle : undefined}
      aria-checked={selectable ? checked : undefined}
      aria-label={operationRowLabel(op)}
      className={cn(
        'flex items-start gap-2.5 px-3 py-2 select-none outline-none transition-colors',
        selectable ? 'cursor-pointer hover:bg-surface-highlight/50' : 'cursor-default',
        '[&[data-selected=true]]:bg-primary/[0.06]',
        checked && 'bg-primary/[0.03]',
      )}
    >
      {selectable && (
        <div
          className={cn(
            'mt-0.5 w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors',
            checked ? 'bg-primary/30 border-primary' : 'border-border/60 bg-background/50',
          )}
          aria-hidden="true"
        >
          {checked && <Check size={10} className="text-primary" />}
        </div>
      )}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span
            className={cn('text-[9px] font-mono font-semibold uppercase w-12 flex-shrink-0', methodColorClass(op.method))}
          >
            {op.method.toUpperCase()}
          </span>
          <span className="text-[11px] font-mono text-text-secondary truncate">{op.path}</span>
          {op.deprecated && (
            <span className="text-[9px] px-1 rounded bg-status-pending/15 text-status-pending flex-shrink-0">
              deprecated
            </span>
          )}
        </div>
        {op.summary && <div className="text-[10px] text-text-muted truncate mt-0.5">{op.summary}</div>}
        <div className="text-[10px] font-mono text-text-muted/80 truncate mt-0.5">
          {op.operation_id}
          {differs && <span className="text-text-muted/60"> → {op.tool_name}</span>}
        </div>
      </div>
    </Command.Item>
  );
}

// Manual entry stays available for specs that cannot be fetched while
// authoring: unexpanded ${VAR} references, or file paths that only resolve on
// the daemon host.
function ManualEntry({
  mode,
  value,
  onChange,
}: {
  mode: OperationsMode;
  value: string[];
  onChange: (ids: string[]) => void;
}) {
  return (
    <div>
      <textarea
        value={value.join('\n')}
        onChange={(e) => onChange(e.target.value.split('\n').map((s) => s.trim()).filter(Boolean))}
        placeholder="One operationId per line"
        aria-label={mode === 'include' ? 'Operation IDs to include' : 'Operation IDs to exclude'}
        rows={4}
        className={cn(inputClass, 'font-mono resize-none')}
      />
      <p className="mt-1 text-[10px] text-text-muted">
        Use the raw operationId from the spec, not the generated tool name.
      </p>
    </div>
  );
}

// Stated wherever the control appears: this filter runs at generation time, so
// what it removes cannot be restored from the runtime Tools Whitelist.
function StaticCaption() {
  return (
    <p className="mt-2 text-[10px] text-text-muted/80">
      Operations excluded here never become tools, so they cannot be re-enabled from the Tools
      Whitelist after deploy. The Tools Whitelist in Advanced is the reversible runtime filter.
    </p>
  );
}

function PreviewErrorPanel({
  error,
  onRetry,
}: {
  error: ProbeError | Error;
  onRetry?: () => void;
}) {
  const isProbeErr = error instanceof ProbeError;
  const code = isProbeErr ? error.code : 'error';
  return (
    <div
      role="alert"
      className="flex items-start gap-2 rounded-md border border-status-error/40 bg-status-error/[0.05] px-3 py-2 text-left"
    >
      <AlertCircle size={12} className="text-status-error flex-shrink-0 mt-0.5" />
      <div className="flex-1 min-w-0 space-y-1">
        <p className="text-[11px] text-status-error font-medium">{error.message}</p>
        {isProbeErr && error.hint && <p className="text-[10px] text-text-muted">{error.hint}</p>}
        {code !== 'invalid_request' && (
          <p className="text-[10px] text-text-muted">
            If the spec is only reachable from the gateway host, or its path still contains an
            unexpanded variable, enter operation IDs manually instead.
          </p>
        )}
        {onRetry && (
          <button
            type="button"
            onClick={onRetry}
            aria-label="Retry loading the spec"
            className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
          >
            Retry
          </button>
        )}
      </div>
    </div>
  );
}

function hasTLS(tls: OperationsTLS | undefined): boolean {
  if (!tls) return false;
  return !!(tls.certFile || tls.keyFile || tls.caFile || tls.insecureSkipVerify);
}

function firstSkipReason(operations: OpenAPIOperation[] | null): string | null {
  const skipped = (operations ?? []).find((op) => op.skipped);
  return skipped ? describeSkipReason(skipped.skip_reason) : null;
}

function formatRelativeTime(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const deltaSec = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (deltaSec < 5) return 'just now';
  if (deltaSec < 60) return `${deltaSec}s ago`;
  if (deltaSec < 3600) return `${Math.floor(deltaSec / 60)}m ago`;
  return `${Math.floor(deltaSec / 3600)}h ago`;
}
