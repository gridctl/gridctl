export const STACK_INSPECTOR_SELECTOR = 'aside[aria-label="Stack inspector"]';

function findNodeWrapper(nodeId: string): HTMLElement | null {
  for (const el of document.querySelectorAll<HTMLElement>('.react-flow__node[data-id]')) {
    if (el.getAttribute('data-id') === nodeId) return el;
  }
  return null;
}

/**
 * Returns focus to the canvas node the Stack inspector was showing. Call it
 * before the inspector closes. It acts only when focus is inside the inspector
 * or already lost to <body>, so a dialog, popover, or field elsewhere keeps
 * its focus. A node that no longer exists is left alone.
 */
export function restoreNodeFocus(nodeId: string | null): void {
  if (!nodeId) return;
  const active = document.activeElement;
  const onBody = active === null || active === document.body;
  const inspector = document.querySelector(STACK_INSPECTOR_SELECTOR);
  const inInspector = inspector !== null && active !== null && inspector.contains(active);
  if (!onBody && !inInspector) return;
  findNodeWrapper(nodeId)?.focus();
}
