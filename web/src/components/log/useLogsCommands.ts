import { createElement, useEffect, useLayoutEffect, useRef } from 'react';
import {
  AlertCircle,
  Copy,
  Download,
  ExternalLink,
  FilterX,
  WrapText,
} from 'lucide-react';
import { useCommandRegistry } from '../../hooks/useCommandRegistry';
import { showToast } from '../ui/Toast';
import { downloadTextFile, logExportFilename } from '../../lib/download';
import type { PaletteCommand } from '../../types/palette';
import { LOG_WINDOW_SIZES, serializeLogsJSONL } from './logTypes';
import type { LogsViewState } from './useLogsView';

interface UseLogsCommandsOptions {
  view: LogsViewState;
  onOpenDetached: () => void;
}

/**
 * Workspace-scoped palette commands for /logs. Registered once on mount (the
 * live view is read through a ref — the poll replaces the view object every
 * tick and re-registering per tick would churn the registry), unregistered on
 * unmount so other workspaces never see them.
 */
export function useLogsCommands({ view, onOpenDetached }: UseLogsCommandsOptions): void {
  const { registerCommands, unregisterCommands } = useCommandRegistry();

  const viewRef = useRef(view);
  const openDetachedRef = useRef(onOpenDetached);
  useLayoutEffect(() => {
    viewRef.current = view;
    openDetachedRef.current = onOpenDetached;
  });

  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'logs:errors-only',
        label: 'Logs: Toggle Errors Only',
        section: 'logs',
        workspaces: ['logs'],
        icon: createElement(AlertCircle, { size: 14 }),
        keywords: ['error', 'severity', 'level', 'filter'],
        onSelect: () => viewRef.current.toggleErrorsOnly(),
      },
      {
        id: 'logs:clear-filters',
        label: 'Logs: Clear Filters',
        section: 'logs',
        workspaces: ['logs'],
        icon: createElement(FilterX, { size: 14 }),
        keywords: ['reset', 'filter', 'all', 'clear'],
        onSelect: () => viewRef.current.clearFilters(),
      },
      {
        id: 'logs:toggle-wrap',
        label: 'Logs: Toggle Line Wrap',
        section: 'logs',
        workspaces: ['logs'],
        icon: createElement(WrapText, { size: 14 }),
        keywords: ['wrap', 'lines', 'truncate'],
        onSelect: () => viewRef.current.toggleWrap(),
      },
      {
        id: 'logs:copy-filtered',
        label: 'Logs: Copy Filtered View',
        section: 'logs',
        workspaces: ['logs'],
        icon: createElement(Copy, { size: 14 }),
        keywords: ['copy', 'clipboard'],
        onSelect: () => viewRef.current.copyFiltered(),
      },
      {
        id: 'logs:export-jsonl',
        label: 'Logs: Export Filtered as JSONL',
        section: 'logs',
        workspaces: ['logs'],
        icon: createElement(Download, { size: 14 }),
        keywords: ['export', 'download', 'jsonl', 'save'],
        onSelect: () => {
          const logs = viewRef.current.filteredLogs;
          if (logs.length === 0) {
            showToast('warning', 'Nothing to export');
            return;
          }
          downloadTextFile(serializeLogsJSONL(logs), logExportFilename('jsonl'), 'application/jsonl');
          showToast('success', `Exported ${logs.length} entries as JSONL`);
        },
      },
      ...LOG_WINDOW_SIZES.map((n) => ({
        id: `logs:window-${n}`,
        label: `Logs: Window Last ${n}`,
        section: 'logs' as const,
        workspaces: ['logs' as const],
        keywords: ['window', 'size', 'lines', String(n)],
        onSelect: () => viewRef.current.setWindowSize(n),
      })),
      {
        id: 'logs:open-detached',
        label: 'Logs: Open in Separate Window',
        section: 'logs',
        workspaces: ['logs'],
        icon: createElement(ExternalLink, { size: 14 }),
        keywords: ['popout', 'detach', 'window'],
        onSelect: () => openDetachedRef.current(),
      },
    ];

    registerCommands('logs-workspace', commands);
    return () => unregisterCommands('logs-workspace');
  }, [registerCommands, unregisterCommands]);
}
