import type { ServerPins } from '../../lib/api';

// Shared status display metadata for pin surfaces (bottom-panel tab,
// workspace rail, and detail header), keyed to the status color tokens.
export function pinStatusMeta(status: ServerPins['status']): {
  label: string;
  colorClass: string;
} {
  switch (status) {
    case 'drift':
      return { label: 'Drift', colorClass: 'text-status-pending' };
    case 'approved_pending_redeploy':
      return { label: 'Approved', colorClass: 'text-status-running' };
    default:
      return { label: 'Pinned', colorClass: 'text-status-running' };
  }
}
