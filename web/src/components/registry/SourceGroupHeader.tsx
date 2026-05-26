import { useState } from 'react';
import { ExternalLink, FolderGit2, PenLine, RefreshCw } from 'lucide-react';
import { cn } from '../../lib/cn';
import { showToast } from '../ui/Toast';
import { updateSkillSource } from '../../lib/api';
import { extractRepoInfo } from '../../lib/repo';
import type { SkillSourceStatus } from '../../types';

interface SourceGroupHeaderProps {
  /** The imported source for this group, or undefined for the "My Skills" group. */
  source?: SkillSourceStatus;
  count: number;
  hasSearch: boolean;
  /** True when this group is the active provenance isolate. */
  isActive: boolean;
  /** Toggle isolate on this group (clears when re-toggled). */
  onToggle: () => void;
  /** Called after a successful inline source update so the caller can refresh. */
  onUpdated?: () => void;
}

/**
 * Full-width section header for the Library's provenance grouping. Mirrors the
 * category GroupSection header but is a clickable isolate control: the label
 * button toggles "show only this group", and imported sources additionally
 * expose a repo link, short commit SHA, and an inline Update action when an
 * update is available. The "My Skills" variant (no source) shows an authored
 * indicator and is isolatable via the `local` key.
 */
export function SourceGroupHeader({
  source,
  count,
  hasSearch,
  isActive,
  onToggle,
  onUpdated,
}: SourceGroupHeaderProps) {
  const [updating, setUpdating] = useState(false);

  const repoInfo = source ? extractRepoInfo(source.repo) : null;
  const label = source ? (repoInfo ? `${repoInfo.owner}/${repoInfo.repo}` : source.name) : 'My Skills';
  const shortSha = source?.commitSha ? source.commitSha.slice(0, 7) : null;

  const handleUpdate = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!source || updating) return;
    setUpdating(true);
    try {
      await updateSkillSource(source.name);
      showToast('success', `Updated "${source.name}"`);
      onUpdated?.();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Update failed');
    } finally {
      setUpdating(false);
    }
  };

  return (
    <div
      style={{ gridColumn: '1 / -1' }}
      className="flex flex-col gap-1 mt-2 first:mt-0 animate-fade-in-scale"
    >
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <button
            onClick={onToggle}
            aria-pressed={isActive}
            title={isActive ? 'Show all groups' : `Show only ${label}`}
            className={cn(
              'flex items-center gap-1.5 rounded-md px-1.5 py-0.5 -ml-1.5 transition-colors',
              isActive
                ? 'text-primary'
                : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight',
            )}
          >
            {source ? (
              <FolderGit2 size={11} className="flex-shrink-0" />
            ) : (
              <PenLine size={11} className="flex-shrink-0" />
            )}
            <span className="text-[10px] uppercase tracking-widest font-medium truncate">{label}</span>
          </button>
          {shortSha && (
            <span className="text-[10px] font-mono text-text-muted/60" title={source?.commitSha}>
              {shortSha}
            </span>
          )}
          {source && repoInfo && (
            <a
              href={`https://github.com/${repoInfo.owner}/${repoInfo.repo}`}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => e.stopPropagation()}
              title="Open repository on GitHub"
              aria-label={`Open ${label} on GitHub`}
              className="text-text-muted/50 hover:text-primary transition-colors flex-shrink-0"
            >
              <ExternalLink size={11} />
            </a>
          )}
          {source?.updateAvailable && (
            <button
              onClick={handleUpdate}
              disabled={updating}
              title="Update available — pull latest"
              className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded-full border border-amber-400/30 bg-amber-400/10 text-amber-300 hover:bg-amber-400/20 transition-colors disabled:opacity-60 flex-shrink-0"
            >
              <RefreshCw size={10} className={updating ? 'animate-spin' : undefined} />
              {updating ? 'Updating…' : 'Update'}
            </button>
          )}
        </div>
        <span className="text-[10px] px-1.5 rounded-full bg-surface-highlight text-text-muted flex-shrink-0">
          {count} {hasSearch ? 'matched' : 'skills'}
        </span>
      </div>
      <div className="border-b border-border/30" />
    </div>
  );
}
