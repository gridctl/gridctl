import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { fireEvent, render, screen } from '@testing-library/react';
import { LibraryHeaderActions } from '../components/registry/LibraryHeaderActions';

type Props = Parameters<typeof LibraryHeaderActions>[0];

function setup(overrides: Partial<Props> = {}) {
  const props: Props = {
    onNewSkill: vi.fn(),
    onImportSkill: vi.fn(),
    onRefresh: vi.fn(),
    onSync: vi.fn(),
    onPopout: vi.fn(),
    hasSources: true,
    updateCount: 0,
    syncing: false,
    detached: false,
    collapsed: true,
    ...overrides,
  };
  const utils = render(<LibraryHeaderActions {...props} />);
  return { props, ...utils };
}

function key(k: string) {
  fireEvent.keyDown(document.activeElement!, { key: k });
}

function openMenu() {
  fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
  return screen.getAllByRole('menuitem');
}

describe('LibraryHeaderActions', () => {
  it('renders the full cluster and no menu when not collapsed', () => {
    setup({ collapsed: false });
    expect(screen.getByRole('button', { name: /New Skill/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Import/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Global Context/ })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Open in separate window' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument();
  });

  it('moves secondary actions into the menu when collapsed', () => {
    setup();
    expect(screen.getByRole('button', { name: /New Skill/ })).toBeInTheDocument();
    const trigger = screen.getByRole('button', { name: 'More actions' });
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('button', { name: /Import/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Global Context/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Refresh' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Open in separate window' })).not.toBeInTheDocument();

    const items = openMenu();
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('menu', { name: 'More actions' })).toBeInTheDocument();
    expect(items.map((i) => i.textContent)).toEqual([
      'Import',
      'Sync sources',
      'Refresh',
      'Open in separate window',
    ]);
    expect(items[0]).toHaveFocus();
  });

  it('omits the Sync item when there are no sources', () => {
    setup({ hasSources: false });
    const items = openMenu();
    expect(items.map((i) => i.textContent)).not.toContain('Sync sources');
  });

  it('labels the popout item for a detached window and still calls onPopout', () => {
    const { props } = setup({ detached: true });
    const items = openMenu();
    const last = items[items.length - 1];
    expect(last).toHaveAccessibleName('Focus detached window');
    fireEvent.click(last);
    expect(props.onPopout).toHaveBeenCalledTimes(1);
  });

  it('keeps the update pill inline and drops the Sync item', () => {
    setup({ updateCount: 2 });
    expect(screen.getByRole('button', { name: 'Sync sources, 2 updates available' })).toHaveTextContent(
      'Sync sources (2 updates)',
    );
    const items = openMenu();
    expect(items.map((i) => i.textContent)).not.toContain('Sync sources');
  });

  it('calls each handler once and closes the menu', () => {
    const { props } = setup();
    const cases: [string, () => void][] = [
      ['Import', props.onImportSkill],
      ['Sync sources', props.onSync],
      ['Refresh', props.onRefresh],
      ['Open in separate window', props.onPopout],
    ];
    for (const [label, handler] of cases) {
      openMenu();
      fireEvent.click(screen.getByRole('menuitem', { name: label }));
      expect(handler).toHaveBeenCalledTimes(1);
      expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    }
  });

  it('disables the Sync item while syncing and skips it with arrows', () => {
    setup({ syncing: true });
    const items = openMenu();
    const sync = screen.getByRole('menuitem', { name: 'Syncing…' });
    expect(sync).toBeDisabled();
    expect(items[0]).toHaveFocus();
    key('ArrowDown');
    expect(screen.getByRole('menuitem', { name: 'Refresh' })).toHaveFocus();
  });

  it('supports arrows with wrap, Home, End, Escape, and Tab', () => {
    setup();
    const items = openMenu();
    const trigger = screen.getByRole('button', { name: 'More actions' });
    key('ArrowUp');
    expect(items[items.length - 1]).toHaveFocus();
    key('ArrowDown');
    expect(items[0]).toHaveFocus();
    key('ArrowDown');
    expect(items[1]).toHaveFocus();
    key('End');
    expect(items[items.length - 1]).toHaveFocus();
    key('Home');
    expect(items[0]).toHaveFocus();
    key('Escape');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();

    openMenu();
    key('Tab');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('closes when the trigger is clicked while open', () => {
    setup();
    openMenu();
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('moves focus to New Skill when the menu is removed while it holds focus', () => {
    const { props, rerender } = setup();
    openMenu();
    rerender(<LibraryHeaderActions {...props} collapsed={false} />);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /New Skill/ })).toHaveFocus();
  });
});
