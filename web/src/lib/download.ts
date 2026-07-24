// Client-side file download via a transient object URL. Shared by the log
// export toolbar and palette command so every entry point produces the same
// file the same way.
export function downloadTextFile(content: string, filename: string, mime: string): void {
  const blob = new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

export function logExportFilename(format: 'jsonl' | 'txt'): string {
  return `gridctl-logs-${new Date().toISOString().slice(0, 19).replaceAll(':', '-')}.${format}`;
}
