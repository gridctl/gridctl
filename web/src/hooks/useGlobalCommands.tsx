import { useEffect } from 'react';
import { useReactFlow } from '@xyflow/react';
import {
  Activity,
  Key,
  Library,
  Terminal,
  BarChart2,
  Layers,
  Maximize2,
  RefreshCw,
  ZoomIn,
  ZoomOut,
  Eye,
  GitBranch,
  LayoutGrid,
  Flame,
  Server,
  FileCode,
  Plus,
  PanelBottom,
} from 'lucide-react';
import { useCommandRegistry } from './useCommandRegistry';
import { useUIStore } from '../stores/useUIStore';
import { useStackStore } from '../stores/useStackStore';
import { useTracesStore } from '../stores/useTracesStore';
import { useVaultStore } from '../stores/useVaultStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useWizardStore } from '../stores/useWizardStore';
import type { PaletteCommand } from '../types/palette';

interface GlobalCommandsOptions {
  onRefresh?: () => void;
}

export function useGlobalCommands({ onRefresh }: GlobalCommandsOptions = {}) {
  const { fitView, zoomIn, zoomOut } = useReactFlow();
  const { registerCommands, unregisterCommands } = useCommandRegistry();

  const setBottomPanelTab = useUIStore((s) => s.setBottomPanelTab);
  const setBottomPanelOpen = useUIStore((s) => s.setBottomPanelOpen);
  const toggleBottomPanel = useUIStore((s) => s.toggleBottomPanel);
  const toggleHeatMap = useUIStore((s) => s.toggleHeatMap);
  const toggleDriftOverlay = useUIStore((s) => s.toggleDriftOverlay);
  const toggleCompactCards = useUIStore((s) => s.toggleCompactCards);
  const toggleSpecMode = useUIStore((s) => s.toggleSpecMode);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const setShowVault = useUIStore((s) => s.setShowVault);

  const mcpServers = useStackStore((s) => s.mcpServers);
  const selectNode = useStackStore((s) => s.selectNode);

  const traces = useTracesStore((s) => s.traces);
  const secrets = useVaultStore((s) => s.secrets);
  const skills = useRegistryStore((s) => s.skills);

  // Static navigation commands
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'navigate:canvas',
        label: 'Go to Canvas',
        section: 'global',
        icon: <Layers size={14} />,
        keywords: ['canvas', 'graph', 'topology', 'nodes', 'home', 'main'],
        onSelect: () => setBottomPanelOpen(false),
      },
      {
        id: 'navigate:traces',
        label: 'Open Traces',
        section: 'global',
        icon: <Activity size={14} />,
        keywords: ['traces', 'activity', 'spans', 'calls', 'open'],
        onSelect: () => setBottomPanelTab('traces'),
      },
      {
        id: 'navigate:logs',
        label: 'Open Logs',
        section: 'global',
        icon: <Terminal size={14} />,
        keywords: ['logs', 'output', 'console', 'stream', 'open'],
        onSelect: () => setBottomPanelTab('logs'),
      },
      {
        id: 'navigate:metrics',
        label: 'Open Metrics',
        section: 'global',
        icon: <BarChart2 size={14} />,
        keywords: ['metrics', 'stats', 'tokens', 'usage', 'charts', 'open'],
        onSelect: () => setBottomPanelTab('metrics'),
      },
      {
        id: 'navigate:spec',
        label: 'Open Spec Editor',
        section: 'global',
        icon: <FileCode size={14} />,
        keywords: ['spec', 'yaml', 'editor', 'config', 'open'],
        onSelect: () => setBottomPanelTab('spec'),
      },
      {
        id: 'navigate:vault',
        label: 'Open Vault',
        section: 'global',
        icon: <Key size={14} />,
        keywords: ['vault', 'secrets', 'keys', 'env', 'open'],
        onSelect: () => setShowVault(true),
      },
    ];
    registerCommands('navigation', commands);
    return () => unregisterCommands('navigation');
  }, [registerCommands, unregisterCommands, setBottomPanelTab, setBottomPanelOpen, setShowVault]);

  // Canvas actions and global actions
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'canvas:zoom-fit',
        label: 'Zoom to fit',
        section: 'canvas',
        icon: <Maximize2 size={14} />,
        shortcut: ['⌘', '0'],
        keywords: ['zoom', 'fit', 'view', 'reset', 'all', 'fit view'],
        onSelect: () => fitView({ padding: 0.2, duration: 300 }),
      },
      {
        id: 'canvas:zoom-in',
        label: 'Zoom in',
        section: 'canvas',
        icon: <ZoomIn size={14} />,
        shortcut: ['⌘', '+'],
        keywords: ['zoom', 'in', 'magnify', 'larger'],
        onSelect: () => zoomIn({ duration: 200 }),
      },
      {
        id: 'canvas:zoom-out',
        label: 'Zoom out',
        section: 'canvas',
        icon: <ZoomOut size={14} />,
        shortcut: ['⌘', '−'],
        keywords: ['zoom', 'out', 'shrink', 'smaller'],
        onSelect: () => zoomOut({ duration: 200 }),
      },
      {
        id: 'canvas:refresh',
        label: 'Refresh canvas',
        section: 'canvas',
        icon: <RefreshCw size={14} />,
        shortcut: ['⌘', '⇧', 'R'],
        keywords: ['refresh', 'reload', 'update', 'sync', 'data'],
        onSelect: () => onRefresh?.(),
      },
      {
        id: 'canvas:toggle-panel',
        label: 'Toggle bottom panel',
        section: 'canvas',
        icon: <PanelBottom size={14} />,
        shortcut: ['⌘', 'J'],
        keywords: ['panel', 'bottom', 'toggle', 'hide', 'show', 'collapse'],
        onSelect: () => toggleBottomPanel(),
      },
      {
        id: 'canvas:toggle-heatmap',
        label: 'Toggle heatmap overlay',
        section: 'canvas',
        icon: <Flame size={14} />,
        keywords: ['heatmap', 'heat', 'overlay', 'tokens', 'usage', 'toggle'],
        onSelect: () => toggleHeatMap(),
      },
      {
        id: 'canvas:toggle-drift',
        label: 'Toggle drift overlay',
        section: 'canvas',
        icon: <GitBranch size={14} />,
        keywords: ['drift', 'overlay', 'diff', 'changes', 'diverged', 'toggle'],
        onSelect: () => toggleDriftOverlay(),
      },
      {
        id: 'canvas:toggle-compact',
        label: 'Toggle compact cards',
        section: 'canvas',
        icon: <LayoutGrid size={14} />,
        keywords: ['compact', 'cards', 'nodes', 'size', 'dense', 'toggle'],
        onSelect: () => toggleCompactCards(),
      },
      {
        id: 'canvas:toggle-spec-mode',
        label: 'Toggle spec mode',
        section: 'canvas',
        icon: <Eye size={14} />,
        keywords: ['spec', 'mode', 'ghost', 'undeployed', 'preview', 'toggle'],
        onSelect: () => toggleSpecMode(),
      },
      {
        id: 'action:wizard',
        label: 'Open server wizard',
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
    fitView,
    zoomIn,
    zoomOut,
    toggleBottomPanel,
    toggleHeatMap,
    toggleDriftOverlay,
    toggleCompactCards,
    toggleSpecMode,
    onRefresh,
  ]);

  // Dynamic node commands from stack store
  useEffect(() => {
    const commands: PaletteCommand[] = (mcpServers ?? []).map((server) => ({
      id: `node:${server.name}`,
      label: `View node: ${server.name}`,
      section: 'canvas' as const,
      icon: <Server size={14} />,
      keywords: [server.name, 'node', 'server', 'mcp'],
      onSelect: () => {
        selectNode(server.name);
        setSidebarOpen(true);
      },
    }));
    registerCommands('dynamic-nodes', commands);
    return () => unregisterCommands('dynamic-nodes');
  }, [mcpServers, registerCommands, unregisterCommands, selectNode, setSidebarOpen]);

  // Dynamic trace commands
  useEffect(() => {
    const commands: PaletteCommand[] = (traces ?? []).slice(0, 20).map((trace) => ({
      id: `trace:${trace.traceId}`,
      label: `Search traces: ${trace.operation} — ${trace.server}`,
      section: 'traces' as const,
      icon: <Activity size={14} />,
      keywords: [trace.traceId, trace.server, trace.operation, 'trace'],
      onSelect: () => setBottomPanelTab('traces'),
    }));
    registerCommands('dynamic-traces', commands);
    return () => unregisterCommands('dynamic-traces');
  }, [traces, registerCommands, unregisterCommands, setBottomPanelTab]);

  // Dynamic vault secret commands
  useEffect(() => {
    const commands: PaletteCommand[] = (secrets ?? []).slice(0, 30).map((secret) => ({
      id: `vault:${secret.key}`,
      label: `Open secret: ${secret.key}`,
      section: 'vault' as const,
      icon: <Key size={14} />,
      keywords: [secret.key, 'vault', 'secret', 'env', secret.set ?? ''],
      onSelect: () => setShowVault(true),
    }));
    registerCommands('dynamic-vault', commands);
    return () => unregisterCommands('dynamic-vault');
  }, [secrets, registerCommands, unregisterCommands, setShowVault]);

  // Dynamic registry skill commands
  useEffect(() => {
    const commands: PaletteCommand[] = (skills ?? []).slice(0, 20).map((skill) => ({
      id: `skill:${skill.name}`,
      label: `View skill: ${skill.name}`,
      section: 'registry' as const,
      icon: <Library size={14} />,
      keywords: [skill.name, 'skill', 'registry', skill.description],
      onSelect: () => {
        // Registry panel integration — open bottom panel to spec for now
        setBottomPanelTab('spec');
      },
    }));
    registerCommands('dynamic-skills', commands);
    return () => unregisterCommands('dynamic-skills');
  }, [skills, registerCommands, unregisterCommands, setBottomPanelTab]);
}
