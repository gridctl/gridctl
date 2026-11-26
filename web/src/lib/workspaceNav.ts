import { useNavigationGuardStore } from '../stores/useNavigationGuardStore';

// True when `pathname` is inside the workspace rooted at `/${workspace}`.
export function inWorkspace(pathname: string, workspace: string): boolean {
  return pathname === `/${workspace}` || pathname.startsWith(`/${workspace}/`);
}

// Navigation for the palette and the Cmd/Ctrl+1-9 shortcuts. A target with no
// query string whose path is the current pathname does nothing: navigating
// there would drop the current params (in Tools, the selected server, which
// rebuilds a dirty editor without its own confirm). Anything else goes
// through the shared leave guard, so an unsaved Tools or Access Lens draft
// asks first and a confirm continues to the full target. `then` runs once the
// navigation proceeds, or right away when it is skipped as same-path.
export function navigateToWorkspace(
  navigate: (to: string) => void,
  pathname: string,
  target: string,
  then?: () => void,
): void {
  const [path] = target.split(/[?#]/, 1);
  if (!target.includes('?') && path === pathname) {
    then?.();
    return;
  }
  useNavigationGuardStore.getState().guard(target, () => {
    navigate(target);
    then?.();
  });
}
