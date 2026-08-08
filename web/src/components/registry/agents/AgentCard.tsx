import { memo } from 'react';
import { Bot, GitBranch, Pencil, Trash2 } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { ModelChip } from '../ModelChip';
import { PackChip } from '../PackChip';
import { StatePill } from '../../ui/StatePill';
import { agentClientName } from './agentModel';
import type { AgentProjectionStatus, RegistryAgent } from '../../../types';

export interface AgentCardProps {
  agent: RegistryAgent;
  /** This agent's projection rows (may be empty: never synced). */
  statuses: AgentProjectionStatus[];
  onSelect: (agent: RegistryAgent) => void;
  onEdit: (agent: RegistryAgent) => void;
  onDelete: (agent: RegistryAgent) => void;
  /** Whether this card is the one shown in the inspector. */
  isActive?: boolean;
}

/**
 * One agent in the catalog grid. A sibling of SkillCard rather than a
 * parameterization of it: agents carry no lifecycle state, no usage, and
 * no file count — their scannability signal is the per-client projection
 * summary. Visual grammar (accents, hover, footer actions) matches
 * SkillCard so the two segments read as one workspace.
 */
export const AgentCard = memo(({ agent, statuses, onSelect, onEdit, onDelete, isActive = false }: AgentCardProps) => {
  return (
    <div
      aria-current={isActive ? 'true' : undefined}
      className={cn(
        'group relative rounded-xl overflow-hidden flex flex-col',
        'backdrop-blur-xl border transition-all duration-200 ease-out',
        'bg-gradient-to-b from-surface/95 via-surface/90 to-primary/[0.02]',
        'border-white/[0.08] hover:border-primary/40 focus-within:border-primary/40 hover:shadow-node-hover',
        isActive && 'border-primary/50 bg-primary/[0.06] shadow-node-hover',
      )}
    >
      <div className={cn(
        'absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent to-transparent transition-colors duration-200',
        'via-white/10 group-hover:via-primary/40 group-focus-within:via-primary/40',
        isActive && 'via-primary/50',
      )} />
      <div
        aria-hidden="true"
        className={cn(
          'absolute top-0 bottom-0 left-0 w-0.5 transition-colors duration-200',
          isActive ? 'bg-primary' : 'bg-white/10',
        )}
      />

      <div
        className="p-3 flex flex-col gap-2 flex-1 cursor-pointer"
        role="button"
        tabIndex={0}
        aria-label={`Inspect ${agent.name}`}
        onClick={() => onSelect(agent)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            onSelect(agent);
          }
        }}
      >
        <div className="flex items-start gap-2">
          <div className="p-1.5 rounded-md border bg-surface-highlight/60 border-border/40 flex-shrink-0 mt-0.5 transition-colors duration-200 group-hover:bg-primary/10 group-hover:border-primary/20 group-focus-within:bg-primary/10 group-focus-within:border-primary/20">
            <Bot size={14} className="text-text-muted transition-colors duration-200 group-hover:text-primary/70 group-focus-within:text-primary/70" />
          </div>
          <span className="font-semibold log-text text-text-primary truncate flex-1 min-w-0 leading-tight mt-0.5">
            {agent.name}
          </span>
          {agent.source && (
            <span
              title={`Imported from source ${agent.source}`}
              aria-label={`Imported from source ${agent.source}`}
              className="flex-shrink-0 inline-flex items-center text-text-muted/50 transition-colors group-hover:text-text-muted/80 mt-0.5"
            >
              <GitBranch size={12} />
            </span>
          )}
          {agent.source && <PackChip source={agent.source} className="mt-0.5" />}
          <ModelChip modelPreference={agent.modelPreference} className="mt-0.5" />
        </div>

        <p className={cn(
          'log-text leading-relaxed line-clamp-2',
          agent.description ? 'text-text-secondary' : 'text-text-muted/40 italic',
        )}>
          {agent.description || 'No description'}
        </p>

        {/* Projection summary: one chip per client, state colors from the
            shared vocabulary. flex-wrap so chips stack instead of clipping
            at narrow card widths. */}
        <div className="flex items-center gap-1 flex-wrap min-h-4" data-testid="agent-projection-summary">
          {statuses.length === 0 ? (
            <span className="log-text-detail text-text-muted/60">Not projected yet</span>
          ) : (
            statuses.map((s) => (
              <span key={s.client} className="inline-flex items-center gap-1" title={`${agentClientName(s.client)}: ${s.state} (${s.render} render)`}>
                <StatePill state={s.state} className="px-1.5" />
                <span className="log-text-detail text-text-muted">{agentClientName(s.client)}</span>
                {s.render === 'lossy' && (
                  <span className="log-text-detail text-text-muted/60 font-mono">lossy</span>
                )}
              </span>
            ))
          )}
        </div>
      </div>

      <div className="px-3 pb-3 pt-2 border-t border-border-subtle/50 flex items-center justify-end gap-2">
        <button
          onClick={(e) => { e.stopPropagation(); onEdit(agent); }}
          aria-label={`Edit ${agent.name}`}
          className="p-1.5 rounded-md text-text-muted hover:text-primary hover:bg-primary/10 transition-colors"
        >
          <Pencil size={13} />
        </button>
        <button
          onClick={(e) => { e.stopPropagation(); onDelete(agent); }}
          aria-label={`Delete ${agent.name}`}
          className="p-1.5 rounded-md text-text-muted hover:text-red-400 hover:bg-red-400/10 transition-colors"
        >
          <Trash2 size={13} />
        </button>
      </div>
    </div>
  );
});

AgentCard.displayName = 'AgentCard';
