import { createElement, useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router';
import { Bot, CloudUpload, FileDiff, List, Package, RefreshCw, Webhook } from 'lucide-react';
import { useContextStore } from '../../stores/useContextStore';
import { useCommandRegistry } from '../../hooks/useCommandRegistry';
import { useNavigationGuard } from '../../hooks/useNavigationGuard';
import type { RegistryKind } from '../../lib/registryKind';
import type { PaletteCommand } from '../../types/palette';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { FailedState } from '../ui/EmptyState';
import { showToast } from '../ui/Toast';
import { ContextFrame } from './ContextFrame';
import type { ContextInspectorTab } from './ContextInspector';
import {
  EditorView,
  FragmentsView,
  SetupView,
  type ContextViewActions,
} from './GlobalContextEditor';

interface ContextWorkspaceProps {
  onKindChange: (kind: RegistryKind) => void;
}

/**
 * The Context segment of the Library workspace (/library?kind=context):
 * manages the canonical global agent context (single AGENTS.md or rule
 * fragments) with URL-synced fragment selection, inspector tab state,
 * one-shot ?client= deep links, and unsaved-draft protection.
 */
export function ContextWorkspace({ onKindChange }: ContextWorkspaceProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const doc = useContextStore((s) => s.doc);
  const loading = useContextStore((s) => s.loading);
  const error = useContextStore((s) => s.error);
  const refresh = useContextStore((s) => s.refresh);

  const [dirty, setDirty] = useState(false);
  const [pendingKind, setPendingKind] = useState<RegistryKind | null>(null);
  const viewActionsRef = useRef<ContextViewActions | null>(null);
  const registerViewActions = useCallback((actions: ContextViewActions | null) => {
    viewActionsRef.current = actions;
  }, []);

  const fragmentParam = searchParams.get('fragment');
  const tab: ContextInspectorTab = searchParams.get('tab') === 'details' ? 'details' : 'clients';
  const clientParam = searchParams.get('client');

  const setFragmentParam = useCallback(
    (name: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (name) next.set('fragment', name);
          else next.delete('fragment');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setTab = useCallback(
    (nextTab: ContextInspectorTab) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (nextTab === 'clients') next.delete('tab');
          else next.set('tab', nextTab);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Single-file mode and setup have no fragments; drop a stale ?fragment= silently.
  useEffect(() => {
    if (doc && !doc.fragments_active && fragmentParam !== null) {
      setFragmentParam(null);
    }
  }, [doc, fragmentParam, setFragmentParam]);

  // One-shot ?client= deep link: wait for the document to load so the active
  // view's useDriftSeed runs first, switch to the Clients tab, toast if no
  // client matches, and remove the parameter.
  const lastClientRef = useRef<string | null>(null);
  useEffect(() => {
    if (!clientParam) {
      lastClientRef.current = null;
      return;
    }
    if (!doc || lastClientRef.current === clientParam) return;
    lastClientRef.current = clientParam;
    if (!doc.clients.some((c) => c.slug === clientParam)) {
      showToast('error', `Client "${clientParam}" not found`);
    }
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('client');
        next.delete('tab');
        return next;
      },
      { replace: true },
    );
  }, [clientParam, doc, setSearchParams]);

  // Unmount drops the draft; no blocks predicate because leaving the Context
  // segment (even to /library) unmounts the editor.
  useNavigationGuard({ dirty, label: 'Global Context' });

  const handleKindChange = useCallback(
    (next: RegistryKind) => {
      if (next === 'context') return;
      if (dirty) {
        setPendingKind(next);
        return;
      }
      onKindChange(next);
    },
    [dirty, onKindChange],
  );

  const { registerCommands, unregisterCommands } = useCommandRegistry();
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'library:context-refresh',
        label: 'Library: Refresh Global Context',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(RefreshCw, { size: 14 }),
        keywords: ['reload', 'refresh', 'context', 'global', 'agents.md', 'rules'],
        onSelect: () => {
          if (viewActionsRef.current) viewActionsRef.current.refresh();
          else void refresh();
        },
      },
      {
        id: 'library:context-sync-all',
        label: 'Library: Sync Global Context to All Clients',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(CloudUpload, { size: 14 }),
        keywords: ['sync', 'context', 'global', 'clients', 'agents.md', 'rules'],
        onSelect: () => {
          viewActionsRef.current?.syncAll();
        },
      },
      {
        id: 'library:view-skills',
        label: 'Library: View Skills',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(List, { size: 14 }),
        keywords: ['skills', 'segment', 'switch', 'kind'],
        onSelect: () => handleKindChange('skill'),
      },
      {
        id: 'library:view-agents',
        label: 'Library: View Agents',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Bot, { size: 14 }),
        keywords: ['agents', 'segment', 'switch', 'kind'],
        onSelect: () => handleKindChange('agent'),
      },
      {
        id: 'library:view-hooks',
        label: 'Library: View Hooks',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Webhook, { size: 14 }),
        keywords: ['hooks', 'hook', 'segment', 'switch', 'kind'],
        onSelect: () => handleKindChange('hook'),
      },
      {
        id: 'library:view-packs',
        label: 'Library: View Packs',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Package, { size: 14 }),
        keywords: ['packs', 'pack', 'segment', 'switch', 'kind'],
        onSelect: () => handleKindChange('pack'),
      },
      {
        id: 'library:view-proposals',
        label: 'Library: View Proposals',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(FileDiff, { size: 14 }),
        keywords: ['proposals', 'proposal', 'review', 'staged', 'segment', 'switch', 'kind'],
        onSelect: () => handleKindChange('proposal'),
      },
    ];
    registerCommands('library-context', commands);
    return () => unregisterCommands('library-context');
  }, [registerCommands, unregisterCommands, refresh, handleKindChange]);

  const confirmDialog = (
    <ConfirmDialog
      isOpen={pendingKind !== null}
      onClose={() => setPendingKind(null)}
      onConfirm={() => {
        const next = pendingKind;
        setPendingKind(null);
        if (next) onKindChange(next);
      }}
      title="Discard changes"
      message={<p>The global context has unsaved edits. Discard them?</p>}
      confirmLabel="Discard"
      variant="danger"
    />
  );

  if (!doc && error && !loading) {
    return (
      <>
        <ContextFrame onKindChange={handleKindChange} onRefresh={() => void refresh()}>
          <FailedState
            feature="Global context"
            message={error}
            onRetry={() => void refresh()}
            className="flex-1"
          />
        </ContextFrame>
        {confirmDialog}
      </>
    );
  }

  if (!doc) {
    return (
      <>
        <ContextFrame onKindChange={handleKindChange} onRefresh={() => void refresh()}>
          <div className="flex-1 flex items-center justify-center text-sm text-text-muted">
            Loading global context…
          </div>
        </ContextFrame>
        {confirmDialog}
      </>
    );
  }

  if (doc.fragments_active) {
    return (
      <>
        <FragmentsView
          doc={doc}
          refreshError={error}
          initialDriftSlug={clientParam}
          fragmentParam={fragmentParam}
          onFragmentParamChange={setFragmentParam}
          tab={tab}
          onTabChange={setTab}
          onKindChange={handleKindChange}
          onDirtyChange={setDirty}
          onRegisterActions={registerViewActions}
        />
        {confirmDialog}
      </>
    );
  }

  if (!doc.canonical.exists) {
    return (
      <>
        <ContextFrame onKindChange={handleKindChange} onRefresh={() => void refresh()}>
          <SetupView />
        </ContextFrame>
        {confirmDialog}
      </>
    );
  }

  return (
    <>
      <EditorView
        doc={doc}
        refreshError={error}
        initialDriftSlug={clientParam}
        tab={tab}
        onTabChange={setTab}
        onKindChange={handleKindChange}
        onDirtyChange={setDirty}
        onRegisterActions={registerViewActions}
      />
      {confirmDialog}
    </>
  );
}
