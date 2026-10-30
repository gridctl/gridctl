import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useSplitPane } from '../useSplitPane';

function Harness({ onCommit }: { onCommit: (ratio: number) => void }) {
  const { ratio, containerRef, handlePointerDown, isDragging } = useSplitPane(0.25, 0.25, 0.75, onCommit);
  return (
    <div ref={containerRef} data-testid="container">
      <div data-testid="handle" onPointerDown={handlePointerDown} />
      <span data-testid="ratio">{ratio}</span>
      <span data-testid="dragging">{isDragging ? 'yes' : 'no'}</span>
    </div>
  );
}

beforeEach(() => {
  cleanup();
  Element.prototype.setPointerCapture = vi.fn();
  Element.prototype.releasePointerCapture = vi.fn();
  Element.prototype.hasPointerCapture = vi.fn(() => true);
});

describe('useSplitPane', () => {
  it('resizes from a touch pointer and commits once on cancel', () => {
    const onCommit = vi.fn();
    render(<Harness onCommit={onCommit} />);
    const handle = screen.getByTestId('handle');
    const container = screen.getByTestId('container');
    container.getBoundingClientRect = () => ({
      left: 0,
      top: 0,
      right: 400,
      bottom: 100,
      width: 400,
      height: 100,
      x: 0,
      y: 0,
      toJSON() {
        return {};
      },
    });

    fireEvent.pointerDown(handle, { pointerId: 1, pointerType: 'touch', clientX: 100, button: 0 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 200 });
    expect(screen.getByTestId('ratio')).toHaveTextContent('0.5');

    fireEvent.pointerCancel(handle, { pointerId: 1 });
    expect(screen.getByTestId('dragging')).toHaveTextContent('no');
    expect(onCommit).toHaveBeenCalledTimes(1);
    expect(onCommit).toHaveBeenCalledWith(0.5);
    expect(document.body.style.cursor).toBe('');
    expect(document.body.style.userSelect).toBe('');
  });

  it('ignores a non-primary button', () => {
    const onCommit = vi.fn();
    render(<Harness onCommit={onCommit} />);
    fireEvent.pointerDown(screen.getByTestId('handle'), { pointerId: 2, button: 2, clientX: 100 });
    expect(screen.getByTestId('dragging')).toHaveTextContent('no');
    expect(onCommit).not.toHaveBeenCalled();
  });
});
