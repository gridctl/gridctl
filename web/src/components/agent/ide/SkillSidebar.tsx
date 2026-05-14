import { useRef, useState } from 'react';
import { type SkillSummary } from '../../../lib/agent-api';
import { cn } from '../../../lib/cn';
import { SkillRunButton } from './SkillRunButton';
import { RunsList } from './RunsList';

type SidebarTab = 'skills' | 'runs';

interface SkillSidebarProps {
  skills: SkillSummary[];
  active: string | null;
  onSelect: (name: string) => void;
  onRunSkill?: (name: string, originRef: React.RefObject<HTMLButtonElement | null>) => void;
  loading?: boolean;
  error?: string | null;
  /**
   * AgentIDE bumps this each time a run is launched — the Runs tab
   * uses it to re-fetch the list so the newly-started run appears
   * at the top without waiting for the next mount.
   */
  runsRefreshKey: number;
  /**
   * Currently-active run id from the URL. Highlighted in the Runs
   * list so the operator can scan back to where they are.
   */
  activeRunID: string | null;
}

/**
 * SkillSidebar is the IDE's left rail. The Skills tab lists every
 * typed skill the project exposes; the Runs tab lists recent agent
 * runs (across all skills) with parent-child indent so nested
 * `handoff` calls render their hierarchy. The rail is dense and
 * monospace by design; this is a developer tool, not a marketing
 * surface.
 */
export function SkillSidebar({
  skills,
  active,
  onSelect,
  onRunSkill,
  loading,
  error,
  runsRefreshKey,
  activeRunID,
}: SkillSidebarProps) {
  const [tab, setTab] = useState<SidebarTab>('skills');
  return (
    <aside
      className="h-full bg-surface/30 border-r border-border-subtle flex flex-col overflow-hidden"
      aria-label="Skills and runs"
    >
      <header className="px-5 pt-6 pb-4 border-b border-border-subtle">
        <div className="font-sans text-text-muted/70 text-[10px] uppercase tracking-[0.4em] mb-1">
          gridctl
        </div>
        <div className="flex items-baseline gap-2">
          <h1 className="font-sans text-text-primary text-xl font-semibold tracking-tight">
            agent ide
          </h1>
          <span className="font-mono text-[10px] text-text-muted/70 uppercase tracking-[0.2em]">
            phase F
          </span>
        </div>
        <p className="font-sans text-text-muted text-xs mt-2 leading-snug">
          Code is canon — the canvas is the derived view.
        </p>
      </header>

      <div
        role="tablist"
        aria-label="Sidebar sections"
        className="px-5 pt-3 flex items-center gap-1 border-b border-border-subtle/50"
      >
        <TabButton
          active={tab === 'skills'}
          onClick={() => setTab('skills')}
          label="Skills"
          controls="sidebar-tab-skills"
        />
        <TabButton
          active={tab === 'runs'}
          onClick={() => setTab('runs')}
          label="Runs"
          controls="sidebar-tab-runs"
        />
      </div>

      <div className="flex-1 overflow-y-auto py-3">
        {tab === 'skills' && (
          <div role="tabpanel" id="sidebar-tab-skills" aria-label="Skills">
            <div className="px-5 mb-3 flex items-center justify-between">
              <span className="font-sans text-text-muted text-[10px] uppercase tracking-[0.3em]">
                skills
              </span>
              {loading && (
                <span className="font-mono text-[10px] text-text-muted/70 animate-pulse">
                  loading…
                </span>
              )}
            </div>
            {error && (
              <div className="mx-5 mb-3 px-3 py-2 rounded text-xs text-status-error bg-status-error/5 border border-status-error/20">
                {error}
              </div>
            )}
            {!loading && skills.length === 0 && !error && (
              <div className="mx-5 my-8 text-center text-text-muted text-xs leading-relaxed">
                <div className="font-sans text-text-muted/40 text-[10px] uppercase tracking-[0.4em] mb-2">
                  empty project
                </div>
                <p>
                  No SKILL.md found. Run{' '}
                  <code className="font-mono text-text-secondary">gridctl agent init</code> to
                  scaffold a starter.
                </p>
              </div>
            )}
            <ul className="space-y-px px-2">
              {skills.map((s) => (
                <SkillRow
                  key={s.name}
                  skill={s}
                  active={active === s.name}
                  onSelect={onSelect}
                  onRunSkill={onRunSkill}
                />
              ))}
            </ul>
          </div>
        )}

        {tab === 'runs' && (
          <div role="tabpanel" id="sidebar-tab-runs" aria-label="Runs">
            <div className="px-5 mb-3 flex items-center justify-between">
              <span className="font-sans text-text-muted text-[10px] uppercase tracking-[0.3em]">
                recent runs
              </span>
            </div>
            <RunsList refreshKey={runsRefreshKey} activeRunID={activeRunID} />
          </div>
        )}
      </div>
    </aside>
  );
}

interface TabButtonProps {
  active: boolean;
  onClick: () => void;
  label: string;
  controls: string;
}

function TabButton({ active, onClick, label, controls }: TabButtonProps) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      aria-controls={controls}
      onClick={onClick}
      className={cn(
        'px-3 py-1.5 -mb-px',
        'font-mono text-[10px] uppercase tracking-[0.2em]',
        'border-b-2 transition-colors',
        active
          ? 'border-primary text-text-primary'
          : 'border-transparent text-text-muted hover:text-text-primary',
        'focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60',
      )}
    >
      {label}
    </button>
  );
}

interface SkillRowProps {
  skill: SkillSummary;
  active: boolean;
  onSelect: (name: string) => void;
  onRunSkill?: (name: string, originRef: React.RefObject<HTMLButtonElement | null>) => void;
}

function SkillRow({ skill: s, active, onSelect, onRunSkill }: SkillRowProps) {
  const runButtonRef = useRef<HTMLButtonElement>(null);
  const canRun = s.lang === 'ts' && Boolean(onRunSkill);
  return (
    <li>
      <div
        className={cn(
          'group relative flex items-stretch rounded-md transition-colors',
          'border border-transparent',
          active
            ? 'bg-surface-elevated/80 border-border-subtle'
            : 'hover:bg-surface/50 hover:border-border-subtle/60',
        )}
      >
        <button
          type="button"
          onClick={() => onSelect(s.name)}
          className="flex-1 min-w-0 text-left px-3 py-2"
        >
          <div className="flex items-center gap-2 mb-0.5">
            <span
              className={cn(
                'font-mono text-[10px] px-1.5 py-px rounded uppercase tracking-[0.16em]',
                s.lang === 'go'
                  ? 'bg-secondary/15 text-secondary-light border border-secondary/30'
                  : s.lang === 'ts'
                    ? 'bg-tertiary/15 text-tertiary-light border border-tertiary/30'
                    : 'bg-surface text-text-muted border border-border',
              )}
            >
              {s.lang || '—'}
            </span>
            <span className="font-mono text-sm text-text-primary truncate">{s.name}</span>
          </div>
          <div className="flex items-center justify-between font-mono text-[10px] text-text-muted">
            <span className="truncate">{s.dir === '.' ? '·' : s.dir}</span>
            <span className="tabular-nums">
              {s.has_error ? '⚠' : ''} {s.node_count} {s.node_count === 1 ? 'node' : 'nodes'}
            </span>
          </div>
        </button>
        {canRun && (
          <div className="flex items-center pr-2">
            <SkillRunButton
              ref={runButtonRef}
              skillName={s.name}
              onClick={() => onRunSkill?.(s.name, runButtonRef)}
            />
          </div>
        )}
      </div>
    </li>
  );
}
