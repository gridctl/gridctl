import { Check, GitBranch } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StateBadge } from './StateBadge';
import { SkillActions } from './SkillActions';
import { skillCategory } from '../../lib/skillMeta';
import { formatLastUsed } from '../../lib/toolAudit';
import { toTitleCase } from '../../lib/text';
import type { AgentSkill, SkillSourceStatus, SkillUsageStat } from '../../types';

// The sortable column keys. Declared locally (rather than imported from the
// workspace) so the table has no dependency cycle with its parent; the union
// matches the workspace's SortMode for the sortable axes. 'usage' is only an
// option when a usageMap is supplied (the Last used column is then rendered).
export type LibraryTableSort = 'name' | 'state' | 'files' | 'usage';

const BASE_SORT_COLUMNS: { key: LibraryTableSort; label: string }[] = [
  { key: 'state', label: 'State' },
  { key: 'name', label: 'Name' },
  { key: 'files', label: 'Files' },
];

const USAGE_SORT_COLUMN: { key: LibraryTableSort; label: string } = { key: 'usage', label: 'Last used' };

export interface LibraryTableProps {
  skills: AgentSkill[];
  sortMode: LibraryTableSort;
  onSort: (mode: LibraryTableSort) => void;
  selectedNames: Set<string>;
  onToggleSelect: (skill: AgentSkill) => void;
  onSelectAll: () => void;
  onClearSelection: () => void;
  allSelected: boolean;
  someSelected: boolean;
  onSelect: (skill: AgentSkill) => void;
  activeSkillName: string | null;
  sourceMap?: Map<string, SkillSourceStatus>;
  // Per-skill usage, joined by name. When supplied, a sortable "Last used"
  // column is rendered; when absent the column is omitted entirely (the usage
  // endpoint is unavailable), keeping DetachedRegistryPage and prior callers
  // unchanged.
  usageMap?: Map<string, SkillUsageStat>;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  compact: boolean;
}

/**
 * Power-user table view of the Library. A flat, sortable list (grouping is a
 * cards-view concept) with a multi-select column. State, Name, and Files
 * headers drive the shared ?sort axis; the header checkbox selects or clears
 * all rows with an indeterminate middle state.
 */
export function LibraryTable({
  skills,
  sortMode,
  onSort,
  selectedNames,
  onToggleSelect,
  onSelectAll,
  onClearSelection,
  allSelected,
  someSelected,
  onSelect,
  activeSkillName,
  sourceMap,
  usageMap,
  onEnable,
  onDisable,
  onEdit,
  onDelete,
  compact,
}: LibraryTableProps) {
  const handleToggle = (s: AgentSkill) => (s.state === 'active' ? onDisable(s) : onEnable(s));
  const cellPad = compact ? 'py-1.5' : 'py-2.5';
  const selectAllChecked = allSelected ? 'true' : someSelected ? 'mixed' : 'false';
  const showUsage = usageMap !== undefined;
  const sortColumns = showUsage ? [...BASE_SORT_COLUMNS, USAGE_SORT_COLUMN] : BASE_SORT_COLUMNS;

  return (
    <div className="p-4">
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="border-b border-border/40">
            <th scope="col" className="w-8 px-2 py-2 text-left">
              <button
                type="button"
                role="checkbox"
                aria-checked={selectAllChecked}
                aria-label={allSelected ? 'Clear selection' : 'Select all skills'}
                onClick={() => (allSelected ? onClearSelection() : onSelectAll())}
                className={cn(
                  'w-4 h-4 rounded border flex items-center justify-center transition-colors',
                  allSelected || someSelected
                    ? 'bg-primary/20 border-primary/50 text-primary'
                    : 'border-border/50 text-transparent hover:border-border',
                )}
              >
                {allSelected ? (
                  <Check size={11} />
                ) : someSelected ? (
                  <span aria-hidden="true" className="w-2 h-px bg-primary" />
                ) : null}
              </button>
            </th>
            {sortColumns.map((col) => (
              <th key={col.key} scope="col" className="px-2 py-2 text-left">
                <button
                  type="button"
                  onClick={() => onSort(col.key)}
                  aria-pressed={sortMode === col.key}
                  className={cn(
                    'inline-flex items-center gap-1 text-[10px] uppercase tracking-wider font-medium transition-colors',
                    sortMode === col.key ? 'text-primary' : 'text-text-muted hover:text-text-secondary',
                  )}
                >
                  {col.label}
                  {sortMode === col.key && <span aria-hidden="true">↓</span>}
                </button>
              </th>
            ))}
            <th scope="col" className="px-2 py-2 text-left text-[10px] uppercase tracking-wider font-medium text-text-muted">
              Category
            </th>
            <th scope="col" className="px-2 py-2 text-right text-[10px] uppercase tracking-wider font-medium text-text-muted">
              Actions
            </th>
          </tr>
        </thead>
        <tbody>
          {skills.map((skill) => {
            const isSel = selectedNames.has(skill.name);
            const src = sourceMap?.get(skill.name);
            const category = skillCategory(skill.dir);
            const usage = usageMap?.get(skill.name);
            const lastUsed = usage?.lastCalledAt ? formatLastUsed(usage.lastCalledAt) : '–';
            return (
              <tr
                key={skill.name}
                aria-current={skill.name === activeSkillName ? 'true' : undefined}
                className={cn(
                  'border-b border-border-subtle/40 transition-colors hover:bg-surface-highlight/40',
                  skill.name === activeSkillName && 'bg-primary/[0.06]',
                )}
              >
                <td className={cn('px-2', cellPad)}>
                  <button
                    type="button"
                    role="checkbox"
                    aria-checked={isSel}
                    aria-label={isSel ? `Deselect ${skill.name}` : `Select ${skill.name}`}
                    onClick={() => onToggleSelect(skill)}
                    className={cn(
                      'w-4 h-4 rounded border flex items-center justify-center transition-colors',
                      isSel
                        ? 'bg-primary/20 border-primary/50 text-primary'
                        : 'border-border/50 text-transparent hover:border-border',
                    )}
                  >
                    <Check size={11} />
                  </button>
                </td>
                <td className={cn('px-2', cellPad)}>
                  <StateBadge state={skill.state} />
                </td>
                <td className={cn('px-2 min-w-0', cellPad)}>
                  <button
                    type="button"
                    onClick={() => onSelect(skill)}
                    className="inline-flex items-center gap-1.5 text-left text-text-primary hover:text-primary transition-colors max-w-full"
                  >
                    <span className="font-medium truncate">{skill.name}</span>
                    {src && (
                      <GitBranch
                        size={11}
                        className="text-text-muted/50 flex-shrink-0"
                        aria-label={`Imported from ${src.repo}`}
                      />
                    )}
                  </button>
                </td>
                <td className={cn('px-2 text-xs font-mono text-text-muted', cellPad)}>{skill.fileCount}</td>
                {showUsage && (
                  <td className={cn('px-2 text-xs text-text-muted whitespace-nowrap', cellPad)}>{lastUsed}</td>
                )}
                <td className={cn('px-2 text-xs text-text-muted', cellPad)}>
                  {category ? toTitleCase(category) : '–'}
                </td>
                <td className={cn('px-2 text-right', cellPad)}>
                  <div className="inline-flex justify-end">
                    <SkillActions skill={skill} onToggle={handleToggle} onEdit={onEdit} onDelete={onDelete} />
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
