import { describe, it, expect, afterEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { hasMixedGenerations } from '../lib/graph/nodes';
import { LiveSessionsCard } from '../components/workspaces/ConnectionsWorkspace';
import * as api from '../lib/api';
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

describe('LiveSessionsCard', () => {
  it('renders session entries with their generation tags', async () => {
    vi.spyOn(api, 'fetchSessions').mockResolvedValue({
      count: 2,
      sessions: ['s-1', 's-2'],
      entries: [
        { id: 's-1', generation: 'handshake', protocolVersion: '2025-11-25' },
        { id: 's-2', generation: 'handshake', protocolVersion: '2025-06-18' },
      ],
    });
    render(<LiveSessionsCard />);

    await waitFor(() => {
      expect(screen.getByText('s-1')).toBeInTheDocument();
    });
    expect(screen.getByText('s-2')).toBeInTheDocument();
    expect(screen.getAllByText('handshake')).toHaveLength(2);
    expect(screen.getByText('2025-11-25')).toBeInTheDocument();
    expect(screen.getByText(/2 active/)).toBeInTheDocument();
  });

  it('explains the stateless-era absence when no sessions exist', async () => {
    vi.spyOn(api, 'fetchSessions').mockResolvedValue({ count: 0, sessions: [], entries: [] });
    render(<LiveSessionsCard />);

    await waitFor(() => {
      expect(screen.getByText(/No active sessions/)).toBeInTheDocument();
    });
    expect(screen.getByText(/sessionless/)).toBeInTheDocument();
  });

  it('reports unavailability instead of failing when the fetch errors', async () => {
    vi.spyOn(api, 'fetchSessions').mockRejectedValue(new Error('boom'));
    render(<LiveSessionsCard />);

    await waitFor(() => {
      expect(screen.getByText('unavailable')).toBeInTheDocument();
    });
  });

  it('falls back to the bare ID list from pre-dual-stack daemons', async () => {
    // An old daemon serves {count, sessions} with no entries; every
    // session it reports is handshake-generation by definition.
    vi.spyOn(api, 'fetchSessions').mockResolvedValue({
      count: 1,
      sessions: ['old-session'],
    });
    render(<LiveSessionsCard />);

    await waitFor(() => {
      expect(screen.getByText('old-session')).toBeInTheDocument();
    });
    expect(screen.getByText('handshake')).toBeInTheDocument();
    expect(screen.queryByText(/No active sessions/)).not.toBeInTheDocument();
  });
});
