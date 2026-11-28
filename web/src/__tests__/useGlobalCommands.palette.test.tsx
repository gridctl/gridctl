import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import '@testing-library/jest-dom';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import { ReactFlowProvider } from '@xyflow/react';
import type { ReactElement } from 'react';
import { CommandRegistryProvider, useCommandRegistry } from '../hooks/useCommandRegistry';
import { useGlobalCommands } from '../hooks/useGlobalCommands';
import { COMPACT_MODE_WORKSPACES, useUIStore } from '../stores/useUIStore';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import { useTracesStore } from '../stores/useTracesStore';
import { useVaultStore } from '../stores/useVaultStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useShellActionsStore } from '../stores/useShellActionsStore';
import { leavesWorkspace, useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';
import { CommandPalette } from '../components/palette/CommandPalette';
import { WORKSPACE_PALETTE_SECTION } from '../lib/paletteWorkspaceCommands';
import { MOD_KEY } from '../lib/platform';
import { WORKSPACE_CONFIG, WORKSPACES } from '../types/workspace';
import type { AgentSkill, GatewayStatus } from '../types';
import type { TraceSummary, Variable } from '../lib/api';

vi.mock('../components/ui/Toast', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../components/ui/Toast')>()),
  showToast: vi.fn(),
}));

import { showToast } from '../components/ui/Toast';

type Registry = ReturnType<typeof useCommandRegistry>;

let registry: Registry | null = null;
const onRefresh = vi.fn();

function Harness({ onReady }: { onReady: (r: Registry) => void }) {
  useGlobalCommands({ onRefresh });
  onReady(useCommandRegistry());
  return null;
}

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

async function renderAt(path = '/stack', { palette = false } = {}) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <ReactFlowProvider>
        <CommandRegistryProvider>
          <Harness onReady={(r) => { registry = r; }} />
          <LocationProbe />
          <NavigationGuardDialog />
          {palette && <CommandPalette isOpen onClose={() => {}} />}
        </CommandRegistryProvider>
      </ReactFlowProvider>
    </MemoryRouter>,
  );
  await waitFor(() => expect(registry?.commands.some((c) => c.id === 'skill:skill-01' || c.id === 'navigate:spec')).toBe(true));
  await waitFor(() => expect(registry?.commands.some((c) => c.id === 'tools:open-audit')).toBe(true));
}

function reg(): Registry {
  if (!registry) throw new Error('registry not ready');
  return registry;
}

function select(id: string) {
  const command = reg().commands.find((c) => c.id === id);
  expect(command, id).toBeDefined();
  act(() => command!.onSelect());
}

function location() {
  return screen.getByTestId('location').textContent;
}

function ids(list: { id: string }[]) {
  return list.map((c) => c.id);
}

function makeSkills(n: number): AgentSkill[] {
  return Array.from({ length: n }, (_, i) => ({
    name: `skill-${String(i + 1).padStart(2, '0')}`,
    description: `Skill number ${i + 1}`,
    state: 'active',
  })) as AgentSkill[];
}

function makeVariables(n: number): Variable[] {
  return Array.from({ length: n }, (_, i) => ({
    key: `VAR_${String(i + 1).padStart(2, '0')}`,
    type: 'string',
    is_secret: i % 2 === 0,
  })) as Variable[];
}

function makeTraces(n: number): TraceSummary[] {
  return Array.from({ length: n }, (_, i) => ({
    traceId: `trace${String(i + 1).padStart(2, '0')}`,
    operation: 'tools/call',
    server: 'github',
  })) as TraceSummary[];
}

function registerToolsDirty() {
  useNavigationGuardStore.getState().register({
    id: 'tools',
    label: 'Tools for b',
    blocks: (to) => leavesWorkspace(to, 'tools'),
  });
}

function resetStores() {
  localStorage.clear();
  registry = null;
  onRefresh.mockReset();
  vi.mocked(showToast).mockClear();
  useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
  useAuthStore.setState({ credential: null });
  useUIStore.setState({ resetDialogOpen: false, activeWorkspace: 'stack' });
  useUIStore.getState().setToolsPrefs({ destructiveOnly: false });
  useShellActionsStore.getState().clear();
  useRegistryStore.setState({ skills: [] });
  useVaultStore.setState({ variables: null });
  useTracesStore.setState({ traces: [], selectedTraceId: null });
  const status: GatewayStatus = {
    gateway: { name: 'gw', version: '0.0.0' },
    'mcp-servers': [{ name: 'github', transport: 'http', initialized: true, toolCount: 1, tools: ['search'] }],
  };
  useStackStore.getState().setGatewayStatus(status);
  useStackStore.getState().setClients([]);
}

describe('palette navigation commands', () => {
  beforeEach(resetStores);
  afterEach(resetStores);

  it('registers a Go to command for every workspace with its icon and shortcut', async () => {
    await renderAt();
    const commands = reg().commands;
    for (const ws of WORKSPACE_CONFIG) {
      const command = commands.find((c) => c.id === `navigate:workspace-${ws.id}`);
      expect(command, ws.id).toBeDefined();
      expect(command!.label).toBe(`Go to ${ws.label}`);
      expect(command!.section).toBe('global');
      expect((command!.icon as ReactElement).type).toBe(ws.icon);
      expect((command!.icon as ReactElement<{ size: number }>).props.size).toBe(14);
      expect(command!.shortcut).toEqual([MOD_KEY, ws.shortcutKey]);
      expect(command!.aliases).toEqual([...new Set([ws.label.toLowerCase(), ws.id])]);
    }
  });

  it('lists the Go to commands in switcher order before any usage', async () => {
    await renderAt();
    const nav = reg()
      .getSortedCommands(undefined, undefined, 'stack')
      .filter((c) => c.id.startsWith('navigate:'));
    expect(ids(nav)).toEqual([
      ...WORKSPACE_CONFIG.map((w) => `navigate:workspace-${w.id}`),
      'navigate:library-context',
      'navigate:spec',
    ]);
  });

  it('registers Go to Global Context and navigates to /library?kind=context', async () => {
    await renderAt('/stack');
    const cmd = reg().commands.find((c) => c.id === 'navigate:library-context');
    expect(cmd).toBeDefined();
    expect(cmd!.label).toBe('Go to Global Context');
    expect(cmd!.keywords).toEqual(expect.arrayContaining(['context', 'agents.md', 'rules', 'fragments', 'global']));
    act(() => cmd!.onSelect());
    expect(screen.getByTestId('location')).toHaveTextContent('/library?kind=context');
  });

  it.each(['stack', 'tools'] as const)('ranks Go to Tools first for "tools" in %s', async (ws) => {
    await renderAt(`/${ws}`);
    const results = reg().getSortedCommands('tools', undefined, ws);
    expect(results[0].id).toBe('navigate:workspace-tools');
    expect(ids(results)).toContain('tools:open-audit');
  });

  it('ranks Go to Logs above a more used Go to Library for "logs"', async () => {
    await renderAt();
    for (let i = 0; i < 5; i++) reg().recordUsage('navigate:workspace-library');
    const results = ids(reg().getSortedCommands('logs'));
    expect(results[0]).toBe('navigate:workspace-logs');
    expect(results).toContain('navigate:workspace-library');
  });

  it.each(['vault', 'variables'])('ranks Go to Variables first for "%s"', async (q) => {
    await renderAt();
    expect(reg().getSortedCommands(q)[0].id).toBe('navigate:workspace-vault');
  });

  it('ranks Refresh above Variables: Show Not Referenced for "ref"', async () => {
    await renderAt();
    const results = ids(reg().getSortedCommands('ref'));
    expect(results.indexOf('system:refresh')).toBeGreaterThanOrEqual(0);
    expect(results.indexOf('system:refresh')).toBeLessThan(results.indexOf('vault:show-unreferenced'));
  });

  it('puts every alias in its label or keywords', async () => {
    useRegistryStore.setState({ skills: makeSkills(2) });
    useVaultStore.setState({ variables: makeVariables(2) });
    useTracesStore.setState({ traces: makeTraces(2) });
    useStackStore.getState().setClients([
      { name: 'Claude', slug: 'claude', detected: true, linked: true, transport: 'http' },
    ]);
    await renderAt();
    await waitFor(() => expect(reg().commands.some((c) => c.id === 'skill:skill-02')).toBe(true));
    const withAliases = reg().commands.filter((c) => c.aliases?.length);
    expect(withAliases.length).toBeGreaterThan(WORKSPACE_CONFIG.length);
    for (const c of withAliases) {
      const text = [c.label, ...(c.keywords ?? [])].map((s) => s.toLowerCase());
      for (const alias of c.aliases!) {
        expect(text.some((t) => t.includes(alias.toLowerCase())), `${c.id}: ${alias}`).toBe(true);
      }
    }
  });
});

describe('palette Stack view commands', () => {
  beforeEach(resetStores);
  afterEach(resetStores);

  it('offers table and graph views in Stack only and writes the preference', async () => {
    useUIStore.setState({ stackPrefs: { view: null } });
    await renderAt();
    const inStack = ids(reg().getSortedCommands(undefined, undefined, 'stack'));
    expect(inStack).toContain('canvas:view-table');
    expect(inStack).toContain('canvas:view-graph');
    const inTools = ids(reg().getSortedCommands(undefined, undefined, 'tools'));
    expect(inTools).not.toContain('canvas:view-table');
    expect(inTools).not.toContain('canvas:view-graph');

    const find = (id: string) => reg().commands.find((c) => c.id === id)!;
    expect(find('canvas:view-table').label).toBe('Show Stack as table');
    expect(find('canvas:view-graph').label).toBe('Show Stack as graph');
    act(() => find('canvas:view-table').onSelect());
    expect(useUIStore.getState().stackPrefs.view).toBe('table');
    act(() => find('canvas:view-graph').onSelect());
    expect(useUIStore.getState().stackPrefs.view).toBe('graph');

    // Access Lens keeps the graph: the table command explains and saves nothing.
    useAccessLensStore.setState({ enabled: true });
    try {
      act(() => find('canvas:view-table').onSelect());
      expect(useUIStore.getState().stackPrefs.view).toBe('graph');
      expect(showToast).toHaveBeenCalledWith('warning', 'Close Access Lens to use the table view');
    } finally {
      useAccessLensStore.setState({ enabled: false });
    }
  });
});

describe('palette dynamic entries', () => {
  beforeEach(resetStores);
  afterEach(resetStores);

  it('registers every skill, variable, and trace and finds the last by name', async () => {
    useRegistryStore.setState({ skills: makeSkills(25) });
    useVaultStore.setState({ variables: makeVariables(35) });
    useTracesStore.setState({ traces: makeTraces(25) });
    await renderAt();
    await waitFor(() => expect(reg().commands.some((c) => c.id === 'skill:skill-25')).toBe(true));
    const commands = reg().commands;
    expect(commands.filter((c) => c.id.startsWith('skill:'))).toHaveLength(25);
    expect(commands.filter((c) => c.id.startsWith('vault:') && c.section === 'vault' && !c.id.startsWith('vault:show'))).toHaveLength(35);
    expect(commands.filter((c) => c.id.startsWith('trace:'))).toHaveLength(25);
    expect(reg().getSortedCommands('skill-25')[0].id).toBe('skill:skill-25');
    expect(reg().getSortedCommands('VAR_35')[0].id).toBe('vault:VAR_35');
    expect(reg().getSortedCommands('trace25')[0].id).toBe('trace:trace25');
  });

  it.each([
    ['v:', 'No variables loaded yet. Open the Variables workspace to load them.'],
    ['t:', 'No traces loaded yet. Open the Traces workspace to load the buffer.'],
  ])('explains a %s search before its store has loaded', async (prefix, text) => {
    await renderAt('/stack', { palette: true });
    fireEvent.change(screen.getByRole('combobox'), { target: { value: `${prefix}abc` } });
    expect(screen.getByText(text)).toBeInTheDocument();
  });

  it('keeps the view commands listed under the not loaded note', async () => {
    await renderAt('/stack', { palette: true });
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 't:' } });
    expect(screen.getByText(/No traces loaded yet/)).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /Traces: Show Runs/ })).toBeInTheDocument();
  });

  it.each([
    ['v:', () => useVaultStore.setState({ variables: makeVariables(1) }), 'vault:VAR_01', /No variables loaded yet/],
    ['t:', () => useTracesStore.setState({ traces: makeTraces(1) }), 'trace:trace01', /No traces loaded yet/],
  ])('keeps the generic %s text once its store has entries', async (prefix, load, id, unloaded) => {
    load();
    await renderAt('/stack', { palette: true });
    await waitFor(() => expect(reg().commands.some((c) => c.id === id)).toBe(true));
    fireEvent.change(screen.getByRole('combobox'), { target: { value: `${prefix}zzz` } });
    expect(screen.getByText(/No results for/)).toBeInTheDocument();
    expect(screen.queryByText(unloaded)).not.toBeInTheDocument();
  });

  it('selects the trace when Traces is already open', async () => {
    useTracesStore.setState({ traces: makeTraces(2), selectedTraceId: 'trace01' });
    await renderAt('/traces?trace=trace01');
    await waitFor(() => expect(reg().commands.some((c) => c.id === 'trace:trace02')).toBe(true));
    select('trace:trace02');
    expect(useTracesStore.getState().selectedTraceId).toBe('trace02');
    expect(location()).toBe('/traces?trace=trace02');
  });
});

describe('palette workspace context', () => {
  beforeEach(resetStores);
  afterEach(resetStores);

  it.each(['tools', 'metrics', 'traces', 'connections', 'vault'] as const)(
    'gives %s a non-empty Current Context with no canvas commands',
    async (ws) => {
      await renderAt(`/${ws}`);
      const context = reg().getSortedCommands(undefined, WORKSPACE_PALETTE_SECTION[ws], ws).slice(0, 6);
      expect(context.length).toBeGreaterThan(0);
      expect(context.some((c) => c.section === 'canvas')).toBe(false);
      expect(context.every((c) => c.id.startsWith(`${ws}:`))).toBe(true);
    },
  );
});

describe('palette deep-link commands', () => {
  beforeEach(resetStores);
  afterEach(resetStores);

  it.each([
    ['tools:open-audit', '/tools?audit=1'],
    ['tools:show-destructive', '/tools?risk=destructive'],
    ['tools:show-all-risk', '/tools?risk=all'],
    ['metrics:show-overview', '/metrics'],
    ['metrics:show-clients', '/metrics?scope=clients'],
    ['metrics:show-servers', '/metrics?scope=servers'],
    ['metrics:show-tools', '/metrics?scope=tools'],
    ['traces:show-traces', '/traces'],
    ['traces:show-runs', '/traces?view=runs'],
    ['connections:show-attention', '/connections?attention=1'],
    ['connections:show-all', '/connections'],
    ['vault:show-unreferenced', '/vault?ref=none'],
    ['vault:show-all', '/vault'],
  ])('%s from another workspace lands on %s', async (id, url) => {
    await renderAt('/pins?kind=skill');
    select(id);
    expect(location()).toBe(url);
  });

  it.each([
    ['/tools?server=b&tool=t', 'tools:open-audit', '/tools?server=b&tool=t&audit=1'],
    ['/metrics?range=1h&scope=servers&selected=github&q=x', 'metrics:show-clients', '/metrics?range=1h&scope=clients'],
    ['/metrics?range=1h&scope=servers&selected=github', 'metrics:show-overview', '/metrics?range=1h'],
    ['/metrics?selected=github&scope=clients', 'metrics:show-tools', '/metrics?scope=tools'],
    ['/traces?trace=abc&view=runs', 'traces:show-traces', '/traces?trace=abc'],
    ['/traces?trace=abc', 'traces:show-runs', '/traces?trace=abc&view=runs'],
    ['/connections?client=claude', 'connections:show-attention', '/connections?client=claude&attention=1'],
    ['/connections?client=claude&attention=1', 'connections:show-all', '/connections?client=claude'],
    ['/vault?q=API', 'vault:show-unreferenced', '/vault?q=API&ref=none'],
    ['/vault?q=API&ref=none', 'vault:show-all', '/vault?q=API'],
  ])('inside %s, %s changes only its params', async (start, id, url) => {
    await renderAt(start);
    select(id);
    expect(location()).toBe(url);
  });

  it('applies the risk toggle and its preference inside Tools', async () => {
    await renderAt('/tools?server=b&tool=t');
    select('tools:show-destructive');
    expect(location()).toBe('/tools?server=b&tool=t&risk=destructive');
    expect(useUIStore.getState().toolsPrefs.destructiveOnly).toBe(true);
    select('tools:show-all-risk');
    expect(location()).toBe('/tools?server=b&tool=t');
    expect(useUIStore.getState().toolsPrefs.destructiveOnly).toBe(false);
  });

  it('leaves the risk preference alone from another workspace', async () => {
    await renderAt('/stack');
    select('tools:show-destructive');
    expect(location()).toBe('/tools?risk=destructive');
    expect(useUIStore.getState().toolsPrefs.destructiveOnly).toBe(false);
  });

  it('keeps the risk preference when Show All Tools comes from elsewhere', async () => {
    useUIStore.getState().setToolsPrefs({ destructiveOnly: true });
    await renderAt('/logs');
    select('tools:show-all-risk');
    expect(location()).toBe('/tools?risk=all');
    expect(useUIStore.getState().toolsPrefs.destructiveOnly).toBe(true);
  });

  it('asks before leaving a dirty workspace and continues to the full target', async () => {
    await renderAt('/tools?server=b');
    registerToolsDirty();
    select('metrics:show-clients');
    expect(location()).toBe('/tools?server=b');
    expect(useNavigationGuardStore.getState().pending).not.toBeNull();
    act(() => useNavigationGuardStore.getState().confirm());
    expect(location()).toBe('/metrics?scope=clients');
  });
});

describe('palette shell commands', () => {
  beforeEach(resetStores);
  afterEach(resetStores);

  it('offers Refresh in every workspace and calls onRefresh', async () => {
    await renderAt();
    for (const ws of WORKSPACES) {
      expect(ids(reg().getSortedCommands(undefined, undefined, ws))).toContain('system:refresh');
    }
    expect(reg().commands.some((c) => c.id === 'canvas:refresh')).toBe(false);
    select('system:refresh');
    expect(onRefresh).toHaveBeenCalledOnce();
  });

  it('runs the registered Reload Config action, or warns when none is registered', async () => {
    await renderAt();
    select('system:reload-config');
    expect(showToast).toHaveBeenCalledWith('warning', expect.stringContaining('Reload Config'));
    const reload = vi.fn();
    useShellActionsStore.getState().register({ reloadConfig: reload });
    select('system:reload-config');
    expect(reload).toHaveBeenCalledOnce();
    expect(showToast).toHaveBeenCalledOnce();
  });

  it('offers Toggle compact mode only where compact mode exists and flips the active workspace', async () => {
    await renderAt();
    for (const ws of WORKSPACES) {
      const has = ids(reg().getSortedCommands(undefined, undefined, ws)).includes('view:toggle-compact-mode');
      expect(has, ws).toBe(COMPACT_MODE_WORKSPACES.includes(ws));
    }
    useUIStore.setState({ activeWorkspace: 'logs' });
    const before = useUIStore.getState().compactMode.logs;
    select('view:toggle-compact-mode');
    expect(useUIStore.getState().compactMode.logs).toBe(!before);
    expect(useUIStore.getState().compactMode.metrics).toBe(false);
    select('view:toggle-compact-mode');
    expect(useUIStore.getState().compactMode.logs).toBe(before);
  });
});

describe('palette navigation guard', () => {
  beforeEach(resetStores);
  afterEach(resetStores);

  it('holds Go to Stack behind a dirty Tools draft', async () => {
    await renderAt('/tools');
    registerToolsDirty();
    select('navigate:workspace-stack');
    expect(location()).toBe('/tools');
    expect(useNavigationGuardStore.getState().pending?.labels).toEqual(['Tools for b']);
  });

  it('holds Go to Library behind a dirty Access Lens draft', async () => {
    await renderAt('/stack');
    useNavigationGuardStore.getState().register({
      id: 'lens',
      label: 'Access Lens for claude',
      blocks: (to) => leavesWorkspace(to, 'stack'),
    });
    select('navigate:workspace-library');
    expect(location()).toBe('/stack');
    expect(useNavigationGuardStore.getState().pending?.labels).toEqual(['Access Lens for claude']);
  });

  it('navigates at once when nothing is dirty', async () => {
    await renderAt('/tools');
    select('navigate:workspace-stack');
    expect(location()).toBe('/stack');
    expect(useNavigationGuardStore.getState().pending).toBeNull();
  });

  it('keeps the URL when Go to Tools runs inside a dirty Tools', async () => {
    await renderAt('/tools?server=b');
    registerToolsDirty();
    select('navigate:workspace-tools');
    expect(location()).toBe('/tools?server=b');
    expect(useNavigationGuardStore.getState().pending).toBeNull();
  });

  it('keeps the URL when Go to Global Context runs inside the Context segment', async () => {
    await renderAt('/library?kind=context&fragment=00-default.md');
    select('navigate:library-context');
    expect(location()).toBe('/library?kind=context&fragment=00-default.md');
    expect(useNavigationGuardStore.getState().pending).toBeNull();
  });

  it('opens the reset dialog without navigating when Connections is open', async () => {
    await renderAt('/connections?client=claude');
    select('system:reset-gridctl');
    expect(useUIStore.getState().resetDialogOpen).toBe(true);
    expect(location()).toBe('/connections?client=claude');
  });
});
