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

export interface AnnotationChipLegendEntry extends AnnotationChip {
  name: string;
}

// The single chip table read by annotationChips and the Tools workspace
// legend, so the two cannot drift.
const CHIP = {
  RO: { label: 'RO', name: 'Read-only', title: 'readOnlyHint: true — does not modify state', tone: 'safe' },
  RW: { label: 'RW', name: 'Read-write', title: 'readOnlyHint: false — may modify state', tone: 'neutral' },
  DESTR: { label: 'DESTR', name: 'Destructive', title: 'destructiveHint: true — may perform destructive updates', tone: 'danger' },
  SAFE: { label: 'SAFE', name: 'Non-destructive', title: 'destructiveHint: false — updates are additive', tone: 'safe' },
  IDEM: { label: 'IDEM', name: 'Idempotent', title: 'idempotentHint: true — repeat calls have no extra effect', tone: 'neutral' },
  OPEN: { label: 'OPEN', name: 'Open world', title: 'openWorldHint: true — interacts with external entities', tone: 'neutral' },
  CLOSED: { label: 'CLOSED', name: 'Closed world', title: 'openWorldHint: false — closed domain of interaction', tone: 'neutral' },
} as const satisfies Record<string, AnnotationChipLegendEntry>;

export const ANNOTATION_CHIP_LEGEND: readonly AnnotationChipLegendEntry[] = [
  CHIP.RO,
  CHIP.RW,
  CHIP.DESTR,
  CHIP.SAFE,
  CHIP.IDEM,
  CHIP.OPEN,
  CHIP.CLOSED,
];

function chipOf(entry: AnnotationChipLegendEntry): AnnotationChip {
  return { label: entry.label, title: entry.title, tone: entry.tone };
}

export function annotationChips(annotations?: {
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
}): AnnotationChip[] {
  if (!annotations) return [];
  const chips: AnnotationChip[] = [];
  if (annotations.readOnlyHint !== undefined) {
    chips.push(chipOf(annotations.readOnlyHint ? CHIP.RO : CHIP.RW));
  }
  if (annotations.destructiveHint !== undefined) {
    chips.push(chipOf(annotations.destructiveHint ? CHIP.DESTR : CHIP.SAFE));
  }
  if (annotations.idempotentHint !== undefined && annotations.idempotentHint) {
    chips.push(chipOf(CHIP.IDEM));
  }
  if (annotations.openWorldHint !== undefined) {
    chips.push(chipOf(annotations.openWorldHint ? CHIP.OPEN : CHIP.CLOSED));
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
