import { create } from 'zustand';
import type { To } from 'react-router';

// One leave guard for every unsaved editor.
//
// The app uses BrowserRouter, and useBlocker only works with a data router, so
// in-app navigation is guarded by hand: editors register here while dirty
// (useNavigationGuard), and shell-level navigation goes through guard()
// (useGuardedNavigate). A blocked request waits in `pending` until the shared
// NavigationGuardDialog confirms or cancels it. Without a mounted dialog host
// (detached popouts, most tests) nothing could resolve a pending request, so
// guard() lets every navigation through.
export interface NavigationGuard {
  id: string;
  label: string;
  // Whether a navigation to `to` leaves the editor. Missing means every
  // navigation does.
  blocks?: (to: To) => boolean;
  onDiscard?: () => void;
}

export interface PendingNavigation {
  labels: string[];
  proceed: () => void;
  // Guards that blocked the request; confirm() discards only these.
  guardIds: string[];
}

interface NavigationGuardState {
  guards: NavigationGuard[];
  hosts: number;
  pending: PendingNavigation | null;
  register: (guard: NavigationGuard) => () => void;
  hostMounted: () => () => void;
  // `to === null` means any destination.
  isBlocked: (to: To | null) => boolean;
  guard: (to: To | null, proceed: () => void) => void;
  confirm: () => void;
  cancel: () => void;
}

function blockingGuards(state: Pick<NavigationGuardState, 'guards' | 'hosts'>, to: To | null): NavigationGuard[] {
  if (state.hosts === 0) return [];
  return state.guards.filter((g) => !g.blocks || to === null || g.blocks(to));
}

export const useNavigationGuardStore = create<NavigationGuardState>((set, get) => ({
  guards: [],
  hosts: 0,
  pending: null,

  register: (guard) => {
    set((s) => ({ guards: [...s.guards.filter((g) => g.id !== guard.id), guard] }));
    return () => set((s) => ({ guards: s.guards.filter((g) => g !== guard) }));
  },

  hostMounted: () => {
    set((s) => ({ hosts: s.hosts + 1 }));
    let mounted = true;
    return () => {
      if (!mounted) return;
      mounted = false;
      set((s) => {
        const hosts = s.hosts - 1;
        return hosts === 0 ? { hosts, pending: null } : { hosts };
      });
    };
  },

  isBlocked: (to) => blockingGuards(get(), to).length > 0,

  guard: (to, proceed) => {
    const blocking = blockingGuards(get(), to);
    if (blocking.length === 0) {
      proceed();
      return;
    }
    // A newer request replaces a pending one rather than stacking dialogs.
    set({
      pending: {
        labels: blocking.map((g) => g.label),
        proceed,
        guardIds: blocking.map((g) => g.id),
      },
    });
  },

  // Discard before proceeding: an editor's close path may navigate itself,
  // and the requested destination must be the last navigation.
  confirm: () => {
    const { pending, guards } = get();
    if (!pending) return;
    set({ pending: null });
    for (const id of pending.guardIds) {
      guards.find((g) => g.id === id)?.onDiscard?.();
    }
    pending.proceed();
  },

  cancel: () => set({ pending: null }),
}));

// The pathname a navigation targets, or null when `to` carries none (a bare
// search or hash), which stays on the current route.
export function pathnameOf(to: To): string | null {
  if (typeof to === 'string') {
    const path = to.split(/[?#]/, 1)[0];
    return path === '' ? null : path;
  }
  return to.pathname ? to.pathname : null;
}

// True when `to` lands outside the workspace rooted at `/${workspace}`.
export function leavesWorkspace(to: To, workspace: string): boolean {
  const path = pathnameOf(to);
  if (path === null) return false;
  return path !== `/${workspace}` && !path.startsWith(`/${workspace}/`);
}
