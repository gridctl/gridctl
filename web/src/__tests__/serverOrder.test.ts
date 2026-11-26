import { describe, expect, it } from 'vitest';

import { attentionBucket, orderServersByAttention } from '../lib/graph/serverOrder';
import type { MCPServerStatus } from '../types';

function makeServer(name: string, overrides: Partial<MCPServerStatus> = {}): MCPServerStatus {
  return {
    name,
    transport: 'http',
    initialized: true,
    healthy: true,
    toolCount: 1,
    tools: ['read'],
    ...overrides,
  };
}

function pin(status: 'pinned' | 'drift' | 'blocked' | 'approved_pending_redeploy') {
  return { status, tool_count: 1 };
}

const names = (servers: MCPServerStatus[]) => servers.map((s) => s.name);

describe('attentionBucket', () => {
  it.each([
    { label: 'error', server: makeServer('a', { healthy: false }), pins: null, bucket: 0 },
    {
      label: 'restarting',
      server: makeServer('a', { healthy: false, replicas: [{ replicaId: 0, state: 'restarting', healthy: false, inFlight: 0 }] }),
      pins: null,
      bucket: 0,
    },
    { label: 'blocked pin', server: makeServer('a'), pins: { a: pin('blocked') }, bucket: 0 },
    { label: 'needs-auth', server: makeServer('a', { authStatus: 'needs_auth' }), pins: null, bucket: 1 },
    { label: 'drift', server: makeServer('a'), pins: { a: pin('drift') }, bucket: 2 },
    { label: 'running', server: makeServer('a'), pins: null, bucket: 3 },
    { label: 'initializing', server: makeServer('a', { initialized: false }), pins: null, bucket: 3 },
    { label: 'idle', server: makeServer('a', { autoscale: { min: 0, max: 2, current: 0, target: 0 } } as Partial<MCPServerStatus>), pins: null, bucket: 3 },
    { label: 'approved pending redeploy', server: makeServer('a'), pins: { a: pin('approved_pending_redeploy') }, bucket: 3 },
    { label: 'disabled unhealthy', server: makeServer('a', { enabled: false, healthy: false }), pins: { a: pin('blocked') }, bucket: 3 },
  ])('puts $label in bucket $bucket', ({ server, pins, bucket }) => {
    expect(attentionBucket(server, pins)).toBe(bucket);
  });
});

describe('orderServersByAttention', () => {
  it('orders buckets unhealthy, needs-auth, drift, then the rest', () => {
    const servers = [
      makeServer('alpha'),
      makeServer('bravo', { initialized: false }),
      makeServer('charlie'),
      makeServer('delta', { authStatus: 'needs_auth' }),
      makeServer('echo', { healthy: false }),
      makeServer('foxtrot'),
    ];
    const ordered = orderServersByAttention(servers, { charlie: pin('drift'), foxtrot: pin('blocked') });
    expect(names(ordered)).toEqual(['echo', 'foxtrot', 'delta', 'charlie', 'alpha', 'bravo']);
  });

  it('keeps input order within a bucket', () => {
    const servers = [
      makeServer('a'),
      makeServer('z', { healthy: false }),
      makeServer('b'),
      makeServer('m', { healthy: false }),
      makeServer('c'),
    ];
    expect(names(orderServersByAttention(servers, null))).toEqual(['z', 'm', 'a', 'b', 'c']);
  });

  it('keeps error and restarting in one bucket, in input order', () => {
    const restarting = makeServer('r', {
      healthy: false,
      replicas: [{ replicaId: 0, state: 'restarting', healthy: false, inFlight: 0 }],
    });
    const error = makeServer('e', { healthy: false });
    expect(names(orderServersByAttention([makeServer('a'), restarting, error], null))).toEqual(['r', 'e', 'a']);
    expect(names(orderServersByAttention([makeServer('a'), error, restarting], null))).toEqual(['e', 'r', 'a']);
  });

  it('leaves a disabled unhealthy server with the rest', () => {
    const servers = [makeServer('a'), makeServer('off', { enabled: false, healthy: false }), makeServer('b')];
    expect(orderServersByAttention(servers, null)).toBe(servers);
  });

  it('does not promote an initializing server', () => {
    const servers = [makeServer('a'), makeServer('init', { initialized: false })];
    expect(orderServersByAttention(servers, null)).toBe(servers);
  });

  it('promotes nothing from pin state when pins is null', () => {
    const servers = [makeServer('a'), makeServer('b')];
    expect(orderServersByAttention(servers, null)).toBe(servers);
    expect(names(orderServersByAttention(servers, { b: pin('drift') }))).toEqual(['b', 'a']);
  });

  it('returns the input array when no server is promoted', () => {
    const servers = [makeServer('a'), makeServer('b')];
    expect(orderServersByAttention(servers, { a: pin('pinned') })).toBe(servers);
    expect(orderServersByAttention([], null)).toEqual([]);
  });

  it('does not mutate the input', () => {
    const servers = [makeServer('a'), makeServer('b', { healthy: false })];
    const copy = [...servers];
    const ordered = orderServersByAttention(servers, null);
    expect(ordered).not.toBe(servers);
    expect(servers).toEqual(copy);
    expect(servers.map((s, i) => s === copy[i])).toEqual([true, true]);
  });
});
