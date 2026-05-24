import { useState } from 'react';
import {
  ArrowUpRight,
  ChevronDown,
  ChevronRight,
  Eye,
  EyeOff,
  Link2,
  Pencil,
  Trash2,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { VariableTypeBadge } from './VariableTypeBadge';
import { VariableVisibilityIcon } from './VariableVisibilityIcon';
import { SecretGenerator } from './SecretGenerator';
import { VariableValueInput } from './VariableValueInput';
import type { Consumer, Variable } from '../../lib/api';

// How many consumers to show before collapsing behind a "see all" toggle.
const CONSUMER_PREVIEW_LIMIT = 3;

// A consumer is navigable to a topology node only when it is a server or a
// resource (those are the kinds the graph renders as nodes).
function isNavigable(c: Consumer): boolean {
  return c.kind === 'mcp-server' || c.kind === 'resource';
}

export interface SecretItemProps {
  secret: Variable;
  revealed?: string;
  isEditing: boolean;
  editValue: string;
  showEditValue: boolean;
  onReveal: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onEditValueChange: (val: string) => void;
  onEditToggleShow: () => void;
  onEditSave: () => void;
  onEditCancel: () => void;
  sets: string[];
  onAssignSet: (set: string) => void;
  // consumers are the stack sites referencing this variable (the "used by"
  // index). Empty (the default) renders no badge — absence is the signal.
  consumers?: Consumer[];
  // onConsumerClick is invoked when a navigable consumer row is clicked;
  // VaultWorkspace wires this to topology node selection. Omit to render
  // consumer rows as non-interactive.
  onConsumerClick?: (consumer: Consumer) => void;
  compact?: boolean;
  // When true, the key + value text uses the `.log-text` class so its size
  // is driven by the parent's `--log-font-size` CSS variable (used by the
  // detached page's zoom controls). VaultPanel leaves this off.
  enableZoom?: boolean;
}

// Expandable variable row matching the SkillItem visual pattern. Used by
// VaultPanel and the VaultWorkspace.
export function SecretItem({
  secret,
  revealed,
  isEditing,
  editValue,
  showEditValue,
  onReveal,
  onEdit,
  onDelete,
  onEditValueChange,
  onEditToggleShow,
  onEditSave,
  onEditCancel,
  sets,
  onAssignSet,
  consumers = [],
  onConsumerClick,
  compact,
  enableZoom,
}: SecretItemProps) {
  const [expanded, setExpanded] = useState(false);
  const [showConsumers, setShowConsumers] = useState(false);
  const [showAllConsumers, setShowAllConsumers] = useState(false);
  // Tracks whether the in-edit value satisfies its type (json/number can be
  // invalid); gates the Save button alongside the empty-value check.
  const [editValueValid, setEditValueValid] = useState(true);

  const keyTextClass = cn(
    'text-xs font-mono font-medium text-text-primary flex-1 text-left truncate',
    enableZoom && 'log-text',
  );
  const valuePreviewClass = cn(
    'text-[10px] font-mono text-text-muted truncate max-w-[120px]',
    enableZoom && 'log-text-detail',
  );
  const valueDisplayClass = cn(
    'text-xs font-mono text-text-secondary bg-background/60 px-2 py-1.5 rounded break-all',
    enableZoom && 'log-text',
  );
  const editKeyClass = cn(
    'text-xs font-mono text-primary',
    enableZoom && 'log-text',
  );

  if (isEditing) {
    return (
      <div className="rounded-lg bg-surface-elevated/50 border border-primary/20 p-2 space-y-2">
        <div className={editKeyClass}>{secret.key}</div>
        <VariableValueInput
          type={secret.type}
          value={editValue}
          onChange={onEditValueChange}
          isSecret={secret.is_secret}
          revealed={showEditValue}
          onToggleReveal={onEditToggleShow}
          onValidityChange={setEditValueValid}
          onRequestSubmit={onEditSave}
          onRequestCancel={onEditCancel}
          compact={compact}
          enableZoom={enableZoom}
          autoFocus
        />
        {secret.type === 'string' && (
          <SecretGenerator
            onGenerate={onEditValueChange}
            onReveal={() => {
              if (!showEditValue) onEditToggleShow();
            }}
          />
        )}
        <div className="flex justify-end gap-1.5">
          <button
            onClick={onEditCancel}
            className="px-2 py-1 text-[10px] text-text-secondary hover:text-text-primary rounded transition-colors"
          >
            Cancel
          </button>
          <Button
            variant="primary"
            size="sm"
            onClick={onEditSave}
            disabled={!editValue || !editValueValid}
          >
            Save
          </Button>
        </div>
      </div>
    );
  }

  // Plaintext variables show their value inline by default — the Reveal
  // affordance is reserved for actual secrets.
  const isPlaintext = !secret.is_secret;
  const displayValue =
    revealed !== undefined
      ? revealed
      : isPlaintext
        ? '•••' // not yet fetched; placeholder until refresh completes
        : '••••••••';

  return (
    <div className="rounded-lg bg-surface-elevated/50 border border-border-subtle overflow-hidden">
      {/* Header row. The expand toggle and the usage badge are sibling buttons
          (never nested) so each stays independently clickable. */}
      <div
        className={cn(
          'flex items-center gap-2 hover:bg-surface-highlight/50 transition-colors',
          compact ? 'px-2 py-1.5' : 'p-3',
        )}
      >
        <button
          onClick={() => setExpanded(!expanded)}
          className="flex flex-1 min-w-0 items-center gap-2 text-left"
          aria-expanded={expanded}
        >
          <div className="p-0.5 text-text-muted">
            {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          </div>
          <VariableVisibilityIcon isSecret={secret.is_secret} />
          <span className={keyTextClass}>{secret.key}</span>
          <VariableTypeBadge type={secret.type} />
          <span className={valuePreviewClass}>{displayValue}</span>
        </button>
        {consumers.length > 0 && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setShowConsumers((v) => !v);
            }}
            onKeyDown={(e) => {
              if (e.key === 'Escape') setShowConsumers(false);
            }}
            aria-expanded={showConsumers}
            aria-label={`Used by ${consumers.length} ${consumers.length === 1 ? 'consumer' : 'consumers'}`}
            title={`Used by ${consumers.length} ${consumers.length === 1 ? 'consumer' : 'consumers'}`}
            className={cn(
              'flex-shrink-0 inline-flex items-center gap-1 rounded-md px-1.5 py-px text-[10px] font-mono transition-colors',
              showConsumers
                ? 'bg-primary/10 text-primary'
                : 'bg-surface-elevated text-text-muted hover:bg-surface-highlight hover:text-text-primary',
            )}
          >
            <Link2 size={9} />
            {consumers.length}
          </button>
        )}
      </div>

      {/* Consumer drill-down — toggled by the usage badge, independent of the
          row's expand state. */}
      {showConsumers && consumers.length > 0 && (
        <ConsumerList
          consumers={consumers}
          showAll={showAllConsumers}
          onToggleShowAll={() => setShowAllConsumers((v) => !v)}
          onConsumerClick={onConsumerClick}
        />
      )}

      {/* Expanded content */}
      {expanded && (
        <div className="px-3 pb-3 border-t border-border-subtle">
          {/* Value display */}
          <div className="mt-2 mb-2">
            <div className="text-[10px] text-text-muted mb-1">Value</div>
            <div className={valueDisplayClass}>
              {revealed !== undefined
                ? revealed
                : '••••••••••••'}
            </div>
          </div>

          {/* Set assignment */}
          {!compact && sets.length > 0 && !secret.set && (
            <div className="mb-2">
              <select
                value=""
                onChange={(e) => {
                  if (e.target.value) onAssignSet(e.target.value);
                }}
                className="w-full bg-surface border border-border rounded-lg px-2 py-1 text-[10px] text-text-muted focus:outline-none focus:border-primary/40 transition-colors"
                title="Assign to set"
              >
                <option value="">Assign to set...</option>
                {sets.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </div>
          )}

          {/* Actions */}
          <div className="flex items-center gap-1.5 mt-2 pt-2 border-t border-border-subtle/50">
            {!isPlaintext && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onReveal();
                }}
                className={cn(
                  'flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-semibold transition-all duration-200',
                  revealed !== undefined
                    ? 'bg-status-pending text-background shadow-[0_1px_8px_rgba(234,179,8,0.2)] hover:shadow-[0_2px_12px_rgba(234,179,8,0.3)] hover:-translate-y-0.5 active:translate-y-0'
                    : 'bg-status-running text-background shadow-[0_1px_8px_rgba(16,185,129,0.2)] hover:shadow-[0_2px_12px_rgba(16,185,129,0.3)] hover:-translate-y-0.5 active:translate-y-0',
                )}
              >
                {revealed !== undefined ? <EyeOff size={10} /> : <Eye size={10} />}
                {revealed !== undefined ? 'Hide' : 'Reveal'}
              </button>
            )}
            <button
              onClick={(e) => {
                e.stopPropagation();
                onEdit();
              }}
              className="flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-semibold bg-gradient-to-r from-primary to-primary-dark text-background shadow-[0_1px_8px_rgba(245,158,11,0.2)] hover:shadow-[0_2px_12px_rgba(245,158,11,0.3)] hover:-translate-y-0.5 active:translate-y-0 transition-all duration-200"
            >
              <Pencil size={10} /> Edit
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onDelete();
              }}
              className="flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-semibold bg-gradient-to-r from-status-error to-rose-600 text-white shadow-[0_1px_8px_rgba(244,63,94,0.2)] hover:shadow-[0_2px_12px_rgba(244,63,94,0.3)] hover:-translate-y-0.5 active:translate-y-0 transition-all duration-200"
            >
              <Trash2 size={10} /> Delete
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

interface ConsumerListProps {
  consumers: Consumer[];
  showAll: boolean;
  onToggleShowAll: () => void;
  onConsumerClick?: (consumer: Consumer) => void;
}

// ConsumerList renders the variable's reference sites. Server/resource sites
// are clickable (they navigate to a topology node); other kinds render as
// plain rows. Long lists collapse behind a "see all" toggle.
function ConsumerList({
  consumers,
  showAll,
  onToggleShowAll,
  onConsumerClick,
}: ConsumerListProps) {
  const visible = showAll
    ? consumers
    : consumers.slice(0, CONSUMER_PREVIEW_LIMIT);
  const hiddenCount = consumers.length - visible.length;

  return (
    <div
      role="group"
      aria-label="Variables consuming this value"
      className="px-3 pb-2 pt-1 border-t border-border-subtle/60 space-y-0.5"
    >
      {visible.map((c, i) => {
        const label = `${c.name || c.kind} · ${c.field}`;
        if (isNavigable(c) && onConsumerClick) {
          return (
            <button
              key={`${c.kind}-${c.name}-${c.field}-${i}`}
              onClick={(e) => {
                e.stopPropagation();
                onConsumerClick(c);
              }}
              aria-label={`Go to ${c.name} (${c.field})`}
              className="w-full flex items-center gap-1.5 px-2 py-1 rounded text-[10px] font-mono text-text-secondary hover:text-primary hover:bg-surface-highlight/50 transition-colors text-left"
            >
              <ArrowUpRight
                size={10}
                className="flex-shrink-0 text-text-muted"
              />
              <span className="truncate">{label}</span>
            </button>
          );
        }
        return (
          <div
            key={`${c.kind}-${c.name}-${c.field}-${i}`}
            className="flex items-center gap-1.5 px-2 py-1 text-[10px] font-mono text-text-muted"
          >
            <span className="w-2.5 flex-shrink-0 text-center">·</span>
            <span className="truncate">{label}</span>
          </div>
        );
      })}
      {(hiddenCount > 0 || showAll) && consumers.length > CONSUMER_PREVIEW_LIMIT && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onToggleShowAll();
          }}
          className="px-2 py-0.5 text-[10px] text-primary hover:text-primary/80 transition-colors"
        >
          {showAll ? 'Show less' : `See all ${consumers.length}`}
        </button>
      )}
    </div>
  );
}
