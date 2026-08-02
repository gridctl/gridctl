import type { SkillGovernance } from '../types';

// Governance display helpers live in lib/ (not a component module) so fast
// refresh keeps working, per the pinFindings.ts precedent.

// governanceNeedsAttention gates the Library's single governance indicator:
// quiet skills (pinned, no alert findings, not policy-denied) show nothing,
// matching the status-bar badges' no-chip-on-quiet convention. Info-tier
// findings are deliberately excluded from attention, same as everywhere.
export function governanceNeedsAttention(g: SkillGovernance | undefined): boolean {
  if (!g) return false;
  if (g.pinStatus === 'drift' || g.policyDenied) return true;
  return (
    (g.findingsCount ?? 0) > 0 &&
    (g.maxFindingSeverity === 'warn' || g.maxFindingSeverity === 'critical')
  );
}

// governanceSummary is the tooltip/label text for the indicator: every
// attention-worthy fact, factual wording only ("pin drift", never bare
// "drift", which the Library already uses for git sync state; "advisory
// findings", never "vulnerable"; no trust judgments).
export function governanceSummary(g: SkillGovernance | undefined): string {
  if (!g) return '';
  const parts: string[] = [];
  if (g.pinStatus === 'drift') parts.push('Pin drift');
  const findings = g.findingsCount ?? 0;
  if (findings > 0 && (g.maxFindingSeverity === 'warn' || g.maxFindingSeverity === 'critical')) {
    parts.push(`${findings} advisory finding${findings > 1 ? 's' : ''} (${g.maxFindingSeverity})`);
  }
  if (g.policyDenied) {
    parts.push(g.policyRule ? `Blocked by policy (rule: ${g.policyRule})` : 'Blocked by policy');
  }
  return parts.join(' · ');
}

// originLabel renders provenance as a fact, never a trust judgment.
export function originLabel(g: SkillGovernance | undefined): string {
  if (g?.source === 'git' && g.origin?.repo) {
    return `Imported: ${g.origin.repo}${g.origin.ref ? `@${g.origin.ref}` : ''}`;
  }
  return 'Local';
}

// skillPinsUrl owns the deep-link grammar into the Pins workspace's skill
// review; the param names are a load-bearing contract with PinsWorkspace's
// URL parsing, so every producer routes through here.
export function skillPinsUrl(name?: string | null, view?: 'drift' | 'findings'): string {
  if (!name) return '/pins?kind=skill';
  const suffix = view ? `&view=${view}` : '';
  return `/pins?kind=skill&skill=${encodeURIComponent(name)}${suffix}`;
}
