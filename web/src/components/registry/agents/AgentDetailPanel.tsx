import { useEffect, useState } from 'react';
import { Bot, Pencil, Trash2 } from 'lucide-react';
import { InspectorHeader, InspectorTabList, InspectorTabButton, PaneAnchor } from '../../inspector';
import { MarkdownPreview } from '../MarkdownPreview';
import { AgentProjectionRows } from './AgentProjectionRows';
import { agentClientName, formatExtraValue } from './agentModel';
import { PackChip } from '../PackChip';
import { fetchRegistryAgent } from '../../../lib/api';
import type { AgentProjectionStatus, RegistryAgent } from '../../../types';

type AgentTab = 'overview' | 'body' | 'projection';

interface AgentDetailPanelProps {
  agent: RegistryAgent | null;
  statuses: AgentProjectionStatus[];
  onClose: () => void;
  onEdit: (agent: RegistryAgent) => void;
  onDelete: (agent: RegistryAgent) => void;
  onRefresh: () => Promise<void> | void;
}

// The two frontmatter keys with cross-client meaning. Everything else in
// extra is client-specific or vendor data and renders in the plain list.
const TRANSLATED_KEYS = ['tools', 'model'];

/**
 * Right-rail inspector for one agent: Overview (frontmatter), Body
 * (markdown), Projection (per-client rows). Built on the shared inspector
 * shell so it reads as SkillDetailPanel's sibling.
 */
export function AgentDetailPanel({ agent, statuses, onClose, onEdit, onDelete, onRefresh }: AgentDetailPanelProps) {
  const [tab, setTab] = useState<AgentTab>('overview');
  // Body cache: the list payload omits body/raw, so the Body tab fetches
  // the full agent on demand and keeps it until the data may have changed.
  const [full, setFull] = useState<RegistryAgent | null>(null);
  const [bodyError, setBodyError] = useState<string | null>(null);
  const [bodyView, setBodyView] = useState<'preview' | 'source'>('preview');

  const name = agent?.name ?? null;
  // Render-time reset (the sanctioned adjust-during-render pattern; a
  // reset effect would double-render instead). Keyed on the agent OBJECT,
  // not the name: every refresh replaces the array, so identity changes
  // exactly when the data may have — an edit or adopt to the same agent
  // must invalidate the cached body. The tab only resets when the
  // selection actually moves, so a refresh never yanks the user off the
  // Projection tab.
  const [prevAgent, setPrevAgent] = useState(agent);
  if (agent !== prevAgent) {
    setPrevAgent(agent);
    setFull(null);
    setBodyError(null);
    if (agent?.name !== prevAgent?.name) {
      setTab('overview');
      setBodyView('preview');
    }
  }

  useEffect(() => {
    if (tab !== 'body' || !name || full !== null) return;
    let cancelled = false;
    fetchRegistryAgent(name)
      .then((result) => {
        if (!cancelled) setFull(result);
      })
      .catch((err) => {
        if (!cancelled) setBodyError(err instanceof Error ? err.message : 'Failed to load body');
      });
    return () => {
      cancelled = true;
    };
  }, [tab, name, full]);

  if (!agent) {
    return (
      <div className="h-full flex flex-col items-center justify-center gap-2 text-text-muted p-6 text-center">
        <Bot size={24} className="text-text-muted/40" />
        <p className="text-sm">Select an agent to inspect it</p>
      </div>
    );
  }

  const translated = TRANSLATED_KEYS
    .map((key) => ({ key, field: agent.extra?.find((f) => f.key === key) }))
    .filter((e) => e.field !== undefined);
  const otherExtra = (agent.extra ?? []).filter((f) => !TRANSLATED_KEYS.includes(f.key));
  const vendorKeyCount = otherExtra.length;

  return (
    <div className="relative h-full flex flex-col overflow-hidden">
      <PaneAnchor />
      <InspectorHeader
        title={agent.name}
        subtitle={
          agent.source ? (
            <span className="text-[11px] text-text-muted inline-flex items-center gap-1.5">
              from {agent.source}
              <PackChip source={agent.source} />
            </span>
          ) : (
            <span className="text-[11px] text-text-muted">local agent</span>
          )
        }
        icon={Bot}
        accent="primary"
        onClose={onClose}
        actions={
          <>
            <button
              onClick={() => onEdit(agent)}
              aria-label={`Edit ${agent.name}`}
              className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors text-text-muted hover:text-text-primary"
            >
              <Pencil size={14} />
            </button>
            <button
              onClick={() => onDelete(agent)}
              aria-label={`Delete ${agent.name}`}
              className="p-1.5 rounded-lg hover:bg-red-400/10 transition-colors text-text-muted hover:text-red-400"
            >
              <Trash2 size={14} />
            </button>
          </>
        }
      />

      <InspectorTabList ariaLabel="Agent detail tabs">
        <InspectorTabButton active={tab === 'overview'} onClick={() => setTab('overview')} label="Overview" controls="agent-tab-overview" />
        <InspectorTabButton active={tab === 'body'} onClick={() => setTab('body')} label="Body" controls="agent-tab-body" />
        <InspectorTabButton active={tab === 'projection'} onClick={() => setTab('projection')} label="Projection" controls="agent-tab-projection" />
      </InspectorTabList>

      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {tab === 'overview' && (
          <div id="agent-tab-overview" role="tabpanel" className="p-5 flex flex-col gap-4">
            <p className="text-sm text-text-secondary leading-relaxed">
              {agent.description || <span className="italic text-text-muted/50">No description</span>}
            </p>

            {translated.length > 0 && (
              <div className="flex flex-col gap-1.5">
                <span className="text-[10px] uppercase tracking-wider text-text-muted">
                  Portable frontmatter
                </span>
                {translated.map(({ key, field }) => (
                  <div key={key} className="flex items-baseline gap-2">
                    <span className="text-[11px] font-mono text-text-muted w-14 flex-shrink-0">{key}</span>
                    <span className="text-xs text-text-primary font-mono break-all">
                      {formatExtraValue(field?.value)}
                    </span>
                  </div>
                ))}
                <p className="text-[10px] text-text-muted/70">
                  Translated per client on sync; lossy renders drop what a dialect cannot express.
                </p>
              </div>
            )}

            {otherExtra.length > 0 && (
              <div className="flex flex-col gap-1.5">
                <span className="text-[10px] uppercase tracking-wider text-text-muted">
                  Other frontmatter ({vendorKeyCount} {vendorKeyCount === 1 ? 'key' : 'keys'}, client-specific)
                </span>
                {otherExtra.map((f) => (
                  <div key={f.key} className="flex items-baseline gap-2">
                    <span className="text-[11px] font-mono text-text-muted flex-shrink-0">{f.key}</span>
                    <span className="text-xs text-text-secondary font-mono break-all">
                      {formatExtraValue(f.value)}
                    </span>
                  </div>
                ))}
              </div>
            )}

            {agent.dir && (
              <div className="flex flex-col gap-1">
                <span className="text-[10px] uppercase tracking-wider text-text-muted">Location</span>
                <span className="text-[11px] font-mono text-text-muted break-all">{agent.dir}</span>
              </div>
            )}

            {statuses.length > 0 && (
              <div className="flex flex-col gap-1">
                <span className="text-[10px] uppercase tracking-wider text-text-muted">Projected to</span>
                <span className="text-xs text-text-secondary">
                  {statuses.map((s) => agentClientName(s.client)).join(', ')}
                </span>
              </div>
            )}
          </div>
        )}

        {tab === 'body' && (
          <div id="agent-tab-body" role="tabpanel" className="p-5 flex flex-col gap-3">
            {bodyError && <p className="text-xs text-status-error">{bodyError}</p>}
            {!bodyError && full === null && <p className="text-xs text-text-muted">Loading…</p>}
            {full !== null && (
              <>
                <div className="flex items-center gap-1" role="group" aria-label="Body view">
                  {(['preview', 'source'] as const).map((view) => (
                    <button
                      key={view}
                      onClick={() => setBodyView(view)}
                      aria-pressed={bodyView === view}
                      className={
                        bodyView === view
                          ? 'px-2 py-1 rounded-md text-[11px] font-medium bg-primary/10 text-primary border border-primary/25 transition-colors'
                          : 'px-2 py-1 rounded-md text-[11px] font-medium text-text-muted hover:text-text-secondary hover:bg-surface-highlight border border-transparent transition-colors'
                      }
                    >
                      {view === 'preview' ? 'Preview' : 'Source'}
                    </button>
                  ))}
                </div>
                {bodyView === 'preview' ? (
                  <div className="skill-md">
                    <MarkdownPreview content={full.body ?? ''} emptyHint="This agent has an empty body." />
                  </div>
                ) : (
                  <pre className="text-[11px] font-mono bg-background/60 border border-border/30 rounded-lg p-3 overflow-x-auto whitespace-pre-wrap text-text-secondary">
                    {full.raw ?? ''}
                  </pre>
                )}
              </>
            )}
          </div>
        )}

        {tab === 'projection' && (
          <div id="agent-tab-projection" role="tabpanel" className="p-4">
            <AgentProjectionRows agentName={agent.name} statuses={statuses} onRefresh={onRefresh} />
          </div>
        )}
      </div>
    </div>
  );
}
