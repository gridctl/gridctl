import { useCallback, useEffect, useMemo, useState, type RefObject } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  fetchSkill,
  fetchSkills,
  type SkillGraph,
  type SkillSummary,
} from '../../../lib/agent-api';
import { SkillSidebar } from './SkillSidebar';
import { NodeList } from './NodeList';
import { Canvas } from './Canvas';
import { NodeDetail } from './NodeDetail';
import { RunLauncherModal, type SkillForLaunch } from './RunLauncherModal';
import { useDevSocket } from '../../../hooks/useDevSocket';
import { useRunTrace } from './useRunTrace';
import { cn } from '../../../lib/cn';

type ViewMode = 'list' | 'canvas';

/**
 * AgentIDE is the top-level IDE shell — three-pane layout: skill
 * sidebar, main view (NodeList or Canvas), inspector. Connects to
 * /api/agent/dev for skills/AST and /api/agent/runs/{id}/events for
 * trace overlay. Both connections degrade quietly when the daemon
 * doesn't have a project root configured.
 *
 * The view-mode toggle picks NodeList (Slice 1) or Canvas (Slice
 * 3); both share the same trace overlay (Slice 2). Code is canon —
 * neither view mutates source.
 */
export function AgentIDE() {
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

  // RunLauncher state — null when no modal is open. The originRef
  // points at the SkillSidebar Run button that opened the modal so
  // focus can return to it on close.
  const [launcher, setLauncher] = useState<{
    skill: SkillForLaunch;
    originRef: RefObject<HTMLButtonElement | null>;
  } | null>(null);

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

  // Initial skills load.
  useEffect(() => {
    void refreshSkills();
  }, [refreshSkills]);

  // Re-fetch active skill graph whenever (a) the active skill
  // changes, or (b) the watcher pushes a change event.
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
      // Note: SkillSummary doesn't carry description today — the dev
      // server only surfaces name/lang/dir/node_count. The modal
      // omits the description paragraph when undefined. Surfacing
      // SKILL.md descriptions through the dev API is a follow-up.
      setLauncher({ skill: { name }, originRef });
    },
    [],
  );

  const handleLaunched = useCallback(
    (response: { run_id: string; started_at: string }) => {
      // Update URL to activate useRunTrace on the new run. Use replace
      // so back-button navigation doesn't accumulate intermediate runs.
      const next = new URLSearchParams(params);
      if (launcher?.skill.name) next.set('skill', launcher.skill.name);
      next.set('run', response.run_id);
      setParams(next, { replace: true });
      setLauncher(null);
    },
    [params, setParams, launcher],
  );

  const selectedNodeObj = useMemo(() => {
    if (!graph || !selectedNode) return null;
    return graph.nodes.find((n) => n.id === selectedNode) ?? null;
  }, [graph, selectedNode]);

  return (
    <div className="h-screen w-screen bg-background text-text-primary overflow-hidden">
      <div
        className="grid h-full"
        style={{
          gridTemplateColumns: '280px minmax(0, 1fr) 360px',
        }}
      >
        <SkillSidebar
          skills={skills}
          active={activeSkill}
          onSelect={handleSelectSkill}
          onRunSkill={handleRunSkill}
          loading={skillsLoading}
          error={skillsError}
        />

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
            {!graph && !graphLoading && !graphError && (
              <Welcome />
            )}
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

        <NodeDetail
          node={selectedNodeObj}
          skillDir={skillDir}
          trace={selectedNode ? runTrace.byNode[selectedNode] : undefined}
          runID={runID}
          runTrace={runTrace}
          onClose={() => setSelectedNode(null)}
        />
      </div>

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
            <div className="font-mono text-sm text-text-primary truncate">
              {graph.skill}
            </div>
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
        {runID && <ConnectionDot label={`run ${runID.slice(0, 8)}`} state={runStatus === 'idle' ? 'connecting' : runStatus} />}
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
  return (
    <div className="inline-flex rounded border border-border-subtle p-px bg-surface">
      {(['list', 'canvas'] as const).map((mode) => (
        <button
          key={mode}
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
  const glow =
    state === 'open'
      ? '0 0 6px var(--color-status-running)'
      : 'none';
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
