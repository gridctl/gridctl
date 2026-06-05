import {
  useCallback,
  useEffect,
  useState,
  type ComponentType,
  type ReactNode,
} from 'react';
import {
  ArrowUpRight,
  Copy,
  Eye,
  EyeOff,
  Lock,
  Pencil,
  RefreshCw,
  Trash2,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { CodeViewer } from '../ui/CodeViewer';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { IconButton } from '../ui/IconButton';
import { showToast } from '../ui/Toast';
import { InspectorHeader } from '../inspector';
import { generateSecret } from '../../lib/generateSecret';
import { useRevealedValues } from '../../hooks/useRevealedValues';
import { ConsumerList } from './ConsumerList';
import { isNavigable } from './consumerHelpers';
import { SecretGenerator } from './SecretGenerator';
import { VariableTypeBadge } from './VariableTypeBadge';
import { VariableValueInput } from './VariableValueInput';
import { parseBool, validateVariableInput } from './variableTypeHelpers';
import type {
  Consumer,
  UpdateVariableInput,
  Variable,
  VariableType,
} from '../../lib/api';

// Fixed-length mask for unrevealed secrets — deliberately independent of the
// real value's length so the mask leaks nothing.
const SECRET_MASK = '••••••••••';

// Rotated secrets use the generator's defaults: 24 chars, all classes.
const ROTATE_OPTIONS = {
  length: 24,
  upper: true,
  lower: true,
  digits: true,
  symbols: true,
} as const;

// Converts a stored (normalized) value back into the human form
// VariableValueInput expects: list values are stored as JSON arrays but edited
// comma-separated; bool tokens collapse to "true"/"false".
function toInputForm(type: VariableType, raw: string): string {
  if (type === 'list') {
    try {
      const parsed: unknown = JSON.parse(raw);
      if (Array.isArray(parsed)) return parsed.join(', ');
    } catch {
      // Fall through to the raw value.
    }
    return raw;
  }
  if (type === 'bool') {
    return parseBool(raw) ? 'true' : 'false';
  }
  return raw;
}

// Splits a stored list value into its items for the chip read view.
function parseListItems(raw: string): string[] {
  try {
    const parsed: unknown = JSON.parse(raw);
    if (Array.isArray(parsed)) return parsed.map(String);
  } catch {
    // Fall through to comma splitting.
  }
  return raw
    .split(',')
    .map((p) => p.trim())
    .filter((p) => p !== '');
}

export interface VariableInspectorProps {
  // The selected variable, or null for the overview state.
  variable: Variable | null;
  consumers: Consumer[];
  // Full variable list + usage index, for the overview stats. Null while the
  // store hasn't loaded (or the vault is locked).
  allVariables: Variable[] | null;
  usage: Record<string, Consumer[]>;
  // All set names, regardless of the active list filter.
  setNames: string[];
  locked: boolean;
  compact?: boolean;
  // Increment to request edit mode for the selected variable (keyboard 'e' /
  // Enter from the list). Ignored when nothing is selected.
  editSignal?: number;
  getValue: (key: string) => Promise<{ value: string }>;
  onUpdate: (key: string, input: UpdateVariableInput) => Promise<void>;
  onAssignSet: (key: string, set: string) => void;
  onDelete: (key: string) => void;
  onConsumerClick?: (consumer: Consumer) => void;
  onClose: () => void;
}

/**
 * VariableInspector fills the Variables workspace right rail with the
 * selected variable's stack usage, typed value, and properties — selection
 * lives in the workspace (URL ?selected=). With no selection it renders a
 * workspace overview instead of dead space. It owns its own reveal map so
 * secret auto-hide never bleeds into the list's plaintext previews.
 */
export function VariableInspector({
  variable,
  consumers,
  allVariables,
  usage,
  setNames,
  locked,
  compact,
  editSignal = 0,
  getValue,
  onUpdate,
  onAssignSet,
  onDelete,
  onConsumerClick,
  onClose,
}: VariableInspectorProps) {
  const { revealed, reveal, hide } = useRevealedValues();
  const [editing, setEditing] = useState(false);
  const [editValue, setEditValue] = useState('');
  const [editValid, setEditValid] = useState(true);
  const [showEditValue, setShowEditValue] = useState(false);
  const [confirmRotate, setConfirmRotate] = useState(false);

  const key = variable?.key;
  const value = key !== undefined ? revealed[key] : undefined;
  const isRevealed = value !== undefined;

  const startEdit = useCallback(() => {
    if (!variable) return;
    const current = revealed[variable.key];
    setEditValue(
      current !== undefined ? toInputForm(variable.type, current) : '',
    );
    setShowEditValue(!variable.is_secret);
    setEditing(true);
  }, [variable, revealed]);

  // Reset transient state when the selection changes, so switching variables
  // never strands an edit form or a confirm dialog on the previous key.
  // Adjusting state during render (vs. an effect) avoids a cascading
  // re-render — same pattern as SkillDetailPanel.
  const [prevKey, setPrevKey] = useState(key);
  if (key !== prevKey) {
    setPrevKey(key);
    setEditing(false);
    setEditValue('');
    setShowEditValue(false);
    setConfirmRotate(false);
  }

  // Keyboard 'e' / Enter from the list requests edit mode via a counter prop.
  const [prevEditSignal, setPrevEditSignal] = useState(editSignal);
  if (editSignal !== prevEditSignal) {
    setPrevEditSignal(editSignal);
    if (variable && !editing) startEdit();
  }

  // Re-mask when selection moves away: drop the previous key's revealed value
  // so coming back always starts masked (plaintext is simply refetched).
  useEffect(() => {
    if (key === undefined) return;
    return () => hide(key);
  }, [key, hide]);

  // Plaintext values display without a Reveal click — fetch eagerly (sticky,
  // no auto-hide) once the selection lands on a plaintext variable.
  useEffect(() => {
    if (!variable || variable.is_secret) return;
    if (revealed[variable.key] !== undefined) return;
    void reveal(
      variable.key,
      async () => (await getValue(variable.key)).value,
      false,
    ).catch(() => {
      // Read view falls back to the placeholder; no toast for a passive load.
    });
  }, [variable, revealed, reveal, getValue]);

  const handleReveal = useCallback(() => {
    if (!variable) return;
    void reveal(
      variable.key,
      async () => (await getValue(variable.key)).value,
      true,
    ).catch(() => showToast('error', `Failed to reveal ${variable.key}`));
  }, [variable, reveal, getValue]);

  // Copy-without-reveal: fetches the value and puts it on the clipboard while
  // the on-screen mask stays in place.
  const handleCopy = useCallback(async () => {
    if (!variable) return;
    try {
      const detail = await getValue(variable.key);
      await navigator.clipboard.writeText(detail.value);
      showToast('success', 'Copied');
    } catch {
      showToast('error', 'Failed to copy value');
    }
  }, [variable, getValue]);

  const cancelEdit = useCallback(() => {
    setEditing(false);
    setEditValue('');
    setShowEditValue(false);
  }, []);

  const handleSave = useCallback(async () => {
    if (!variable || !editValue) return;
    const validation = validateVariableInput(variable.type, editValue);
    if (!validation.ok) {
      showToast('error', validation.error);
      return;
    }
    try {
      await onUpdate(variable.key, {
        value: validation.normalized,
        type: variable.type,
        isSecret: variable.is_secret,
      });
      setEditing(false);
      setEditValue('');
      setShowEditValue(false);
      // Drop the stale revealed value: secrets re-mask, plaintext refetches.
      hide(variable.key);
      showToast('success', `Variable "${variable.key}" updated`);
    } catch {
      showToast('error', 'Failed to update variable');
    }
  }, [variable, editValue, onUpdate, hide]);

  const handleRotate = useCallback(async () => {
    if (!variable) return;
    try {
      await onUpdate(variable.key, {
        value: generateSecret(ROTATE_OPTIONS),
        type: 'string',
        isSecret: true,
      });
      hide(variable.key);
      showToast('success', `Rotated "${variable.key}"`);
    } catch {
      showToast('error', 'Failed to rotate secret');
    } finally {
      setConfirmRotate(false);
    }
  }, [variable, onUpdate, hide]);

  if (!variable) {
    return (
      <aside
        aria-label="Variable inspector"
        className="h-full flex flex-col bg-surface/40 backdrop-blur-sm border-l border-border-subtle"
      >
        <InspectorOverview
          variables={allVariables}
          usage={usage}
          locked={locked}
          compact={compact}
        />
      </aside>
    );
  }

  const canRotate = variable.type === 'string' && variable.is_secret;
  const navigableConsumer = consumers.find(isNavigable);

  return (
    <aside
      aria-label="Variable inspector"
      className="h-full flex flex-col bg-surface/40 backdrop-blur-sm border-l border-border-subtle"
    >
      <InspectorHeader
        title={variable.key}
        icon={variable.is_secret ? Lock : Eye}
        accent="primary"
        onClose={onClose}
        subtitle={
          <div className="flex items-center gap-1.5 flex-wrap mt-0.5">
            <span className="inline-flex items-center text-[10px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-border/40 bg-surface-elevated text-text-muted">
              {variable.is_secret ? 'Secret' : 'Plaintext'}
            </span>
            {variable.type === 'string' ? (
              <span className="text-[10px] font-mono text-text-muted/60">
                string
              </span>
            ) : (
              <VariableTypeBadge type={variable.type} />
            )}
            {variable.set && (
              <span className="inline-flex items-center text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-elevated text-text-secondary">
                {variable.set}
              </span>
            )}
          </div>
        }
        actions={
          <div className="flex items-center gap-0.5">
            <IconButton
              icon={Pencil}
              size="sm"
              variant="ghost"
              onClick={startEdit}
              tooltip="Edit variable"
            />
            <IconButton
              icon={Trash2}
              size="sm"
              variant="ghost"
              onClick={() => onDelete(variable.key)}
              tooltip="Delete variable"
              className="hover:text-status-error"
            />
          </div>
        }
      />

      <div
        className={cn(
          'flex-1 min-h-0 overflow-y-auto scrollbar-dark',
          compact ? 'px-3 py-3 space-y-4' : 'px-4 py-4 space-y-5',
        )}
      >
        {/* Usage leads: the consumer index is what no external secret store
            can show, so it gets the top slot. */}
        <Section title="Referenced in stack">
          {consumers.length > 0 ? (
            <div className="space-y-1">
              <p className="text-[11px] text-text-secondary">
                Used by {consumers.length}{' '}
                {consumers.length === 1 ? 'site' : 'sites'}
              </p>
              <ConsumerList
                consumers={consumers}
                previewLimit={null}
                onConsumerClick={onConsumerClick}
                className="-mx-2 space-y-0.5"
              />
            </div>
          ) : (
            <div className="rounded-lg border border-border/30 bg-background/40 p-2.5 space-y-1.5">
              <p className="text-[11px] text-text-secondary leading-relaxed">
                Not referenced by{' '}
                <code className="font-mono text-text-primary">
                  {'${var:…}'}
                </code>{' '}
                in the current stack.yaml.
              </p>
              <p className="text-[10px] text-text-muted/80 leading-relaxed">
                Variables injected through <code>secrets.sets</code> may not
                appear here.
              </p>
              <button
                type="button"
                onClick={() => onDelete(variable.key)}
                className="text-[10px] text-status-error hover:text-status-error/80 underline underline-offset-2 transition-colors"
              >
                Delete this variable
              </button>
            </div>
          )}
        </Section>

        <Section title="Value">
          {editing ? (
            <div className="space-y-2">
              <VariableValueInput
                type={variable.type}
                value={editValue}
                onChange={setEditValue}
                isSecret={variable.is_secret}
                revealed={showEditValue}
                onToggleReveal={() => setShowEditValue((v) => !v)}
                onValidityChange={setEditValid}
                onRequestSubmit={handleSave}
                onRequestCancel={cancelEdit}
                compact={compact}
                autoFocus
              />
              {variable.type === 'string' && (
                <SecretGenerator
                  onGenerate={setEditValue}
                  onReveal={() => setShowEditValue(true)}
                />
              )}
              <div className="flex justify-end gap-1.5">
                <button
                  type="button"
                  onClick={cancelEdit}
                  className="px-2 py-1 text-[10px] text-text-secondary hover:text-text-primary rounded transition-colors"
                >
                  Cancel
                </button>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={handleSave}
                  disabled={!editValue || !editValid}
                >
                  Save
                </Button>
              </div>
            </div>
          ) : (
            <div className="space-y-2">
              {isRevealed ? (
                <ValueReadView type={variable.type} value={value} />
              ) : (
                <div
                  className="text-xs font-mono text-text-muted bg-background/60 px-2 py-1.5 rounded select-none"
                  aria-label={
                    variable.is_secret ? 'Value hidden' : 'Value loading'
                  }
                >
                  {variable.is_secret ? SECRET_MASK : '•••'}
                </div>
              )}
              <div className="flex items-center flex-wrap gap-1.5">
                <Button variant="primary" size="sm" onClick={handleCopy}>
                  <span className="inline-flex items-center gap-1">
                    <Copy size={10} /> Copy value
                  </span>
                </Button>
                {variable.is_secret && (
                  <PillButton
                    icon={isRevealed ? EyeOff : Eye}
                    label={isRevealed ? 'Hide' : 'Reveal'}
                    onClick={handleReveal}
                  />
                )}
                <PillButton icon={Pencil} label="Edit value" onClick={startEdit} />
              </div>
            </div>
          )}
        </Section>

        <Section title="Properties">
          <dl className="space-y-1.5">
            <MetaRow label="Type" value={variable.type} mono />
            <MetaRow
              label="Visibility"
              value={variable.is_secret ? 'Secret' : 'Plaintext'}
            />
            <MetaRow
              label="Set"
              value={variable.set ?? 'Unassigned'}
              mono={Boolean(variable.set)}
            />
          </dl>
          {setNames.filter((n) => n !== variable.set).length > 0 && (
            <select
              value=""
              onChange={(e) => {
                if (e.target.value) onAssignSet(variable.key, e.target.value);
              }}
              aria-label="Move to set"
              className="mt-2 w-full bg-surface border border-border rounded-lg px-2 py-1 text-[10px] text-text-muted focus:outline-none focus:border-primary/40 transition-colors"
            >
              <option value="">Move to set…</option>
              {setNames
                .filter((n) => n !== variable.set)
                .map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
            </select>
          )}
        </Section>
      </div>

      {(canRotate || navigableConsumer) && (
        <div
          className={cn(
            'flex-shrink-0 border-t border-border-subtle flex items-center flex-wrap gap-1.5',
            compact ? 'px-3 py-2' : 'px-4 py-3',
          )}
        >
          {canRotate && (
            <PillButton
              icon={RefreshCw}
              label="Rotate secret"
              onClick={() => setConfirmRotate(true)}
            />
          )}
          {navigableConsumer && (
            <PillButton
              icon={ArrowUpRight}
              label="Jump to Topology"
              onClick={() => onConsumerClick?.(navigableConsumer)}
            />
          )}
        </div>
      )}

      <ConfirmDialog
        isOpen={confirmRotate}
        onClose={() => setConfirmRotate(false)}
        onConfirm={handleRotate}
        title="Rotate secret"
        message={
          <>
            <p>
              Generate a new value for{' '}
              <span className="font-mono text-primary">{variable.key}</span>?
            </p>
            <p>
              The current value is replaced immediately and cannot be
              recovered. Consumers pick up the new value on their next read.
            </p>
          </>
        }
        confirmLabel="Rotate"
        variant="danger"
      />
    </aside>
  );
}

// Ghost-pill action button shared by the value and footer action rows.
function PillButton({
  icon: Icon,
  label,
  onClick,
}: {
  icon: ComponentType<{ size?: number; className?: string }>;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-medium text-text-secondary hover:text-text-primary border border-border/40 hover:border-border transition-colors"
    >
      <Icon size={10} /> {label}
    </button>
  );
}

// Type-aware read view for a revealed (or plaintext) value.
function ValueReadView({ type, value }: { type: VariableType; value: string }) {
  if (type === 'json') {
    let pretty = value;
    try {
      pretty = JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      // Show the raw value when it isn't parseable.
    }
    return (
      <CodeViewer
        language="json"
        content={pretty}
        wrap
        ariaLabel="Variable value"
        className="rounded-md border border-border/30 bg-background/60 max-h-[40vh]"
      />
    );
  }
  if (type === 'list') {
    return (
      <div className="flex flex-wrap gap-1">
        {parseListItems(value).map((item, i) => (
          <span
            key={`${item}-${i}`}
            className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-elevated text-text-secondary border border-border/30"
          >
            {item}
          </span>
        ))}
      </div>
    );
  }
  if (type === 'bool') {
    const truthy = parseBool(value);
    return (
      <div
        className={cn(
          'text-sm font-mono font-semibold',
          truthy ? 'text-emerald-300' : 'text-text-muted',
        )}
      >
        {truthy ? 'true' : 'false'}
      </div>
    );
  }
  if (type === 'number') {
    return (
      <div className="text-sm font-mono font-semibold text-emerald-300">
        {value}
      </div>
    );
  }
  return (
    <div className="text-xs font-mono text-text-secondary bg-background/60 px-2 py-1.5 rounded break-all">
      {value}
    </div>
  );
}

// Overview shown when nothing is selected: a left-aligned block (not a
// centered hero) with at-a-glance stats and teaching tips, so the rail earns
// its space even before the first click.
function InspectorOverview({
  variables,
  usage,
  locked,
  compact,
}: {
  variables: Variable[] | null;
  usage: Record<string, Consumer[]>;
  locked: boolean;
  compact?: boolean;
}) {
  const list = variables ?? [];
  const secretCount = list.filter((v) => v.is_secret).length;
  const unreferencedCount = list.filter(
    (v) => (usage[v.key] ?? []).length === 0,
  ).length;
  const typeCounts = new Map<VariableType, number>();
  for (const v of list) {
    typeCounts.set(v.type, (typeCounts.get(v.type) ?? 0) + 1);
  }

  return (
    <div
      className={cn(
        'flex-1 min-h-0 overflow-y-auto scrollbar-dark',
        compact ? 'px-3 py-4 space-y-5' : 'px-4 py-5 space-y-6',
      )}
    >
      <div className="space-y-1">
        <h2 className="text-sm font-semibold text-text-primary">
          Variables overview
        </h2>
        <p className="text-[11px] text-text-muted leading-relaxed">
          Select a variable to inspect its value, properties, and stack usage.
        </p>
      </div>

      {locked ? (
        <p className="text-[11px] text-text-muted leading-relaxed">
          Vault is locked — unlock to browse variables and values.
        </p>
      ) : (
        variables !== null && (
          <Section title="At a glance">
            <dl className="space-y-1.5">
              <MetaRow label="Variables" value={String(list.length)} mono />
              <MetaRow label="Secrets" value={String(secretCount)} mono />
              <MetaRow
                label="Plaintext"
                value={String(list.length - secretCount)}
                mono
              />
              <MetaRow
                label="Unreferenced"
                value={String(unreferencedCount)}
                mono
              />
              {[...typeCounts.entries()].map(([type, count]) => (
                <MetaRow
                  key={type}
                  label={`Type: ${type}`}
                  value={String(count)}
                  mono
                />
              ))}
            </dl>
          </Section>
        )
      )}

      <Section title="Tips">
        <ul className="space-y-2">
          <Tip>
            Secrets auto-hide 10 seconds after reveal; plaintext values stay
            visible.
          </Tip>
          <Tip>
            Group variables into sets and inject them in bulk with{' '}
            <code className="font-mono text-text-secondary">secrets.sets</code>{' '}
            in stack.yaml.
          </Tip>
          <Tip>Drop a .env or .json file anywhere on this page to import.</Tip>
        </ul>
      </Section>
    </div>
  );
}

function Tip({ children }: { children: ReactNode }) {
  return (
    <li className="text-[11px] text-text-muted leading-relaxed pl-3 relative before:content-['·'] before:absolute before:left-0 before:text-text-muted/60">
      {children}
    </li>
  );
}

function MetaRow({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="text-[11px] text-text-muted flex-shrink-0">{label}</dt>
      <dd
        className={cn(
          'text-[11px] text-text-secondary text-right break-words',
          mono && 'font-mono',
        )}
      >
        {value}
      </dd>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
        {title}
      </h3>
      {children}
    </section>
  );
}
