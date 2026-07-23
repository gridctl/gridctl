import { useEffect } from 'react';
import { useReactFlow } from '@xyflow/react';
import { useNavigate } from 'react-router-dom';
import {
  Activity,
  DollarSign,
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
  LayoutGrid,
  Flame,
  Server,
  FileCode,
  Plus,
  Pin,
  Sun,
  Moon,
  Monitor,
} from 'lucide-react';
import type { ThemeMode } from '../themes/types';
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
  const navigate = useNavigate();

  const toggleHeatMap = useUIStore((s) => s.toggleHeatMap);
  const toggleCompactCards = useUIStore((s) => s.toggleCompactCards);
  const toggleSpecMode = useUIStore((s) => s.toggleSpecMode);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const setPricingManagerOpen = useUIStore((s) => s.setPricingManagerOpen);
  const setThemeMode = useUIStore((s) => s.setThemeMode);

  const mcpServers = useStackStore((s) => s.mcpServers);
  const selectNode = useStackStore((s) => s.selectNode);

  const traces = useTracesStore((s) => s.traces);
  const secrets = useVaultStore((s) => s.variables);
  const skills = useRegistryStore((s) => s.skills);

  // Static navigation commands
  useEffect(() => {
    const commands: PaletteCommand[] = [
      // Cross-workspace navigation — visible in every workspace.
      {
        id: 'navigate:workspace-stack',
        label: 'Go to Stack',
        section: 'global',
        icon: <Layers size={14} />,
        shortcut: ['⌘', '1'],
        keywords: ['stack', 'topology', 'workspace', 'operator', 'graph', 'go'],
        onSelect: () => navigate('/stack'),
      },
      {
        id: 'navigate:workspace-library',
        label: 'Go to Library',
        section: 'global',
        icon: <Library size={14} />,
        shortcut: ['⌘', '2'],
        keywords: ['library', 'workspace', 'registry', 'catalog', 'skills', 'go'],
        onSelect: () => navigate('/library'),
      },
      {
        id: 'navigate:traces',
        label: 'Open Traces',
        section: 'global',
        icon: <Activity size={14} />,
        shortcut: ['⌘', '8'],
        keywords: ['traces', 'activity', 'spans', 'calls', 'workspace', 'open'],
        onSelect: () => navigate('/traces'),
      },
      {
        id: 'navigate:logs',
        label: 'Open Logs',
        section: 'global',
        icon: <Terminal size={14} />,
        shortcut: ['⌘', '7'],
        keywords: ['logs', 'output', 'console', 'stream', 'workspace', 'open'],
        onSelect: () => navigate('/logs'),
      },
      {
        id: 'navigate:metrics',
        label: 'Open Metrics',
        section: 'global',
        icon: <BarChart2 size={14} />,
        keywords: ['metrics', 'stats', 'tokens', 'usage', 'cost', 'charts', 'dashboard', 'open'],
        onSelect: () => navigate('/metrics'),
      },
      {
        id: 'navigate:workspace-pins',
        label: 'Go to Pins',
        section: 'global',
        icon: <Pin size={14} />,
        shortcut: ['⌘', '6'],
        keywords: ['pins', 'workspace', 'drift', 'schema', 'tofu', 'approve', 'go'],
        onSelect: () => navigate('/pins'),
      },
      {
        id: 'navigate:spec',
        label: 'Open Spec Editor',
        section: 'global',
        icon: <FileCode size={14} />,
        keywords: ['spec', 'yaml', 'editor', 'config', 'open'],
        onSelect: () => navigate('/stack?spec=1'),
      },
      {
        id: 'navigate:vault',
        label: 'Open Variables',
        section: 'global',
        icon: <Key size={14} />,
        shortcut: ['⌘', '3'],
        keywords: ['variables', 'vault', 'secrets', 'keys', 'env', 'workspace', 'open'],
        onSelect: () => navigate('/vault'),
      },
    ];
    registerCommands('navigation', commands);
    return () => unregisterCommands('navigation');
  }, [registerCommands, unregisterCommands, navigate]);

  // Appearance — theme switching, mirrors the StatusBar ThemePicker.
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
    ];
    registerCommands('appearance', commands);
    return () => unregisterCommands('appearance');
  }, [registerCommands, unregisterCommands, setThemeMode]);

  // Canvas actions and global actions
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'canvas:zoom-fit',
        label: 'Zoom to fit',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <Maximize2 size={14} />,
        shortcut: ['⌘', '0'],
        keywords: ['zoom', 'fit', 'view', 'reset', 'all', 'fit view'],
        onSelect: () => fitView({ padding: 0.2, duration: 300 }),
      },
      {
        id: 'canvas:zoom-in',
        label: 'Zoom in',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <ZoomIn size={14} />,
        shortcut: ['⌘', '+'],
        keywords: ['zoom', 'in', 'magnify', 'larger'],
        onSelect: () => zoomIn({ duration: 200 }),
      },
      {
        id: 'canvas:zoom-out',
        label: 'Zoom out',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <ZoomOut size={14} />,
        shortcut: ['⌘', '−'],
        keywords: ['zoom', 'out', 'shrink', 'smaller'],
        onSelect: () => zoomOut({ duration: 200 }),
      },
      {
        id: 'canvas:refresh',
        label: 'Refresh canvas',
        section: 'canvas',
        workspaces: ['stack'],
        icon: <RefreshCw size={14} />,
        shortcut: ['⌘', '⇧', 'R'],
        keywords: ['refresh', 'reload', 'update', 'sync', 'data'],
        onSelect: () => onRefresh?.(),
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
        id: 'action:wizard',
        label: 'Open server wizard',
        section: 'global',
        icon: <Plus size={14} />,
        keywords: ['wizard', 'create', 'new', 'server', 'add', 'resource'],
        onSelect: () => useWizardStore.getState().open(),
      },
      {
        id: 'action:pricing-models',
        label: 'Edit pricing models',
        section: 'global',
        icon: <DollarSign size={14} />,
        keywords: ['pricing', 'cost', 'model', 'models', 'attribution', 'usd', 'edit'],
        onSelect: () => {
          // The manager mounts in the Stack workspace's canvas column.
          navigate('/stack');
          setPricingManagerOpen(true);
        },
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
    toggleHeatMap,
    toggleCompactCards,
    toggleSpecMode,
    setPricingManagerOpen,
    navigate,
    onRefresh,
  ]);

  // Dynamic node commands from stack store
  useEffect(() => {
    const commands: PaletteCommand[] = (mcpServers ?? []).map((server) => ({
      id: `node:${server.name}`,
      label: `View node: ${server.name}`,
      section: 'canvas' as const,
      workspaces: ['stack' as const],
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
      onSelect: () => navigate(`/traces?trace=${encodeURIComponent(trace.traceId)}`),
    }));
    registerCommands('dynamic-traces', commands);
    return () => unregisterCommands('dynamic-traces');
  }, [traces, registerCommands, unregisterCommands, navigate]);

  // Dynamic variable store commands — both secrets and plaintext entries
  // surface here. The label distinguishes the two so the user knows what
  // kind of value will appear when the panel opens.
  useEffect(() => {
    const commands: PaletteCommand[] = (secrets ?? []).slice(0, 30).map((variable) => ({
      id: `vault:${variable.key}`,
      label: `Open variable: ${variable.key}${variable.is_secret ? '' : ' (plaintext)'}`,
      section: 'vault' as const,
      icon: <Key size={14} />,
      keywords: [
        variable.key,
        'variable',
        'vault',
        variable.is_secret ? 'secret' : 'plaintext',
        'env',
        variable.set ?? '',
      ],
      onSelect: () => navigate(`/vault?q=${encodeURIComponent(variable.key)}`),
    }));
    registerCommands('dynamic-vault', commands);
    return () => unregisterCommands('dynamic-vault');
  }, [secrets, registerCommands, unregisterCommands, navigate]);

  // Dynamic registry skill commands — open the Library workspace at the
  // skill's deep-link so the SkillEditor mounts.
  useEffect(() => {
    const commands: PaletteCommand[] = (skills ?? []).slice(0, 20).map((skill) => ({
      id: `skill:${skill.name}`,
      label: `View skill: ${skill.name}`,
      section: 'registry' as const,
      icon: <Library size={14} />,
      keywords: [skill.name, 'skill', 'library', 'registry', skill.description],
      onSelect: () => {
        navigate(`/library/${encodeURIComponent(skill.name)}`);
      },
    }));
    registerCommands('dynamic-skills', commands);
    return () => unregisterCommands('dynamic-skills');
  }, [skills, registerCommands, unregisterCommands, navigate]);
}
