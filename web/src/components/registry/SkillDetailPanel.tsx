import { useEffect, useRef, useState, type ReactNode } from 'react';
import { BookOpen, GitBranch, Pencil, Power, PowerOff, Trash2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { extractRepoInfo } from '../../lib/repo';
import { toTitleCase } from '../../lib/text';
import { parseAcceptanceCriterion } from '../../lib/skillCriteria';
import { InspectorHeader, InspectorTabList, InspectorTabButton } from '../inspector';
import { IconButton } from '../ui/IconButton';
import { StateBadge } from './StateBadge';
import { MarkdownPreview } from './MarkdownPreview';
import { SkillFileTree } from './SkillFileTree';
import type { AgentSkill, SkillSourceStatus } from '../../types';

type SkillTab = 'overview' | 'instructions' | 'files';

const TABS: { key: SkillTab; label: string }[] = [
  { key: 'overview', label: 'Overview' },
  { key: 'instructions', label: 'Instructions' },
  { key: 'files', label: 'Files' },
];

const tabBtnId = (tab: SkillTab) => `skill-tab-${tab}`;
const tabPanelId = (tab: SkillTab) => `skill-tabpanel-${tab}`;

export interface SkillDetailPanelProps {
  /** The selected skill, or null for the empty state. */
  skill: AgentSkill | null;
  /** Owning source, when the skill was imported from a git source. */
  source?: SkillSourceStatus;
  /** Other skills in the same top-level category, for the "Related" list. */
  relatedSkills?: AgentSkill[];
  onClose: () => void;
  onEdit: (skill: AgentSkill) => void;
  onToggle: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  onSelectRelated?: (name: string) => void;
}

/**
 * SkillDetailPanel fills the Library workspace right rail with a read-first,
 * tabbed view of the selected skill (Overview / Instructions / Files). It is a
 * pure presentational sibling of the grid — selection lives in the workspace.
 * The header stays fixed across tabs; "Edit" promotes to the SkillEditor modal.
 */
export function SkillDetailPanel({
  skill,
  source,
  relatedSkills = [],
  onClose,
  onEdit,
  onToggle,
  onDelete,
  onSelectRelated,
}: SkillDetailPanelProps) {
  const [activeTab, setActiveTab] = useState<SkillTab>('overview');
  const tablistRef = useRef<HTMLDivElement>(null);

  // Reset to Overview when the selected skill changes, so switching skills never
  // strands the user on Files (which would refetch for the new skill).
  useEffect(() => {
    setActiveTab('overview');
  }, [skill?.name]);

  if (!skill) {
    return (
      <aside className="h-full flex flex-col bg-surface/40 backdrop-blur-sm border-l border-border-subtle">
        <SkillDetailEmpty />
      </aside>
    );
  }

  // APG tabs: Left/Right (and Home/End) move the active tab, focusing it.
  const onTabsKeyDown = (e: React.KeyboardEvent) => {
    const idx = TABS.findIndex((t) => t.key === activeTab);
    let next = idx;
    if (e.key === 'ArrowRight') next = (idx + 1) % TABS.length;
    else if (e.key === 'ArrowLeft') next = (idx - 1 + TABS.length) % TABS.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = TABS.length - 1;
    else return;
    e.preventDefault();
    setActiveTab(TABS[next].key);
    const buttons = tablistRef.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]');
    buttons?.[next]?.focus();
  };

  const repoInfo = source ? extractRepoInfo(source.repo) : null;

  return (
    <aside className="h-full flex flex-col bg-surface/40 backdrop-blur-sm border-l border-border-subtle">
      <InspectorHeader
        title={skill.name}
        icon={BookOpen}
        accent="primary"
        onClose={onClose}
        subtitle={
          <div className="flex items-center gap-1.5 flex-wrap mt-0.5">
            <StateBadge state={skill.state} />
            {source && (
              <span
                title={source.repo}
                className="inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-elevated text-text-muted"
              >
                <GitBranch size={10} />
                {repoInfo ? `${repoInfo.owner}/${repoInfo.repo}` : source.name}
              </span>
            )}
          </div>
        }
        actions={
          <div className="flex items-center gap-0.5">
            <button
              type="button"
              onClick={() => onEdit(skill)}
              className="flex items-center gap-1 px-2 py-1 text-[11px] font-medium text-primary hover:text-primary/80 bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-md transition-colors"
            >
              <Pencil size={11} /> Edit
            </button>
            <IconButton
              icon={skill.state === 'active' ? PowerOff : Power}
              size="sm"
              variant="ghost"
              onClick={() => onToggle(skill)}
              tooltip={skill.state === 'active' ? 'Disable skill' : 'Activate skill'}
              className={skill.state === 'active' ? 'hover:text-amber-400' : 'hover:text-emerald-400'}
            />
            <IconButton
              icon={Trash2}
              size="sm"
              variant="ghost"
              onClick={() => onDelete(skill)}
              tooltip="Delete skill"
              className="hover:text-status-error"
            />
          </div>
        }
      />

      <div ref={tablistRef} onKeyDown={onTabsKeyDown}>
        <InspectorTabList ariaLabel={`${skill.name} detail tabs`}>
          {TABS.map((tab) => (
            <InspectorTabButton
              key={tab.key}
              id={tabBtnId(tab.key)}
              active={activeTab === tab.key}
              onClick={() => setActiveTab(tab.key)}
              label={tab.label}
              controls={tabPanelId(tab.key)}
            />
          ))}
        </InspectorTabList>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark">
        {/* Overview */}
        <div
          role="tabpanel"
          id={tabPanelId('overview')}
          aria-labelledby={tabBtnId('overview')}
          hidden={activeTab !== 'overview'}
          className="px-4 py-4 space-y-5"
        >
          {activeTab === 'overview' && (
            <SkillOverview
              skill={skill}
              relatedSkills={relatedSkills}
              onSelectRelated={onSelectRelated}
            />
          )}
        </div>

        {/* Instructions */}
        <div
          role="tabpanel"
          id={tabPanelId('instructions')}
          aria-labelledby={tabBtnId('instructions')}
          hidden={activeTab !== 'instructions'}
          className="px-4 py-4"
        >
          {activeTab === 'instructions' && (
            <MarkdownPreview
              content={skill.body}
              emptyHint="This skill has no instructions."
            />
          )}
        </div>

        {/* Files */}
        <div
          role="tabpanel"
          id={tabPanelId('files')}
          aria-labelledby={tabBtnId('files')}
          hidden={activeTab !== 'files'}
        >
          {/* Mount the tree only while the Files tab is active so switching
              skills/tabs doesn't fire the file fetch for unviewed tabs. */}
          {activeTab === 'files' && <SkillFileTree skillName={skill.name} readOnly />}
        </div>
      </div>
    </aside>
  );
}

function SkillOverview({
  skill,
  relatedSkills,
  onSelectRelated,
}: {
  skill: AgentSkill;
  relatedSkills: AgentSkill[];
  onSelectRelated?: (name: string) => void;
}) {
  const category = skill.dir ? toTitleCase(skill.dir.split('/')[0]) : null;
  const tools = (skill.allowedTools ?? '').split(/\s+/).filter(Boolean);
  const metadataEntries = Object.entries(skill.metadata ?? {});
  const criteria = skill.acceptanceCriteria ?? [];

  return (
    <>
      <Section title="Description">
        {skill.description ? (
          <p className="text-xs text-text-secondary leading-relaxed whitespace-pre-wrap break-words">
            {skill.description}
          </p>
        ) : (
          <p className="text-[11px] text-text-muted/70 italic">No description.</p>
        )}
      </Section>

      <Section title="Details">
        <dl className="space-y-1.5">
          {category && <MetaRow label="Category" value={category} />}
          {skill.license && <MetaRow label="License" value={skill.license} mono />}
          {skill.compatibility && <MetaRow label="Compatibility" value={skill.compatibility} />}
          <MetaRow label="Files" value={String(skill.fileCount)} mono />
        </dl>
      </Section>

      {tools.length > 0 && (
        <Section title="Allowed tools">
          <div className="flex flex-wrap gap-1">
            {tools.map((tool) => (
              <span
                key={tool}
                className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-elevated text-text-secondary border border-border/30"
              >
                {tool}
              </span>
            ))}
          </div>
        </Section>
      )}

      {metadataEntries.length > 0 && (
        <Section title="Metadata">
          <dl className="space-y-1.5">
            {metadataEntries.map(([key, value]) => (
              <MetaRow key={key} label={key} value={value} mono />
            ))}
          </dl>
        </Section>
      )}

      {criteria.length > 0 && (
        <Section title="Acceptance criteria">
          <ul className="space-y-2">
            {criteria.map((raw, i) => {
              const c = parseAcceptanceCriterion(raw);
              return (
                <li
                  key={i}
                  className="rounded-lg border border-border/30 bg-background/40 p-2.5 space-y-1"
                >
                  {c.matched ? (
                    <>
                      <CriterionRow keyword="GIVEN" text={c.given} />
                      <CriterionRow keyword="WHEN" text={c.when} />
                      <CriterionRow keyword="THEN" text={c.then} />
                    </>
                  ) : (
                    <p className="text-xs text-text-secondary leading-relaxed">{c.raw}</p>
                  )}
                </li>
              );
            })}
          </ul>
        </Section>
      )}

      {relatedSkills.length > 0 && (
        <Section title="Related skills">
          <div className="space-y-1">
            {relatedSkills.map((rel) => (
              <button
                key={rel.name}
                type="button"
                onClick={() => onSelectRelated?.(rel.name)}
                className="w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-left hover:bg-surface-highlight/60 transition-colors group"
              >
                <BookOpen size={12} className="text-text-muted group-hover:text-primary/70 flex-shrink-0 transition-colors" />
                <span className="text-xs text-text-secondary group-hover:text-text-primary truncate flex-1 transition-colors">
                  {rel.name}
                </span>
                <StateBadge state={rel.state} />
              </button>
            ))}
          </div>
        </Section>
      )}
    </>
  );
}

function CriterionRow({ keyword, text }: { keyword: string; text: string }) {
  return (
    <div className="flex items-start gap-2">
      <span className="text-[9px] text-text-muted uppercase tracking-wider w-10 pt-0.5 flex-shrink-0 font-mono">
        {keyword}
      </span>
      <span className="text-xs text-text-secondary leading-relaxed flex-1 break-words">{text}</span>
    </div>
  );
}

function MetaRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="text-[11px] text-text-muted flex-shrink-0">{label}</dt>
      <dd className={cn('text-[11px] text-text-secondary text-right break-words', mono && 'font-mono')}>
        {value}
      </dd>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">{title}</h3>
      {children}
    </section>
  );
}

function SkillDetailEmpty() {
  return (
    <div className="h-full flex items-center justify-center px-6 text-center">
      <div className="space-y-3">
        <div className="mx-auto w-12 h-12 rounded-2xl bg-surface-highlight/40 border border-border/40 flex items-center justify-center">
          <BookOpen size={20} className="text-text-muted/60" aria-hidden="true" />
        </div>
        <p className="text-xs text-text-muted leading-relaxed max-w-[220px]">
          Select a skill to inspect its details, instructions, and files.
        </p>
      </div>
    </div>
  );
}
