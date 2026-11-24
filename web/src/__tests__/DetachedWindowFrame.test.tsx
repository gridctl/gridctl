import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { DetachedWindowFrame } from '../components/detached/DetachedWindowFrame';
import { useAuthStore } from '../stores/useAuthStore';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
  useBroadcastChannel: () => ({ postMessage: vi.fn() }),
}));

const HEADER = {
  icon: <span>icon</span>,
  start: <span>Start</span>,
};

const region = () => screen.getByRole('region', { name: 'Notifications' });

function dismissToasts() {
  const live = screen.queryByRole('region', { name: 'Notifications' });
  if (!live) return;
  within(live).queryAllByRole('button', { name: 'Dismiss' }).forEach((b) => fireEvent.click(b));
}

beforeEach(() => {
  useAuthStore.setState({ authRequired: false });
  document.title = '';
});

afterEach(() => {
  dismissToasts();
  vi.clearAllMocks();
});

describe('DetachedWindowFrame', () => {
  it('registers the window and sets the document title for its type', () => {
    render(
      <DetachedWindowFrame windowType="logs" header={HEADER}>
        <main />
      </DetachedWindowFrame>,
    );
    expect(document.title).toBe('Gridctl - Logs');
    expect(useDetachedWindowSync).toHaveBeenCalledWith('logs');
  });

  it('appends a detail to the title, follows changes, and drops it when cleared', () => {
    const { rerender } = render(
      <DetachedWindowFrame windowType="sidebar" titleDetail="db" header={HEADER}>
        <main />
      </DetachedWindowFrame>,
    );
    expect(document.title).toBe('Gridctl - Details: db');
    rerender(
      <DetachedWindowFrame windowType="sidebar" titleDetail="cache" header={HEADER}>
        <main />
      </DetachedWindowFrame>,
    );
    expect(document.title).toBe('Gridctl - Details: cache');
    rerender(
      <DetachedWindowFrame windowType="sidebar" titleDetail={null} header={HEADER}>
        <main />
      </DetachedWindowFrame>,
    );
    expect(document.title).toBe('Gridctl - Details');
  });

  it('renders one notifications region, a header landmark, and a footer that reads Detached Window', () => {
    render(
      <DetachedWindowFrame windowType="traces" header={HEADER} footerStart="Traces">
        <main />
      </DetachedWindowFrame>,
    );
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
    expect(screen.getByRole('banner')).toHaveTextContent('Start');
    const footer = screen.getByRole('contentinfo');
    expect(footer).toHaveTextContent('Detached Window');
    expect(footer).toHaveTextContent('Traces');
    expect(footer.querySelector('.animate-pulse')).toHaveClass('motion-reduce:animate-none');
  });

  it('renders no header or footer landmark when both are turned off', () => {
    render(
      <DetachedWindowFrame windowType="editor" footer={false}>
        <div>editor</div>
      </DetachedWindowFrame>,
    );
    expect(screen.queryByRole('banner')).not.toBeInTheDocument();
    expect(screen.queryByRole('contentinfo')).not.toBeInTheDocument();
    expect(region()).toBeInTheDocument();
    expect(document.title).toBe('Gridctl - Editor');
  });

  it('shows the disconnected banner with message and a working Retry, keeping content mounted', () => {
    const onRetry = vi.fn();
    render(
      <DetachedWindowFrame
        windowType="metrics"
        header={HEADER}
        connection={{ lost: true, message: 'Status failed: down.', onRetry }}
      >
        <main>content</main>
      </DetachedWindowFrame>,
    );
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Status failed: down.');
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('main')).toHaveTextContent('content');
  });

  it('uses the default message and omits Retry without a handler', () => {
    render(
      <DetachedWindowFrame windowType="metrics" header={HEADER} connection={{ lost: true }}>
        <main />
      </DetachedWindowFrame>,
    );
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Gateway unavailable. Showing the last loaded data.');
    expect(within(alert).queryByRole('button')).not.toBeInTheDocument();
  });

  it('renders no banner when the connection is not lost, when it is omitted, or while auth is required', () => {
    const { rerender } = render(
      <DetachedWindowFrame windowType="logs" header={HEADER} connection={{ lost: false, message: 'x' }}>
        <main />
      </DetachedWindowFrame>,
    );
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    rerender(
      <DetachedWindowFrame windowType="logs" header={HEADER}>
        <main />
      </DetachedWindowFrame>,
    );
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    act(() => useAuthStore.setState({ authRequired: true }));
    rerender(
      <DetachedWindowFrame windowType="logs" header={HEADER} connection={{ lost: true, message: 'x' }}>
        <main />
      </DetachedWindowFrame>,
    );
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  describe('fullscreen', () => {
    const root = document.documentElement as HTMLElement & { requestFullscreen?: unknown };
    const doc = document as Document & { exitFullscreen?: unknown };
    let originalRequest: unknown;
    let originalExit: unknown;

    beforeEach(() => {
      originalRequest = root.requestFullscreen;
      originalExit = doc.exitFullscreen;
    });

    afterEach(() => {
      Object.defineProperty(root, 'requestFullscreen', { value: originalRequest, configurable: true, writable: true });
      Object.defineProperty(doc, 'exitFullscreen', { value: originalExit, configurable: true, writable: true });
      Object.defineProperty(document, 'fullscreenElement', { value: null, configurable: true });
    });

    it('renders no toggle unless enabled', () => {
      render(
        <DetachedWindowFrame windowType="sidebar" header={HEADER}>
          <main />
        </DetachedWindowFrame>,
      );
      expect(screen.queryByRole('button', { name: 'Fullscreen' })).not.toBeInTheDocument();
    });

    it('requests fullscreen and tracks state from fullscreenchange', async () => {
      const request = vi.fn().mockResolvedValue(undefined);
      Object.defineProperty(root, 'requestFullscreen', { value: request, configurable: true, writable: true });
      render(
        <DetachedWindowFrame windowType="logs" header={HEADER} fullscreen>
          <main />
        </DetachedWindowFrame>,
      );
      fireEvent.click(screen.getByRole('button', { name: 'Fullscreen' }));
      await waitFor(() => expect(request).toHaveBeenCalledTimes(1));
      // No optimistic flip: the label changes only on the event.
      expect(screen.getByRole('button', { name: 'Fullscreen' })).toBeInTheDocument();

      Object.defineProperty(document, 'fullscreenElement', { value: document.documentElement, configurable: true });
      act(() => {
        document.dispatchEvent(new Event('fullscreenchange'));
      });
      expect(screen.getByRole('button', { name: 'Exit Fullscreen' })).toBeInTheDocument();
    });

    it('reports a rejected request with a warning toast and no unhandled rejection', async () => {
      const unhandled = vi.fn();
      process.on('unhandledRejection', unhandled);
      try {
        const request = vi.fn().mockRejectedValue(new TypeError('denied'));
        Object.defineProperty(root, 'requestFullscreen', { value: request, configurable: true, writable: true });
        render(
          <DetachedWindowFrame windowType="metrics" header={HEADER} fullscreen>
            <main />
          </DetachedWindowFrame>,
        );
        fireEvent.click(screen.getByRole('button', { name: 'Fullscreen' }));
        await waitFor(() => {
          expect(region()).toHaveTextContent('Fullscreen is not available in this window');
        });
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(unhandled).not.toHaveBeenCalled();
        expect(screen.getByRole('button', { name: 'Fullscreen' })).toBeInTheDocument();
      } finally {
        process.off('unhandledRejection', unhandled);
      }
    });

    it('reports a missing fullscreen API the same way', async () => {
      Object.defineProperty(root, 'requestFullscreen', { value: undefined, configurable: true, writable: true });
      render(
        <DetachedWindowFrame windowType="traces" header={HEADER} fullscreen>
          <main />
        </DetachedWindowFrame>,
      );
      fireEvent.click(screen.getByRole('button', { name: 'Fullscreen' }));
      await waitFor(() => {
        expect(region()).toHaveTextContent('Fullscreen is not available in this window');
      });
    });

    it('reports a rejected exit with a warning toast', async () => {
      const exit = vi.fn().mockRejectedValue(new TypeError('denied'));
      Object.defineProperty(doc, 'exitFullscreen', { value: exit, configurable: true, writable: true });
      Object.defineProperty(document, 'fullscreenElement', { value: document.documentElement, configurable: true });
      render(
        <DetachedWindowFrame windowType="logs" header={HEADER} fullscreen>
          <main />
        </DetachedWindowFrame>,
      );
      fireEvent.click(screen.getByRole('button', { name: 'Exit Fullscreen' }));
      await waitFor(() => expect(exit).toHaveBeenCalledTimes(1));
      await waitFor(() => {
        expect(region()).toHaveTextContent('Fullscreen is not available in this window');
      });
    });
  });
});
