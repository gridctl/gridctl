import {
  CheckCircle2,
  CircleDashed,
  CircleSlash,
  AlertCircle,
  PauseCircle,
  Activity,
  type LucideIcon,
} from 'lucide-react';

/**
 * RunStatus is the closed vocabulary the grid renders against. The
 * server emits any of the listed values plus the legacy `started` /
 * `in_progress` / `failed` synonyms; `normalizeStatus` maps them onto
 * the canonical set so callers branch on a small enum.
 */
export type RunStatus =
  | 'running'
  | 'ok'
  | 'error'
  | 'cancelled'
  | 'awaiting_approval'
  | 'suspended'
  | 'unknown';

export interface RunStatusTone {
  status: RunStatus;
  label: string;
  /** Tailwind class for the status dot fill. */
  dot: string;
  /** Tailwind class for the status text + icon tint. */
  text: string;
  /** Tailwind class for a subtle background highlight on the row. */
  rowAccent: string;
  /** Lucide icon to render alongside the dot — color is conveyed via
   *  shape too, satisfying the a11y rule that color isn't the only
   *  signal. */
  icon: LucideIcon;
  /** Whether the dot should pulse (running / awaiting). */
  pulse: boolean;
  /** Optional CSS box-shadow string for the IDE sidebar's glow halo.
   *  Undefined for terminal states (no glow on completed/cancelled). */
  glow?: string;
}

const TONES: Record<RunStatus, RunStatusTone> = {
  running: {
    status: 'running',
    label: 'Running',
    dot: 'bg-status-running',
    text: 'text-status-running',
    rowAccent: 'border-l-status-running/60',
    icon: Activity,
    pulse: true,
    glow: '0 0 8px var(--color-status-running)',
  },
  ok: {
    status: 'ok',
    label: 'Completed',
    dot: 'bg-status-running/60',
    text: 'text-status-running/80',
    rowAccent: 'border-l-transparent',
    icon: CheckCircle2,
    pulse: false,
  },
  error: {
    status: 'error',
    label: 'Errored',
    dot: 'bg-status-error',
    text: 'text-status-error',
    rowAccent: 'border-l-status-error/70',
    icon: AlertCircle,
    pulse: false,
    glow: '0 0 6px var(--color-status-error)',
  },
  cancelled: {
    status: 'cancelled',
    label: 'Cancelled',
    dot: 'bg-text-muted/40',
    text: 'text-text-muted',
    rowAccent: 'border-l-transparent',
    icon: CircleSlash,
    pulse: false,
  },
  awaiting_approval: {
    status: 'awaiting_approval',
    label: 'Awaiting approval',
    dot: 'bg-status-pending',
    text: 'text-status-pending',
    rowAccent: 'border-l-status-pending/70',
    icon: PauseCircle,
    pulse: true,
    glow: '0 0 6px var(--color-status-pending)',
  },
  suspended: {
    status: 'suspended',
    label: 'Suspended',
    dot: 'bg-status-pending/70',
    text: 'text-status-pending/80',
    rowAccent: 'border-l-status-pending/40',
    icon: PauseCircle,
    pulse: false,
  },
  unknown: {
    status: 'unknown',
    label: 'Unknown',
    dot: 'bg-text-muted/40',
    text: 'text-text-muted',
    rowAccent: 'border-l-transparent',
    icon: CircleDashed,
    pulse: false,
  },
};

export function normalizeStatus(raw: string | undefined | null): RunStatus {
  const s = (raw ?? '').toLowerCase();
  if (s === 'running' || s === 'started' || s === 'in_progress') return 'running';
  if (s === 'ok' || s === 'completed' || s === 'success') return 'ok';
  if (s === 'error' || s === 'errored' || s === 'failed') return 'error';
  if (s === 'cancelled' || s === 'canceled') return 'cancelled';
  if (s === 'awaiting_approval' || s === 'pending_approval' || s === 'pending')
    return 'awaiting_approval';
  if (s === 'suspended') return 'suspended';
  return 'unknown';
}

export function statusTone(raw: string | undefined | null): RunStatusTone {
  return TONES[normalizeStatus(raw)];
}

/** The status list rendered in the filter bar (in display order). */
export const FILTERABLE_STATUSES: { value: string; label: string }[] = [
  { value: 'running', label: 'Running' },
  { value: 'ok', label: 'Completed' },
  { value: 'error', label: 'Errored' },
  { value: 'awaiting_approval', label: 'Awaiting approval' },
  { value: 'cancelled', label: 'Cancelled' },
];
