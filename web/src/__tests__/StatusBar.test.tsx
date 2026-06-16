import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { StatusBar } from '../components/layout/StatusBar';
import { useStackStore } from '../stores/useStackStore';
import type { MCPServerStatus } from '../types';

function server(name: string, healthy: boolean): MCPServerStatus {
  return { name, transport: 'http', initialized: true, healthy, toolCount: 1, tools: ['t'] };
}

function renderBar() {
  return render(
    <MemoryRouter>
      <StatusBar />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  useStackStore.setState({
    mcpServers: [],
    resources: [],
    sessions: 0,
    codeMode: 'off',
    tokenUsage: null,
    costUsage: null,
    connectionStatus: 'connected',
    lastUpdated: null,
    error: null,
  });
});

describe('StatusBar', () => {
  it('owns the connection indicator', () => {
    renderBar();
    expect(screen.getByText('Connected')).toBeInTheDocument();
  });

  it('owns the server-count and unhealthy indicators', () => {
    useStackStore.setState({
      mcpServers: [server('s1', true), server('s2', false), server('s3', false)],
    });
    renderBar();
    expect(screen.getByText('MCP')).toBeInTheDocument();
    // Two unhealthy servers surface here, where the header used to mirror them.
    expect(screen.getByText('err')).toBeInTheDocument();
  });

  it('omits the unhealthy indicator when all servers are healthy', () => {
    useStackStore.setState({ mcpServers: [server('s1', true), server('s2', true)] });
    renderBar();
    expect(screen.getByText('MCP')).toBeInTheDocument();
    expect(screen.queryByText('err')).not.toBeInTheDocument();
  });
});
