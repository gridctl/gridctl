import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import type { ReactNode } from 'react';

// jsdom cannot drive d3-zoom far enough to emit move events, so this file
// mocks ReactFlow to capture the props CanvasBase passes it.
const { flowProps } = vi.hoisted(() => ({
  flowProps: { current: {} as Record<string, unknown> },
}));

vi.mock('@xyflow/react', () => ({
  ReactFlow: (props: { children?: ReactNode } & Record<string, unknown>) => {
    flowProps.current = props;
    return <div data-testid="react-flow">{props.children}</div>;
  },
  Background: () => null,
  BackgroundVariant: { Dots: 'dots', Lines: 'lines', Cross: 'cross' },
}));

import { CanvasBase } from '../components/canvas/CanvasBase';

describe('CanvasBase move callbacks', () => {
  it('forwards onMoveStart to React Flow', () => {
    const onMoveStart = vi.fn();
    render(<CanvasBase nodes={[]} edges={[]} onMoveStart={onMoveStart} />);
    expect(flowProps.current.onMoveStart).toBe(onMoveStart);
  });

  it('forwards onMove to React Flow', () => {
    const onMove = vi.fn();
    render(<CanvasBase nodes={[]} edges={[]} onMove={onMove} />);
    expect(flowProps.current.onMove).toBe(onMove);
  });

  it('passes nothing when the callbacks are omitted', () => {
    render(<CanvasBase nodes={[]} edges={[]} />);
    expect(flowProps.current.onMoveStart).toBeUndefined();
    expect(flowProps.current.onMove).toBeUndefined();
  });
});
