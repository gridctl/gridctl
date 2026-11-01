import { memo, useCallback } from 'react';
import { Command } from 'cmdk';
import { Check, ChevronRight } from 'lucide-react';
import { cn } from '../../lib/cn';
import { CHIP_TONE_CLASS, annotationChips, type AnnotationChip } from '../../lib/groups';
import type { AuditState } from '../../lib/toolAudit';
import type { ToolAnnotations } from '../../types';
import type { ToolRow } from '../../hooks/useToolsEditor';

// NUL cannot appear in a group name (the same join used for tool signatures).
const GROUP_SEP = '\u0000';

// eslint-disable-next-line react-refresh/only-export-components -- group join is shared with the list parent
export function joinToolGroups(names: readonly string[]): string {
  return names.join(GROUP_SEP);
}

function splitToolGroups(groups: string): string[] {
  return groups === '' ? [] : groups.split(GROUP_SEP);
}

const AUDIT_STYLES: Record<AuditState, { dot: string; text: string; label: string }> = {
  used: { dot: 'bg-status-running', text: 'text-status-running', label: 'used' },
  unused: { dot: 'bg-status-pending', text: 'text-status-pending', label: 'unused' },
  disabled: { dot: 'bg-text-muted/50', text: 'text-text-muted', label: 'disabled' },
};

const ANNOTATION_KEYS = [
  'title',
  'readOnlyHint',
  'destructiveHint',
  'idempotentHint',
  'openWorldHint',
] as const satisfies readonly (keyof ToolAnnotations)[];

function annotationsEqual(a?: ToolAnnotations, b?: ToolAnnotations): boolean {
  if (a === b) return true;
  for (const key of ANNOTATION_KEYS) {
    if (a?.[key] !== b?.[key]) return false;
  }
  return true;
}

export interface ToolListRowProps {
  opt: ToolRow;
  groups: string;
  lastUsed: string;
  isEnabled: boolean;
  isActive: boolean;
  auditState: AuditState | null;
  /** Rendered policy chip label. Empty when the verdict paints nothing. */
  policyLabel: string;
  policyTitle: string;
  /** Tone declared by policyChip. Empty when the chip is absent. */
  policyTone: AnnotationChip['tone'] | '';
  onSelect: (name: string) => void;
  onToggle: (name: string) => void;
  registerRef: (name: string, el: HTMLDivElement | null) => void;
}

// allTools allocates a new object whenever the catalog or status poll changes,
// so opt is compared field-wise. Derived values arrive as strings.
// eslint-disable-next-line react-refresh/only-export-components -- comparator is the memo test seam
export function toolListRowPropsEqual(prev: ToolListRowProps, next: ToolListRowProps): boolean {
  return (
    prev.opt.name === next.opt.name &&
    prev.opt.description === next.opt.description &&
    annotationsEqual(prev.opt.annotations, next.opt.annotations) &&
    prev.groups === next.groups &&
    prev.lastUsed === next.lastUsed &&
    prev.isEnabled === next.isEnabled &&
    prev.isActive === next.isActive &&
    prev.auditState === next.auditState &&
    prev.policyLabel === next.policyLabel &&
    prev.policyTitle === next.policyTitle &&
    prev.policyTone === next.policyTone &&
    prev.onSelect === next.onSelect &&
    prev.onToggle === next.onToggle &&
    prev.registerRef === next.registerRef
  );
}

export const ToolListRow = memo(function ToolListRow({
  opt,
  groups,
  lastUsed,
  isEnabled,
  isActive,
  auditState,
  policyLabel,
  policyTitle,
  policyTone,
  onSelect,
  onToggle,
  registerRef,
}: ToolListRowProps) {
  const groupNames = splitToolGroups(groups);
  // Stable per row so a parent render does not detach and reattach the node.
  const setRowRef = useCallback((el: HTMLDivElement | null) => {
    registerRef(opt.name, el);
  }, [registerRef, opt.name]);

  return (
    <div
      ref={setRowRef}
      className={cn(
        'border-b border-border/20 last:border-b-0 border-l-2',
        isActive
          ? 'border-l-primary bg-primary/[0.06]'
          : 'border-l-transparent',
        isEnabled && !isActive && 'bg-primary/[0.03]',
      )}
    >
      <Command.Item
        value={opt.name}
        onSelect={() => onSelect(opt.name)}
        aria-current={isActive}
        aria-label={`${opt.name} details`}
        className={cn(
          'flex items-start gap-2.5 px-3 py-2 cursor-pointer select-none outline-none transition-colors',
          'hover:bg-surface-highlight/40',
          '[&[data-selected=true]]:bg-surface-highlight/40',
          'focus-visible:bg-surface-highlight/40',
        )}
      >
        <button
          type="button"
          role="checkbox"
          aria-checked={isEnabled}
          aria-label={isEnabled ? `Disable ${opt.name}` : `Enable ${opt.name}`}
          onClick={(e) => {
            // Toggling exposure must not select the row for the panel.
            e.stopPropagation();
            onToggle(opt.name);
          }}
          className={cn(
            'mt-0.5 w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors',
            isEnabled
              ? 'bg-primary/20 border-primary/60'
              : 'border-border/60 bg-background/50 hover:border-primary/40',
          )}
        >
          {isEnabled && <Check size={10} className="text-primary" aria-hidden="true" />}
        </button>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5 min-w-0">
            <span
              className={cn(
                'text-xs font-mono truncate',
                isEnabled ? 'text-text-primary' : 'text-text-secondary',
              )}
            >
              {opt.name}
            </span>
            {groupNames.map((g) => (
              <span
                key={g}
                title={`In tool group "${g}"`}
                className="flex-shrink-0 px-1.5 py-px rounded text-3xs font-medium bg-secondary/10 text-secondary"
              >
                {g}
              </span>
            ))}
            {/* Server-reported annotation chips. Undeclared hints
                render nothing (spec: absent = worst-case, which
                the detail panel's Hints section spells out). */}
            {annotationChips(opt.annotations).map((chip) => (
              <span
                key={chip.label}
                title={`${chip.title} (reported by the server, not verified)`}
                className={cn(
                  'flex-shrink-0 px-1.5 py-px rounded text-3xs font-medium',
                  CHIP_TONE_CLASS[chip.tone],
                )}
              >
                {chip.label}
              </span>
            ))}
            {policyLabel && (
              <span
                title={policyTitle}
                className={cn(
                  'flex-shrink-0 px-1.5 py-px rounded text-3xs font-medium',
                  CHIP_TONE_CLASS[policyTone === '' ? 'danger' : policyTone],
                )}
              >
                {policyLabel}
              </span>
            )}
          </div>
          {opt.description && (
            <div className="text-3xs text-text-muted truncate">{opt.description}</div>
          )}
          {auditState && (
            <div
              className={cn(
                'flex items-center gap-1 text-3xs mt-0.5',
                AUDIT_STYLES[auditState].text,
              )}
            >
              <span
                className={cn(
                  'inline-block w-1.5 h-1.5 rounded-full flex-shrink-0',
                  AUDIT_STYLES[auditState].dot,
                )}
                aria-hidden="true"
              />
              <span>{AUDIT_STYLES[auditState].label}</span>
              {auditState !== 'disabled' && lastUsed && (
                <span className="text-text-muted/70 truncate">
                  · {lastUsed}
                </span>
              )}
            </div>
          )}
        </div>
        <ChevronRight
          size={13}
          className={cn(
            'flex-shrink-0 mt-0.5 transition-colors',
            isActive ? 'text-primary' : 'text-text-muted/40',
          )}
          aria-hidden="true"
        />
      </Command.Item>
    </div>
  );
}, toolListRowPropsEqual);

ToolListRow.displayName = 'ToolListRow';
