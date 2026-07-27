import { useCallback, useRef, useState, type ReactNode } from 'react';
import { AlertTriangle, BookOpen, Code2, Eye, GitBranch, GitCompareArrows, Pencil, Power, PowerOff, Trash2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { updateSkillSource } from '../../lib/api';
import { summarizeSkillResults, syncCountsMessage } from '../../lib/skillSync';
import { showToast } from '../ui/Toast';
import { extractRepoInfo } from '../../lib/repo';
import { toTitleCase } from '../../lib/text';
import { parseAcceptanceCriterion } from '../../lib/skillCriteria';
import { skillCategory } from '../../lib/skillMeta';
import { formatLastUsed } from '../../lib/toolAudit';
import { InspectorHeader, InspectorTabList, InspectorTabButton, PaneAnchor } from '../inspector';
import { IconButton } from '../ui/IconButton';
import { ZoomControls } from '../ui/ZoomControls';
import { StateBadge } from './StateBadge';
import { MarkdownPreview } from './MarkdownPreview';
import { SkillFileTree } from './SkillFileTree';
import { SkillCompareDialog } from './SkillCompareDialog';
import { useSkillBody } from '../../hooks/useSkillBody';
import { useSkillFiles } from '../../hooks/useSkillFiles';
import { formatManagedDirs, missingManagedDirs } from '../../lib/skillPackage';
import { useTextZoom } from '../../hooks/useTextZoom';
import type { AgentSkill, SkillSourceStatus, SkillUsageStat } from '../../types';

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
  /**
   * Whether the usage endpoint is available. When false, the Usage section is
   * omitted entirely (no column/KPI/strip on graceful degradation). When true,
   * a skill with no `usage` entry is shown as "no recorded calls".
   */
  usageTracked?: boolean;
  /** This skill's usage, joined by name. Undefined means zero recorded calls. */
  usage?: SkillUsageStat;
  /** When the gateway began recording usage, to label the young-window case. */
  observedSince?: string | null;
  onClose: () => void;
  onEdit: (skill: AgentSkill) => void;
  onToggle: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  onSelectRelated?: (name: string) => void;
  /** Refresh the registry after a reconciliation (take upstream, or a sync). */
  onRefresh?: () => void;
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
  usageTracked = false,
  usage,
  observedSince,
  onClose,
  onEdit,
  onToggle,
  onDelete,
  onSelectRelated,
  onRefresh,
}: SkillDetailPanelProps) {
  const [activeTab, setActiveTab] = useState<SkillTab>('overview');
  const [viewSource, setViewSource] = useState(false);
  const [showCompare, setShowCompare] = useState(false);
  const [prevName, setPrevName] = useState(skill?.name);
  const tablistRef = useRef<HTMLDivElement>(null);

  // Content text-size control for the rendered Instructions markdown. Scoped to
  // this pane (own storageKey) like the Logs/Traces zoom; the .skill-md scope
  // reads --text-zoom-size off the wrapper, so 13px stays the default.
  const instructionsRef = useRef<HTMLDivElement>(null);
  const {
    fontSize: instrFontSize,
    zoomIn: instrZoomIn,
    zoomOut: instrZoomOut,
    resetZoom: instrResetZoom,
    isMin: instrIsMin,
    isMax: instrIsMax,
    isDefault: instrIsDefault,
  } = useTextZoom({
    storageKey: 'gridctl-library-zoom',
    defaultSize: 13,
    containerRef: instructionsRef,
  });

  // Reset to Overview (and rendered view) when the selected skill changes, so
  // switching skills never strands the user on Files (which would refetch for
  // the new skill) or on a raw-source view of the previous skill. Adjusting
  // state during render (rather than in an effect) avoids a cascading re-render.
  if (skill?.name !== prevName) {
    setPrevName(skill?.name);
    setActiveTab('overview');
    setViewSource(false);
    setShowCompare(false);
  }

  // One body fetch per selected skill, shared by the Overview package check and
  // the Instructions tab. Hoisted rather than fetched per tab so opening
  // Instructions is instant and the package check does not need a second copy.
  // Declared above the empty-state return: hooks cannot run conditionally.
  const { body, loading: bodyLoading, error: bodyError } = useSkillBody(
    skill?.name ?? null,
    true,
    skill?.body,
  );

  // Re-run the owning source's update so a package missing its supporting files
  // gets them. Same endpoint the per-source header sync uses.
  const [syncing, setSyncing] = useState(false);
  const handleSyncSource = useCallback(async () => {
    if (!source || syncing) return;
    setSyncing(true);
    try {
      const { results } = await updateSkillSource(source.name);
      const detail = syncCountsMessage(summarizeSkillResults(results));
      showToast('success', detail ? `Synced "${source.name}": ${detail}` : `"${source.name}" is up to date`);
      onRefresh?.();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Sync failed');
    } finally {
      setSyncing(false);
    }
  }, [source, syncing, onRefresh]);

  if (!skill) {
    return (
      <aside className="relative h-full flex flex-col bg-surface-elevated border-l border-border">
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
  const hasLocalEdits = source?.driftedSkills?.includes(skill.name) ?? false;


  return (
    <aside className="relative h-full flex flex-col bg-surface-elevated border-l border-border">
      <PaneAnchor />
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
            {/* The chip is the natural place to ask "modified how?", so it
                opens the same diff the editor's Compare action does. */}
            {hasLocalEdits && source && (
              <button
                type="button"
                onClick={() => setShowCompare(true)}
                aria-label={`Compare ${skill.name} with upstream`}
                title="Edited locally; a sync will skip this unless you overwrite. Click to compare with upstream."
                className="inline-flex items-center gap-1 text-[10px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-amber-400/30 bg-amber-400/10 text-amber-300 hover:bg-amber-400/20 transition-colors"
              >
                <GitCompareArrows size={9} aria-hidden="true" />
                Modified
              </button>
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
              body={body}
              source={source}
              onSyncSource={handleSyncSource}
              syncing={syncing}
              relatedSkills={relatedSkills}
              usageTracked={usageTracked}
              usage={usage}
              observedSince={observedSince}
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
          className="px-4 py-4 space-y-3"
          ref={instructionsRef}
          style={{ '--text-zoom-size': `${instrFontSize}px` } as React.CSSProperties}
        >
          {/* Mounted only while the tab is active, so the body fetch fires on
              tab open rather than on every selection. */}
          {activeTab === 'instructions' && (
            <SkillInstructions
              body={body}
              loading={bodyLoading}
              error={bodyError}
              viewSource={viewSource}
              onToggleSource={() => setViewSource((v) => !v)}
              zoom={{
                fontSize: instrFontSize,
                zoomIn: instrZoomIn,
                zoomOut: instrZoomOut,
                resetZoom: instrResetZoom,
                isMin: instrIsMin,
                isMax: instrIsMax,
                isDefault: instrIsDefault,
              }}
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

      {source && (
        <SkillCompareDialog
          isOpen={showCompare}
          sourceName={source.name}
          skillName={skill.name}
          onClose={() => setShowCompare(false)}
          onTookUpstream={() => { setShowCompare(false); onRefresh?.(); }}
        />
      )}
    </aside>
  );
}

/**
 * Warns that a skill's instructions expect supporting files that are not
 * installed. This is a safety net, not the fix: import copies `scripts/`,
 * `references/`, and `assets/` as of the supporting-file installer, so a skill
 * that trips this was installed before that and needs a re-sync. Without the
 * warning the failure is silent, surfacing only as an agent that follows an
 * instruction to run a file that is not on disk.
 */
function IncompletePackageNotice({
  skill,
  body,
  source,
  onSync,
  syncing,
}: {
  skill: AgentSkill;
  body: string | null;
  source?: SkillSourceStatus;
  onSync?: () => void;
  syncing?: boolean;
}) {
  // A skill reporting zero files needs no request to answer "what is
  // installed"; only a skill that has files pays for the lookup.
  const { files } = useSkillFiles(skill.name, skill.fileCount === 0);
  const missing = missingManagedDirs(body, files);
  if (missing.length === 0) return null;

  return (
    <div
      role="status"
      className="flex items-start gap-2 rounded-lg border border-amber-400/30 bg-amber-400/10 px-3 py-2.5"
    >
      <AlertTriangle size={14} className="text-amber-300 flex-shrink-0 mt-0.5" aria-hidden="true" />
      <div className="space-y-1.5 min-w-0">
        <p className="text-[11px] text-amber-200 leading-relaxed">
          These instructions reference {formatManagedDirs(missing)}, but no such files are
          installed. Steps that invoke them will fail at run time.
        </p>
        {source ? (
          <button
            type="button"
            onClick={onSync}
            disabled={syncing}
            className="text-[11px] font-medium text-amber-200 underline underline-offset-2 hover:text-amber-100 disabled:opacity-60 disabled:no-underline transition-colors"
          >
            {syncing ? 'Syncing…' : `Sync from ${source.name}`}
          </button>
        ) : (
          <p className="text-[10px] text-amber-200/70 leading-relaxed">
            This is a local skill, so add the files directly or re-import it from its source.
          </p>
        )}
      </div>
    </div>
  );
}

interface SkillInstructionsProps {
  /** The loaded body, or null while unknown. */
  body: string | null;
  loading: boolean;
  error: string | null;
  viewSource: boolean;
  onToggleSource: () => void;
  zoom: {
    fontSize: number;
    zoomIn: () => void;
    zoomOut: () => void;
    resetZoom: () => void;
    isMin: boolean;
    isMax: boolean;
    isDefault: boolean;
  };
}

/**
 * Instructions tab body. The registry list no longer carries Markdown bodies;
 * the panel loads one and passes it here, along with the read controls (text
 * size, rendered/source) that only make sense once there is content.
 */
function SkillInstructions({ body, loading, error, viewSource, onToggleSource, zoom }: SkillInstructionsProps) {
  if (loading) {
    return <p className="text-[11px] text-text-muted">Loading instructions…</p>;
  }
  if (error) {
    return (
      <p className="text-[11px] text-status-error" role="alert">
        Could not load instructions: {error}
      </p>
    );
  }

  return (
    <>
      {body && (
        <div className="flex items-center justify-between gap-2">
          {/* Text-size control acts on the rendered markdown only, so hide it
              in raw-source view where it would do nothing. */}
          {viewSource ? (
            <span />
          ) : (
            <ZoomControls
              fontSize={zoom.fontSize}
              onZoomIn={zoom.zoomIn}
              onZoomOut={zoom.zoomOut}
              onReset={zoom.resetZoom}
              isMin={zoom.isMin}
              isMax={zoom.isMax}
              isDefault={zoom.isDefault}
            />
          )}
          <button
            type="button"
            onClick={onToggleSource}
            aria-pressed={viewSource}
            className="flex items-center gap-1.5 px-2 py-1 text-[11px] font-medium text-text-muted hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight border border-border/40 rounded-md transition-colors"
          >
            {viewSource ? <Eye size={11} /> : <Code2 size={11} />}
            {viewSource ? 'Rendered' : 'View source'}
          </button>
        </div>
      )}
      {viewSource ? (
        <pre className="text-xs font-mono text-text-secondary whitespace-pre-wrap break-words bg-background/40 border border-border/30 rounded-lg p-3 overflow-x-auto">
          {body}
        </pre>
      ) : (
        <MarkdownPreview
          content={body ?? ''}
          emptyHint="This skill has no instructions."
        />
      )}
    </>
  );
}

function SkillOverview({
  skill,
  body,
  source,
  onSyncSource,
  syncing,
  relatedSkills,
  usageTracked,
  usage,
  observedSince,
  onSelectRelated,
}: {
  skill: AgentSkill;
  body: string | null;
  source?: SkillSourceStatus;
  onSyncSource?: () => void;
  syncing?: boolean;
  relatedSkills: AgentSkill[];
  usageTracked: boolean;
  usage?: SkillUsageStat;
  observedSince?: string | null;
  onSelectRelated?: (name: string) => void;
}) {
  const categoryKey = skillCategory(skill.dir, skill.metadata);
  const category = categoryKey ? toTitleCase(categoryKey) : null;
  const tools = (skill.allowedTools ?? '').split(/\s+/).filter(Boolean);
  const metadataEntries = Object.entries(skill.metadata ?? {});
  const criteria = skill.acceptanceCriteria ?? [];
  const calls = usage?.calls ?? 0;

  return (
    <>
      <IncompletePackageNotice
        skill={skill}
        body={body}
        source={source}
        onSync={onSyncSource}
        syncing={syncing}
      />

      <Section title="Description">
        {skill.description ? (
          <p className="text-xs text-text-secondary leading-relaxed whitespace-pre-wrap break-words">
            {skill.description}
          </p>
        ) : (
          <p className="text-[11px] text-text-muted/70 italic">No description.</p>
        )}
      </Section>

      {usageTracked && (
        <Section title="Usage">
          {calls > 0 ? (
            <p className="text-xs text-text-secondary">
              Last used {usage?.lastCalledAt ? formatLastUsed(usage.lastCalledAt) : 'recently'} ·{' '}
              {calls} {calls === 1 ? 'call' : 'calls'}
            </p>
          ) : (
            <p className="text-[11px] text-text-muted/80 leading-relaxed">
              No recorded calls{observedSince ? `, tracking since ${formatLastUsed(observedSince)}` : ''}.
              Counts are cumulative across served clients, so a zero may mean the skill predates tracking.
            </p>
          )}
        </Section>
      )}

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
