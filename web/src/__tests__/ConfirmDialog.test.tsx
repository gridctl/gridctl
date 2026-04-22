import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { render, screen, cleanup, fireEvent, act } from '@testing-library/react';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';

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

function Harness({
  onConfirm,
  onClose,
  variant = 'danger',
}: {
  onConfirm?: () => void;
  onClose?: () => void;
  variant?: 'default' | 'danger';
}) {
  const [open, setOpen] = useState(true);
  return (
    <ConfirmDialog
      isOpen={open}
      onClose={() => {
        setOpen(false);
        onClose?.();
      }}
      onConfirm={() => {
        onConfirm?.();
        setOpen(false);
      }}
      title="Delete skill"
      message={
        <p>
          Delete <span data-testid="name">my-skill</span>?
        </p>
      }
      confirmLabel={`Delete "my-skill"`}
      variant={variant}
    />
  );
}

describe('ConfirmDialog', () => {
  it('renders with alertdialog semantics and autofocuses Cancel', async () => {
    render(<Harness />);
    await act(async () => {
      await nextFrame();
    });

    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(dialog).toHaveAttribute('aria-labelledby');
    expect(dialog).toHaveAttribute('aria-describedby');
    expect(screen.getByText('Cancel')).toHaveFocus();
  });

  it('closes on Escape', async () => {
    const onClose = vi.fn();
    render(<Harness onClose={onClose} />);
    await act(async () => {
      await nextFrame();
    });

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('calls onConfirm when the destructive button is activated', async () => {
    const onConfirm = vi.fn();
    render(<Harness onConfirm={onConfirm} />);
    await act(async () => {
      await nextFrame();
    });

    fireEvent.click(screen.getByText(/Delete "my-skill"/));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('renders the destructive label with the echoed name', async () => {
    render(<Harness />);
    await act(async () => {
      await nextFrame();
    });
    expect(screen.getByText(/Delete "my-skill"/)).toBeInTheDocument();
  });

  it('returns null when closed', () => {
    function ClosedHarness() {
      return (
        <ConfirmDialog
          isOpen={false}
          onClose={() => {}}
          onConfirm={() => {}}
          title="x"
          message="x"
          confirmLabel="x"
        />
      );
    }
    const { container } = render(<ClosedHarness />);
    expect(container.firstChild).toBeNull();
  });
});
