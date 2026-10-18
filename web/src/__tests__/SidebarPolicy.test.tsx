import { describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { Node } from '@xyflow/react';

// useWindowManager opens a BroadcastChannel. The per-server inspector does
// not need it, and jsdom does not provide the constructor.
vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({ openDetachedWindow: vi.fn() }),
}));

// A synchronous configured report. If PolicySummary were mounted on this
// inspector, "Policy:" would be in the first paint instead of a loading state.
vi.mock('../hooks/usePolicy', () => ({
  usePolicy: () => ({
    report: {
      configured: true,
      default: 'allow',
      block_destructive: true,
      rules: [{ index: 0, match: 'vault__*', action: 'block' }],
      tools: {},
    },
    error: null,
  }),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchSecurityReport: vi.fn().mockResolvedValue({ checks: [] }),
  };
});

import { Sidebar } from '../components/layout/Sidebar';
import { useStackStore } from '../stores/useStackStore';

const serverNode: Node = {
  id: 'mcp-vault',
  position: { x: 0, y: 0 },
  data: {
    type: 'mcp-server',
    name: 'vault',
    transport: 'stdio',
    initialized: true,
    toolCount: 2,
    tools: ['list_paths', 'read_secret'],
    status: 'running',
  },
};

describe('per-server inspector', () => {
  it('does not mount the gateway policy summary', () => {
    useStackStore.setState({
      selectedNodeId: 'mcp-vault',
      nodes: [serverNode],
      mcpServers: [],
    });
    render(
      <MemoryRouter>
        <Sidebar />
      </MemoryRouter>,
    );

    expect(screen.getByRole('button', { name: /mcp catalog/i })).toBeInTheDocument();
    expect(screen.getByText('vault')).toBeInTheDocument();
    expect(screen.queryByText(/^Policy:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/policy status is unavailable/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/loading policy/i)).not.toBeInTheDocument();
  });
});
