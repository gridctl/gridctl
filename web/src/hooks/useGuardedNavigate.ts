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

// onClick for a <Link to={to}>: an unsaved editor that the navigation would
// leave cancels the link and asks through the shared confirm. Modified clicks
// open a new tab and pass through untouched.
export function useGuardedLinkClick(): (e: React.MouseEvent, to: To) => void {
  const navigate = useNavigate();
  return useCallback(
    (e: React.MouseEvent, to: To) => {
      if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
      const { isBlocked, guard } = useNavigationGuardStore.getState();
      if (!isBlocked(to)) return;
      e.preventDefault();
      guard(to, () => {
        void navigate(to);
      });
    },
    [navigate],
  );
}
