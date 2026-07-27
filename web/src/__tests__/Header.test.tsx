import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { Header } from '../components/layout/Header';
import { useStackStore } from '../stores/useStackStore';
import type { MCPServerStatus } from '../types';

const SERVERS: MCPServerStatus[] = [
  { name: 'github', transport: 'http', initialized: true, healthy: true, toolCount: 2, tools: ['search', 'create'] },
  { name: 'gitlab', transport: 'http', initialized: true, healthy: true, toolCount: 1, tools: ['list'] },
];

function renderHeader() {
  return render(
    <MemoryRouter initialEntries={['/stack']}>
      <Header />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  useStackStore.setState({
    gatewayInfo: { name: 'acme-stack', version: 'v9.9.9' },
    mcpServers: SERVERS,
    connectionStatus: 'connected',
  });
});

describe('Header', () => {
  it('does not mirror connection state or server count (the StatusBar owns those)', () => {
    renderHeader();
    // The redundant header chips are gone.
    expect(screen.queryByText('Connected')).not.toBeInTheDocument();
    expect(screen.queryByText('Disconnected')).not.toBeInTheDocument();
    expect(screen.queryByText(/active/i)).not.toBeInTheDocument();
  });

  // The gateway name and version are both properties of the gateway, surfaced
  // on the canvas node and inspector sidebar. Neither belongs in the header.
  // gatewayInfo is populated in beforeEach, so these assertions prove the
  // header omits values that are available to it rather than an empty store.
  it('drops the gateway-name chip and the version beside the logo', () => {
    renderHeader();
    expect(screen.queryByText('acme-stack')).not.toBeInTheDocument();
    expect(screen.queryByText('v9.9.9')).not.toBeInTheDocument();
  });

  it('keeps the persistence quick-toggle while connected', () => {
    renderHeader();
    expect(screen.getByText(/Persistence:/)).toBeInTheDocument();
  });

  it('hides the persistence toggle when disconnected', () => {
    useStackStore.setState({ connectionStatus: 'disconnected' });
    renderHeader();
    expect(screen.queryByText(/Persistence:/)).not.toBeInTheDocument();
  });
});
