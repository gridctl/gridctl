import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { inWorkspace, navigateToWorkspace } from '../lib/workspaceNav';
import { leavesWorkspace, useNavigationGuardStore } from '../stores/useNavigationGuardStore';

function reset() {
  useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
}

describe('inWorkspace', () => {
  it.each([
    ['/tools', 'tools', true],
    ['/tools/x', 'tools', true],
    ['/toolsx', 'tools', false],
    ['/stack', 'tools', false],
  ])('%s in %s is %s', (path, ws, expected) => {
    expect(inWorkspace(path, ws)).toBe(expected);
  });
});

describe('navigateToWorkspace', () => {
  beforeEach(reset);
  afterEach(reset);

  it('navigates right away when nothing is dirty', () => {
    const navigate = vi.fn();
    const then = vi.fn();
    navigateToWorkspace(navigate, '/stack', '/logs', then);
    expect(navigate).toHaveBeenCalledWith('/logs');
    expect(then).toHaveBeenCalledOnce();
  });

  it('does nothing for a bare target equal to the current path, even with a dirty draft', () => {
    useNavigationGuardStore.getState().hostMounted();
    useNavigationGuardStore.getState().register({
      id: 'tools',
      label: 'Tools for b',
      blocks: (to) => leavesWorkspace(to, 'tools'),
    });
    const navigate = vi.fn();
    const then = vi.fn();
    navigateToWorkspace(navigate, '/tools', '/tools', then);
    expect(navigate).not.toHaveBeenCalled();
    expect(useNavigationGuardStore.getState().pending).toBeNull();
    // Follow-up work (such as opening a dialog) still runs.
    expect(then).toHaveBeenCalledOnce();
  });

  it('navigates to the same path when the target carries a query', () => {
    const navigate = vi.fn();
    navigateToWorkspace(navigate, '/stack', '/stack?spec=1');
    expect(navigate).toHaveBeenCalledWith('/stack?spec=1');
  });

  it('holds a navigation out of a dirty workspace until the confirm', () => {
    useNavigationGuardStore.getState().hostMounted();
    useNavigationGuardStore.getState().register({
      id: 'lens',
      label: 'Access Lens',
      blocks: (to) => leavesWorkspace(to, 'stack'),
    });
    const navigate = vi.fn();
    const then = vi.fn();
    navigateToWorkspace(navigate, '/stack', '/library?x=1', then);
    expect(navigate).not.toHaveBeenCalled();
    expect(then).not.toHaveBeenCalled();
    expect(useNavigationGuardStore.getState().pending?.labels).toEqual(['Access Lens']);
    useNavigationGuardStore.getState().confirm();
    expect(navigate).toHaveBeenCalledWith('/library?x=1');
    expect(then).toHaveBeenCalledOnce();
  });
});
