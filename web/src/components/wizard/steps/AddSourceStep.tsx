import { useState, useCallback, useRef } from 'react';
import {
  GitBranch,
  Search,
  Loader2,
  AlertCircle,
  ExternalLink,
  FolderGit2,
  Clock,
  ShieldCheck,
  ChevronDown,
  ChevronRight,
  KeyRound,
  X,
} from 'lucide-react';
import { cn } from '../../../lib/cn';
import { Button } from '../../ui/Button';
import {
  previewSkillSource,
  fetchSkillSources,
  HTTPError,
  type SkillAuth,
} from '../../../lib/api';
import { showToast } from '../../ui/Toast';
import { SecretsPopover } from '../SecretsPopover';
import type { SkillPreview, SkillSourceStatus } from '../../../types';

interface AddSourceStepProps {
  onPreviewLoaded: (
    skills: SkillPreview[],
    repo: string,
    ref: string,
    path: string,
    auth: SkillAuth | undefined,
  ) => void;
}

const GITHUB_URL_REGEX = /^https?:\/\/github\.com\/[\w.-]+\/[\w.-]+\/?$/;
const GIT_URL_REGEX = /^(?:https?:\/\/|git@)[\w.-]+[:/][\w.-]+\/[\w.-]+(?:\.git)?$/;

type AuthMode = 'vault' | 'token';

function isSSHUrl(url: string): boolean {
  const trimmed = url.trim();
  return trimmed.startsWith('git@') || trimmed.startsWith('ssh://');
}

// Classify a scan error to decide whether to auto-open the auth card.
function shouldOpenAuthCard(err: unknown): boolean {
  if (err instanceof HTTPError) {
    if (err.status === 401 || err.status === 404) return true;
  }
  const msg = err instanceof Error ? err.message.toLowerCase() : '';
  return (
    msg.includes('authentication required') ||
    msg.includes('authentication failed') ||
    msg.includes('repository not found') ||
    msg.includes('credentials were rejected')
  );
}

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
  const [authOpen, setAuthOpen] = useState(false);
  const [authMode, setAuthMode] = useState<AuthMode>('vault');
  const [vaultRef, setVaultRef] = useState<string>('');
  const [pasteToken, setPasteToken] = useState<string>('');
  const [authBanner, setAuthBanner] = useState<string | null>(null);

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

  const extractRepoInfo = (val: string): { owner: string; repo: string } | null => {
    const match = val.match(/github\.com[/:]([^/]+)\/([^/.]+)/);
    if (match) return { owner: match[1], repo: match[2] };
    return null;
  };

  // Build a SkillAuth from the current card state, or undefined to signal
  // "ambient" (no auth header at all) which preserves public-repo behavior.
  const buildAuth = useCallback((): SkillAuth | undefined => {
    if (ssh) return undefined; // ambient ssh-agent
    if (authMode === 'vault' && vaultRef) {
      return { method: 'token', credentialRef: vaultRef };
    }
    if (authMode === 'token' && pasteToken) {
      return { method: 'token', token: pasteToken };
    }
    return undefined;
  }, [ssh, authMode, vaultRef, pasteToken]);

  const handleScan = async () => {
    const trimmedUrl = url.trim();
    if (!trimmedUrl) return;

    if (!isValidUrl(trimmedUrl)) {
      setError('Enter a valid Git repository URL');
      return;
    }

    setLoading(true);
    setError(null);
    setAuthBanner(null);

    const auth = buildAuth();

    try {
      const sourceName = trimmedUrl.split('/').pop()?.replace(/\.git$/, '') ?? 'source';
      const result = await previewSkillSource(sourceName, {
        repo: trimmedUrl,
        ref: ref || undefined,
        path: path || undefined,
        auth,
      });

      if (result.skills.length === 0) {
        setError('No SKILL.md files found in this repository');
        return;
      }

      onPreviewLoaded(result.skills, trimmedUrl, ref, path, auth);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to scan repository';
      setError(msg);
      showToast('error', msg);

      if (shouldOpenAuthCard(err) && !ssh) {
        setAuthOpen(true);
        setAuthBanner(
          err instanceof HTTPError && err.status === 404
            ? 'Not found. If this is a private repository, add credentials below and try again.'
            : 'This repository requires authentication.',
        );
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

        {/* Auth card — inline collapsible. Collapsed by default; auto-opens
            on 401/404 from a scan. Transient only: never persisted. */}
        <div
          data-testid="auth-card"
          className={cn(
            'rounded-lg border border-border/30 bg-white/[0.02] transition-colors',
            authOpen && 'border-border/50 bg-white/[0.03]',
          )}
        >
          <button
            type="button"
            aria-expanded={authOpen}
            aria-controls="auth-card-body"
            onClick={() => setAuthOpen((v) => !v)}
            className="w-full flex items-center gap-2 px-3 py-2 text-[11px] text-text-secondary hover:text-text-primary transition-colors"
          >
            <ShieldCheck size={12} className="text-primary/70" />
            <span className="font-medium">Authentication</span>
            <span className="text-text-muted text-[10px]">
              {ssh ? '— using ssh-agent' : authOpen ? '' : '(optional)'}
            </span>
            <span className="ml-auto text-text-muted">
              {authOpen ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
            </span>
          </button>

          {authOpen && (
            <div
              id="auth-card-body"
              className="px-3 pb-3 pt-1 space-y-2.5 border-t border-border/20"
            >
              {authBanner && (
                <div className="flex items-start gap-1.5 text-[10px] text-status-pending bg-status-pending/5 border border-status-pending/20 rounded-md px-2 py-1.5">
                  <AlertCircle size={10} className="mt-0.5 flex-shrink-0" />
                  <span>{authBanner}</span>
                </div>
              )}

              {ssh ? (
                <div className="flex items-center gap-1.5 text-[10px] text-text-muted">
                  <KeyRound size={10} />
                  <span>Using ssh-agent — no token needed.</span>
                </div>
              ) : (
                <>
                  {/* Mode selector — real radios for accessibility */}
                  <fieldset className="flex gap-1 text-[10px]">
                    <legend className="sr-only">Authentication method</legend>
                    {(
                      [
                        { v: 'vault', label: 'Vault secret', sub: 'recommended' },
                        { v: 'token', label: 'Paste token', sub: 'not saved' },
                      ] as const
                    ).map((opt) => (
                      <label
                        key={opt.v}
                        className={cn(
                          'flex-1 cursor-pointer rounded-md px-2 py-1.5 border text-center transition-colors',
                          authMode === opt.v
                            ? 'bg-primary/10 border-primary/30 text-primary'
                            : 'bg-white/[0.02] border-white/[0.06] text-text-muted hover:text-text-secondary',
                        )}
                      >
                        <input
                          type="radio"
                          name="auth-mode"
                          value={opt.v}
                          checked={authMode === opt.v}
                          onChange={() => setAuthMode(opt.v)}
                          className="sr-only"
                        />
                        <span className="block font-medium">{opt.label}</span>
                        <span className="block text-[9px] opacity-70">{opt.sub}</span>
                      </label>
                    ))}
                  </fieldset>

                  {authMode === 'vault' ? (
                    <div className="flex items-center gap-2">
                      {vaultRef ? (
                        <div className="flex-1 flex items-center justify-between gap-2 bg-background/60 border border-border/40 rounded-md px-2 py-1.5 text-[10px] font-mono text-text-primary">
                          <span className="truncate">{vaultRef}</span>
                          <button
                            type="button"
                            onClick={() => setVaultRef('')}
                            className="text-text-muted hover:text-status-error transition-colors"
                            aria-label="Clear vault selection"
                          >
                            <X size={11} />
                          </button>
                        </div>
                      ) : (
                        <div className="flex-1 text-[10px] text-text-muted italic px-1">
                          Choose a vault key →
                        </div>
                      )}
                      <SecretsPopover onSelect={setVaultRef} />
                    </div>
                  ) : (
                    <>
                      <input
                        type="password"
                        value={pasteToken}
                        onChange={(e) => setPasteToken(e.target.value)}
                        placeholder="Personal Access Token"
                        className="w-full bg-background/60 border border-border/40 rounded-md px-2 py-1.5 text-[11px] font-mono focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
                      />
                      <p className="text-[10px] text-text-muted">
                        Used once for this scan — not saved anywhere.
                      </p>
                    </>
                  )}
                </>
              )}
            </div>
          )}
        </div>

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
