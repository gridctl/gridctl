import { useState, useCallback, useRef } from 'react';
import {
  GitBranch,
  Search,
  Loader2,
  AlertCircle,
  ExternalLink,
  FolderGit2,
  Clock,
} from 'lucide-react';
import { cn } from '../../../lib/cn';
import { extractRepoInfo } from '../../../lib/repo';
import { Button } from '../../ui/Button';
import { previewSkillSource, fetchSkillSources } from '../../../lib/api';
import { authBannerFor, isSSHUrl, shouldOpenAuthCard } from '../../../lib/gitAuthErrors';
import { showToast } from '../../ui/Toast';
import { AuthCard } from '../AuthCard';
import { useAuthCard } from '../useAuthCard';
import type { SkillAuth } from '../../../lib/api';
import type { AgentPreview, SkillPreview, SkillSourceStatus } from '../../../types';

interface AddSourceStepProps {
  onPreviewLoaded: (
    skills: SkillPreview[],
    repo: string,
    ref: string,
    path: string,
    auth: SkillAuth | undefined,
    agents: AgentPreview[],
  ) => void;
}

const GITHUB_URL_REGEX = /^https?:\/\/github\.com\/[\w.-]+\/[\w.-]+\/?$/;
const GIT_URL_REGEX = /^(?:https?:\/\/|git@)[\w.-]+[:/][\w.-]+\/[\w.-]+(?:\.git)?$/;

export function AddSourceStep({ onPreviewLoaded }: AddSourceStepProps) {
  const [url, setUrl] = useState('');
  const [ref, setRef] = useState('');
  const [path, setPath] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [recentSources, setRecentSources] = useState<SkillSourceStatus[]>([]);
  const [loadedRecent, setLoadedRecent] = useState(false);
  const urlInputRef = useRef<HTMLInputElement>(null);

  // Auth card state — the whole card is transient, never persisted.
  const auth = useAuthCard();

  const ssh = isSSHUrl(url);

  // Load recent sources on mount
  const loadRecent = useCallback(async () => {
    if (loadedRecent) return;
    setLoadedRecent(true);
    try {
      const sources = await fetchSkillSources();
      setRecentSources(sources);
    } catch {
      // Silent
    }
  }, [loadedRecent]);

  // Load recent on first render
  if (!loadedRecent) {
    loadRecent();
  }

  const isValidUrl = (val: string) => {
    return GIT_URL_REGEX.test(val.trim()) || GITHUB_URL_REGEX.test(val.trim());
  };

  const handleScan = async () => {
    const trimmedUrl = url.trim();
    if (!trimmedUrl) return;

    if (!isValidUrl(trimmedUrl)) {
      setError('Enter a valid Git repository URL');
      return;
    }

    setLoading(true);
    setError(null);
    auth.clearBanner();

    const authPayload = auth.buildAuth(ssh);

    try {
      const sourceName = trimmedUrl.split('/').pop()?.replace(/\.git$/, '') ?? 'source';
      const result = await previewSkillSource(sourceName, {
        repo: trimmedUrl,
        ref: ref || undefined,
        path: path || undefined,
        auth: authPayload,
      });

      const malformed = result.malformed ?? [];
      const malformedAgents = result.malformedAgents ?? [];
      const agents = result.agents ?? [];

      if (result.skills.length === 0 && agents.length === 0) {
        if (malformed.length > 0) {
          setError(
            `${malformed.length} SKILL.md file${malformed.length === 1 ? '' : 's'} found but none could be parsed (${malformed[0].path}: ${malformed[0].error})`,
          );
        } else if (malformedAgents.length > 0) {
          setError(
            `${malformedAgents.length} agent definition${malformedAgents.length === 1 ? '' : 's'} found but none could be parsed (${malformedAgents[0].path}: ${malformedAgents[0].error})`,
          );
        } else {
          setError('No SKILL.md files or agents/*.md definitions found in this repository');
        }
        return;
      }

      if (malformed.length > 0) {
        showToast(
          'warning',
          `${malformed.length} SKILL.md file${malformed.length === 1 ? '' : 's'} could not be parsed and ${malformed.length === 1 ? 'is' : 'are'} not shown (${malformed[0].path})`,
        );
      }

      onPreviewLoaded(result.skills, trimmedUrl, ref, path, authPayload, agents);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to scan repository';
      setError(msg);
      showToast('error', msg);

      if (shouldOpenAuthCard(err) && !ssh) {
        auth.openWithBanner(authBannerFor(err));
      }
    } finally {
      setLoading(false);
    }
  };

  const handleRecentClick = (source: SkillSourceStatus) => {
    setUrl(source.repo);
    setRef(source.ref ?? '');
    setPath(source.path ?? '');
    urlInputRef.current?.focus();
  };

  const repoInfo = url ? extractRepoInfo(url) : null;

  return (
    <div className="space-y-6">
      {/* URL input section */}
      <div className="space-y-3">
        <div>
          <h3 className="text-sm font-medium text-text-primary mb-0.5">Import from Git</h3>
          <p className="text-[10px] text-text-muted">
            Enter a repository URL to discover and import skills
          </p>
        </div>

        {/* URL field */}
        <div className="space-y-1.5">
          <label className="text-[10px] text-text-muted uppercase tracking-wider">
            Repository URL
          </label>
          <div className="relative">
            <FolderGit2 size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-muted/50" />
            <input
              ref={urlInputRef}
              type="text"
              value={url}
              onChange={(e) => {
                setUrl(e.target.value);
                setError(null);
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleScan();
              }}
              placeholder="https://github.com/owner/repo"
              className={cn(
                'w-full bg-background/60 border rounded-lg pl-9 pr-3 py-2.5 text-xs',
                'focus:outline-none text-text-primary placeholder:text-text-muted/50 transition-colors',
                error
                  ? 'border-status-error/40 focus:border-status-error/60'
                  : 'border-border/40 focus:border-primary/50',
              )}
              autoFocus
            />
          </div>

          {/* Validation feedback */}
          {error && (
            <div className="flex items-center gap-1.5 text-status-error text-[10px]">
              <AlertCircle size={10} />
              <span>{error}</span>
            </div>
          )}

          {/* Repo info badge */}
          {repoInfo && !error && (
            <div className="flex items-center gap-1.5 text-text-muted text-[10px]">
              <GitBranch size={10} className="text-primary/60" />
              <span className="text-primary/80 font-medium">{repoInfo.owner}/{repoInfo.repo}</span>
            </div>
          )}
        </div>

        {/* Optional fields row */}
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1">
            <label className="text-[10px] text-text-muted uppercase tracking-wider">
              Ref <span className="normal-case">(optional)</span>
            </label>
            <input
              type="text"
              value={ref}
              onChange={(e) => setRef(e.target.value)}
              placeholder="main, v1.0, ^1.2"
              className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
            />
          </div>
          <div className="space-y-1">
            <label className="text-[10px] text-text-muted uppercase tracking-wider">
              Path <span className="normal-case">(optional)</span>
            </label>
            <input
              type="text"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="skills/"
              className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
            />
          </div>
        </div>

        {/* Credentials — collapsed and optional; auto-opens on an
            auth-shaped failure. Transient only: never persisted. */}
        <AuthCard controller={auth} ssh={ssh} />

        {/* Scan button */}
        <Button
          variant="primary"
          size="sm"
          onClick={handleScan}
          disabled={!url.trim() || loading}
          className="w-full"
        >
          {loading ? (
            <>
              <Loader2 size={14} className="animate-spin" />
              Scanning repository...
            </>
          ) : (
            <>
              <Search size={14} />
              Scan for Skills
            </>
          )}
        </Button>

        {/* First-run tip when the list is empty */}
        {loadedRecent && recentSources.length === 0 && (
          <p className="text-[10px] text-text-muted text-center italic">
            Private repo? Set your token in Vault first, then choose it above.
          </p>
        )}
      </div>

      {/* Recent sources */}
      {recentSources.length > 0 && (
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Clock size={10} className="text-text-muted" />
            <span className="text-[10px] text-text-muted uppercase tracking-wider">
              Recent Sources
            </span>
          </div>

          <div className="space-y-1.5">
            {recentSources.map((source) => {
              const info = extractRepoInfo(source.repo);
              return (
                <button
                  key={source.name}
                  onClick={() => handleRecentClick(source)}
                  className={cn(
                    'w-full flex items-center gap-3 px-3 py-2.5 rounded-lg',
                    'bg-white/[0.02] border border-white/[0.04]',
                    'hover:bg-white/[0.04] hover:border-white/[0.08] transition-all',
                    'text-left group',
                  )}
                >
                  <div className="p-1.5 rounded-lg bg-surface-elevated border border-border/30">
                    <FolderGit2 size={12} className="text-secondary" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-1.5">
                      <span className="text-xs font-medium text-text-primary truncate">
                        {info ? `${info.owner}/${info.repo}` : source.name}
                      </span>
                      {source.updateAvailable && (
                        <span className="text-[8px] px-1 py-0.5 rounded-full bg-primary/10 text-primary font-medium">
                          update
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-2 mt-0.5">
                      <span className="text-[10px] text-text-muted truncate">
                        {source.skills?.length ?? 0} skill{(source.skills?.length ?? 0) !== 1 ? 's' : ''}
                      </span>
                      {source.ref && (
                        <span className="text-[10px] text-text-muted font-mono">
                          @ {source.ref}
                        </span>
                      )}
                    </div>
                  </div>
                  <ExternalLink size={12} className="text-text-muted/40 group-hover:text-text-muted/80 transition-colors flex-shrink-0" />
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
