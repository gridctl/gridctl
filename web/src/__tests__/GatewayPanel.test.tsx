import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';

// Mock dependencies before imports
vi.mock('../stores/useStackStore', () => ({
  useSelectedNodeData: vi.fn(),
  useStackStore: vi.fn((selector) => selector({
    selectNode: vi.fn(),
  })),
}));

vi.mock('../stores/useUIStore', () => ({
  useUIStore: vi.fn((selector) => selector({
    registryDetached: false,
    setSidebarOpen: mockSetSidebarOpen,
  })),
}));

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: mockOpenDetachedWindow,
  }),
}));

vi.mock('../components/registry/RegistrySidebar', () => ({
  RegistrySidebar: ({ embedded }: { embedded?: boolean }) => (
    <div data-testid="registry-sidebar" data-embedded={embedded} />
  ),
}));

vi.mock('../components/ui/PopoutButton', () => ({
  PopoutButton: ({ onClick, disabled }: { onClick: () => void; disabled?: boolean }) => (
    <button data-testid="popout-button" onClick={onClick} disabled={disabled}>
      Popout
    </button>
  ),
}));

const mockSetSidebarOpen = vi.fn();
const mockOpenDetachedWindow = vi.fn();

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
    ...overrides,
  };
}

describe('GatewaySidebar', () => {
  it('renders gateway name from selected node', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData({ name: 'my-gateway' }));
    render(<GatewaySidebar onClose={vi.fn()} />);
    expect(screen.getByText('my-gateway')).toBeInTheDocument();
  });

  it('renders version from selected node', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData({ version: 'v0.2.0' }));
    render(<GatewaySidebar onClose={vi.fn()} />);
    expect(screen.getByText('v0.2.0')).toBeInTheDocument();
  });

  it('shows fallback name when no node selected', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(null);
    render(<GatewaySidebar onClose={vi.fn()} />);
    expect(screen.getByText('Gateway')).toBeInTheDocument();
  });

  it('renders embedded RegistrySidebar', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData());
    render(<GatewaySidebar onClose={vi.fn()} />);
    const registry = screen.getByTestId('registry-sidebar');
    expect(registry).toBeInTheDocument();
    expect(registry).toHaveAttribute('data-embedded', 'true');
  });

  it('calls onClose when close button clicked', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData());
    const onClose = vi.fn();
    render(<GatewaySidebar onClose={onClose} />);

    // The close button is the last button (X icon)
    const buttons = screen.getAllByRole('button');
    const closeButton = buttons[buttons.length - 1];
    fireEvent.click(closeButton);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('renders popout button', () => {
    vi.mocked(useSelectedNodeData).mockReturnValue(makeGatewayData());
    render(<GatewaySidebar onClose={vi.fn()} />);
    expect(screen.getByTestId('popout-button')).toBeInTheDocument();
  });
});
