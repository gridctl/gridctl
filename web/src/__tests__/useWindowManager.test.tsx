import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { useEffect } from 'react';
import { useUIStore } from '../stores/useUIStore';
import { useWindowManager } from '../hooks/useWindowManager';

// A test harness that calls useWindowManager from inside a component which is
// conditionally rendered based on useUIStore.sidebarOpen. This mirrors how
// Sidebar / GatewaySidebar are mounted under StackWorkspace: when
// openDetachedWindow('registry') flips sidebarOpen to false, this component
// unmounts in the same commit. Before the fix, the unmount cleanup closed the
// just-opened window. After the fix, the module-scope windowRefs survives the
// unmount and the window stays open.
function PopoutHarness({ onReady }: { onReady: (open: () => void) => void }) {
  const { openDetachedWindow } = useWindowManager();
  useEffect(() => {
    onReady(() => openDetachedWindow('registry'));
  }, [onReady, openDetachedWindow]);
  return null;
}

function ConditionalHost({ onReady }: { onReady: (open: () => void) => void }) {
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
});
