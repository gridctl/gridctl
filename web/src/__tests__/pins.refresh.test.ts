import { describe, it, expect } from 'vitest';
import { serverPinsChanged } from '../stores/usePinsStore';
import type { ServerPins } from '../lib/api';

function pin(status: ServerPins['status'], toolCount: number): ServerPins {
  return {
    server_hash: 'abc',
    pinned_at: '2026-01-01T00:00:00Z',
    last_verified_at: '2026-01-01T00:00:00Z',
    tool_count: toolCount,
    status,
    tools: {},
  };
}

describe('serverPinsChanged', () => {
  it('returns true when the previous snapshot is null', () => {
    expect(serverPinsChanged(null, { github: pin('pinned', 3) })).toBe(true);
  });

  it('returns true when a server is added or removed', () => {
    const prev = { github: pin('pinned', 3) };
    expect(serverPinsChanged(prev, { github: pin('pinned', 3), jira: pin('pinned', 1) })).toBe(true);
    expect(serverPinsChanged({ github: pin('pinned', 3), jira: pin('pinned', 1) }, prev)).toBe(true);
  });

  it('returns true when status or tool_count differs', () => {
    const prev = { github: pin('pinned', 3) };
    expect(serverPinsChanged(prev, { github: pin('drift', 3) })).toBe(true);
    expect(serverPinsChanged(prev, { github: pin('pinned', 4) })).toBe(true);
  });

  it('returns false for a structurally identical snapshot', () => {
    const prev = { github: pin('pinned', 3), jira: pin('drift', 2) };
    const next = {
      jira: { ...pin('drift', 2), tools: { search: { hash: 'h', name: 'search', pinned_at: 't' } } },
      github: pin('pinned', 3),
    };
    expect(serverPinsChanged(prev, next)).toBe(false);
  });
});
