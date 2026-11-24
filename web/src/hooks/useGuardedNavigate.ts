import { useCallback } from 'react';
import { useNavigate, type NavigateFunction, type NavigateOptions, type To } from 'react-router';
import { useNavigationGuardStore } from '../stores/useNavigationGuardStore';

// useNavigate, but a navigation that would leave an unsaved editor waits for
// the shared discard confirm. History deltas pass straight through.
export function useGuardedNavigate(): NavigateFunction {
  const navigate = useNavigate();
  const guarded = useCallback(
    (to: To | number, options?: NavigateOptions) => {
      if (typeof to === 'number') return navigate(to);
      useNavigationGuardStore.getState().guard(to, () => {
        void navigate(to, options);
      });
    },
    [navigate],
  );
  return guarded as NavigateFunction;
}
