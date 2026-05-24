import {
  ChevronDown,
  ChevronRight,
  FolderOpen,
  Trash2,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { SecretItem } from './SecretItem';
import type { Variable, VariableSet } from '../../lib/api';

export interface SetGroupRowHandlers {
  revealed: Record<string, string>;
  editingKey: string | null;
  editValue: string;
  showEditValue: boolean;
  setNames: string[];
  onReveal: (key: string) => void;
  onEdit: (key: string) => void;
  onDeleteSecret: (key: string) => void;
  onEditValueChange: (val: string) => void;
  onEditToggleShow: () => void;
  onEditSave: () => void;
  onEditCancel: () => void;
  onAssignSet: (key: string, set: string) => void;
}

export interface SetGroupProps {
  set: VariableSet;
  variables: Variable[];
  expanded: boolean;
  onToggleExpand: () => void;
  // When omitted, the per-set delete (trash) affordance is not rendered.
  // The sidebar (quick-lookup) hides it; the workspace shows it.
  onDeleteSet?: () => void;
  // When true, a small dot marks the set as containing a variable edited this
  // session. Absence is the signal — nothing renders when false/omitted.
  recentlyEdited?: boolean;
  handlers: SetGroupRowHandlers;
  // Use `.log-text` on key/value text for zoom-scaled surfaces.
  enableZoom?: boolean;
  // Inner row padding ("space-y-1" or "p-2 space-y-2" etc.) — callers tune the
  // spacing inside the set.
  innerClassName?: string;
  // Name label uses `.log-text` for zoom-scaled surfaces.
  nameClassName?: string;
}

// One expandable Variable Set row + its member secrets. The parent owns the
// expanded/edit/reveal state and threads it via `handlers`; SetGroup just
// renders the list.
export function SetGroup({
  set,
  variables,
  expanded,
  onToggleExpand,
  onDeleteSet,
  recentlyEdited,
  handlers,
  enableZoom,
  innerClassName,
  nameClassName,
}: SetGroupProps) {
  return (
    <div className="group rounded-lg bg-surface-elevated/50 border border-border-subtle overflow-hidden">
      <button
        onClick={onToggleExpand}
        className="w-full flex items-center justify-between px-3 py-2 text-left hover:bg-surface-highlight/50 transition-colors"
      >
        <div className="flex items-center gap-2">
          {expanded ? (
            <ChevronDown size={12} className="text-text-muted" />
          ) : (
            <ChevronRight size={12} className="text-text-muted" />
          )}
          <FolderOpen size={12} className="text-secondary" />
          <span className={cn('text-xs font-mono text-text-primary', nameClassName)}>
            {set.name}
          </span>
          {recentlyEdited && (
            <span
              className="h-1.5 w-1.5 rounded-full bg-secondary/70 flex-shrink-0"
              title="Recently edited"
              aria-label="Recently edited"
            />
          )}
          <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-secondary/10 text-secondary">
            {set.count}
          </span>
        </div>
        {onDeleteSet && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              onDeleteSet();
            }}
            className="p-1 rounded hover:bg-status-error/10 transition-colors opacity-0 group-hover:opacity-100"
            title="Delete set"
          >
            <Trash2 size={10} className="text-text-muted hover:text-status-error" />
          </button>
        )}
      </button>
      {expanded && variables.length > 0 && (
        <div className={cn('px-2 pb-2 space-y-1', innerClassName)}>
          {variables.map((variable) => (
            <SecretItem
              key={variable.key}
              secret={variable}
              revealed={handlers.revealed[variable.key]}
              isEditing={handlers.editingKey === variable.key}
              editValue={handlers.editValue}
              showEditValue={handlers.showEditValue}
              onReveal={() => handlers.onReveal(variable.key)}
              onEdit={() => handlers.onEdit(variable.key)}
              onDelete={() => handlers.onDeleteSecret(variable.key)}
              onEditValueChange={handlers.onEditValueChange}
              onEditToggleShow={handlers.onEditToggleShow}
              onEditSave={handlers.onEditSave}
              onEditCancel={handlers.onEditCancel}
              sets={handlers.setNames}
              onAssignSet={(s) => handlers.onAssignSet(variable.key, s)}
              compact
              enableZoom={enableZoom}
            />
          ))}
        </div>
      )}
      {expanded && variables.length === 0 && (
        <div className="px-3 pb-2">
          <p className="text-[10px] text-text-muted italic">
            No secrets in this set
          </p>
        </div>
      )}
    </div>
  );
}
