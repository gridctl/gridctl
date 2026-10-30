import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import type { ReactNode } from 'react';
import { cleanup, render } from '@testing-library/react';
import { WorkspaceShell } from '../components/layout/WorkspaceShell';

const recorded = vi.hoisted(() => ({
  groups: [] as Array<Record<string, unknown>>,
  panels: [] as Array<Record<string, unknown>>,
}));

vi.mock('react-resizable-panels', () => ({
  Group: ({ children, ...props }: { children?: ReactNode } & Record<string, unknown>) => {
    recorded.groups.push(props);
    return <div data-testid="rrp-group">{children}</div>;
  },
  Panel: ({ children, ...props }: { children?: ReactNode; id?: string } & Record<string, unknown>) => {
    recorded.panels.push(props);
    return <div data-testid={`panel-${String(props.id)}`}>{children}</div>;
  },
  Separator: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  usePanelRef: () => ({ current: null }),
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: () => {} }),
}));

beforeEach(() => {
  cleanup();
  recorded.groups.length = 0;
  recorded.panels.length = 0;
});

describe('WorkspaceShell', () => {
  it('passes pixel minimums, string percent defaults, and a widened hit target', () => {
    render(
      <WorkspaceShell workspace="library" left={<div />} right={<div />} minLeftPx={220} minRightPx={380}>
        <div>center</div>
      </WorkspaceShell>,
    );

    const left = recorded.panels.find((panel) => panel.id === 'left');
    const center = recorded.panels.find((panel) => panel.id === 'center');
    const right = recorded.panels.find((panel) => panel.id === 'right');
    expect(left?.minSize).toBe(220);
    expect(right?.minSize).toBe(380);
    expect(typeof left?.defaultSize).toBe('string');
    expect(typeof center?.defaultSize).toBe('string');
    expect(typeof right?.defaultSize).toBe('string');
    expect(center?.minSize).toBe('20%');

    const hit = recorded.groups.at(-1)?.resizeTargetMinimumSize as { fine: number; coarse: number };
    expect(hit.fine).toBeGreaterThanOrEqual(16);
    expect(hit.coarse).toBeGreaterThanOrEqual(24);
  });
});
