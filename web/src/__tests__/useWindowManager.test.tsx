import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, act, screen, fireEvent, cleanup } from '@testing-library/react';
import '@testing-library/jest-dom';
import { useEffect } from 'react';
import { useUIStore } from '../stores/useUIStore';
import { useWindowManager } from '../hooks/useWindowManager';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { ToastContainer } from '../components/ui/Toast';

// A test harness that calls useWindowManager from inside a component which is
// conditionally rendered based on useUIStore.sidebarOpen. This mirrors how
// Sidebar / GatewaySidebar are mounted under StackWorkspace: when
// openDetachedWindow('registry') flips sidebarOpen to false, this component
// unmounts in the same commit. Before the fix, the unmount cleanup closed the
// just-opened window. After the fix, the module-scope windowRefs survives the
// unmount and the window stays open.
function PopoutHarness({ onReady }: { onReady: (open: () => void, focus: (type?: 'registry' | 'logs') => void) => void }) {
  const { openDetachedWindow, focusDetachedWindow } = useWindowManager();
  useEffect(() => {
    onReady(
      () => openDetachedWindow('registry'),
      (type = 'registry') => focusDetachedWindow(type),
    );
  }, [onReady, openDetachedWindow, focusDetachedWindow]);
  return null;
}

function ConditionalHost({ onReady }: { onReady: (open: () => void, focus: (type?: 'registry' | 'logs') => void) => void }) {
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  return sidebarOpen ? <PopoutHarness onReady={onReady} /> : null;
}

const STARTING_STATE = {
  sidebarOpen: true,
  registryDetached: false,
} as const;

interface StubWindow {
  closed: boolean;
  focus: ReturnType<typeof vi.fn>;
  close: ReturnType<typeof vi.fn>;
  addEventListener: ReturnType<typeof vi.fn>;
  document: { title: string };
  location: { href: string };
}

describe('useWindowManager.openDetachedWindow', () => {
  let openSpy: ReturnType<typeof vi.spyOn>;
  let stubWindow: StubWindow;

  beforeEach(() => {
    useUIStore.setState({ ...STARTING_STATE });
    stubWindow = {
      closed: false,
      focus: vi.fn(),
      close: vi.fn(),
      addEventListener: vi.fn(),
      document: { title: '' },
      location: { href: 'http://localhost/library-window' },
    };
    openSpy = vi.spyOn(window, 'open').mockReturnValue(stubWindow as unknown as Window);
    vi.useFakeTimers();
  });

  afterEach(() => {
    // Mark the stub as closed so the next test's existing-window check in
    // openDetachedWindow (which reads module-scope windowRefs) sees it as
    // gone and opens fresh.
    stubWindow.closed = true;
    vi.useRealTimers();
    openSpy.mockRestore();
  });

  it('does not close the just-opened window when the caller unmounts', () => {
    let openRegistry: (() => void) | null = null;
    render(<ConditionalHost onReady={(open) => { openRegistry = open; }} />);

    expect(openRegistry).not.toBeNull();

    act(() => {
      openRegistry!();
    });

    expect(openSpy).toHaveBeenCalledTimes(1);
    // The 'registry' window key now points at /library-window after the
    // workspace promotion; the key itself stays for back-compat.
    expect(openSpy).toHaveBeenCalledWith('/library-window', 'gridctl-registry');

    // Eager state flip should have run, which unmounts the harness via
    // ConditionalHost's `sidebarOpen` gate.
    expect(useUIStore.getState().registryDetached).toBe(true);
    expect(useUIStore.getState().sidebarOpen).toBe(false);

    // The regression we're guarding against: the unmount fires a cleanup
    // that closes the window we just opened.
    expect(stubWindow.close).not.toHaveBeenCalled();
  });

  it('rolls back the eager state flip when window.open returns null (popup blocked)', () => {
    openSpy.mockReturnValueOnce(null);

    let openRegistry: (() => void) | null = null;
    render(<ConditionalHost onReady={(open) => { openRegistry = open; }} />);

    act(() => {
      openRegistry!();
    });

    expect(openSpy).toHaveBeenCalledTimes(1);
    expect(useUIStore.getState().registryDetached).toBe(false);
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('shows a warning toast whose action opens the same route when the popup is blocked', () => {
    openSpy.mockReturnValueOnce(null);
    let openRegistry: (() => void) | null = null;
    render(
      <>
        <ToastContainer />
        <ConditionalHost onReady={(open) => { openRegistry = open; }} />
      </>,
    );

    act(() => {
      openRegistry!();
    });

    expect(useUIStore.getState().registryDetached).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Open' }));
    expect(openSpy).toHaveBeenLastCalledWith('/library-window', '_blank');
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
  });

  it('focuses the existing window without opening another', () => {
    let openRegistry: (() => void) | null = null;
    let focus: ((type?: 'registry' | 'logs') => void) | null = null;
    render(<ConditionalHost onReady={(open, focusWindow) => { openRegistry = open; focus = focusWindow; }} />);

    act(() => {
      openRegistry!();
    });
    stubWindow.focus.mockClear();
    openSpy.mockClear();

    act(() => {
      focus!();
    });

    expect(stubWindow.focus).toHaveBeenCalledTimes(1);
    expect(openSpy).not.toHaveBeenCalled();
  });

  it('recovers a named window, focuses it, and re-arms the close poll', () => {
    stubWindow.closed = true;
    useUIStore.setState({ logsDetached: true });
    const recovered = {
      closed: false,
      focus: vi.fn(),
      close: vi.fn(),
      addEventListener: vi.fn(),
      document: { title: '' },
      location: { href: 'http://localhost/logs-window' },
    };
    openSpy.mockReturnValueOnce(recovered as unknown as Window);

    let focus: ((type?: 'registry' | 'logs') => void) | null = null;
    render(<ConditionalHost onReady={(_open, focusWindow) => { focus = focusWindow; }} />);

    act(() => {
      focus!('logs');
    });

    expect(openSpy).toHaveBeenCalledWith('', 'gridctl-logs');
    expect(recovered.focus).toHaveBeenCalledTimes(1);
    expect(recovered.close).not.toHaveBeenCalled();
    expect(useUIStore.getState().logsDetached).toBe(true);

    act(() => {
      recovered.closed = true;
      vi.advanceTimersByTime(500);
    });
    expect(useUIStore.getState().logsDetached).toBe(false);
  });

  it('closes a blank named window and clears the detached flag', () => {
    stubWindow.closed = true;
    useUIStore.setState({ logsDetached: true });
    const blank = {
      closed: false,
      focus: vi.fn(),
      close: vi.fn(),
      addEventListener: vi.fn(),
      document: { title: '' },
      location: { href: 'about:blank' },
    };
    openSpy.mockReturnValueOnce(blank as unknown as Window);

    let focus: ((type?: 'registry' | 'logs') => void) | null = null;
    render(<ConditionalHost onReady={(_open, focusWindow) => { focus = focusWindow; }} />);

    act(() => {
      focus!('logs');
    });

    expect(openSpy).toHaveBeenCalledWith('', 'gridctl-logs');
    expect(blank.close).toHaveBeenCalledTimes(1);
    expect(useUIStore.getState().logsDetached).toBe(false);
    blank.closed = true;
  });
});

function DetachedLogs() {
  useDetachedWindowSync('logs');
  return null;
}

function DetachedRegistry() {
  useDetachedWindowSync('registry');
  return null;
}

function MainWindow() {
  useWindowManager();
  return null;
}

describe('useWindowManager WHO_IS_OPEN', () => {
  afterEach(() => {
    cleanup();
    useUIStore.setState({ logsDetached: false });
  });

  it('restores the detached flag from a popout reply without calling window.open', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue(null);
    useUIStore.setState({ logsDetached: false });
    const detached = render(<DetachedLogs />);
    await act(async () => {
      await Promise.resolve();
    });
    useUIStore.setState({ logsDetached: false });

    render(<MainWindow />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(useUIStore.getState().logsDetached).toBe(true);
    expect(openSpy).not.toHaveBeenCalled();
    detached.unmount();
    openSpy.mockRestore();
  });

  it('does not close the inspector when a ping reply is for an already-detached popout', async () => {
    useUIStore.setState({ registryDetached: true, sidebarOpen: true });
    const detached = render(<DetachedRegistry />);
    await act(async () => {
      await Promise.resolve();
    });
    useUIStore.setState({ registryDetached: true, sidebarOpen: true });

    render(<MainWindow />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(useUIStore.getState().registryDetached).toBe(true);
    detached.unmount();
  });

  it('closes the in-shell panel only when a registry popout is first reported', async () => {
    useUIStore.setState({ registryDetached: false, sidebarOpen: true });
    const detached = render(<DetachedRegistry />);
    await act(async () => {
      await Promise.resolve();
    });
    useUIStore.setState({ registryDetached: false, sidebarOpen: true });

    render(<MainWindow />);
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(useUIStore.getState().registryDetached).toBe(true);
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    detached.unmount();
  });
});
