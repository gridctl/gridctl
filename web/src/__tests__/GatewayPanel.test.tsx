import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import '@testing-library/jest-dom';

// Mock dependencies before imports
vi.mock('../stores/useStackStore', () => ({
  useSelectedNodeData: vi.fn(),
  useStackStore: vi.fn((selector) => selector({
    selectNode: vi.fn(),
    mcpServers: [],
  })),
}));

vi.mock('../stores/useRegistryStore', () => ({
  useRegistryStore: vi.fn((selector) => selector({
    skills: [],
  })),
}));

import { GatewaySidebar } from '../components/gateway/GatewaySidebar';
import { useSelectedNodeData } from '../stores/useStackStore';
import type { GatewayNodeData } from '../types';

beforeEach(() => {
  vi.clearAllMocks();
});

function makeGatewayData(overrides: Partial<GatewayNodeData> = {}): GatewayNodeData {
  return {
    type: 'gateway',
    name: 'test-gateway',
    version: 'v0.1.0-beta.1',
    status: 'running',
    serverCount: 3,
    resourceCount: 0,
    agentCount: 0,
    a2aAgentCount: 0,
    clientCount: 0,
    totalToolCount: 0,
    sessions: 0,
    a2aTasks: null,
    codeMode: null,
    totalSkills: 0,
    activeSkills: 0,
    ...overrides,
  };
}

function renderWithRouter(ui: React.ReactNode) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

describe('GatewaySidebar', () => {
  it('renders gateway name from selected node', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData({ name: 'my-gateway' }));
    renderWithRouter(<GatewaySidebar onClose={vi.fn()} />);
    expect(screen.getByText('my-gateway')).toBeInTheDocument();
  });

  it('renders version from selected node', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData({ version: 'v0.2.0' }));
    renderWithRouter(<GatewaySidebar onClose={vi.fn()} />);
    expect(screen.getByText('v0.2.0')).toBeInTheDocument();
  });

  it('shows fallback name when no node selected', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(undefined);
    renderWithRouter(<GatewaySidebar onClose={vi.fn()} />);
    expect(screen.getByText('Gateway')).toBeInTheDocument();
  });

  it('renders a Manage Skills link pointing to /library', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData());
    renderWithRouter(<GatewaySidebar onClose={vi.fn()} />);
    const link = screen.getByRole('link', { name: /manage skills/i });
    expect(link).toHaveAttribute('href', '/library');
  });

  it('calls onClose when close button clicked', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData());
    const onClose = vi.fn();
    renderWithRouter(<GatewaySidebar onClose={onClose} />);

    fireEvent.click(screen.getByLabelText('Close gateway sidebar'));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
