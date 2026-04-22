import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { useRef, useState } from 'react';
import { render, screen, cleanup, fireEvent, act } from '@testing-library/react';
import { useFocusTrap } from '../hooks/useFocusTrap';

// jsdom doesn't implement layout, so offsetParent is null for everything by
// default. Patch it to return the parent element so our visibility check works.
beforeEach(() => {
  cleanup();
  Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
    configurable: true,
    get() {
      return this.parentNode;
    },
  });
});

function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()));
}

function TestHarness({ initiallyOpen = false }: { initiallyOpen?: boolean }) {
  const [open, setOpen] = useState(initiallyOpen);
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const containerRef = useFocusTrap<HTMLDivElement>({
    active: open,
    initialFocusRef: cancelRef,
  });

  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>
        open
      </button>
      {open && (
        <div ref={containerRef} data-testid="trap">
          <button type="button" ref={cancelRef}>
            cancel
          </button>
          <button type="button">confirm</button>
          <button type="button" onClick={() => setOpen(false)}>
            close
          </button>
        </div>
      )}
    </div>
  );
}

describe('useFocusTrap', () => {
  it('focuses the initial ref when activated', async () => {
    render(<TestHarness />);
    fireEvent.click(screen.getByText('open'));
    await act(async () => {
      await nextFrame();
    });

    expect(screen.getByText('cancel')).toHaveFocus();
  });

  it('wraps Tab from last element back to first', async () => {
    render(<TestHarness />);
    fireEvent.click(screen.getByText('open'));
    await act(async () => {
      await nextFrame();
    });

    const cancel = screen.getByText('cancel');
    const confirm = screen.getByText('confirm');
    const close = screen.getByText('close');

    expect(cancel).toHaveFocus();

    // Move focus to the last element, then Tab: should wrap to first.
    close.focus();
    expect(close).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(cancel).toHaveFocus();

    // Confirm Shift+Tab from first wraps to last.
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(close).toHaveFocus();

    void confirm;
  });

  it('restores focus to the trigger when trap deactivates', async () => {
    render(<TestHarness />);
    const trigger = screen.getByText('open');
    trigger.focus();
    expect(trigger).toHaveFocus();

    fireEvent.click(trigger);
    await act(async () => {
      await nextFrame();
    });
    expect(screen.getByText('cancel')).toHaveFocus();

    fireEvent.click(screen.getByText('close'));
    expect(trigger).toHaveFocus();
  });

  it('does not trap when inactive', () => {
    function Inactive() {
      const ref = useFocusTrap<HTMLDivElement>({ active: false });
      return (
        <div ref={ref}>
          <button type="button">outside-ok</button>
        </div>
      );
    }
    render(
      <div>
        <button type="button">sibling</button>
        <Inactive />
      </div>,
    );
    const sibling = screen.getByText('sibling');
    sibling.focus();
    expect(sibling).toHaveFocus();
    // Pressing Tab should not be intercepted; we just verify no error is thrown
    // and focus does not move away from sibling to the trap container.
    fireEvent.keyDown(document, { key: 'Tab' });
    // jsdom doesn't actually move focus on keydown, but we can at least verify
    // the handler isn't active (focus stays where JS left it).
    expect(document.activeElement).toBe(sibling);
  });
});
