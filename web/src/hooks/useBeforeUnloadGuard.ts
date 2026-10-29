import { useEffect } from 'react';

// Warn on reload or tab close while `active` is true. The browser shows its
// own generic prompt; the empty returnValue is what makes that prompt appear.
export function useBeforeUnloadGuard(active: boolean): void {
  useEffect(() => {
    if (!active) return;
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [active]);
}
