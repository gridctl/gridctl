import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import '@testing-library/jest-dom';
import { act, renderHook, render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import type { ReactNode } from 'react';
import {
  leavesWorkspace,
  pathnameOf,
  useNavigationGuardStore,
} from '../stores/useNavigationGuardStore';
import { useNavigationGuard } from '../hooks/useNavigationGuard';
import { useGuardedNavigate } from '../hooks/useGuardedNavigate';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';

function reset() {
  useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
}

beforeEach(reset);
afterEach(reset);

const store = () => useNavigationGuardStore.getState();

describe('useNavigationGuardStore', () => {
  it('proceeds synchronously with no guard registered', () => {
    const unhost = store().hostMounted();
    const proceed = vi.fn();
    expect(store().isBlocked('/logs')).toBe(false);
    store().guard('/logs', proceed);
    expect(proceed).toHaveBeenCalledTimes(1);
    expect(store().pending).toBeNull();
    unhost();
  });

  it('holds a request behind a dirty guard and a mounted host', () => {
    const unhost = store().hostMounted();
    const order: string[] = [];
    const onDiscard = vi.fn(() => order.push('discard'));
    store().register({ id: 'a', label: 'Tools for github', onDiscard });
    const proceed = vi.fn(() => order.push('proceed'));

    expect(store().isBlocked('/logs')).toBe(true);
    expect(store().pending).toBeNull();
    store().guard('/logs', proceed);
    expect(proceed).not.toHaveBeenCalled();
    expect(store().pending?.labels).toEqual(['Tools for github']);

    store().confirm();
    expect(store().pending).toBeNull();
    expect(onDiscard).toHaveBeenCalledTimes(1);
    expect(proceed).toHaveBeenCalledTimes(1);
    expect(order).toEqual(['discard', 'proceed']);
    unhost();
  });

  it('cancel clears the request without discarding or proceeding', () => {
    const unhost = store().hostMounted();
    const onDiscard = vi.fn();
    store().register({ id: 'a', label: 'x', onDiscard });
    const proceed = vi.fn();
    store().guard('/logs', proceed);
    store().cancel();
    expect(store().pending).toBeNull();
    expect(onDiscard).not.toHaveBeenCalled();
    expect(proceed).not.toHaveBeenCalled();
    unhost();
  });

  it('lets every navigation through when no host is mounted', () => {
    store().register({ id: 'a', label: 'x' });
    const proceed = vi.fn();
    expect(store().isBlocked('/logs')).toBe(false);
    expect(store().isBlocked(null)).toBe(false);
    store().guard('/logs', proceed);
    expect(proceed).toHaveBeenCalledTimes(1);
    expect(store().pending).toBeNull();
  });

  it('honors a blocks predicate', () => {
    const unhost = store().hostMounted();
    store().register({ id: 'a', label: 'x', blocks: (to) => leavesWorkspace(to, 'tools') });

    const through = vi.fn();
    expect(store().isBlocked('/tools?server=b')).toBe(false);
    store().guard('/tools?server=b', through);
    expect(through).toHaveBeenCalledTimes(1);
    expect(store().pending).toBeNull();

    const held = vi.fn();
    expect(store().isBlocked('/metrics')).toBe(true);
    expect(store().pending).toBeNull();
    store().guard('/metrics', held);
    expect(held).not.toHaveBeenCalled();
    expect(store().pending).not.toBeNull();

    // A null target means any destination.
    store().cancel();
    expect(store().isBlocked(null)).toBe(true);
    unhost();
  });

  it('discards only the guards that blocked the request', () => {
    const unhost = store().hostMounted();
    const blocking = vi.fn();
    const passing = vi.fn();
    store().register({ id: 'a', label: 'A', onDiscard: blocking });
    store().register({ id: 'b', label: 'B', blocks: () => false, onDiscard: passing });
    store().guard('/logs', () => {});
    expect(store().pending?.labels).toEqual(['A']);
    store().confirm();
    expect(blocking).toHaveBeenCalledTimes(1);
    expect(passing).not.toHaveBeenCalled();
    unhost();
  });

  it('replaces a pending request instead of stacking', () => {
    const unhost = store().hostMounted();
    store().register({ id: 'a', label: 'A' });
    const first = vi.fn();
    const second = vi.fn();
    store().guard('/logs', first);
    store().guard('/metrics', second);
    store().confirm();
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
    unhost();
  });

  it('drops a pending request when the last host unmounts', () => {
    const unhost = store().hostMounted();
    store().register({ id: 'a', label: 'A' });
    store().guard('/logs', () => {});
    unhost();
    expect(store().pending).toBeNull();
    expect(store().hosts).toBe(0);
  });
});

describe('pathnameOf and leavesWorkspace', () => {
  it('reads the pathname of a string or path object', () => {
    expect(pathnameOf('/tools?server=a#x')).toBe('/tools');
    expect(pathnameOf('?server=a')).toBeNull();
    expect(pathnameOf('#x')).toBeNull();
    expect(pathnameOf({ pathname: '/logs', search: '?a=1' })).toBe('/logs');
    expect(pathnameOf({ search: '?a=1' })).toBeNull();
  });

  it('treats a target without a pathname as staying put', () => {
    expect(leavesWorkspace('?server=x', 'tools')).toBe(false);
    expect(leavesWorkspace('/tools', 'tools')).toBe(false);
    expect(leavesWorkspace('/tools/x', 'tools')).toBe(false);
    expect(leavesWorkspace('/toolsx', 'tools')).toBe(true);
    expect(leavesWorkspace('/metrics', 'tools')).toBe(true);
  });
});

describe('useNavigationGuard', () => {
  it('registers only while dirty and unregisters on unmount', () => {
    const { rerender, unmount } = renderHook(
      ({ dirty }: { dirty: boolean }) => useNavigationGuard({ dirty, label: 'editor' }),
      { initialProps: { dirty: false } },
    );
    expect(store().guards).toHaveLength(0);
    rerender({ dirty: true });
    expect(store().guards).toHaveLength(1);
    expect(store().guards[0].label).toBe('editor');
    rerender({ dirty: false });
    expect(store().guards).toHaveLength(0);
    rerender({ dirty: true });
    expect(store().guards).toHaveLength(1);
    unmount();
    expect(store().guards).toHaveLength(0);
  });

  it('keeps one registration when callback identities change', () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = renderHook(
      ({ onDiscard }: { onDiscard: () => void }) =>
        useNavigationGuard({ dirty: true, label: 'editor', onDiscard, blocks: () => true }),
      { initialProps: { onDiscard: first } },
    );
    const registered = store().guards[0];
    rerender({ onDiscard: second });
    expect(store().guards).toHaveLength(1);
    expect(store().guards[0]).toBe(registered);
    registered.onDiscard?.();
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it('adds a beforeunload listener only while dirty', () => {
    const add = vi.spyOn(window, 'addEventListener');
    const remove = vi.spyOn(window, 'removeEventListener');
    const { rerender } = renderHook(
      ({ dirty }: { dirty: boolean }) => useNavigationGuard({ dirty, label: 'editor' }),
      { initialProps: { dirty: false } },
    );
    const count = (spy: typeof add) => spy.mock.calls.filter(([type]) => type === 'beforeunload').length;
    expect(count(add)).toBe(0);
    rerender({ dirty: true });
    expect(count(add)).toBe(1);
    rerender({ dirty: false });
    expect(count(remove)).toBe(1);
    add.mockRestore();
    remove.mockRestore();
  });
});

function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="loc">{`${loc.pathname}${loc.search}`}</div>;
}

function GuardedButtons() {
  const navigate = useGuardedNavigate();
  return (
    <>
      <button type="button" onClick={() => navigate('/logs', { replace: true })}>
        replace-logs
      </button>
      <button type="button" onClick={() => navigate('/metrics')}>
        metrics
      </button>
      <button type="button" onClick={() => navigate(-1)}>
        back
      </button>
    </>
  );
}

function wrap(children: ReactNode, entries: string[]) {
  return (
    <MemoryRouter initialEntries={entries} initialIndex={entries.length - 1}>
      {children}
      <NavigationGuardDialog />
      <LocationProbe />
    </MemoryRouter>
  );
}

describe('useGuardedNavigate', () => {
  it('navigates directly with nothing dirty', () => {
    render(wrap(<GuardedButtons />, ['/stack']));
    fireEvent.click(screen.getByText('metrics'));
    expect(screen.getByTestId('loc')).toHaveTextContent('/metrics');
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('passes a numeric delta straight through', () => {
    act(() => {
      store().register({ id: 'a', label: 'A' });
    });
    render(wrap(<GuardedButtons />, ['/stack', '/tools']));
    fireEvent.click(screen.getByText('back'));
    expect(screen.getByTestId('loc')).toHaveTextContent('/stack');
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('preserves options on a confirmed navigation', () => {
    const onDiscard = vi.fn();
    act(() => {
      store().register({ id: 'a', label: 'Tools for github', onDiscard });
    });
    render(wrap(<GuardedButtons />, ['/stack', '/tools']));

    fireEvent.click(screen.getByText('replace-logs'));
    expect(screen.getByTestId('loc')).toHaveTextContent('/tools');
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Discard unsaved changes');
    expect(dialog).toHaveTextContent('Tools for github');

    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('loc')).toHaveTextContent('/logs');

    // replace swapped /tools for /logs, so Back lands on /stack.
    act(() => store().cancel());
    useNavigationGuardStore.setState({ guards: [] });
    fireEvent.click(screen.getByText('back'));
    expect(screen.getByTestId('loc')).toHaveTextContent('/stack');
  });

  it('keeps the route when cancelled', () => {
    act(() => {
      store().register({ id: 'a', label: 'A' });
    });
    render(wrap(<GuardedButtons />, ['/tools']));
    fireEvent.click(screen.getByText('metrics'));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByTestId('loc')).toHaveTextContent('/tools');
  });

  it('names every blocking label in the confirm', () => {
    act(() => {
      store().register({ id: 'a', label: 'skill alpha' });
      store().register({ id: 'b', label: 'Global Context' });
      store().register({ id: 'c', label: 'agent beta' });
    });
    render(wrap(<GuardedButtons />, ['/tools']));
    fireEvent.click(screen.getByText('metrics'));
    expect(screen.getByRole('alertdialog')).toHaveTextContent(
      'skill alpha, Global Context, and agent beta',
    );
  });
});
