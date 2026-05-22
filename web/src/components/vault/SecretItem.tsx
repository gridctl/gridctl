import { useState } from 'react';
import {
  ChevronDown,
  ChevronRight,
  Eye,
  EyeOff,
  Pencil,
  Trash2,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { VariableTypeBadge } from './VariableTypeBadge';
import { VariableVisibilityIcon } from './VariableVisibilityIcon';
import type { Variable } from '../../lib/api';

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
  compact?: boolean;
  // When true, the key + value text uses the `.log-text` class so its size
  // is driven by the parent's `--log-font-size` CSS variable (used by the
  // detached page's zoom controls). VaultPanel leaves this off.
  enableZoom?: boolean;
}

// Expandable variable row matching the SkillItem visual pattern. Used by
// VaultPanel, DetachedVaultPage, and the upcoming VaultWorkspace.
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
  compact,
  enableZoom,
}: SecretItemProps) {
  const [expanded, setExpanded] = useState(false);

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
  const editValueClass = cn(
    'w-full bg-surface border border-border rounded-lg px-2 py-1.5 pr-8 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 outline-none transition-colors',
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
        <div className="relative">
          <input
            type={showEditValue ? 'text' : 'password'}
            value={editValue}
            onChange={(e) => onEditValueChange(e.target.value)}
            placeholder="New value"
            autoFocus
            className={editValueClass}
            onKeyDown={(e) => {
              if (e.key === 'Enter') onEditSave();
              if (e.key === 'Escape') onEditCancel();
            }}
          />
          <button
            type="button"
            onClick={onEditToggleShow}
            className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 rounded text-text-muted hover:text-text-primary transition-colors"
          >
            {showEditValue ? <EyeOff size={10} /> : <Eye size={10} />}
          </button>
        </div>
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
            disabled={!editValue}
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
      {/* Header row */}
      <button
        onClick={() => setExpanded(!expanded)}
        className={cn(
          'w-full flex items-center gap-2 hover:bg-surface-highlight/50 transition-colors',
          compact ? 'px-2 py-1.5' : 'p-3',
        )}
      >
        <div className="p-0.5 text-text-muted">
          {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </div>
        <VariableVisibilityIcon isSecret={secret.is_secret} />
        <span className={keyTextClass}>{secret.key}</span>
        <VariableTypeBadge type={secret.type} />
        <span className={valuePreviewClass}>{displayValue}</span>
      </button>

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
