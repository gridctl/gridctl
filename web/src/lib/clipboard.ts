// Copies text via the async clipboard API, falling back to the legacy
// textarea + execCommand path in contexts where the API is unavailable
// (non-secure origins, detached windows on some platforms). Rejects when
// neither path copied anything, so callers can report the failure.
export async function copyTextToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    return;
  } catch {
    // Fall through to the legacy path.
  }
  if (typeof document.execCommand !== 'function') {
    throw new Error('Clipboard is unavailable');
  }
  const textArea = document.createElement('textarea');
  textArea.value = text;
  document.body.appendChild(textArea);
  try {
    textArea.select();
    if (!document.execCommand('copy')) {
      throw new Error('Copy command was refused');
    }
  } finally {
    textArea.remove();
  }
}
