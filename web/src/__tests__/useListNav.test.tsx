import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { useListNav } from '../hooks/useListNav';

beforeEach(() => {
  cleanup();
});

interface HarnessProps {
  itemCount?: number;
  onEnter?: () => void;
  onEdit?: () => void;
  onToggle?: () => void;
  enabled?: boolean;
}

function Harness({
  itemCount = 3,
  onEnter,
  onEdit,
  onToggle,
  enabled = true,
}: HarnessProps) {
  const [index, setIndex] = useState(0);
  useListNav({
    itemCount,
    selectedIndex: index,
    setSelectedIndex: setIndex,
    onEnter,
    onEdit,
    onToggle,
    enabled,
  });
  return <div data-testid="index">{index}</div>;
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
});
