import { describe, it, expect, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { hasMixedGenerations } from '../lib/graph/nodes';
import { attributeSessions, sessionIdentity } from '../components/connections/connectionsModel';
import type { MCPServerStatus } from '../types';

function server(overrides: Partial<MCPServerStatus> & { name: string }): MCPServerStatus {
  return {
    transport: 'http',
    initialized: true,
    toolCount: 0,
    tools: [],
    ...overrides,
  } as MCPServerStatus;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('hasMixedGenerations', () => {
  it('is false for a uniform fleet', () => {
    expect(
      hasMixedGenerations([
        server({ name: 'a', protocolGeneration: 'handshake' }),
        server({ name: 'b', protocolGeneration: 'handshake' }),
      ]),
    ).toBe(false);
  });

  it('is true when generations differ', () => {
    expect(
      hasMixedGenerations([
        server({ name: 'a', protocolGeneration: 'handshake' }),
        server({ name: 'b', protocolGeneration: 'stateless' }),
      ]),
    ).toBe(true);
  });

  it('ignores servers without a generation (OpenAPI adapters)', () => {
    expect(
      hasMixedGenerations([
        server({ name: 'a', protocolGeneration: 'stateless' }),
        server({ name: 'openapi', openapi: true }),
      ]),
    ).toBe(false);
  });

  it('is false for an empty fleet', () => {
    expect(hasMixedGenerations([])).toBe(false);
  });
});

describe('session attribution (Connections hub)', () => {
  it('joins entries to client slugs by accessId and buckets the rest honestly', () => {
    const { bySlug, unattributed } = attributeSessions(
      [
        { id: 's-1', generation: 'handshake', protocolVersion: '2025-11-25', accessId: 'claude' },
        { id: 's-2', generation: 'handshake', accessId: 'mystery' },
        { id: 's-3', generation: 'handshake' },
      ],
      new Set(['claude', 'cursor']),
    );
    expect(bySlug.get('claude')).toHaveLength(1);
    // Unknown identities are never force-matched to a guess.
    expect(unattributed).toHaveLength(2);
  });

  it('synthesizes a fallback identity instead of showing unknown', () => {
    expect(
      sessionIdentity({ id: 'abcdef0123456789', generation: 'handshake' }),
    ).toBe('handshake \u00b7 abcdef01');
    expect(
      sessionIdentity({ id: 'x', generation: 'handshake', clientName: 'claude-code', clientVersion: '2.1' }),
    ).toBe('claude-code 2.1');
  });

  it('treats the pre-dual-stack bare ID list as handshake-generation sessions', () => {
    // An old daemon serves {count, sessions} with no entries; the
    // workspace maps every bare ID to a handshake entry before
    // attribution, so nothing is dropped.
    const entries = ['old-session'].map((id) => ({ id, generation: 'handshake' }));
    const { unattributed } = attributeSessions(entries, new Set());
    expect(unattributed).toHaveLength(1);
    expect(sessionIdentity(unattributed[0])).toContain('handshake');
  });
});
