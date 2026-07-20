import { AlertOctagon, AlertTriangle, Info } from 'lucide-react';

// gridctl's one severity vocabulary, shared by optimize findings and
// poisoning-scan findings. Icon and color live here once so a token change
// never needs edits in every consumer, and the surfaces cannot drift apart:
// critical is red (failed or hostile), warn is amber (informs, never blocks),
// info is muted (advisory detail).
export type FindingSeverity = 'info' | 'warn' | 'critical';

export const severityIcon: Record<FindingSeverity, typeof AlertOctagon> = {
  critical: AlertOctagon,
  warn: AlertTriangle,
  info: Info,
};

export const severityClasses: Record<FindingSeverity, string> = {
  critical: 'text-status-error border-status-error/30 bg-status-error/10',
  warn: 'text-status-pending border-status-pending/30 bg-status-pending/10',
  info: 'text-text-muted border-border/40 bg-surface-elevated/40',
};
