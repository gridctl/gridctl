import { afterEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { useRef } from 'react';
import { cleanup, render, screen } from '@testing-library/react';
import { useContainerWidth } from '../hooks/useContainerWidth';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function Probe() {
  const ref = useRef<HTMLDivElement>(null);
  const { width, layoutSize } = useContainerWidth(ref);
  return <div ref={ref}>{`${width} ${layoutSize}`}</div>;
}

describe('useContainerWidth', () => {
  it('measures synchronously on the first commit', () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 750, height: 0, x: 0, y: 0, top: 0, left: 0, right: 750, bottom: 0, toJSON: () => ({}),
    } as DOMRect);
    // The setup ResizeObserver never fires, so only the layout effect can report this width.
    render(<Probe />);
    expect(screen.getByText('750 medium')).toBeInTheDocument();
  });

  it('reports zero when the element has no size', () => {
    render(<Probe />);
    expect(screen.getByText('0 small')).toBeInTheDocument();
  });
});
