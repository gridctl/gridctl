// Copies text via the async clipboard API, falling back to the legacy
// textarea + execCommand path in contexts where the API is unavailable
// (non-secure origins, detached windows on some platforms).
export async function copyTextToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const textArea = document.createElement('textarea');
    textArea.value = text;
    document.body.appendChild(textArea);
    textArea.select();
    document.execCommand('copy');
    document.body.removeChild(textArea);
  }
}
