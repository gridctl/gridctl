import { describe, it, expect } from 'vitest';
import { compactPinGlyph, compactStateGlyph, GLYPH_META, mcpHealthSummary } from '../lib/graph/serverState';
import type { MCPServerNodeData, NodeStatus, ResourceNodeData } from '../types';

function server(overrides: Partial<MCPServerNodeData> = {}): MCPServerNodeData {
  return {
    type: 'mcp-server',
    name: 's',
    transport: 'http',
    initialized: true,
    toolCount: 0,
    tools: [],
    status: 'running',
    ...overrides,
  };
}

function resource(status: NodeStatus): ResourceNodeData {
  return { type: 'resource', name: 'r', image: 'img', status };
}

describe('compactStateGlyph', () => {
  it.each<NodeStatus>(['running', 'error', 'needs-auth', 'initializing', 'idle', 'restarting', 'stopped'])(
    'returns disabled for a disabled %s server',
    (status) => {
      expect(compactStateGlyph(server({ enabled: false, status }))).toBe('disabled');
    },
  );

  it('returns null for a running server', () => {
    expect(compactStateGlyph(server())).toBeNull();
    expect(compactStateGlyph(server({ enabled: true }))).toBeNull();
  });

  it.each<NodeStatus>(['error', 'needs-auth', 'initializing', 'idle', 'restarting', 'stopped'])(
    'returns the %s status otherwise',
    (status) => {
      expect(compactStateGlyph(server({ status }))).toBe(status);
    },
  );

  it('works for resources', () => {
    expect(compactStateGlyph(resource('stopped'))).toBe('stopped');
    expect(compactStateGlyph(resource('error'))).toBe('error');
    expect(compactStateGlyph(resource('running'))).toBeNull();
  });
});

describe('compactPinGlyph', () => {
  it('maps pin status to blocked, drift, or null', () => {
    expect(compactPinGlyph(server({ pinStatus: 'blocked' }))).toBe('blocked');
    expect(compactPinGlyph(server({ pinStatus: 'drift' }))).toBe('drift');
    expect(compactPinGlyph(server({ pinStatus: 'pinned' }))).toBeNull();
    expect(compactPinGlyph(server({ pinStatus: 'approved_pending_redeploy' }))).toBeNull();
    expect(compactPinGlyph(server())).toBeNull();
  });

  it('never gives a resource a pin glyph', () => {
    const data = { ...resource('running'), pinStatus: 'blocked' } as ResourceNodeData;
    expect(compactPinGlyph(data)).toBeNull();
  });
});

describe('GLYPH_META', () => {
  it('labels every glyph kind', () => {
    expect(Object.values(GLYPH_META).map((m) => m.label)).toEqual([
      'Disabled', 'Needs authorization', 'Error', 'Restarting', 'Initializing', 'Stopped', 'Idle', 'Schema drift', 'Blocked',
    ]);
  });
});

describe('mcpHealthSummary', () => {
  it('reports the health error, exit, and retry', () => {
    expect(mcpHealthSummary(server({ healthError: 'boom' }))).toBe('boom');
    expect(mcpHealthSummary(server())).toBe('Health check failed');
    const now = Date.parse('2026-11-26T12:00:00Z');
    expect(mcpHealthSummary(server({
      replicas: [
        { replicaId: 0, state: 'unhealthy', healthy: false, inFlight: 0, exit: { code: 137, oomKilled: true, status: 'exited' } },
        { replicaId: 1, state: 'restarting', healthy: false, inFlight: 0, restartAttempts: 3, nextRetryAt: '2026-11-26T12:00:05Z' },
      ],
    }), now)).toBe('exited 137, OOM killed; restarting (attempt 3, retry in 5s)');
  });
});
