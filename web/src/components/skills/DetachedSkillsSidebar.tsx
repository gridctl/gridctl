import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { AlertCircle, Layers } from 'lucide-react';
import { fetchSkill, fetchSkills, type SkillGraph } from '../../lib/agent-api';
import { NodeDetail } from '../agent/ide/NodeDetail';
import { useRunTrace } from '../agent/ide/useRunTrace';
import { useDetachedWindowSync } from '../../hooks/useBroadcastChannel';

/**
 * DetachedSkillsSidebar mirrors the right-rail Inspector from /skills into a
 * standalone window. URL params drive the entire view — no Zustand state is
 * shared across windows, so deep-link continuity is the contract:
 *
 *   /sidebar?workspace=skills&skill=triage_input&node=tool_classify&run=abc
 *
 * `skill` is required; `node` and `run` are optional. The component degrades
 * gracefully when params are missing (empty state) or the skill 404s (error
 * banner) so a stale bookmark never wedges the popout.
 */
export function DetachedSkillsSidebar() {
  const [params] = useSearchParams();
  const skillName = params.get('skill');
  const nodeID = params.get('node');
  const runID = params.get('run');

  const [graph, setGraph] = useState<SkillGraph | null>(null);
  const [skillDir, setSkillDir] = useState<string>('');
  const [loading, setLoading] = useState<boolean>(Boolean(skillName));
  const [error, setError] = useState<string | null>(null);

  useDetachedWindowSync('sidebar');

  useEffect(() => {
    if (!skillName) {
      setGraph(null);
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    // Skill dir is needed for `Open in $EDITOR` links — fetch the summary
    // list in parallel with the graph so links work in the detached window.
    Promise.all([fetchSkill(skillName), fetchSkills()])
      .then(([g, summaries]) => {
        if (cancelled) return;
        setGraph(g);
        setSkillDir(summaries.find((s) => s.name === skillName)?.dir ?? '');
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [skillName]);

  const runTrace = useRunTrace(runID);

  const selectedNode = useMemo(() => {
    if (!graph || !nodeID) return null;
    return graph.nodes.find((n) => n.id === nodeID) ?? null;
  }, [graph, nodeID]);

  if (!skillName) {
    return <EmptyState message="Provide a ?skill=<name> param to inspect a typed-skill node." />;
  }

  if (loading) {
    return (
      <div className="h-screen w-screen flex items-center justify-center bg-background text-text-muted text-sm font-mono">
        <span className="animate-pulse">parsing…</span>
      </div>
    );
  }

  if (error || !graph) {
    return (
      <div className="h-screen w-screen flex items-center justify-center bg-background">
        <div className="text-center p-8 max-w-md">
          <div className="p-4 rounded-xl bg-status-error/10 border border-status-error/20 inline-block mb-4">
            <AlertCircle size={32} className="text-status-error" />
          </div>
          <h1 className="font-sans text-status-error text-lg mb-2">Skill unavailable</h1>
          <p className="text-text-muted text-xs font-mono">{error ?? `skill "${skillName}" not found`}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-screen w-screen bg-background flex flex-col overflow-hidden">
      <NodeDetail
        node={selectedNode}
        skillDir={skillDir}
        trace={nodeID ? runTrace.byNode[nodeID] : undefined}
        runID={runID}
        runTrace={runTrace}
        onClose={() => window.close()}
      />
    </div>
  );
}

function EmptyState({ message }: { message: string }) {
  return (
    <div className="h-screen w-screen flex flex-col items-center justify-center text-text-muted gap-3 bg-background">
      <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
        <Layers size={32} className="text-text-muted/50" />
      </div>
      <span className="text-sm max-w-sm text-center px-6 font-sans">{message}</span>
    </div>
  );
}

export default DetachedSkillsSidebar;
