import { createElement, useEffect, useLayoutEffect, useRef } from 'react';
import { CheckCircle2, Copy, Download, RefreshCw, ShieldAlert, Trash2 } from 'lucide-react';
import { useCommandRegistry } from '../../hooks/useCommandRegistry';
import { showToast } from '../ui/Toast';
import type { PaletteCommand } from '../../types/palette';
import {
  APPROVE_BUTTON_ID,
  COPY_HASH_BUTTON_ID,
  EXPORT_BUTTON_ID,
  REVERIFY_BUTTON_ID,
} from './PinsDriftSection';
import { RESET_BUTTON_ID } from './PinsServerDetail';

interface UsePinsCommandsOptions {
  activeServerName: string;
  toggleAttention: () => void;
  toggleFindingsOnly: () => void;
}

// clickById dispatches through the rendered button so every entry point runs
// the same gated handler (the approve confirmation, the loaded-diff guards).
// A missing button means the action does not apply to the current selection;
// a disabled one means it is not ready yet - the guard copy must not claim
// the wrong reason.
function clickById(id: string, missingMessage: string): void {
  const el = document.getElementById(id);
  if (!(el instanceof HTMLButtonElement)) {
    showToast('warning', missingMessage);
    return;
  }
  if (el.disabled) {
    showToast('warning', 'Not ready yet; the panel is still loading');
    return;
  }
  el.click();
}

/**
 * Workspace-scoped palette commands for /pins. Registered once on mount (live
 * values are read through refs so poll-driven re-renders never churn the
 * registry), unregistered on unmount so other workspaces never see them.
 */
export function usePinsCommands({
  activeServerName,
  toggleAttention,
  toggleFindingsOnly,
}: UsePinsCommandsOptions): void {
  const { registerCommands, unregisterCommands } = useCommandRegistry();

  const serverRef = useRef(activeServerName);
  const attentionRef = useRef(toggleAttention);
  const findingsRef = useRef(toggleFindingsOnly);
  useLayoutEffect(() => {
    serverRef.current = activeServerName;
    attentionRef.current = toggleAttention;
    findingsRef.current = toggleFindingsOnly;
  });

  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'pins:approve',
        label: 'Pins: Approve Current Drift',
        section: 'pins',
        workspaces: ['pins'],
        icon: createElement(CheckCircle2, { size: 14 }),
        keywords: ['approve', 'drift', 'repin', 'accept'],
        onSelect: () =>
          clickById(APPROVE_BUTTON_ID, 'No reviewable drift on the selected server'),
      },
      {
        id: 'pins:reverify',
        label: 'Pins: Re-verify Selected Server',
        section: 'pins',
        workspaces: ['pins'],
        icon: createElement(RefreshCw, { size: 14 }),
        keywords: ['verify', 'refresh', 'recompute', 'diff'],
        onSelect: () => clickById(REVERIFY_BUTTON_ID, 'No server selected'),
      },
      {
        id: 'pins:export-diff',
        label: 'Pins: Export Drift Diff as JSON',
        section: 'pins',
        workspaces: ['pins'],
        icon: createElement(Download, { size: 14 }),
        keywords: ['export', 'download', 'json', 'audit'],
        onSelect: () => clickById(EXPORT_BUTTON_ID, 'No drift diff to export'),
      },
      {
        id: 'pins:copy-live-hash',
        label: 'Pins: Copy Live Server Hash',
        section: 'pins',
        workspaces: ['pins'],
        icon: createElement(Copy, { size: 14 }),
        keywords: ['copy', 'hash', 'expect', 'cli'],
        onSelect: () => clickById(COPY_HASH_BUTTON_ID, 'No drift diff loaded'),
      },
      {
        id: 'pins:toggle-attention',
        label: 'Pins: Toggle Attention Filter',
        section: 'pins',
        workspaces: ['pins'],
        icon: createElement(ShieldAlert, { size: 14 }),
        keywords: ['attention', 'filter', 'rail', 'all'],
        onSelect: () => attentionRef.current(),
      },
      {
        id: 'pins:toggle-findings',
        label: 'Pins: Toggle Findings Filter',
        section: 'pins',
        workspaces: ['pins'],
        icon: createElement(ShieldAlert, { size: 14 }),
        keywords: ['findings', 'filter', 'tools', 'scan'],
        onSelect: () => {
          if (!serverRef.current) {
            showToast('warning', 'No server selected');
            return;
          }
          findingsRef.current();
        },
      },
      {
        id: 'pins:reset',
        label: 'Pins: Reset Pins for Selected Server',
        section: 'pins',
        workspaces: ['pins'],
        icon: createElement(Trash2, { size: 14 }),
        keywords: ['reset', 'delete', 'danger', 'trust'],
        onSelect: () => clickById(RESET_BUTTON_ID, 'No server selected'),
      },
    ];
    registerCommands('pins-workspace', commands);
    return () => unregisterCommands('pins-workspace');
  }, [registerCommands, unregisterCommands]);
}
