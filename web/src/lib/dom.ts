/** True when the event target is a text-entry element (input, textarea,
 *  select, or contenteditable) that should swallow single-key shortcuts. */
export function isTextInputTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (target.isContentEditable) return true;
  return false;
}

/** True while a modal dialog (aria-modal="true") is in the document. Global
 *  and workspace shortcuts stand down under it, wherever focus is. Non-modal
 *  role="dialog" panels such as SlideOver do not count. */
export function isModalDialogOpen(): boolean {
  return document.querySelector('[aria-modal="true"]') !== null;
}
