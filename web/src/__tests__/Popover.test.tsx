import { useRef, useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { fireEvent, render, screen } from '@testing-library/react';
import { Popover } from '../components/ui/Popover';

function Harness({
  initiallyOpen = true,
  role = 'dialog' as 'dialog' | 'menu',
  label = 'Menu',
  onClose,
}: {
  initiallyOpen?: boolean;
  role?: 'dialog' | 'menu';
  label?: string;
  onClose?: () => void;
}) {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(initiallyOpen);
  return (
    <>
      <button ref={triggerRef} type="button" onClick={() => setOpen((value) => !value)}>
        Trigger
      </button>
      <div data-testid="outside">outside</div>
      <Popover
        open={open}
        onClose={() => {
          onClose?.();
          setOpen(false);
        }}
        triggerRef={triggerRef}
        role={role}
        aria-label={label}
        className="absolute"
      >
        <button type="button">Inside</button>
      </Popover>
    </>
  );
}

describe('Popover', () => {
  it('renders the role and label, and nothing when closed', () => {
    const { unmount } = render(<Harness role="menu" label="Canvas overlays" />);
    expect(screen.getByRole('menu', { name: 'Canvas overlays' })).toBeInTheDocument();
    unmount();
    render(<Harness initiallyOpen={false} role="menu" label="Canvas overlays" />);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('closes on Escape and refocuses the trigger', () => {
    render(<Harness />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Trigger' })).toHaveFocus();
  });

  it('closes on an outside mousedown without moving focus', () => {
    let focusedDuringClose: Element | null = null;
    render(
      <Harness
        onClose={() => {
          focusedDuringClose = document.activeElement;
        }}
      />,
    );
    const inside = screen.getByRole('button', { name: 'Inside' });
    inside.focus();
    fireEvent.mouseDown(screen.getByTestId('outside'));
    expect(focusedDuringClose).toBe(inside);
    expect(screen.getByRole('button', { name: 'Trigger' })).not.toHaveFocus();
  });

  it('ignores a trigger mousedown so a second click closes without reopening', () => {
    const onClose = vi.fn();
    render(<Harness initiallyOpen={false} onClose={onClose} />);
    const trigger = screen.getByRole('button', { name: 'Trigger' });
    fireEvent.click(trigger);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    onClose.mockClear();
    fireEvent.mouseDown(trigger);
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(trigger);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('refocuses the trigger when open flips false programmatically', () => {
    function Controlled() {
      const triggerRef = useRef<HTMLButtonElement>(null);
      const [open, setOpen] = useState(true);
      return (
        <>
          <button ref={triggerRef} type="button">
            Trigger
          </button>
          <button type="button" onClick={() => setOpen(false)}>
            Hide
          </button>
          <Popover open={open} onClose={() => setOpen(false)} triggerRef={triggerRef} aria-label="Menu">
            <span>Body</span>
          </Popover>
        </>
      );
    }
    render(<Controlled />);
    fireEvent.click(screen.getByRole('button', { name: 'Hide' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Trigger' })).toHaveFocus();
  });
});
