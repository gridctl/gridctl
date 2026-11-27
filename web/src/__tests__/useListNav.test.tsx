import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { listNavProps, listNavRowProps, useListNav } from '../hooks/useListNav';

beforeEach(() => {
  cleanup();
});

interface HarnessProps {
  itemCount?: number;
  initialIndex?: number;
  onEnter?: () => void;
  onEdit?: () => void;
  onToggle?: () => void;
  onEscape?: () => void;
  enabled?: boolean;
}

function Harness({
  itemCount = 3,
  initialIndex = 0,
  onEnter,
  onEdit,
  onToggle,
  onEscape,
  enabled = true,
}: HarnessProps) {
  const [index, setIndex] = useState(initialIndex);
  useListNav({
    itemCount,
    selectedIndex: index,
    setSelectedIndex: setIndex,
    onEnter,
    onEdit,
    onToggle,
    onEscape,
    enabled,
  });
  return (
    <>
      <div data-testid="index">{index}</div>
      <button data-testid="outside">outside</button>
      <div {...listNavProps}>
        <div tabIndex={0} data-testid="row">
          row
          <button data-testid="row-btn">act</button>
        </div>
      </div>
    </>
  );
}

// Rows are focusable marked rows exposing aria-selected, like VariableRow.
function RowsHarness({ itemCount = 3 }: { itemCount?: number }) {
  const [index, setIndex] = useState(0);
  useListNav({ itemCount, selectedIndex: index, setSelectedIndex: setIndex });
  return (
    <>
      <div data-testid="index">{index}</div>
      <button data-testid="outside">outside</button>
      <div role="listbox" {...listNavProps}>
        {Array.from({ length: itemCount }, (_, i) => (
          <div
            key={i}
            role="option"
            tabIndex={0}
            aria-selected={i === index}
            data-testid={`row-${i}`}
            {...listNavRowProps}
          >
            row {i}
            <button data-testid={`row-btn-${i}`}>act</button>
          </div>
        ))}
      </div>
    </>
  );
}

// Selection is held by key, as in the workspaces, so rows added ahead of it
// shift the index without changing what is selected.
function KeyedRowsHarness({ items }: { items: string[] }) {
  const [selected, setSelected] = useState(items[0]);
  useListNav({
    itemCount: items.length,
    selectedIndex: items.indexOf(selected),
    setSelectedIndex: (i) => setSelected(items[i]),
  });
  return (
    <div role="listbox" {...listNavProps}>
      {items.map((item) => (
        <div
          key={item}
          role="option"
          tabIndex={0}
          aria-selected={item === selected}
          data-testid={`row-${item}`}
          {...listNavRowProps}
        >
          {item}
        </div>
      ))}
    </div>
  );
}

function getIndex(): number {
  return Number(screen.getByTestId('index').textContent);
}

describe('useListNav', () => {
  it('moves selection down with ArrowDown', () => {
    render(<Harness itemCount={3} />);
    fireEvent.keyDown(document, { key: 'ArrowDown' });
    expect(getIndex()).toBe(1);
    fireEvent.keyDown(document, { key: 'ArrowDown' });
    expect(getIndex()).toBe(2);
  });

  it('wraps at the end with ArrowDown', () => {
    render(<Harness itemCount={3} />);
    fireEvent.keyDown(document, { key: 'ArrowDown' });
    fireEvent.keyDown(document, { key: 'ArrowDown' });
    fireEvent.keyDown(document, { key: 'ArrowDown' });
    expect(getIndex()).toBe(0);
  });

  it('wraps at the top with ArrowUp', () => {
    render(<Harness itemCount={3} />);
    fireEvent.keyDown(document, { key: 'ArrowUp' });
    expect(getIndex()).toBe(2);
  });

  it('jumps to first with Home', () => {
    render(<Harness itemCount={3} />);
    fireEvent.keyDown(document, { key: 'ArrowDown' });
    fireEvent.keyDown(document, { key: 'Home' });
    expect(getIndex()).toBe(0);
  });

  it('jumps to last with End', () => {
    render(<Harness itemCount={3} />);
    fireEvent.keyDown(document, { key: 'End' });
    expect(getIndex()).toBe(2);
  });

  it('calls onEnter on Enter', () => {
    const onEnter = vi.fn();
    render(<Harness onEnter={onEnter} />);
    fireEvent.keyDown(document, { key: 'Enter' });
    expect(onEnter).toHaveBeenCalledTimes(1);
  });

  it('calls onEdit on "e"', () => {
    const onEdit = vi.fn();
    render(<Harness onEdit={onEdit} />);
    fireEvent.keyDown(document, { key: 'e' });
    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it('calls onToggle on "d"', () => {
    const onToggle = vi.fn();
    render(<Harness onToggle={onToggle} />);
    fireEvent.keyDown(document, { key: 'd' });
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it('skips when focus is in an input', () => {
    const onEdit = vi.fn();
    render(
      <>
        <Harness onEdit={onEdit} />
        <input data-testid="input" />
      </>,
    );
    const input = screen.getByTestId('input');
    input.focus();
    fireEvent.keyDown(input, { key: 'e' });
    expect(onEdit).not.toHaveBeenCalled();
  });

  it('skips when focus is inside a dialog', () => {
    const onEdit = vi.fn();
    render(
      <>
        <Harness onEdit={onEdit} />
        <div role="dialog" data-testid="dialog">
          <button data-testid="dialog-btn">ok</button>
        </div>
      </>,
    );
    const btn = screen.getByTestId('dialog-btn');
    btn.focus();
    fireEvent.keyDown(btn, { key: 'e' });
    expect(onEdit).not.toHaveBeenCalled();
  });

  it('skips when enabled is false', () => {
    const onEdit = vi.fn();
    render(<Harness onEdit={onEdit} enabled={false} />);
    fireEvent.keyDown(document, { key: 'e' });
    expect(onEdit).not.toHaveBeenCalled();
  });

  it('skips when modifier keys are pressed', () => {
    render(<Harness itemCount={3} />);
    fireEvent.keyDown(document, { key: 'ArrowDown', metaKey: true });
    expect(getIndex()).toBe(0);
  });

  describe('target scoping', () => {
    it('leaves Enter on a focused control outside the list alone', () => {
      const onEnter = vi.fn();
      render(<Harness onEnter={onEnter} />);
      const outside = screen.getByTestId('outside');
      outside.focus();
      expect(fireEvent.keyDown(outside, { key: 'Enter' })).toBe(true);
      expect(onEnter).not.toHaveBeenCalled();
    });

    it('runs onEnter from the document, the body, and a non-control row', () => {
      const onEnter = vi.fn();
      render(<Harness onEnter={onEnter} />);
      expect(fireEvent.keyDown(document, { key: 'Enter' })).toBe(false);
      expect(onEnter).toHaveBeenCalledTimes(1);
      expect(fireEvent.keyDown(document.body, { key: 'Enter' })).toBe(false);
      expect(onEnter).toHaveBeenCalledTimes(2);
      const row = screen.getByTestId('row');
      row.focus();
      expect(fireEvent.keyDown(row, { key: 'Enter' })).toBe(false);
      expect(onEnter).toHaveBeenCalledTimes(3);
    });

    it('leaves Enter on a control inside the list alone', () => {
      const onEnter = vi.fn();
      render(<Harness onEnter={onEnter} />);
      const btn = screen.getByTestId('row-btn');
      btn.focus();
      expect(fireEvent.keyDown(btn, { key: 'Enter' })).toBe(true);
      expect(onEnter).not.toHaveBeenCalled();
    });

    it('leaves Enter on a focused link, summary, or role=button alone', () => {
      const onEnter = vi.fn();
      render(
        <>
          <Harness onEnter={onEnter} />
          <a href="#x" data-testid="link">link</a>
          <details>
            <summary data-testid="summary">more</summary>
          </details>
          <div role="button" tabIndex={0} data-testid="role-btn">
            <span data-testid="role-btn-child">inner</span>
          </div>
        </>,
      );
      for (const id of ['link', 'summary', 'role-btn', 'role-btn-child']) {
        const el = screen.getByTestId(id);
        el.focus();
        expect(fireEvent.keyDown(el, { key: 'Enter' })).toBe(true);
      }
      expect(onEnter).not.toHaveBeenCalled();
    });

    it('still moves the selection with j and k from a focused button', () => {
      render(<Harness itemCount={3} initialIndex={0} />);
      const outside = screen.getByTestId('outside');
      outside.focus();
      expect(fireEvent.keyDown(outside, { key: 'j' })).toBe(false);
      expect(getIndex()).toBe(1);
      expect(fireEvent.keyDown(outside, { key: 'k' })).toBe(false);
      expect(getIndex()).toBe(0);
    });

    it('still closes with Escape from a focused button', () => {
      const onEscape = vi.fn();
      render(<Harness onEscape={onEscape} />);
      const outside = screen.getByTestId('outside');
      outside.focus();
      fireEvent.keyDown(outside, { key: 'Escape' });
      expect(onEscape).toHaveBeenCalledTimes(1);
    });

    it('does not cancel Enter when there is no onEnter', () => {
      render(<Harness />);
      expect(fireEvent.keyDown(document, { key: 'Enter' })).toBe(true);
    });

    it('ignores arrows, Home, and End on a control outside the list', () => {
      render(<Harness initialIndex={1} />);
      const outside = screen.getByTestId('outside');
      outside.focus();
      for (const key of ['ArrowDown', 'ArrowUp', 'Home', 'End']) {
        expect(fireEvent.keyDown(outside, { key })).toBe(true);
        expect(getIndex()).toBe(1);
      }
    });

    it('moves the selection with arrows from inside the list', () => {
      render(<Harness />);
      const row = screen.getByTestId('row');
      row.focus();
      expect(fireEvent.keyDown(row, { key: 'ArrowDown' })).toBe(false);
      expect(getIndex()).toBe(1);
      expect(fireEvent.keyDown(row, { key: 'End' })).toBe(false);
      expect(getIndex()).toBe(2);
    });

    it('moves the selection with j and k from a control outside the list', () => {
      render(<Harness />);
      const outside = screen.getByTestId('outside');
      outside.focus();
      fireEvent.keyDown(outside, { key: 'j' });
      expect(getIndex()).toBe(1);
      fireEvent.keyDown(outside, { key: 'k' });
      expect(getIndex()).toBe(0);
    });

    it('runs e and d only on the page or inside the list', () => {
      const onEdit = vi.fn();
      const onToggle = vi.fn();
      render(<Harness onEdit={onEdit} onToggle={onToggle} />);
      const outside = screen.getByTestId('outside');
      outside.focus();
      expect(fireEvent.keyDown(outside, { key: 'e' })).toBe(true);
      expect(fireEvent.keyDown(outside, { key: 'd' })).toBe(true);
      expect(onEdit).not.toHaveBeenCalled();
      expect(onToggle).not.toHaveBeenCalled();
      const row = screen.getByTestId('row');
      row.focus();
      fireEvent.keyDown(row, { key: 'e' });
      fireEvent.keyDown(row, { key: 'd' });
      expect(onEdit).toHaveBeenCalledTimes(1);
      expect(onToggle).toHaveBeenCalledTimes(1);
    });

    it('still runs Escape from a control outside the list', () => {
      const onEscape = vi.fn();
      render(<Harness onEscape={onEscape} />);
      const outside = screen.getByTestId('outside');
      outside.focus();
      fireEvent.keyDown(outside, { key: 'Escape' });
      expect(onEscape).toHaveBeenCalledTimes(1);
    });

    it('defers to a component handler that already cancelled the key', () => {
      render(
        <>
          <Harness />
          <div onKeyDown={(e) => e.preventDefault()}>
            <span tabIndex={0} data-testid="child">child</span>
          </div>
        </>,
      );
      const child = screen.getByTestId('child');
      child.focus();
      fireEvent.keyDown(child, { key: 'j' });
      expect(getIndex()).toBe(0);
    });
  });

  describe('negative selectedIndex', () => {
    it('selects the last row on ArrowUp', () => {
      render(<Harness initialIndex={-1} />);
      fireEvent.keyDown(document, { key: 'ArrowUp' });
      expect(getIndex()).toBe(2);
    });

    it('selects the first row on ArrowDown', () => {
      render(<Harness initialIndex={-1} />);
      fireEvent.keyDown(document, { key: 'ArrowDown' });
      expect(getIndex()).toBe(0);
    });

    it('applies the same rule to j and k', () => {
      const { unmount } = render(<Harness initialIndex={-1} />);
      fireEvent.keyDown(document, { key: 'k' });
      expect(getIndex()).toBe(2);
      unmount();
      render(<Harness initialIndex={-1} />);
      fireEvent.keyDown(document, { key: 'j' });
      expect(getIndex()).toBe(0);
    });
  });

  describe('focus follows selection', () => {
    it('moves focus to the newly selected row when a row has focus', () => {
      render(<RowsHarness />);
      const row0 = screen.getByTestId('row-0');
      row0.focus();
      fireEvent.keyDown(row0, { key: 'ArrowDown' });
      expect(getIndex()).toBe(1);
      expect(screen.getByTestId('row-1')).toHaveFocus();
      fireEvent.keyDown(document.activeElement!, { key: 'End' });
      expect(screen.getByTestId('row-2')).toHaveFocus();
    });

    it('keeps focus on a control outside the list', () => {
      render(<RowsHarness />);
      const outside = screen.getByTestId('outside');
      outside.focus();
      fireEvent.keyDown(outside, { key: 'j' });
      expect(getIndex()).toBe(1);
      expect(outside).toHaveFocus();
    });

    it('keeps focus on a control nested in a row', () => {
      render(<RowsHarness />);
      const btn = screen.getByTestId('row-btn-0');
      btn.focus();
      fireEvent.keyDown(btn, { key: 'j' });
      expect(getIndex()).toBe(1);
      expect(btn).toHaveFocus();
    });

    it('keeps focus on a row when the rows shift under an unchanged selection', () => {
      const { rerender } = render(<KeyedRowsHarness items={['a', 'b', 'c']} />);
      const rowC = screen.getByTestId('row-c');
      rowC.focus();
      rerender(<KeyedRowsHarness items={['z', 'a', 'b', 'c']} />);
      expect(screen.getByTestId('row-a')).toHaveAttribute('aria-selected', 'true');
      expect(rowC).toHaveFocus();
      fireEvent.keyDown(rowC, { key: 'ArrowDown' });
      expect(screen.getByTestId('row-b')).toHaveFocus();
    });

    it('never moves focus into the list from the page', () => {
      render(<RowsHarness />);
      fireEvent.keyDown(document, { key: 'ArrowDown' });
      expect(getIndex()).toBe(1);
      expect(document.body).toHaveFocus();
    });
  });
});

describe('useListNav — modal dialogs', () => {
  it('ignores Escape and j from <body> while a modal dialog is open', () => {
    const onEscape = vi.fn();
    render(<Harness onEscape={onEscape} />);
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    dialog.setAttribute('aria-modal', 'true');
    document.body.appendChild(dialog);
    (document.activeElement as HTMLElement | null)?.blur();

    expect(fireEvent.keyDown(document.body, { key: 'Escape' })).toBe(true);
    expect(fireEvent.keyDown(document.body, { key: 'j' })).toBe(true);
    expect(onEscape).not.toHaveBeenCalled();
    expect(screen.getByTestId('index')).toHaveTextContent('0');

    dialog.remove();
    fireEvent.keyDown(document.body, { key: 'j' });
    expect(screen.getByTestId('index')).toHaveTextContent('1');
  });

  it('keeps working beside a non-modal dialog', () => {
    render(<Harness />);
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.appendChild(dialog);
    fireEvent.keyDown(document.body, { key: 'j' });
    expect(screen.getByTestId('index')).toHaveTextContent('1');
    dialog.remove();
  });
});
