// Pure helpers for the tool-groups overlay in the Tools workspace. JSX-free
// (the metricsData/limitsData split) so Fast Refresh stays happy.
import type { GroupsReport, PolicyToolStatus } from './api';

// groupsForTool returns the names of groups whose surface includes the
// canonical server__tool name, for the membership badges on tool rows.
export function groupsForTool(report: GroupsReport | null, canonical: string): string[] {
  if (!report?.configured) return [];
  const names: string[] = [];
  for (const g of report.groups) {
    if (g.members.some((m) => m.canonical === canonical)) names.push(g.name);
  }
  return names;
}

// groupEndpointURL builds the copyable absolute endpoint URL. The UI is
// served by the gateway itself, so the page origin IS the gateway address.
export function groupEndpointURL(endpoint: string): string {
  return `${window.location.origin}${endpoint}`;
}

// annotationChips flattens a member's hints into compact chip labels. Only
// declared hints render; the spec treats undeclared as worst-case, so an
// absent hint is silence, not a badge.
export interface AnnotationChip {
  label: string;
  title: string;
  tone: 'safe' | 'danger' | 'neutral';
}

// Tone-to-class map shared by every annotation-chip render site (GroupsPanel,
// the Tools workspace rows, and the detail panel's Hints block), so a status
// token change lands in one place. Kept as a string map: this module stays
// JSX-free for Fast Refresh.
export const CHIP_TONE_CLASS: Record<AnnotationChip['tone'], string> = {
  safe: 'bg-status-running/10 text-status-running',
  danger: 'bg-status-error/10 text-status-error',
  neutral: 'bg-surface-highlight text-text-muted',
};

export function annotationChips(annotations?: {
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
}): AnnotationChip[] {
  if (!annotations) return [];
  const chips: AnnotationChip[] = [];
  if (annotations.readOnlyHint !== undefined) {
    chips.push(
      annotations.readOnlyHint
        ? { label: 'RO', title: 'readOnlyHint: true — does not modify state', tone: 'safe' }
        : { label: 'RW', title: 'readOnlyHint: false — may modify state', tone: 'neutral' },
    );
  }
  if (annotations.destructiveHint !== undefined) {
    chips.push(
      annotations.destructiveHint
        ? { label: 'DESTR', title: 'destructiveHint: true — may perform destructive updates', tone: 'danger' }
        : { label: 'SAFE', title: 'destructiveHint: false — updates are additive', tone: 'safe' },
    );
  }
  if (annotations.idempotentHint !== undefined && annotations.idempotentHint) {
    chips.push({ label: 'IDEM', title: 'idempotentHint: true — repeat calls have no extra effect', tone: 'neutral' });
  }
  if (annotations.openWorldHint !== undefined) {
    chips.push(
      annotations.openWorldHint
        ? { label: 'OPEN', title: 'openWorldHint: true — interacts with external entities', tone: 'neutral' }
        : { label: 'CLOSED', title: 'openWorldHint: false — closed domain of interaction', tone: 'neutral' },
    );
  }
  return chips;
}

// policyChip is the Tools workspace denial chip. allow renders nothing.
// require_approval is labeled APPROVAL; every other denial is BLOCKED.
// The title names the source the same way the gateway denial does.
export function policyChip(verdict?: PolicyToolStatus): AnnotationChip | null {
  if (!verdict || verdict.action === 'allow') return null;
  if (verdict.action !== 'block' && verdict.action !== 'require_approval') return null;
  const label = verdict.action === 'require_approval' ? 'APPROVAL' : 'BLOCKED';
  return { label, title: policyChipTitle(verdict), tone: 'danger' };
}

export function policyChipTitle(verdict: PolicyToolStatus): string {
  if (verdict.source === 'rule') {
    const index = (verdict.rule_index ?? 0) + 1;
    const pattern = verdict.rule ? `: ${verdict.rule}` : '';
    return `blocked by policy rule ${index}${pattern}`;
  }
  if (verdict.source === 'block_destructive') {
    return 'blocked by policy: block_destructive';
  }
  return 'blocked by policy default';
}

export function policyDetailText(verdict?: PolicyToolStatus): string | null {
  if (!verdict || verdict.action === 'allow') return null;
  const title = policyChipTitle(verdict);
  if (verdict.action === 'require_approval') {
    return `${title} (requires approval, enforced as block)`;
  }
  return title;
}
