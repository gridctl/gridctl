import { type SkillSummary } from '../../../lib/agent-api';
import { cn } from '../../../lib/cn';

interface SkillSidebarProps {
  skills: SkillSummary[];
  active: string | null;
  onSelect: (name: string) => void;
  onNewRun?: () => void;
  loading?: boolean;
  error?: string | null;
}

/**
 * SkillSidebar is the IDE's left rail — every typed skill the
 * project exposes, with a node-count badge and a flavor pill. The
 * rail is dense and monospace by design; this is a developer tool,
 * not a marketing surface.
 */
export function SkillSidebar({
  skills,
  active,
  onSelect,
  loading,
  error,
}: SkillSidebarProps) {
  return (
    <aside
      className="h-full bg-surface/30 border-r border-border-subtle flex flex-col overflow-hidden"
      aria-label="Skills"
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

      <div className="flex-1 overflow-y-auto py-3">
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
              No SKILL.md found. Run <code className="font-mono text-text-secondary">gridctl agent init</code>
              {' '}to scaffold a starter.
            </p>
          </div>
        )}
        <ul className="space-y-px px-2">
          {skills.map((s) => (
            <li key={s.name}>
              <button
                type="button"
                onClick={() => onSelect(s.name)}
                className={cn(
                  'w-full text-left px-3 py-2 rounded-md transition-colors',
                  'border border-transparent',
                  active === s.name
                    ? 'bg-surface-elevated/80 border-border-subtle'
                    : 'hover:bg-surface/50 hover:border-border-subtle/60',
                )}
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
            </li>
          ))}
        </ul>
      </div>
    </aside>
  );
}
