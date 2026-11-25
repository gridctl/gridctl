import type { NodeStatus } from '../types';

/** Human labels for node statuses, shared by the canvas and the inspector. */
export const NODE_STATUS_LABELS: Record<NodeStatus, string> = {
  running: 'running',
  stopped: 'stopped',
  error: 'error',
  initializing: 'initializing',
  idle: 'idle',
  'needs-auth': 'needs auth',
  restarting: 'restarting',
  disabled: 'disabled',
};
