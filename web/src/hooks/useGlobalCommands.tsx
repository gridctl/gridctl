import { useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import { useLocation, useNavigate } from 'react-router';
import {
  Activity,
  Key,
  Library,
  Maximize2,
  RefreshCw,
  ZoomIn,
  ZoomOut,
  Eye,
  LayoutGrid,
  Flame,
  Server,
  FileCode,
  Globe,
  Plus,
  Sun,
  Moon,
  Monitor,
  Plug,
  RotateCcw,
  LogOut,
  SlidersHorizontal,
  Keyboard,
  Database,
  FileCog,
  Rows3,
  Table,
  Network,
} from 'lucide-react';
import type { ThemeMode } from '../themes/types';
import { MOD_KEY, SHIFT_KEY } from '../lib/platform';
import { useCommandRegistry } from './useCommandRegistry';
import { COMPACT_MODE_WORKSPACES, useUIStore } from '../stores/useUIStore';
import { useStackStore } from '../stores/useStackStore';
import { useTracesStore } from '../stores/useTracesStore';
import { useVaultStore } from '../stores/useVaultStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useWizardStore } from '../stores/useWizardStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import { useCanvasActionsStore } from '../stores/useCanvasActionsStore';
import { useShellActionsStore } from '../stores/useShellActionsStore';
import { requestInspectorLeave } from '../stores/useInspectorToolsGuardStore';
import { showToast } from '../components/ui/Toast';
import { navigateToWorkspace } from '../lib/workspaceNav';
import { buildWorkspaceNavCommands, buildWorkspaceViewCommands } from '../lib/paletteWorkspaceCommands';
import type { PaletteCommand } from '../types/palette';

interface GlobalCommandsOptions {
  onRefresh?: () => void;
}

export function useGlobalCommands({ onRefresh }: GlobalCommandsOptions = {}) {
  const { registerCommands, unregisterCommands } = useCommandRegistry();
  const rawNavigate = useNavigate();

  // Commands read the location when selected, not when registered, so URL
  // changes do not re-register them.
  const location = useLocation();
  const locationRef = useRef({ pathname: location.pathname, search: location.search });
  useLayoutEffect(() => {
    locationRef.current = { pathname: location.pathname, search: location.search };
  });

  const go = useCallback(
    (target: string, then?: () => void) =>
      navigateToWorkspace((to) => void rawNavigate(to), locationRef.current.pathname, target, then),
    [rawNavigate],
  );

  const toggleHeatMap = useUIStore((s) => s.toggleHeatMap);
  const toggleCompactCards = useUIStore((s) => s.toggleCompactCards);
  const toggleSpecMode = useUIStore((s) => s.toggleSpecMode);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const setThemeMode = useUIStore((s) => s.setThemeMode);
  const setPreferencesOpen = useUIStore((s) => s.setPreferencesOpen);
  const setShortcutsOpen = useUIStore((s) => s.setShortcutsOpen);
  const requestInspectorFocus = useUIStore((s) => s.requestInspectorFocus);

  const mcpServers = useStackStore((s) => s.mcpServers);
  const clients = useStackStore((s) => s.clients);
  const resources = useStackStore((s) => s.resources);
  const selectNode = useStackStore((s) => s.selectNode);

  const traces = useTracesStore((s) => s.traces);
  const secrets = useVaultStore((s) => s.variables);
  const skills = useRegistryStore((s) => s.skills);
  const credential = useAuthStore((s) => s.credential);

  // Navigation: one Go to command per workspace, then the Spec Editor and
  // Reset. Registered first so they lead Navigate To before any usage.
  useEffect(() => {
    const commands: PaletteCommand[] = [
      ...buildWorkspaceNavCommands((target) => go(target)),
      {
        id: 'navigate:library-context',
        label: 'Go to Global Context',
        section: 'global',
        icon: <Globe size={14} />,
        keywords: ['context', 'agents.md', 'rules', 'fragments', 'global', 'library'],
        onSelect: () => go('/library?kind=context'),
      },
      {
        id: 'navigate:spec',
        label: 'Open spec',
        section: 'global',
        icon: <FileCode size={14} />,
        keywords: ['spec', 'yaml', 'editor', 'config', 'open'],
        onSelect: () => go('/stack?spec=1'),
      },
      {
        id: 'system:reset-gridctl',
        label: 'Reset gridctl…',
        section: 'global',
        icon: <RotateCcw size={14} />,
        keywords: ['reset', 'purge', 'remove', 'uninstall', 'danger', 'factory', 'clean'],
        // The dialog lives in Connections; navigating first keeps one
        // mount point instead of a second dialog instance in the shell. It
        // opens only once the guarded navigation proceeds, or right away
        // when Connections is already open.
        onSelect: () => go('/connections', () => useUIStore.getState().setResetDialogOpen(true)),
      },
    ];
    registerCommands('navigation', commands);
    return () => unregisterCommands('navigation');
  }, [registerCommands, unregisterCommands, go]);

  // Workspace views reached through URL params. Registered before the
  // dynamic entries so they lead Current Context before any usage.
  useEffect(() => {
    registerCommands(
      'workspace-views',
      buildWorkspaceViewCommands({
        getLocation: () => locationRef.current,
        navigate: (to, options) => void rawNavigate(to, options),
      }),
    );
    return () => unregisterCommands('workspace-views');
  }, [registerCommands, unregisterCommands, rawNavigate]);

  // Shell actions available in every workspace (compact mode only where a
  // workspace reads it).
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'system:refresh',
        label: 'Refresh',
        section: 'global',
        icon: <RefreshCw size={14} />,
        shortcut: [MOD_KEY, SHIFT_KEY, 'R'],
        keywords: ['refresh', 'reload', 'update', 'sync', 'data', 'canvas'],
        onSelect: () => onRefresh?.(),
      },
      {
        id: 'system:reload-config',
        label: 'Reload Config',
        section: 'global',
        icon: <FileCog size={14} />,
        keywords: ['reload', 'config', 'stack', 'yaml', 'apply', 'spec'],
        onSelect: () => {
          const reload = useShellActionsStore.getState().reloadConfig;
          if (reload) reload();
          else showToast('warning', 'Reload Config is not available right now');
        },
      },
      {
        id: 'view:toggle-compact-mode',
        label: 'Toggle compact mode',
        section: 'global',
        workspaces: [...COMPACT_MODE_WORKSPACES],
        icon: <Rows3 size={14} />,
        shortcut: [MOD_KEY, '\\'],
        keywords: ['compact', 'density', 'dense', 'rows', 'toggle'],
        onSelect: () => {
          const ui = useUIStore.getState();
          ui.toggleCompactMode(ui.activeWorkspace);
        },
      },
    ];
    registerCommands('shell-actions', commands);
    return () => unregisterCommands('shell-actions');
  }, [registerCommands, unregisterCommands, onRefresh]);

  // Appearance commands mirror the header ThemePicker. Open Preferences is the same dialog.
  useEffect(() => {
    const set = (mode: ThemeMode) => () => setThemeMode(mode);
    const commands: PaletteCommand[] = [
      {
        id: 'appearance:light',
        label: 'Appearance: Use Light theme',
        section: 'global',
        icon: <Sun size={14} />,
        keywords: ['appearance', 'theme', 'light', 'day', 'observatory'],
        onSelect: set('light'),
      },
      {
        id: 'appearance:dark',
        label: 'Appearance: Use Dark theme',
        section: 'global',
        icon: <Moon size={14} />,
        keywords: ['appearance', 'theme', 'dark', 'night', 'obsidian'],
        onSelect: set('dark'),
      },
      {
        id: 'appearance:system',
        label: 'Appearance: Use System theme',
        section: 'global',
        icon: <Monitor size={14} />,
        keywords: ['appearance', 'theme', 'system', 'auto', 'os', 'preference'],
        onSelect: set('system'),
      },
      {
        id: 'preferences:open',
        label: 'Open Preferences',
        section: 'global',
        icon: <SlidersHorizontal size={14} />,
        keywords: ['settings', 'preferences', 'appearance', 'compact', 'text size', 'font'],
        onSelect: () => setPreferencesOpen(true),
      },
      {
        id: 'help:shortcuts',
        label: 'Keyboard shortcuts',
        section: 'global',
        icon: <Keyboard size={14} />,
        shortcut: ['?'],
        keywords: ['keyboard', 'shortcuts', 'help', 'keys', 'bindings'],
        onSelect: () => setShortcutsOpen(true),
      },
    ];
    registerCommands('appearance', commands);
    return () => unregisterCommands('appearance');
  }, [registerCommands, unregisterCommands, setThemeMode, setPreferencesOpen, setShortcutsOpen]);

  // Canvas actions and global actions
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'canvas:zoom-fit',
        label: 'Zoom to fit',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <Maximize2 size={14} />,
        shortcut: [MOD_KEY, '0'],
        keywords: ['zoom', 'fit', 'view', 'reset', 'all', 'fit view'],
        onSelect: () => useCanvasActionsStore.getState().fitView({ padding: 0.2, duration: 300 }),
      },
      {
        id: 'canvas:zoom-in',
        label: 'Zoom in',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <ZoomIn size={14} />,
        shortcut: [MOD_KEY, '+'],
        keywords: ['zoom', 'in', 'magnify', 'larger'],
        onSelect: () => useCanvasActionsStore.getState().zoomIn({ duration: 200 }),
      },
      {
        id: 'canvas:zoom-out',
        label: 'Zoom out',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <ZoomOut size={14} />,
        shortcut: [MOD_KEY, '−'],
        keywords: ['zoom', 'out', 'shrink', 'smaller'],
        onSelect: () => useCanvasActionsStore.getState().zoomOut({ duration: 200 }),
      },
      {
        id: 'canvas:toggle-heatmap',
        label: 'Toggle heatmap overlay',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <Flame size={14} />,
        keywords: ['heatmap', 'heat', 'overlay', 'tokens', 'usage', 'toggle'],
        onSelect: () => toggleHeatMap(),
      },
      {
        id: 'canvas:toggle-compact',
        label: 'Toggle compact cards',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <LayoutGrid size={14} />,
        keywords: ['compact', 'cards', 'nodes', 'size', 'dense', 'toggle'],
        onSelect: () => toggleCompactCards(),
      },
      {
        id: 'canvas:toggle-spec-mode',
        label: 'Toggle spec mode',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <Eye size={14} />,
        keywords: ['spec', 'mode', 'ghost', 'undeployed', 'preview', 'drift', 'diff', 'changes', 'diverged', 'toggle'],
        onSelect: () => toggleSpecMode(),
      },
      {
        id: 'canvas:view-table',
        label: 'Show Stack as table',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <Table size={14} />,
        keywords: ['table', 'list', 'rows', 'view', 'stack', 'servers'],
        onSelect: () => {
          // Access Lens keeps the graph; say so instead of saving a view that
          // would appear later with no feedback.
          if (useAccessLensStore.getState().enabled) {
            showToast('warning', 'Close Access Lens to use the table view');
            return;
          }
          useUIStore.getState().setStackPrefs({ view: 'table' });
        },
      },
      {
        id: 'canvas:view-graph',
        label: 'Show Stack as graph',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <Network size={14} />,
        keywords: ['graph', 'canvas', 'diagram', 'view', 'stack'],
        onSelect: () => useUIStore.getState().setStackPrefs({ view: 'graph' }),
      },
      {
        id: 'action:wizard',
        label: 'Create new',
        section: 'global',
        icon: <Plus size={14} />,
        keywords: ['wizard', 'create', 'new', 'server', 'add', 'resource'],
        onSelect: () => useWizardStore.getState().open(),
      },
    ];
    registerCommands('canvas-actions', commands);
    return () => unregisterCommands('canvas-actions');
  }, [
    registerCommands,
    unregisterCommands,
    toggleHeatMap,
    toggleCompactCards,
    toggleSpecMode,
  ]);

  useEffect(() => {
    if (!credential) {
      unregisterCommands('auth-forget');
      return;
    }
    registerCommands('auth-forget', [
      {
        id: 'auth:forget-credential',
        label: 'Forget gateway credential…',
        section: 'global',
        icon: <LogOut size={14} />,
        keywords: ['auth', 'credential', 'token', 'sign out', 'logout', 'forget'],
        onSelect: () => useUIStore.getState().setForgetCredentialOpen(true),
      },
    ]);
    return () => unregisterCommands('auth-forget');
  }, [credential, registerCommands, unregisterCommands]);

  // Dynamic node commands from stack store. Clients are limited to linked
  // ones, matching createClientNodes: a detected but unlinked client has no
  // canvas node, so inspecting it would open a blank inspector.
  useEffect(() => {
    const inspect = (nodeId: string, targetServer?: string) => {
      requestInspectorLeave(
        () => {
          selectNode(nodeId);
          setSidebarOpen(true);
          requestInspectorFocus(nodeId);
        },
        { targetServer },
      );
    };
    const serverCommands: PaletteCommand[] = (mcpServers ?? []).map((server) => ({
      id: `node:${server.name}`,
      label: `Inspect ${server.name}`,
      section: 'canvas' as const,
      workspaces: ['stack' as const],
      icon: <Server size={14} />,
      aliases: [server.name],
      keywords: [server.name, 'node', 'server', 'mcp', 'inspect'],
      onSelect: () => inspect(`mcp-${server.name}`, server.name),
    }));
    const clientCommands: PaletteCommand[] = (clients ?? [])
      .filter((client) => client.linked)
      .map((client) => ({
        id: `node:client:${client.slug}`,
        label: `Inspect ${client.name}`,
        section: 'canvas' as const,
        workspaces: ['stack' as const],
        icon: <Plug size={14} />,
        aliases: [client.name],
        keywords: [client.name, client.slug, 'node', 'client', 'inspect'],
        onSelect: () => inspect(`client-${client.slug}`),
      }));
    const resourceCommands: PaletteCommand[] = (resources ?? []).map((resource) => ({
      id: `node:resource:${resource.name}`,
      label: `Inspect ${resource.name}`,
      section: 'canvas' as const,
      workspaces: ['stack' as const],
      icon: <Database size={14} />,
      aliases: [resource.name],
      keywords: [resource.name, 'node', 'resource', 'inspect'],
      onSelect: () => inspect(`resource-${resource.name}`),
    }));
    registerCommands('dynamic-nodes', [...serverCommands, ...clientCommands, ...resourceCommands]);
    return () => unregisterCommands('dynamic-nodes');
  }, [
    mcpServers,
    clients,
    resources,
    registerCommands,
    unregisterCommands,
    selectNode,
    setSidebarOpen,
    requestInspectorFocus,
  ]);

  // Dynamic trace commands
  useEffect(() => {
    const commands: PaletteCommand[] = (traces ?? []).map((trace) => ({
      id: `trace:${trace.traceId}`,
      label: `Search traces: ${trace.operation} — ${trace.server}`,
      section: 'traces' as const,
      icon: <Activity size={14} />,
      keywords: [trace.traceId, trace.server, trace.operation, 'trace'],
      // TracesWorkspace reads ?trace= only on mount, so inside Traces the
      // store must be told directly or the URL and selection disagree.
      onSelect: () =>
        go(`/traces?trace=${encodeURIComponent(trace.traceId)}`, () =>
          useTracesStore.getState().selectTrace(trace.traceId),
        ),
    }));
    registerCommands('dynamic-traces', commands);
    return () => unregisterCommands('dynamic-traces');
  }, [traces, registerCommands, unregisterCommands, go]);

  // Dynamic variable store commands — both secrets and plaintext entries
  // surface here. The label distinguishes the two so the user knows what
  // kind of value will appear when the panel opens.
  useEffect(() => {
    const commands: PaletteCommand[] = (secrets ?? []).map((variable) => ({
      id: `vault:${variable.key}`,
      label: `Open variable: ${variable.key}${variable.is_secret ? '' : ' (plaintext)'}`,
      section: 'vault' as const,
      icon: <Key size={14} />,
      aliases: [variable.key],
      keywords: [
        variable.key,
        'variable',
        'vault',
        variable.is_secret ? 'secret' : 'plaintext',
        'env',
        variable.set ?? '',
      ],
      onSelect: () => go(`/vault?q=${encodeURIComponent(variable.key)}`),
    }));
    registerCommands('dynamic-vault', commands);
    return () => unregisterCommands('dynamic-vault');
  }, [secrets, registerCommands, unregisterCommands, go]);

  // Dynamic registry skill commands — open the Library workspace at the
  // skill's deep-link so the SkillEditor mounts.
  useEffect(() => {
    const commands: PaletteCommand[] = (skills ?? []).map((skill) => ({
      id: `skill:${skill.name}`,
      label: `View skill: ${skill.name}`,
      section: 'registry' as const,
      icon: <Library size={14} />,
      aliases: [skill.name],
      keywords: [skill.name, 'skill', 'library', 'registry', skill.description],
      onSelect: () => go(`/library/${encodeURIComponent(skill.name)}`),
    }));
    registerCommands('dynamic-skills', commands);
    return () => unregisterCommands('dynamic-skills');
  }, [skills, registerCommands, unregisterCommands, go]);
}
