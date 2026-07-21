// Pure helpers for the tool-groups overlay in the Tools workspace. JSX-free
// (the metricsData/limitsData split) so Fast Refresh stays happy.
import type { GroupsReport } from './api';

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
