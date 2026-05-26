// Helpers for parsing Git repository URLs into display-friendly parts.

export interface RepoInfo {
  owner: string;
  repo: string;
}

/**
 * Extract `{ owner, repo }` from a GitHub URL. Handles both the HTTPS form
 * (`https://github.com/owner/repo[.git]`) and the SSH form
 * (`git@github.com:owner/repo[.git]`). Returns null for non-GitHub or
 * unparseable URLs.
 */
export function extractRepoInfo(url: string): RepoInfo | null {
  const match = url.match(/github\.com[/:]([^/]+)\/([^/.]+)/);
  if (match) return { owner: match[1], repo: match[2] };
  return null;
}
