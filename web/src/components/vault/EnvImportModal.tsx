import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type DragEvent,
} from 'react';
import {
  AlertTriangle,
  Eye,
  EyeOff,
  FileUp,
  Upload,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { VariableTypeSelector } from './VariableTypeSelector';
import { parseEnv } from '../../lib/envParser';
import type { ParsedEnvEntry } from '../../lib/envParser';
import type {
  ImportVariableInput,
  Variable,
  VariableSet,
  VariableType,
} from '../../lib/api';

export interface EnvImportModalProps {
  onClose: () => void;
  onImport: (vars: ImportVariableInput[]) => Promise<{ imported: number }>;
  existingVariables: Variable[];
  sets: VariableSet[];
  // Optional initial set assignment for every parsed row. Used when the user
  // opens the modal while a set filter is active in the workspace.
  defaultSet?: string;
}

interface RowOverride {
  type?: VariableType;
  set?: string;
  isSecret?: boolean;
  skipped?: boolean;
  revealed?: boolean;
}

interface RowState {
  // Stable client id; not sent to the server.
  id: string;
  key: string;
  value: string;
  type: VariableType;
  set: string; // '' = unassigned
  isSecret: boolean;
  skipped: boolean;
  revealed: boolean;
}

function rowFromParsed(
  entry: ParsedEnvEntry,
  defaultSet: string,
  index: number,
  override?: RowOverride,
): RowState {
  return {
    id: `${entry.line}-${index}`,
    key: entry.key,
    value: entry.value,
    type: override?.type ?? entry.type,
    set: override?.set ?? defaultSet,
    // Default new imports to "secret" per Article XII secure default — the
    // user can toggle to plaintext per row if needed.
    isSecret: override?.isSecret ?? true,
    skipped: override?.skipped ?? false,
    revealed: override?.revealed ?? false,
  };
}

// Parent mounts this conditionally; unmount on close gives us free state
// reset and removes the need for an open/close-driven effect.
export function EnvImportModal({
  onClose,
  onImport,
  existingVariables,
  sets,
  defaultSet = '',
}: EnvImportModalProps) {
  const titleId = useId();
  const closeRef = useRef<HTMLButtonElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const panelRef = useFocusTrap<HTMLDivElement>({
    active: true,
    initialFocusRef: textareaRef,
  });

  const [text, setText] = useState('');
  // Per-key user overrides — survive re-parses so small textarea edits don't
  // wipe per-row tweaks. Keyed by variable key, not by parser line, since
  // line numbers shift as the user types.
  const [overrides, setOverrides] = useState<Record<string, RowOverride>>({});
  const [ignoredOpen, setIgnoredOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);

  const parsed = useMemo(() => parseEnv(text), [text]);

  const rows = useMemo<RowState[]>(
    () =>
      parsed.entries.map((entry, idx) =>
        rowFromParsed(entry, defaultSet, idx, overrides[entry.key]),
      ),
    [parsed.entries, defaultSet, overrides],
  );

  // Escape closes; click on backdrop closes; both common in this codebase.
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [onClose]);

  const existingKeys = useMemo(
    () => new Set(existingVariables.map((v) => v.key)),
    [existingVariables],
  );
  const setNames = useMemo(() => sets.map((s) => s.name), [sets]);

  const counts = useMemo(() => {
    let newCount = 0;
    let conflicts = 0;
    let skipped = 0;
    for (const r of rows) {
      const exists = existingKeys.has(r.key);
      if (r.skipped) {
        skipped++;
        continue;
      }
      if (exists) conflicts++;
      else newCount++;
    }
    return { newCount, conflicts, skipped };
  }, [rows, existingKeys]);

  const handleFile = useCallback(async (file: File) => {
    const content = await file.text();
    setText((prev) => (prev ? prev + '\n' + content : content));
  }, []);

  const onDrop = useCallback(
    (e: DragEvent<HTMLDivElement>) => {
      e.preventDefault();
      setDragOver(false);
      const file = e.dataTransfer.files?.[0];
      if (file) handleFile(file);
    },
    [handleFile],
  );

  const onFilePicked = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (file) handleFile(file);
      // Reset so the same file can be re-picked after editing.
      e.target.value = '';
    },
    [handleFile],
  );

  const updateRow = useCallback(
    (key: string, patch: RowOverride) => {
      setOverrides((prev) => ({
        ...prev,
        [key]: { ...(prev[key] ?? {}), ...patch },
      }));
    },
    [],
  );

  const handleImport = useCallback(async () => {
    const toSubmit = rows
      .filter((r) => !r.skipped)
      .map<ImportVariableInput>((r) => ({
        key: r.key,
        value: r.value,
        type: r.type,
        isSecret: r.isSecret,
        set: r.set || undefined,
      }));
    if (toSubmit.length === 0) {
      setSubmitError('Nothing to import — every row is skipped.');
      return;
    }
    setSubmitting(true);
    setSubmitError(null);
    try {
      await onImport(toSubmit);
      onClose();
    } catch (err) {
      setSubmitError(
        err instanceof Error ? err.message : 'Import failed',
      );
    } finally {
      setSubmitting(false);
    }
  }, [rows, onImport, onClose]);

  return (
    <div
      className={cn(
        'fixed inset-0 z-[60] animate-fade-in-scale',
        'bg-background/80 backdrop-blur-sm flex items-center justify-center',
      )}
    >
      <div className="absolute inset-0" onClick={onClose} />

      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={cn(
          'relative glass-panel-elevated rounded-xl shadow-lg',
          'w-[min(1100px,calc(100vw-2rem))] max-h-[min(840px,calc(100vh-2rem))] flex flex-col',
        )}
      >
        {/* Header */}
        <header className="flex-shrink-0 flex items-center justify-between px-5 py-3 border-b border-border-subtle">
          <div className="flex items-center gap-3">
            <div className="p-1.5 rounded-lg bg-primary/10 border border-primary/20">
              <FileUp size={14} className="text-primary" />
            </div>
            <div>
              <h2
                id={titleId}
                className="text-sm font-semibold text-text-primary tracking-tight"
              >
                Import variables
              </h2>
              <p className="text-[10px] text-text-muted uppercase tracking-[0.18em]">
                paste · drop · pick a .env file
              </p>
            </div>
          </div>
          <button
            ref={closeRef}
            onClick={onClose}
            className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors text-text-muted hover:text-text-primary"
            aria-label="Close import"
          >
            <X size={14} />
          </button>
        </header>

        {/* Body — two-column split on wide viewports, stacked otherwise */}
        <div className="flex-1 min-h-0 grid grid-cols-1 md:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)] overflow-hidden">
          {/* Left column: input */}
          <div className="flex flex-col min-h-0 border-r border-border-subtle">
            <div
              onDragOver={(e) => {
                e.preventDefault();
                setDragOver(true);
              }}
              onDragLeave={() => setDragOver(false)}
              onDrop={onDrop}
              className={cn(
                'flex-1 min-h-0 flex flex-col relative transition-colors',
                dragOver && 'bg-primary/5',
              )}
            >
              <textarea
                ref={textareaRef}
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder={`# Paste .env content or drop a file\nDATABASE_URL=postgres://localhost/app\nAPI_KEY="sk_live_..."`}
                spellCheck={false}
                className={cn(
                  'flex-1 min-h-0 w-full bg-transparent text-xs font-mono text-text-primary',
                  'placeholder:text-text-muted/40 focus:outline-none resize-none',
                  'px-4 py-4 leading-relaxed',
                )}
              />
              {dragOver && (
                <div className="pointer-events-none absolute inset-3 rounded-lg border-2 border-dashed border-primary/50 flex items-center justify-center bg-primary/5 text-primary text-xs">
                  <Upload size={14} className="mr-2" /> Drop the .env file
                </div>
              )}
            </div>

            <div className="flex-shrink-0 flex items-center justify-between gap-2 px-4 py-2 border-t border-border-subtle">
              <label className="flex items-center gap-1.5 text-[10px] text-text-muted hover:text-text-primary cursor-pointer transition-colors">
                <Upload size={11} />
                Pick a file
                <input
                  type="file"
                  accept=".env,text/plain"
                  onChange={onFilePicked}
                  className="hidden"
                />
              </label>
              <span className="text-[10px] text-text-muted font-mono">
                {text.length} chars
              </span>
            </div>
          </div>

          {/* Right column: preview */}
          <div className="flex flex-col min-h-0">
            {rows.length === 0 && parsed.ignored.length === 0 && (
              <div className="flex-1 flex items-center justify-center px-8 py-12 text-center">
                <div className="space-y-3">
                  <div className="mx-auto w-12 h-12 rounded-xl bg-primary/5 border border-primary/15 flex items-center justify-center">
                    <FileUp size={18} className="text-primary/50" />
                  </div>
                  <p className="text-xs text-text-muted leading-relaxed max-w-xs">
                    Paste a few <code className="font-mono text-text-secondary">KEY=value</code>{' '}
                    pairs to preview what will be imported.
                  </p>
                </div>
              </div>
            )}

            {rows.length > 0 && (
              <div className="flex-1 overflow-y-auto scrollbar-dark">
                <table className="w-full text-left">
                  <thead className="sticky top-0 bg-surface-elevated/95 backdrop-blur-sm">
                    <tr className="text-[10px] uppercase tracking-[0.18em] text-text-muted border-b border-border-subtle">
                      <th className="px-3 py-2 font-medium">Key</th>
                      <th className="px-2 py-2 font-medium">Value</th>
                      <th className="px-2 py-2 font-medium">Type</th>
                      <th className="px-2 py-2 font-medium">Set</th>
                      <th className="px-2 py-2 font-medium w-12 text-right">Skip</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((r) => {
                      const exists = existingKeys.has(r.key);
                      return (
                        <tr
                          key={r.id}
                          className={cn(
                            'border-b border-border-subtle/60 transition-opacity',
                            r.skipped && 'opacity-40',
                          )}
                        >
                          <td className="px-3 py-2 align-top">
                            <div className="flex items-center gap-2">
                              <span
                                className={cn(
                                  'text-xs font-mono font-medium text-text-primary',
                                  r.skipped && 'line-through',
                                )}
                              >
                                {r.key}
                              </span>
                              {exists && !r.skipped && (
                                <span
                                  className="text-[9px] font-mono uppercase tracking-wider px-1.5 py-0.5 rounded bg-status-pending/15 text-status-pending"
                                  title="A variable with this key already exists; importing will overwrite it"
                                >
                                  overwrites
                                </span>
                              )}
                            </div>
                          </td>
                          <td className="px-2 py-2 align-top">
                            <div className="flex items-center gap-1.5">
                              <code
                                className={cn(
                                  'text-[11px] font-mono text-text-secondary break-all min-w-0',
                                  r.skipped && 'line-through',
                                )}
                                style={{ wordBreak: 'break-all' }}
                              >
                                {r.revealed ? r.value : maskValue(r.value)}
                              </code>
                              {r.value.length > 0 && (
                                <button
                                  onClick={() =>
                                    updateRow(r.key, { revealed: !r.revealed })
                                  }
                                  className="flex-shrink-0 p-0.5 rounded text-text-muted hover:text-text-primary transition-colors"
                                  title={r.revealed ? 'Hide value' : 'Reveal value'}
                                  aria-label={r.revealed ? 'Hide value' : 'Reveal value'}
                                >
                                  {r.revealed ? <EyeOff size={11} /> : <Eye size={11} />}
                                </button>
                              )}
                            </div>
                          </td>
                          <td className="px-2 py-2 align-top">
                            <VariableTypeSelector
                              value={r.type}
                              onChange={(t) => updateRow(r.key, { type: t })}
                            />
                          </td>
                          <td className="px-2 py-2 align-top">
                            <select
                              value={r.set}
                              onChange={(e) =>
                                updateRow(r.key, { set: e.target.value })
                              }
                              className="bg-surface border border-border rounded-md px-1.5 py-0.5 text-[10px] text-text-secondary focus:border-primary/50 outline-none transition-colors"
                            >
                              <option value="">No set</option>
                              {setNames.map((name) => (
                                <option key={name} value={name}>
                                  {name}
                                </option>
                              ))}
                            </select>
                          </td>
                          <td className="px-2 py-2 align-top text-right">
                            <input
                              type="checkbox"
                              checked={r.skipped}
                              onChange={(e) =>
                                updateRow(r.key, { skipped: e.target.checked })
                              }
                              className="accent-status-error"
                              aria-label={`Skip ${r.key}`}
                            />
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}

            {parsed.ignored.length > 0 && (
              <div className="flex-shrink-0 border-t border-border-subtle">
                <button
                  onClick={() => setIgnoredOpen((v) => !v)}
                  className="w-full flex items-center gap-2 px-3 py-2 text-[10px] uppercase tracking-[0.18em] text-status-pending hover:bg-surface-highlight/40 transition-colors"
                >
                  <AlertTriangle size={11} />
                  {parsed.ignored.length} line
                  {parsed.ignored.length === 1 ? '' : 's'} ignored
                  <span className="ml-auto text-text-muted normal-case tracking-normal">
                    {ignoredOpen ? 'hide' : 'show'}
                  </span>
                </button>
                {ignoredOpen && (
                  <ul className="max-h-32 overflow-y-auto scrollbar-dark px-3 pb-3 space-y-1 text-[11px] font-mono">
                    {parsed.ignored.map((il) => (
                      <li
                        key={`${il.line}-${il.raw}`}
                        className="flex gap-2 text-text-muted"
                      >
                        <span className="flex-shrink-0 w-8 text-right text-status-error">
                          {il.line}
                        </span>
                        <code className="flex-1 break-all">{il.raw || '«empty»'}</code>
                        <span className="flex-shrink-0 text-text-muted/70 italic">
                          {il.reason}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </div>
        </div>

        {/* Footer */}
        <footer className="flex-shrink-0 flex items-center justify-between gap-3 px-5 py-3 border-t border-border-subtle">
          <div className="flex items-center gap-3 text-[11px] font-mono text-text-muted">
            <span>
              <span className="text-status-running font-semibold">{counts.newCount}</span>{' '}
              new
            </span>
            <span className="text-text-muted/40">·</span>
            <span>
              <span className="text-status-pending font-semibold">
                {counts.conflicts}
              </span>{' '}
              overwrite{counts.conflicts === 1 ? '' : 's'}
            </span>
            <span className="text-text-muted/40">·</span>
            <span>
              <span className="text-status-error font-semibold">{counts.skipped}</span>{' '}
              skipped
            </span>
          </div>

          <div className="flex items-center gap-3">
            {submitError && (
              <span className="text-[10px] text-status-error">{submitError}</span>
            )}
            <button
              onClick={onClose}
              className="px-3 py-1.5 text-xs text-text-secondary hover:text-text-primary rounded-lg transition-colors"
            >
              Cancel
            </button>
            <Button
              variant="primary"
              size="sm"
              onClick={handleImport}
              disabled={
                submitting || counts.newCount + counts.conflicts === 0
              }
            >
              <FileUp size={12} />
              {submitting
                ? 'Importing...'
                : counts.newCount + counts.conflicts > 0
                  ? `Import ${counts.newCount + counts.conflicts}`
                  : 'Import'}
            </Button>
          </div>
        </footer>
      </div>
    </div>
  );
}

function maskValue(value: string): string {
  if (value.length === 0) return '';
  // Cap at 12 dots so very long values don't blow out the column width.
  const visible = Math.min(value.length, 12);
  return '•'.repeat(visible);
}
