import { useEffect, useId, useLayoutEffect, useRef } from 'react';
import type { To } from 'react-router';
import { useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { useBeforeUnloadGuard } from './useBeforeUnloadGuard';

interface NavigationGuardOptions {
  dirty: boolean;
  // Names what is unsaved in the shared discard confirm.
  label: string;
  // Whether a navigation to `to` leaves the editor. Omit to block every
  // guarded navigation.
  blocks?: (to: To) => boolean;
  // Throws the unsaved edits away when the user confirms leaving.
  onDiscard?: () => void;
}

// Registers an unsaved editor with the shared navigation guard while `dirty`,
// and warns on reload or tab close for the same span.
export function useNavigationGuard({ dirty, label, blocks, onDiscard }: NavigationGuardOptions): void {
  const id = useId();
  // Callbacks are read through a ref so a new identity each render does not
  // re-register the guard.
  const callbacks = useRef({ blocks, onDiscard });
  useLayoutEffect(() => {
    callbacks.current = { blocks, onDiscard };
  });

  useEffect(() => {
    if (!dirty) return;
    return useNavigationGuardStore.getState().register({
      id,
      label,
      blocks: (to) => {
        const fn = callbacks.current.blocks;
        return fn ? fn(to) : true;
      },
      onDiscard: () => callbacks.current.onDiscard?.(),
    });
  }, [dirty, label, id]);

  useBeforeUnloadGuard(dirty);
}
