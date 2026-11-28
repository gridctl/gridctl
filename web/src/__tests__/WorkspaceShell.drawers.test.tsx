import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { WorkspaceShell } from '../components/layout/WorkspaceShell';

type Handle = { isCollapsed: ReturnType<typeof vi.fn>; collapse: ReturnType<typeof vi.fn>; expand: ReturnType<typeof vi.fn> };

const recorded = vi.hoisted(() => ({
  width: 0,
  panels: [] as Array<Record<string, unknown>>,
  layouts: [] as Array<{ panelIds?: string[] }>,
  handles: {} as Record<string, Handle>,
}));

vi.mock('../hooks/useContainerWidth', () => ({
  useContainerWidth: () => ({ width: recorded.width, layoutSize: 'large' }),
}));

vi.mock('react-resizable-panels', async () => {
  const { useRef: useReactRef } = await vi.importActual<typeof import('react')>('react');
  return {
    Group: ({ children }: { children?: ReactNode }) => <div data-testid="rrp-group">{children}</div>,
    Panel: ({ children, ...props }: { children?: ReactNode; id?: string; panelRef?: { current: unknown } } & Record<string, unknown>) => {
      recorded.panels.push(props);
      if (props.panelRef && props.id) props.panelRef.current = recorded.handles[props.id];
      return <div data-testid={`panel-${String(props.id)}`}>{children}</div>;
    },
    Separator: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
    usePanelRef: () => useReactRef(null),
    useDefaultLayout: (opts: { panelIds?: string[] }) => {
      recorded.layouts.push(opts);
      return { defaultLayout: undefined, onLayoutChanged: () => {} };
    },
  };
});

function makeHandle(): Handle {
  return { isCollapsed: vi.fn(() => false), collapse: vi.fn(), expand: vi.fn() };
}

beforeEach(() => {
  cleanup();
  recorded.width = 0;
  recorded.panels.length = 0;
  recorded.layouts.length = 0;
  recorded.handles = { left: makeHandle(), right: makeHandle() };
});

function renderedPanelIds(): string[] {
  return [...document.querySelectorAll('[data-testid^="panel-"]')].map((el) =>
    el.getAttribute('data-testid')!.replace('panel-', ''),
  );
}

function lastPanel(id: string) {
  return recorded.panels.filter((p) => p.id === id).at(-1);
}

interface ShellProps {
  leftKey?: string | null;
  rightKey?: string | null;
  left?: ReactNode;
  right?: ReactNode;
  center?: ReactNode;
}

function Shell({ leftKey, rightKey, left, right, center }: ShellProps) {
  return (
    <WorkspaceShell
      workspace="tools"
      left={left === undefined ? <aside aria-label="Nav"><button>nav item</button></aside> : left}
      right={right === undefined ? <aside aria-label="Detail"><button>detail action</button></aside> : right}
      leftKey={leftKey}
      rightKey={rightKey}
    >
      {center ?? <button>center row</button>}
    </WorkspaceShell>
  );
}

const rightDrawer = () => document.getElementById(screen.getByRole('button', { name: /right rail/ }).getAttribute('aria-controls')!)!;
const leftDrawer = () => document.getElementById(screen.getByRole('button', { name: /left rail/ }).getAttribute('aria-controls')!)!;

describe('WorkspaceShell docking', () => {
  it('docks only the left rail at 800 and neither rail at 600', () => {
    recorded.width = 800;
    const { rerender } = render(<Shell />);
    expect(renderedPanelIds()).toEqual(['left', 'center']);
    expect(recorded.layouts.at(-1)?.panelIds).toEqual(['left', 'center']);
    expect(lastPanel('center')?.defaultSize).toBe(`${100 - 18}`);

    recorded.width = 600;
    rerender(<Shell />);
    expect(renderedPanelIds()).toEqual(['center']);
    expect(recorded.layouts.at(-1)?.panelIds).toEqual(['center']);
    expect(lastPanel('center')?.defaultSize).toBe('100');
  });

  it('keeps both rails docked while the width is unmeasured', () => {
    render(<Shell />);
    expect(renderedPanelIds()).toEqual(['left', 'center', 'right']);
    expect(screen.getByRole('button', { name: 'Collapse right rail' })).not.toHaveAttribute('aria-controls');
  });
});

describe('WorkspaceShell drawers', () => {
  it('opens and closes the right drawer from rightKey without moving focus', () => {
    recorded.width = 800;
    const { rerender } = render(<Shell rightKey={null} />);
    expect(screen.getByText('detail action')).toBeInTheDocument();
    expect(rightDrawer()).toHaveAttribute('hidden');

    screen.getByText('center row').focus();
    rerender(<Shell rightKey="a" />);
    expect(rightDrawer()).not.toHaveAttribute('hidden');
    expect(screen.getByText('center row')).toHaveFocus();

    rerender(<Shell rightKey={null} />);
    expect(rightDrawer()).toHaveAttribute('hidden');
  });

  it('opens the right drawer on entering drawer mode only with a selection', () => {
    recorded.width = 1000;
    const { rerender } = render(<Shell rightKey="a" />);
    recorded.width = 800;
    rerender(<Shell rightKey="a" />);
    expect(rightDrawer()).not.toHaveAttribute('hidden');

    cleanup();
    recorded.width = 800;
    render(<Shell rightKey={null} />);
    expect(rightDrawer()).toHaveAttribute('hidden');
  });

  it('toggles the right drawer with ] and moves focus in and back', () => {
    recorded.width = 800;
    render(<Shell />);
    fireEvent.keyDown(document.body, { key: ']' });
    expect(rightDrawer()).not.toHaveAttribute('hidden');
    expect(screen.getByText('detail action')).toHaveFocus();

    fireEvent.keyDown(screen.getByText('detail action'), { key: ']' });
    expect(rightDrawer()).toHaveAttribute('hidden');
    expect(screen.getByRole('button', { name: 'Expand right rail' })).toHaveFocus();
  });

  it('ignores ] from a text input', () => {
    recorded.width = 800;
    render(<Shell center={<input aria-label="filter" />} />);
    fireEvent.keyDown(screen.getByLabelText('filter'), { key: ']' });
    expect(rightDrawer()).toHaveAttribute('hidden');
  });

  it('focuses the drawer itself when the rail has nothing focusable', () => {
    recorded.width = 800;
    render(<Shell right={<p>static</p>} />);
    fireEvent.click(screen.getByRole('button', { name: 'Expand right rail' }));
    expect(rightDrawer()).toHaveFocus();

    fireEvent.keyDown(rightDrawer(), { key: 'Escape' });
    expect(rightDrawer()).toHaveAttribute('hidden');
    expect(screen.getByRole('button', { name: 'Expand right rail' })).toHaveFocus();

    fireEvent.keyDown(document.body, { key: ']' });
    expect(rightDrawer()).toHaveFocus();
    fireEvent.keyDown(rightDrawer(), { key: ']' });
    expect(rightDrawer()).toHaveAttribute('hidden');
    expect(screen.getByRole('button', { name: 'Expand right rail' })).toHaveFocus();
  });

  it('closes the left drawer on a leftKey change and an outside press, not on its toggle', () => {
    recorded.width = 600;
    const { rerender } = render(<Shell leftKey="a" />);
    const toggle = () => screen.getByRole('button', { name: /left rail/ });
    expect(leftDrawer()).toHaveAttribute('hidden');

    fireEvent.click(toggle());
    expect(leftDrawer()).not.toHaveAttribute('hidden');
    rerender(<Shell leftKey="b" />);
    expect(leftDrawer()).toHaveAttribute('hidden');

    fireEvent.click(toggle());
    fireEvent.mouseDown(screen.getByText('nav item'));
    expect(leftDrawer()).not.toHaveAttribute('hidden');
    fireEvent.mouseDown(screen.getByText('center row'));
    expect(leftDrawer()).toHaveAttribute('hidden');

    fireEvent.mouseDown(toggle());
    fireEvent.click(toggle());
    expect(leftDrawer()).not.toHaveAttribute('hidden');
    fireEvent.mouseDown(toggle());
    fireEvent.click(toggle());
    expect(leftDrawer()).toHaveAttribute('hidden');
  });

  it('keeps the right drawer open on an outside press', () => {
    recorded.width = 800;
    render(<Shell rightKey="a" />);
    fireEvent.mouseDown(screen.getByText('center row'));
    expect(rightDrawer()).not.toHaveAttribute('hidden');
  });

  it('opens at most one drawer', () => {
    recorded.width = 600;
    render(<Shell />);
    fireEvent.keyDown(document.body, { key: '[' });
    expect(leftDrawer()).not.toHaveAttribute('hidden');
    fireEvent.keyDown(document.body, { key: ']' });
    expect(rightDrawer()).not.toHaveAttribute('hidden');
    expect(leftDrawer()).toHaveAttribute('hidden');
  });
});

describe('WorkspaceShell Escape', () => {
  it('closes the drawer with focus inside and outside the rail', () => {
    recorded.width = 800;
    const { rerender } = render(<Shell rightKey={null} />);
    fireEvent.keyDown(document.body, { key: ']' });
    fireEvent.keyDown(screen.getByText('detail action'), { key: 'Escape' });
    expect(rightDrawer()).toHaveAttribute('hidden');
    expect(screen.getByRole('button', { name: 'Expand right rail' })).toHaveFocus();

    rerender(<Shell rightKey="a" />);
    fireEvent.keyDown(screen.getByText('center row'), { key: 'Escape' });
    expect(rightDrawer()).toHaveAttribute('hidden');
  });

  it('stands down under a modal dialog', () => {
    recorded.width = 800;
    render(<Shell rightKey="a" />);
    const modal = document.createElement('div');
    modal.setAttribute('aria-modal', 'true');
    document.body.appendChild(modal);
    fireEvent.keyDown(screen.getByText('detail action'), { key: 'Escape' });
    fireEvent.keyDown(screen.getByText('center row'), { key: 'Escape' });
    expect(rightDrawer()).not.toHaveAttribute('hidden');
    modal.remove();
  });

  it('defers to a rail child that stops propagation', () => {
    recorded.width = 800;
    render(
      <Shell
        rightKey="a"
        right={<button onKeyDown={(e) => e.key === 'Escape' && e.stopPropagation()}>popover</button>}
      />,
    );
    fireEvent.keyDown(screen.getByText('popover'), { key: 'Escape' });
    expect(rightDrawer()).not.toHaveAttribute('hidden');
  });

  it('keeps an Escape inside the left drawer from reaching document and window listeners', () => {
    recorded.width = 600;
    render(<Shell />);
    const onDocument = vi.fn();
    const onWindow = vi.fn();
    document.addEventListener('keydown', onDocument);
    window.addEventListener('keydown', onWindow);
    fireEvent.keyDown(document.body, { key: '[' });
    onDocument.mockClear();
    onWindow.mockClear();

    fireEvent.keyDown(screen.getByText('nav item'), { key: 'Escape' });
    expect(leftDrawer()).toHaveAttribute('hidden');
    expect(onDocument).not.toHaveBeenCalled();
    expect(onWindow).not.toHaveBeenCalled();
    document.removeEventListener('keydown', onDocument);
    window.removeEventListener('keydown', onWindow);
  });

  it('stays closed while rightKey is unchanged and reopens on a new selection', () => {
    recorded.width = 800;
    const { rerender } = render(<Shell rightKey="a" />);
    fireEvent.keyDown(screen.getByText('center row'), { key: 'Escape' });
    expect(rightDrawer()).toHaveAttribute('hidden');
    rerender(<Shell rightKey="a" />);
    expect(rightDrawer()).toHaveAttribute('hidden');
    rerender(<Shell rightKey="b" />);
    expect(rightDrawer()).not.toHaveAttribute('hidden');
  });
});

function Counter() {
  const [n, setN] = useState(0);
  return <button onClick={() => setN(n + 1)}>count {n}</button>;
}

describe('WorkspaceShell rail state', () => {
  it('keeps rail state across docking changes and drawer toggles', () => {
    recorded.width = 1000;
    const { rerender } = render(<Shell right={<Counter />} />);
    fireEvent.click(screen.getByText('count 0'));

    recorded.width = 800;
    rerender(<Shell right={<Counter />} />);
    expect(screen.getByText('count 1')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Expand right rail' }));
    fireEvent.click(screen.getByText('count 1'));
    fireEvent.click(screen.getByRole('button', { name: 'Collapse right rail' }));

    recorded.width = 1000;
    rerender(<Shell right={<Counter />} />);
    expect(screen.getByText('count 2')).toBeInTheDocument();
  });

  it('keeps focus inside a rail that moves between docked and drawer', () => {
    recorded.width = 1000;
    const { rerender } = render(<Shell />);
    screen.getByText('detail action').focus();

    recorded.width = 800;
    rerender(<Shell />);
    expect(screen.getByText('detail action')).toHaveFocus();
    expect(rightDrawer()).not.toHaveAttribute('hidden');

    recorded.width = 1000;
    rerender(<Shell />);
    expect(screen.getByText('detail action')).toHaveFocus();
  });

  it.each([1000, 600])('mounts rail content connected, with autoFocus working, at %i', (width) => {
    recorded.width = width;
    const seen: boolean[] = [];
    function Probe() {
      const ref = useRef<HTMLDivElement>(null);
      useLayoutEffect(() => {
        seen.push(ref.current?.isConnected ?? false);
      }, []);
      return <div ref={ref}><input aria-label="auto" autoFocus /></div>;
    }
    render(<Shell left={<Probe />} right={<Probe />} />);
    expect(seen).toEqual([true, true]);
    expect(screen.getAllByLabelText('auto')).toContain(document.activeElement);
  });

  it('uses drawer animations without a lingering fill mode', () => {
    recorded.width = 600;
    render(<Shell />);
    expect(leftDrawer().className).not.toContain('animate-slide-in-right');
    expect(rightDrawer().className).not.toContain('animate-slide-in-right');
    expect(rightDrawer().className).toContain('animate-drawer-in-right');
    expect(leftDrawer().className).toContain('animate-drawer-in-left');

    const css = readFileSync(join(__dirname, '..', 'index.css'), 'utf8');
    for (const name of ['animate-drawer-in-right', 'animate-drawer-in-left']) {
      const rule = css.match(new RegExp(`\\.${name}\\s*\\{([^}]*)\\}`))?.[1];
      expect(rule).toBeDefined();
      expect(rule).not.toMatch(/\b(forwards|both)\b/);
    }
  });
});

describe('WorkspaceShell toggles', () => {
  it('names the toggles and wires them to the drawers', () => {
    recorded.width = 600;
    render(<Shell />);
    const leftToggle = screen.getByRole('button', { name: 'Expand left rail' });
    const rightToggle = screen.getByRole('button', { name: 'Expand right rail' });
    expect(leftToggle).toHaveAttribute('title', 'Expand left rail ([)');
    expect(rightToggle).toHaveAttribute('title', 'Expand right rail (])');
    expect(leftToggle).toHaveAttribute('aria-expanded', 'false');
    expect(leftToggle).toHaveAttribute('aria-keyshortcuts', '[');
    expect(rightToggle).toHaveAttribute('aria-keyshortcuts', ']');
    expect(leftToggle).toHaveAttribute('type', 'button');

    fireEvent.click(leftToggle);
    const open = screen.getByRole('button', { name: 'Collapse left rail' });
    expect(open).toHaveAttribute('aria-expanded', 'true');
    expect(open).toHaveAttribute('title', 'Collapse left rail ([)');
    expect(document.getElementById(open.getAttribute('aria-controls')!)).toContainElement(screen.getByText('nav item'));
  });

  it('keeps docked toggles in the center pane and drawer toggles outside it', () => {
    recorded.width = 800;
    render(<Shell />);
    const center = screen.getByTestId('panel-center');
    const leftToggle = screen.getByRole('button', { name: 'Collapse left rail' });
    const rightToggle = screen.getByRole('button', { name: 'Expand right rail' });
    expect(center).toContainElement(leftToggle);
    expect(leftToggle.style.left).toBe('10px');
    expect(center).not.toContainElement(rightToggle);

    fireEvent.click(rightToggle);
    const open = screen.getByRole('button', { name: 'Collapse right rail' });
    expect(center).not.toContainElement(open);
    expect(open.style.right).not.toBe('0px');
  });

  it('collapses and expands docked panels and follows onResize', () => {
    recorded.width = 1200;
    render(<Shell />);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse right rail' }));
    expect(recorded.handles.right.collapse).toHaveBeenCalledTimes(1);

    recorded.handles.right.isCollapsed.mockReturnValue(true);
    act(() => {
      (lastPanel('right')?.onResize as () => void)();
    });
    const expand = screen.getByRole('button', { name: 'Expand right rail' });
    expect(expand).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(expand);
    expect(recorded.handles.right.expand).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole('button', { name: 'Collapse left rail' }));
    expect(recorded.handles.left.collapse).toHaveBeenCalledTimes(1);
  });

  it('keeps a hand-collapsed docked rail collapsed when rightKey changes', () => {
    recorded.width = 1200;
    const { rerender } = render(<Shell rightKey={null} />);
    recorded.handles.right.isCollapsed.mockReturnValue(true);
    act(() => {
      (lastPanel('right')?.onResize as () => void)();
    });
    rerender(<Shell rightKey="a" />);
    expect(recorded.handles.right.expand).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Expand right rail' })).toBeInTheDocument();
  });

  it('renders no toggle and ignores the key for a missing rail', () => {
    recorded.width = 600;
    render(<WorkspaceShell workspace="logs" left={<button>nav</button>}><div /></WorkspaceShell>);
    expect(screen.queryByRole('button', { name: /right rail/ })).toBeNull();
    expect(fireEvent.keyDown(document.body, { key: ']' })).toBe(true);
    cleanup();
    render(<WorkspaceShell workspace="logs" right={<button>detail</button>}><div /></WorkspaceShell>);
    expect(screen.queryByRole('button', { name: /left rail/ })).toBeNull();
    expect(fireEvent.keyDown(document.body, { key: '[' })).toBe(true);
  });
});
