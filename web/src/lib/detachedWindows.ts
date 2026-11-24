import type { DetachableWindow } from '../hooks/useBroadcastChannel';
import { DOCUMENT_TITLE_BASE } from '../types/workspace';

export const DETACHED_WINDOW_LABELS: Record<DetachableWindow, string> = {
  logs: 'Logs',
  sidebar: 'Details',
  editor: 'Editor',
  registry: 'Library',
  metrics: 'Metrics',
  traces: 'Traces',
};

// "Gridctl - Details", or "Gridctl - Details: db" with a detail. An empty or
// missing detail is left out so the title never reads "undefined".
export function detachedDocumentTitle(type: DetachableWindow, detail?: string | null): string {
  const base = `${DOCUMENT_TITLE_BASE} - ${DETACHED_WINDOW_LABELS[type]}`;
  return detail ? `${base}: ${detail}` : base;
}
