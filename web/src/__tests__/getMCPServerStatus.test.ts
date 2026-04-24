import { describe, it, expect } from 'vitest';

import { getMCPServerStatus } from '../lib/graph/nodes';
import type { AutoscaleStatus, MCPServerStatus, ReplicaStatus } from '../types';

function makeServer(overrides: Partial<MCPServerStatus> = {}): MCPServerStatus {
  return {
    name: 'svc',
    transport: 'http',
    initialized: true,
    toolCount: 0,
    tools: [],
    ...overrides,
  };
}

function makeAutoscale(overrides: Partial<AutoscaleStatus> = {}): AutoscaleStatus {
  return {
    min: 0,
    max: 2,
    current: 0,
    target: 0,
    targetInFlight: 3,
    medianInFlight: 0,
    lastDecision: 'noop',
    idleToZero: true,
    ...overrides,
  };
}

const oneReplica: ReplicaStatus[] = [
  { replicaId: 0, state: 'healthy', healthy: true, inFlight: 0, startedAt: '' },
];

describe('getMCPServerStatus', () => {
  it.each([
    [
      'never-booted non-autoscaled server',
      makeServer({ initialized: false }),
      'initializing',
    ],
    [
      'autoscaled server at zero replicas renders as idle',
      makeServer({ initialized: false, autoscale: makeAutoscale(), replicas: [] }),
      'idle',
    ],
    [
      'autoscaled server at zero replicas with undefined replicas renders as idle',
      makeServer({ initialized: false, autoscale: makeAutoscale() }),
      'idle',
    ],
    [
      'healthy running server',
      makeServer({ initialized: true, healthy: true, replicas: oneReplica }),
      'running',
    ],
    [
      'initialized but unhealthy server',
      makeServer({ initialized: true, healthy: false, replicas: oneReplica }),
      'error',
    ],
    [
      'autoscaled server with at least one live replica is not idle',
      makeServer({
        initialized: true,
        healthy: true,
        autoscale: makeAutoscale({ current: 1, target: 1 }),
        replicas: oneReplica,
      }),
      'running',
    ],
  ])('%s → %s', (_label, input, expected) => {
    expect(getMCPServerStatus(input)).toBe(expected);
  });
});
