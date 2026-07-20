import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';

vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
}));

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

vi.mock('../lib/api', () => ({
  fetchAuthServers: vi.fn(),
  beginServerAuthorization: vi.fn(),
  waitServerAuthorization: vi.fn(),
  logoutServerAuthorization: vi.fn(),
}));

import CustomNode from '../components/graph/CustomNode';
import { getMCPServerStatus } from '../lib/graph/nodes';
import { ServerAuthSection } from '../components/sidebar/ServerAuthSection';
import { AuthPendingBadge } from '../components/sidebar/AuthPendingBadge';
import { useStackStore } from '../stores/useStackStore';
import {
  fetchAuthServers,
  beginServerAuthorization,
  waitServerAuthorization,
} from '../lib/api';
import type { MCPServerNodeData, MCPServerStatus } from '../types';

function makeServerStatus(overrides: Partial<MCPServerStatus> = {}): MCPServerStatus {
  return {
    name: 'notion',
    transport: 'http',
    initialized: false,
    toolCount: 0,
    tools: [],
    external: true,
    ...overrides,
  };
}

function makeServerData(overrides: Partial<MCPServerNodeData> = {}): MCPServerNodeData {
  return {
    type: 'mcp-server',
    name: 'notion',
    transport: 'http',
    initialized: false,
    toolCount: 0,
    tools: [],
    status: 'needs-auth',
    external: true,
    authStatus: 'needs_auth',
    ...overrides,
  };
}

beforeEach(() => {
  vi.mocked(fetchAuthServers).mockResolvedValue([]);
});

afterEach(() => {
  vi.clearAllMocks();
  useStackStore.setState({ mcpServers: [], selectedNodeId: null });
});

describe('getMCPServerStatus needs-auth precedence', () => {
  it('maps needs_auth to needs-auth even when unhealthy and uninitialized', () => {
    const status = getMCPServerStatus(
      makeServerStatus({ authStatus: 'needs_auth', healthy: false, initialized: false }),
    );
    expect(status).toBe('needs-auth');
  });

  it('never renders a needs_auth server as error', () => {
    const status = getMCPServerStatus(makeServerStatus({ authStatus: 'needs_auth', healthy: false }));
    expect(status).not.toBe('error');
  });

  it('maps authorized servers by health as before', () => {
    expect(
      getMCPServerStatus(makeServerStatus({ authStatus: 'authorized', healthy: true, initialized: true })),
    ).toBe('running');
    expect(getMCPServerStatus(makeServerStatus({ healthy: false }))).toBe('error');
  });

  it('keeps scale-to-zero idle precedence above needs-auth', () => {
    const status = getMCPServerStatus(
      makeServerStatus({
        authStatus: 'needs_auth',
        autoscale: {
          min: 0, max: 3, current: 0, target: 0, medianInFlight: 0, targetInFlight: 1,
        } as MCPServerStatus['autoscale'],
        replicas: [],
      }),
    );
    expect(status).toBe('idle');
  });
});

describe('CustomNode needs-auth rendering', () => {
  it('shows the amber authorization indicator with an accessible label', () => {
    render(<CustomNode data={makeServerData()} />);
    expect(screen.getByRole('status', { name: 'notion needs authorization' })).toBeInTheDocument();
    expect(screen.getByText('Needs authorization')).toBeInTheDocument();
  });

  it('shows a friendly badge label instead of the raw status token', () => {
    render(<CustomNode data={makeServerData()} />);
    expect(screen.getByText('needs auth')).toBeInTheDocument();
    expect(screen.queryByText('needs-auth')).not.toBeInTheDocument();
  });

  it('renders no authorization indicator for authorized servers', () => {
    render(
      <CustomNode
        data={makeServerData({ status: 'running', initialized: true, authStatus: 'authorized' })}
      />,
    );
    expect(screen.queryByText('Needs authorization')).not.toBeInTheDocument();
  });

  it('suppresses the red health strip while the server needs authorization', () => {
    render(
      <CustomNode
        data={makeServerData({ healthy: false, healthError: 'connection refused' })}
      />,
    );
    expect(screen.getByText('Needs authorization')).toBeInTheDocument();
    expect(screen.queryByText('connection refused')).not.toBeInTheDocument();
    expect(screen.queryByText('Health check failed')).not.toBeInTheDocument();
  });

  it('keeps the red health strip for unhealthy authorized servers', () => {
    render(
      <CustomNode
        data={makeServerData({
          status: 'error',
          authStatus: 'authorized',
          healthy: false,
          healthError: 'connection refused',
        })}
      />,
    );
    expect(screen.getByText('connection refused')).toBeInTheDocument();
  });
});

describe('ServerAuthSection', () => {
  it('renders needs-auth state with an Authorize button and no Sign out', () => {
    render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    expect(screen.getByText('Needs authorization')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Authorize/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Sign out/ })).not.toBeInTheDocument();
  });

  it('renders authorized state with issuer, Re-authorize, and Sign out', () => {
    render(
      <ServerAuthSection
        serverName="notion"
        authStatus="authorized"
        authIssuer="https://as.example.com"
      />,
    );
    expect(screen.getByText('Authorized')).toBeInTheDocument();
    expect(screen.getByText('https://as.example.com')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Re-authorize/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Sign out/ })).toBeInTheDocument();
  });

  it('shows scopes from the auth detail endpoint', async () => {
    vi.mocked(fetchAuthServers).mockResolvedValue([
      {
        server: 'notion',
        resource: 'https://mcp.notion.com/mcp',
        status: 'authorized',
        scopes: ['read', 'write'],
      },
    ]);
    render(<ServerAuthSection serverName="notion" authStatus="authorized" />);
    expect(await screen.findByText('read')).toBeInTheDocument();
    expect(screen.getByText('write')).toBeInTheDocument();
  });

  it('runs the authorize flow: login, popup, wait, done', async () => {
    vi.mocked(beginServerAuthorization).mockResolvedValue({
      authorize_url: 'https://as.example.com/authorize?state=abc',
      state: 'abc',
    });
    vi.mocked(waitServerAuthorization).mockResolvedValue(undefined);
    const openSpy = vi.spyOn(window, 'open').mockReturnValue({} as Window);

    render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    fireEvent.click(screen.getByRole('button', { name: /Authorize/ }));

    await waitFor(() => {
      expect(screen.getByText(/Authorized\. The server reconnects automatically\./)).toBeInTheDocument();
    });
    // No 'noopener' in the feature string: it would make window.open return
    // null even on success, defeating the retained-handle cancel/close logic.
    expect(openSpy).toHaveBeenCalledWith(
      'https://as.example.com/authorize?state=abc',
      '_blank',
      expect.not.stringContaining('noopener'),
    );
    expect(waitServerAuthorization).toHaveBeenCalledWith('notion', 'abc', expect.any(AbortSignal));
    openSpy.mockRestore();
  });

  it('falls back to a clickable anchor plus copy button when the popup is blocked', async () => {
    vi.mocked(beginServerAuthorization).mockResolvedValue({
      authorize_url: 'https://as.example.com/authorize?state=abc',
      state: 'abc',
    });
    // Keep the wait pending so the blocked-popup state stays visible.
    vi.mocked(waitServerAuthorization).mockReturnValue(new Promise(() => {}));
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(null);

    render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    fireEvent.click(screen.getByRole('button', { name: /Authorize/ }));

    expect(await screen.findByText(/Popup blocked/)).toBeInTheDocument();
    const anchor = screen.getByRole('link', { name: 'Open authorization page' });
    expect(anchor).toHaveAttribute('href', 'https://as.example.com/authorize?state=abc');
    expect(anchor).toHaveAttribute('target', '_blank');
    expect(anchor).toHaveAttribute('rel', 'noopener');
    expect(screen.getByRole('button', { name: 'Copy authorization URL' })).toBeInTheDocument();
    expect(screen.getByText(/gridctl auth login notion/)).toBeInTheDocument();
    openSpy.mockRestore();
  });

  it('shows a Cancel button while waiting that closes the popup and returns to idle', async () => {
    vi.mocked(beginServerAuthorization).mockResolvedValue({
      authorize_url: 'https://as.example.com/authorize?state=abc',
      state: 'abc',
    });
    let waitSignal: AbortSignal | undefined;
    vi.mocked(waitServerAuthorization).mockImplementation(
      (_server, _state, signal) =>
        new Promise((_, reject) => {
          waitSignal = signal;
          signal?.addEventListener('abort', () =>
            reject(new DOMException('The operation was aborted.', 'AbortError')),
          );
        }),
    );
    const close = vi.fn();
    const openSpy = vi
      .spyOn(window, 'open')
      .mockReturnValue({ closed: false, close } as unknown as Window);

    render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    fireEvent.click(screen.getByRole('button', { name: /Authorize/ }));

    const cancel = await screen.findByRole('button', { name: 'Cancel' });
    expect(screen.getByRole('button', { name: /Waiting for provider/ })).toBeDisabled();
    expect(screen.getByText(/gridctl auth login notion/)).toBeInTheDocument();

    fireEvent.click(cancel);
    expect(close).toHaveBeenCalled();
    expect(waitSignal?.aborted).toBe(true);
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument();
    });
    // Back to idle: Authorize is enabled again and no failure box appeared.
    expect(screen.getByRole('button', { name: 'Authorize' })).toBeEnabled();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    openSpy.mockRestore();
  });

  it('returns to idle when the user closes the popup by hand', async () => {
    vi.mocked(beginServerAuthorization).mockResolvedValue({
      authorize_url: 'https://as.example.com/authorize?state=abc',
      state: 'abc',
    });
    vi.mocked(waitServerAuthorization).mockImplementation(
      (_server, _state, signal) =>
        new Promise((_, reject) => {
          signal?.addEventListener('abort', () =>
            reject(new DOMException('The operation was aborted.', 'AbortError')),
          );
        }),
    );
    const popup = { closed: false, close: vi.fn() };
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);

    render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    fireEvent.click(screen.getByRole('button', { name: /Authorize/ }));
    await screen.findByRole('button', { name: 'Cancel' });

    vi.useFakeTimers();
    popup.closed = true;
    await vi.advanceTimersByTimeAsync(1100);
    vi.useRealTimers();

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Authorize' })).toBeEnabled();
    });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    openSpy.mockRestore();
  });

  it('aborts the wait long-poll on unmount without flipping to failed', async () => {
    vi.mocked(beginServerAuthorization).mockResolvedValue({
      authorize_url: 'https://as.example.com/authorize?state=abc',
      state: 'abc',
    });
    let waitSignal: AbortSignal | undefined;
    vi.mocked(waitServerAuthorization).mockImplementation(
      (_server, _state, signal) =>
        new Promise((_, reject) => {
          waitSignal = signal;
          signal?.addEventListener('abort', () =>
            reject(new DOMException('The operation was aborted.', 'AbortError')),
          );
        }),
    );
    const openSpy = vi
      .spyOn(window, 'open')
      .mockReturnValue({ closed: false, close: vi.fn() } as unknown as Window);

    const { unmount } = render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    fireEvent.click(screen.getByRole('button', { name: /Authorize/ }));
    await screen.findByRole('button', { name: 'Cancel' });

    unmount();
    expect(waitSignal?.aborted).toBe(true);
    openSpy.mockRestore();
  });

  it('does not carry an in-flight flow onto another server when retargeted', async () => {
    vi.mocked(beginServerAuthorization).mockResolvedValue({
      authorize_url: 'https://as.example.com/authorize?state=abc',
      state: 'abc',
    });
    let waitSignal: AbortSignal | undefined;
    vi.mocked(waitServerAuthorization).mockImplementation(
      (_server, _state, signal) =>
        new Promise((_, reject) => {
          waitSignal = signal;
          signal?.addEventListener('abort', () =>
            reject(new DOMException('The operation was aborted.', 'AbortError')),
          );
        }),
    );
    const openSpy = vi
      .spyOn(window, 'open')
      .mockReturnValue({ closed: false, close: vi.fn() } as unknown as Window);

    // The Sidebar reuses one instance across node selections; simulate that
    // by rerendering the same component with a different serverName mid-wait.
    const { rerender } = render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    fireEvent.click(screen.getByRole('button', { name: /Authorize/ }));
    await screen.findByRole('button', { name: 'Cancel' });

    rerender(<ServerAuthSection serverName="sentry" authStatus="needs_auth" />);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Authorize' })).toBeEnabled();
    });
    expect(waitSignal?.aborted).toBe(true);
    expect(screen.queryByRole('button', { name: 'Cancel' })).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    openSpy.mockRestore();
  });

  it('resets a stale done message when authStatus transitions', async () => {
    vi.mocked(beginServerAuthorization).mockResolvedValue({
      authorize_url: 'https://as.example.com/authorize?state=abc',
      state: 'abc',
    });
    vi.mocked(waitServerAuthorization).mockResolvedValue(undefined);
    const openSpy = vi
      .spyOn(window, 'open')
      .mockReturnValue({ closed: false, close: vi.fn() } as unknown as Window);

    const { rerender } = render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    fireEvent.click(screen.getByRole('button', { name: /Authorize/ }));
    await screen.findByText(/Authorized\. The server reconnects automatically\./);

    // The 3s status poll flips the parent prop; the local done message must
    // clear instead of lingering beside the fresh status badge.
    rerender(<ServerAuthSection serverName="notion" authStatus="authorized" />);
    await waitFor(() => {
      expect(
        screen.queryByText(/Authorized\. The server reconnects automatically\./),
      ).not.toBeInTheDocument();
    });
    openSpy.mockRestore();
  });

  it('surfaces a failed authorization', async () => {
    vi.mocked(beginServerAuthorization).mockRejectedValue(new Error('authorization server returned error: access_denied'));

    render(<ServerAuthSection serverName="notion" authStatus="needs_auth" />);
    fireEvent.click(screen.getByRole('button', { name: /Authorize/ }));

    expect(await screen.findByRole('alert')).toHaveTextContent('access_denied');
  });
});

describe('AuthPendingBadge', () => {
  it('renders nothing when no server needs authorization', () => {
    useStackStore.setState({
      mcpServers: [makeServerStatus({ authStatus: 'authorized' })],
    });
    const { container } = render(<AuthPendingBadge />);
    expect(container).toBeEmptyDOMElement();
  });

  it('shows the pending count and selects the first pending server on click', async () => {
    useStackStore.setState({
      mcpServers: [
        makeServerStatus({ name: 'github', authStatus: 'authorized' }),
        makeServerStatus({ name: 'notion', authStatus: 'needs_auth' }),
        makeServerStatus({ name: 'sentry', authStatus: 'needs_auth' }),
      ],
    });
    render(<AuthPendingBadge />);

    const badge = screen.getByRole('button', { name: /Authorization: 2 pending/ });
    expect(badge).toHaveTextContent('Auth: 2 pending');

    fireEvent.click(badge);
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-notion');
  });
});

describe('setGatewayStatus needs-auth transition toast', () => {
  it('toasts once on the transition into needs_auth, not on first sight or repeats', async () => {
    const { showToast } = await import('../components/ui/Toast');
    const toastSpy = vi.mocked(showToast);

    const status = (authStatus?: 'authorized' | 'needs_auth') => ({
      gateway: { name: 'g', version: '1' },
      'mcp-servers': [makeServerStatus({ name: 'notion', authStatus })],
      resources: [],
      sessions: 0,
    });

    // First sight of an already-pending server: baseline only, no toast.
    useStackStore.getState().setGatewayStatus(status('needs_auth') as never);
    expect(toastSpy).not.toHaveBeenCalled();

    // Authorized, then pending again: exactly one toast for the transition.
    useStackStore.getState().setGatewayStatus(status('authorized') as never);
    useStackStore.getState().setGatewayStatus(status('needs_auth') as never);
    expect(toastSpy).toHaveBeenCalledTimes(1);
    expect(toastSpy).toHaveBeenCalledWith(
      'warning',
      'notion requires authorization',
      expect.objectContaining({ action: expect.anything() }),
    );

    // Staying pending across polls must not re-toast.
    useStackStore.getState().setGatewayStatus(status('needs_auth') as never);
    expect(toastSpy).toHaveBeenCalledTimes(1);
  });
});

describe('GatewaySidebar pending authorization row', () => {
  it('shows the pending count and selects the first pending server', async () => {
    const { MemoryRouter } = await import('react-router-dom');
    const { GatewaySidebar } = await import('../components/gateway/GatewaySidebar');
    useStackStore.setState({
      mcpServers: [
        makeServerStatus({ name: 'github', authStatus: 'authorized' }),
        makeServerStatus({ name: 'notion', authStatus: 'needs_auth' }),
      ],
    });

    render(
      <MemoryRouter>
        <GatewaySidebar onClose={() => {}} />
      </MemoryRouter>,
    );

    const row = screen.getByRole('button', { name: /Authorization: 1 pending/ });
    expect(row).toHaveTextContent('1 pending');

    fireEvent.click(row);
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-notion');
  });

  it('hides the row when nothing is pending', async () => {
    const { MemoryRouter } = await import('react-router-dom');
    const { GatewaySidebar } = await import('../components/gateway/GatewaySidebar');
    useStackStore.setState({
      mcpServers: [makeServerStatus({ name: 'github', authStatus: 'authorized' })],
    });

    render(
      <MemoryRouter>
        <GatewaySidebar onClose={() => {}} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole('button', { name: /Authorization:/ })).not.toBeInTheDocument();
  });
});
