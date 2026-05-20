import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  fetchSkill,
  fetchSkills,
  type SkillGraph,
  type SkillSummary,
} from '../../lib/agent-api';
import { SkillSidebar } from '../agent/ide/SkillSidebar';
import { NodeList } from '../agent/ide/NodeList';
import { Canvas } from '../agent/ide/Canvas';
import { NodeDetail } from '../agent/ide/NodeDetail';
import { RunLauncherModal, type SkillForLaunch } from '../agent/ide/RunLauncherModal';
import { useDevSocket } from '../../hooks/useDevSocket';
import { useRunTrace } from '../agent/ide/useRunTrace';
import { useSkillsCommands } from '../skills/useSkillsCommands';
import { useUIStore } from '../../stores/useUIStore';
import { cn } from '../../lib/cn';
import { WorkspaceShell } from '../layout/WorkspaceShell';

type ViewMode = 'list' | 'canvas';

// Default rail sizes as percentages of a 1440px reference width:
// 280/1440 ≈ 19%, 360/1440 = 25%; compact 220/1440 ≈ 15%, 300/1440 ≈ 21%.
const DEFAULT_SIDEBAR_PCT = 19;
const DEFAULT_INSPECTOR_PCT = 25;
const COMPACT_SIDEBAR_PCT = 15;
const COMPACT_INSPECTOR_PCT = 21;

/**
 * SkillsWorkspace is the Stage tab — the developer's per-skill Agent IDE
 * inside the unified shell. The id stays `'skills'` and the route stays
 * `/skills` to keep stored state, deep-links, and the Go API stable, but
 * the user-facing label is "Stage" (see workspace.ts WORKSPACE_CONFIG).
 * Behaviors preserved from the legacy `/agent` IDE: typed-skill sidebar,
 * list/canvas toggle, trace overlay, run launcher. Adds deep-link
 * continuity (`/skills?skill=foo&run=abc`), workspace-scoped command
 * palette commands, and Compact Mode.
 */
export function SkillsWorkspace() {
  const [params, setParams] = useSearchParams();
  const activeSkill = params.get('skill');
  const runID = params.get('run');
  const viewParam = params.get('view') as ViewMode | null;
  const [viewMode, setViewMode] = useState<ViewMode>(viewParam ?? 'list');

  const [skills, setSkills] = useState<SkillSummary[]>([]);
  const [skillsLoading, setSkillsLoading] = useState(true);
  const [skillsError, setSkillsError] = useState<string | null>(null);

  const [graph, setGraph] = useState<SkillGraph | null>(null);
  const [graphLoading, setGraphLoading] = useState(false);
  const [graphError, setGraphError] = useState<string | null>(null);

  const [selectedNode, setSelectedNode] = useState<string | null>(null);

  const [launcher, setLauncher] = useState<{
    skill: SkillForLaunch;
    originRef: RefObject<HTMLButtonElement | null>;
  } | null>(null);

  const [runsRefreshKey, setRunsRefreshKey] = useState(0);

  const compact = useUIStore((s) => s.compactMode.skills);

  const { lastEvent, connectionState } = useDevSocket(true);
  const runTrace = useRunTrace(runID);

  const refreshSkills = useCallback(async () => {
    setSkillsLoading(true);
    setSkillsError(null);
    try {
      setSkills(await fetchSkills());
    } catch (err) {
      setSkillsError(err instanceof Error ? err.message : String(err));
    } finally {
      setSkillsLoading(false);
    }
  }, []);

  useEffect(() => {
    void refreshSkills();
  }, [refreshSkills]);

  useEffect(() => {
    if (!activeSkill) {
      setGraph(null);
      return;
    }
    let cancelled = false;
    setGraphLoading(true);
    setGraphError(null);
    fetchSkill(activeSkill)
      .then((g) => {
        if (cancelled) return;
        setGraph(g);
      })
      .catch((err) => {
        if (cancelled) return;
        setGraphError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (cancelled) return;
        setGraphLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [activeSkill, lastEvent?.path, lastEvent?.time]);

  // Auto-select first skill once skills load and none is active.
  useEffect(() => {
    if (!activeSkill && skills.length > 0) {
      const next = new URLSearchParams(params);
      next.set('skill', skills[0].name);
      setParams(next, { replace: true });
    }
  }, [activeSkill, skills, params, setParams]);

  // Persist viewMode to URL.
  useEffect(() => {
    const next = new URLSearchParams(params);
    if (viewMode === 'list') {
      next.delete('view');
    } else {
      next.set('view', viewMode);
    }
    if (next.toString() !== params.toString()) {
      setParams(next, { replace: true });
    }
  }, [viewMode, params, setParams]);

  const skillDir = useMemo(() => {
    if (!graph) return '';
    const summary = skills.find((s) => s.name === graph.skill);
    return summary?.dir ?? '';
  }, [graph, skills]);

  const handleSelectSkill = useCallback(
    (name: string) => {
      const next = new URLSearchParams(params);
      next.set('skill', name);
      setSelectedNode(null);
      setParams(next, { replace: true });
    },
    [params, setParams],
  );

  const handleRunSkill = useCallback(
    (name: string, originRef: RefObject<HTMLButtonElement | null>) => {
      setLauncher({ skill: { name }, originRef });
    },
    [],
  );

  const handleLaunched = useCallback(
    (response: { run_id: string; started_at: string }) => {
      const next = new URLSearchParams(params);
      if (launcher?.skill.name) next.set('skill', launcher.skill.name);
      next.set('run', response.run_id);
      setParams(next, { replace: true });
      setLauncher(null);
      setRunsRefreshKey((k) => k + 1);
    },
    [params, setParams, launcher],
  );

  const selectedNodeObj = useMemo(() => {
    if (!graph || !selectedNode) return null;
    return graph.nodes.find((n) => n.id === selectedNode) ?? null;
  }, [graph, selectedNode]);

  // Workspace-scoped palette commands: registered while this workspace is
  // mounted, torn down on unmount so /topology and /runs don't see them.
  useSkillsCommands({
    skills,
    activeSkill,
    viewMode,
    onSetView: setViewMode,
    onSelectSkill: handleSelectSkill,
    onLaunchSkill: handleRunSkill,
    onRefresh: refreshSkills,
  });

  const defaultLeftPct = compact ? COMPACT_SIDEBAR_PCT : DEFAULT_SIDEBAR_PCT;
  const defaultRightPct = compact ? COMPACT_INSPECTOR_PCT : DEFAULT_INSPECTOR_PCT;

  return (
    <div className="absolute inset-0 flex bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="skills"
        defaultLeftPct={defaultLeftPct}
        defaultRightPct={defaultRightPct}
        minLeftPx={220}
        minRightPx={300}
        left={
          <SkillSidebar
            skills={skills}
            active={activeSkill}
            onSelect={handleSelectSkill}
            onRunSkill={handleRunSkill}
            loading={skillsLoading}
            error={skillsError}
            runsRefreshKey={runsRefreshKey}
            activeRunID={runID}
          />
        }
        right={
          <NodeDetail
            node={selectedNodeObj}
            skillDir={skillDir}
            trace={selectedNode ? runTrace.byNode[selectedNode] : undefined}
            runID={runID}
            runTrace={runTrace}
            onClose={() => setSelectedNode(null)}
          />
        }
      >
        <main className="flex flex-col h-full overflow-hidden bg-background">
          <Toolbar
            graph={graph}
            viewMode={viewMode}
            onSetViewMode={setViewMode}
            connectionState={connectionState}
            runID={runID}
            runStatus={runTrace.status}
          />

          <div className="flex-1 overflow-y-auto relative">
            {graphError && (
              <div className="m-6 px-4 py-3 rounded-md border border-status-error/30 bg-status-error/5 text-status-error text-sm font-mono">
                {graphError}
              </div>
            )}
            {graphLoading && !graph && (
              <div className="absolute inset-0 flex items-center justify-center text-text-muted text-sm font-mono">
                <span className="animate-pulse">parsing…</span>
              </div>
            )}
            {!graph && !graphLoading && !graphError && <Welcome />}
            {graph && viewMode === 'list' && (
              <NodeList
                graph={graph}
                skillDir={skillDir}
                selected={selectedNode}
                onSelect={setSelectedNode}
                trace={runTrace.byNode}
              />
            )}
            {graph && viewMode === 'canvas' && (
              <Canvas
                graph={graph}
                skillDir={skillDir}
                selected={selectedNode}
                onSelect={setSelectedNode}
                trace={runTrace.byNode}
              />
            )}
          </div>
        </main>
      </WorkspaceShell>

      {launcher && (
        <RunLauncherModal
          skill={launcher.skill}
          returnFocusRef={launcher.originRef}
          onClose={() => setLauncher(null)}
          onLaunched={handleLaunched}
        />
      )}
    </div>
  );
}

interface ToolbarProps {
  graph: SkillGraph | null;
  viewMode: ViewMode;
  onSetViewMode: (m: ViewMode) => void;
  connectionState: 'connecting' | 'open' | 'error';
  runID: string | null;
  runStatus: 'connecting' | 'open' | 'error' | 'idle';
}

function Toolbar({
  graph,
  viewMode,
  onSetViewMode,
  connectionState,
  runID,
  runStatus,
}: ToolbarProps) {
  return (
    <header className="px-6 py-3 border-b border-border-subtle bg-surface/30 backdrop-blur-sm flex items-center gap-4">
      <div className="flex items-center gap-3 min-w-0 flex-1">
        {graph ? (
          <>
            <div className="font-mono text-sm text-text-primary truncate">{graph.skill}</div>
            <div className="font-mono text-[10px] text-text-muted/70 uppercase tracking-[0.2em]">
              {graph.lang}
            </div>
            <div className="font-mono text-[10px] text-text-muted">
              {graph.nodes.length} {graph.nodes.length === 1 ? 'node' : 'nodes'}
            </div>
          </>
        ) : (
          <div className="font-mono text-sm text-text-muted">no skill selected</div>
        )}
      </div>

      <div className="flex items-center gap-2">
        <ViewToggle current={viewMode} onChange={onSetViewMode} />
        <ConnectionDot label="watch" state={connectionState} />
        {runID && (
          <ConnectionDot
            label={`run ${runID.slice(0, 8)}`}
            state={runStatus === 'idle' ? 'connecting' : runStatus}
          />
        )}
      </div>
    </header>
  );
}

function ViewToggle({
  current,
  onChange,
}: {
  current: ViewMode;
  onChange: (m: ViewMode) => void;
}) {
  // Refs hand the cmd-palette "Toggle view" command a focus target so the
  // active mode pill remains the visible affordance after invocation.
  const listRef = useRef<HTMLButtonElement>(null);
  const canvasRef = useRef<HTMLButtonElement>(null);
  return (
    <div className="inline-flex rounded border border-border-subtle p-px bg-surface">
      {(['list', 'canvas'] as const).map((mode) => (
        <button
          key={mode}
          ref={mode === 'list' ? listRef : canvasRef}
          type="button"
          onClick={() => onChange(mode)}
          className={cn(
            'px-3 py-1 text-[10px] uppercase tracking-[0.2em] font-mono rounded-sm transition-colors',
            current === mode
              ? 'bg-surface-elevated text-text-primary'
              : 'text-text-muted hover:text-text-primary',
          )}
        >
          {mode}
        </button>
      ))}
    </div>
  );
}

function ConnectionDot({
  label,
  state,
}: {
  label: string;
  state: 'connecting' | 'open' | 'error';
}) {
  const color =
    state === 'open'
      ? 'bg-status-running'
      : state === 'error'
      ? 'bg-status-error/60'
      : 'bg-status-pending/60';
  const glow = state === 'open' ? '0 0 6px var(--color-status-running)' : 'none';
  return (
    <span className="inline-flex items-center gap-1.5 font-mono text-[10px] text-text-muted">
      <span
        aria-hidden
        className={cn('w-1.5 h-1.5 rounded-full', color)}
        style={{ boxShadow: glow }}
      />
      {label}
    </span>
  );
}

function Welcome() {
  return (
    <div className="flex flex-col items-center justify-center text-center px-12 py-24">
      <div className="font-sans text-text-muted/40 text-[10px] uppercase tracking-[0.4em] mb-4">
        agent ide
      </div>
      <h2 className="font-sans text-3xl text-text-secondary mb-3 max-w-lg leading-tight">
        Code is canon. The canvas is the derived view.
      </h2>
      <p className="text-text-muted text-sm max-w-md leading-relaxed mb-8">
        Select a skill from the sidebar — the IDE parses the source on disk and renders the typed
        graph. Click any node to jump to that file:line in your editor.
      </p>
      <div className="font-mono text-[11px] text-text-muted/60 space-y-1">
        <div>$ gridctl agent init</div>
        <div>$ gridctl agent dev --port 8181</div>
      </div>
    </div>
  );
}

export default SkillsWorkspace;
