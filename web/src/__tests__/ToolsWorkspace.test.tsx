import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import '@testing-library/jest-dom';
import { ToolsWorkspace } from '../components/workspaces/ToolsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useToolsDirtyStore } from '../stores/useToolsDirtyStore';
import { useUIStore } from '../stores/useUIStore';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import * as api from '../lib/api';
import type { MCPServerStatus, Tool } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function server(
  name: string,
  tools: string[],
  toolWhitelist?: string[],
): MCPServerStatus {
  return {
    name,
    transport: 'stdio',
    initialized: true,
    toolCount: tools.length,
    tools,
    toolWhitelist,
    healthy: true,
  } as unknown as MCPServerStatus;
}

function tool(prefixed: string, description?: string, inputSchema: Record<string, unknown> = {}): Tool {
  return { name: prefixed, description, inputSchema } as Tool;
}

const GITHUB = 'github';
const ATLAS = 'atlassian';

beforeEach(() => {
  // The workspace mounts the groups poll unconditionally; keep the test
  // hermetic instead of letting a real fetch fail in jsdom.
  vi.spyOn(api, 'fetchGroups').mockResolvedValue({ configured: false, groups: [] });
  // Same for the optimize poll behind the header's convergence line.
  vi.spyOn(api, 'fetchOptimizeReport').mockResolvedValue({
    findings: [],
    health_score: 100,
    generated_at: '2026-07-26T00:00:00Z',
  });
  // List facets persist in the UI store; reset so tests stay independent.
  useUIStore.setState({ toolsPrefs: { filter: 'all', sort: 'default', destructiveOnly: false } });
  // The workspace sources per-tool detail (descriptions, schemas, global
  // search) from the catalog, so seed it; `tools` is the MCP-facing list.
  const catalog = [
    tool(`${GITHUB}${TOOL_NAME_DELIMITER}create_issue`, 'Create a GitHub issue', {
      type: 'object',
      properties: { title: { type: 'string' } },
    }),
    tool(`${GITHUB}${TOOL_NAME_DELIMITER}list_repos`, 'List repositories'),
    tool(`${ATLAS}${TOOL_NAME_DELIMITER}get_page`, 'Get a Confluence page'),
  ];
  useStackStore.setState({
    isLoading: false,
    mcpServers: [
      // github: 1 of 2 whitelisted
      server(GITHUB, ['create_issue', 'list_repos'], ['create_issue']),
      // atlassian: empty whitelist = all exposed (1/1)
      server(ATLAS, ['get_page'], []),
    ],
    tools: catalog,
    toolCatalog: catalog,
  });
});

function renderAt(path = '/tools') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ToolsWorkspace />
    </MemoryRouter>,
  );
}

// Exposes the live location so tests can assert what the URL writers produce.
function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="loc">{loc.pathname + loc.search}</div>;
}

function renderWithProbe(path = '/tools') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ToolsWorkspace />
      <LocationProbe />
    </MemoryRouter>,
  );
}

describe('ToolsWorkspace', () => {
  it('renders a rail pill per server with an enabled/total badge', () => {
    renderAt();
    // github is curated 1/2; atlassian exposes all 1/1.
    expect(screen.getByText('1/2')).toBeInTheDocument();
    expect(screen.getByText('1/1')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /github/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /atlassian/i })).toBeInTheDocument();
  });

  it('deep-links to ?server= and seeds the per-tool checkbox from its whitelist', () => {
    renderAt('/tools?server=github');
    // The enable/disable state lives on the per-row checkbox, not the row.
    // github advertises create_issue + list_repos; whitelist is [create_issue].
    expect(screen.getByRole('checkbox', { name: /create_issue/i })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute(
      'aria-checked',
      'false',
    );
  });

  it('defaults to the first server (alphabetical) when no ?server= is given', () => {
    renderAt('/tools');
    // atlassian sorts before github → its tool is shown by default.
    expect(screen.getByRole('option', { name: /get_page/i })).toBeInTheDocument();
  });

  it('selecting a server in the rail switches the detail pane', () => {
    renderAt('/tools');
    fireEvent.click(screen.getByRole('button', { name: /github/i }));
    expect(screen.getByRole('option', { name: /create_issue/i })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /get_page/i })).not.toBeInTheDocument();
  });

  it('global search returns cross-server matches, each labeled with its parent server', () => {
    renderAt('/tools');
    const input = screen.getByPlaceholderText(/search tools across all/i);
    fireEvent.change(input, { target: { value: 'page' } });
    // The atlassian get_page tool surfaces with a parent-server label.
    expect(screen.getByText('get_page')).toBeInTheDocument();
    const result = screen.getByText('get_page').closest('button')!;
    expect(within(result).getByText(ATLAS)).toBeInTheDocument();
  });

  it('clicking a global search result selects that server and clears the search', () => {
    renderAt('/tools?server=atlassian');
    const input = screen.getByPlaceholderText(/search tools across all/i);
    fireEvent.change(input, { target: { value: 'issue' } });
    // The github create_issue tool matches across servers.
    const result = screen.getByText('create_issue').closest('button')!;
    fireEvent.click(result);
    // Search clears and the github detail pane is shown with its tools.
    expect(screen.getByRole('option', { name: /create_issue/i })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /list_repos/i })).toBeInTheDocument();
  });

  it('clicking the checkbox toggles exposure without selecting the row', () => {
    renderAt('/tools?server=github');
    // The panel starts empty (nothing selected).
    expect(screen.getByText(/select a tool to view/i)).toBeInTheDocument();
    // Toggling list_repos on flips its checkbox but does not open the panel.
    const checkbox = screen.getByRole('checkbox', { name: /list_repos/i });
    fireEvent.click(checkbox);
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(screen.getByText(/select a tool to view/i)).toBeInTheDocument();
  });

  it('selecting a tool row shows its schema in the detail panel', () => {
    renderAt('/tools?server=github');
    // Before selection the right rail prompts the user.
    expect(screen.getByText(/select a tool to view/i)).toBeInTheDocument();
    // Clicking the row body (the cmdk option) selects it for the panel.
    fireEvent.click(screen.getByRole('option', { name: /create_issue details/i }));
    // The panel renders the JSON schema via CodeViewer; the prompt is gone.
    expect(screen.getByLabelText('create_issue input schema')).toBeInTheDocument();
    expect(screen.queryByText(/select a tool to view/i)).not.toBeInTheDocument();
  });
});

describe('ToolsWorkspace — empty-save guard and discard', () => {
  it('Clear disables Save and switches the count line to danger copy', () => {
    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('button', { name: /clear all tools/i }));

    // The neutral "empty means all tools exposed" help text is replaced by a
    // blocking warning, and Save cannot fire.
    expect(screen.getByText(/cannot save an empty selection/i)).toBeInTheDocument();
    expect(screen.queryByText(/empty means all tools exposed/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /save 1 change & reload/i })).toBeDisabled();
  });

  it('Discard next to Save restores the saved selection in place', () => {
    renderAt('/tools?server=github');
    // Dirty the editor: enable the non-whitelisted tool.
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute(
      'aria-checked',
      'true',
    );

    fireEvent.click(screen.getByRole('button', { name: /discard unsaved tool changes/i }));

    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute(
      'aria-checked',
      'false',
    );
    // Clean again: the Discard affordance retracts.
    expect(
      screen.queryByRole('button', { name: /discard unsaved tool changes/i }),
    ).not.toBeInTheDocument();
  });

  it('marks the dirty server with an unsaved chip in the rail', () => {
    renderAt('/tools?server=github');
    expect(screen.queryByText('unsaved')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    expect(screen.getByText('unsaved')).toBeInTheDocument();
  });
});

describe('ToolsWorkspace — leave-workspace guard', () => {
  beforeEach(() => {
    useToolsDirtyStore.setState({ dirty: false, exitNavTarget: null });
  });

  it('mirrors the dirty flag and confirms an intercepted navigation', () => {
    renderAt('/tools?server=github');
    expect(useToolsDirtyStore.getState().dirty).toBe(false);

    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    expect(useToolsDirtyStore.getState().dirty).toBe(true);

    // The WorkspaceSwitcher stashes the intercepted target here; the
    // workspace renders the confirm for it.
    act(() => useToolsDirtyStore.getState().requestExitNav('/metrics'));
    expect(
      screen.getByText(/leave tools and discard unsaved tool changes/i),
    ).toBeInTheDocument();

    // Cancel keeps the draft.
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(
      screen.queryByText(/leave tools and discard unsaved tool changes/i),
    ).not.toBeInTheDocument();
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute(
      'aria-checked',
      'true',
    );

    // Confirm discards the draft and clears the intercepted target.
    act(() => useToolsDirtyStore.getState().requestExitNav('/metrics'));
    fireEvent.click(screen.getByRole('button', { name: /discard & leave/i }));
    expect(useToolsDirtyStore.getState().exitNavTarget).toBeNull();
    expect(useToolsDirtyStore.getState().dirty).toBe(false);
  });
});

describe('ToolsWorkspace — deep links', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('restores tool, audit mode, and window from the URL', async () => {
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(),
      servers: {},
    });

    renderAt('/tools?server=github&tool=create_issue&audit=1&window=30d');

    // Tool selection restored: the detail panel renders the schema.
    expect(screen.getByLabelText('create_issue input schema')).toBeInTheDocument();
    // Audit mode restored: rows classify once usage lands (create_issue has
    // no recorded calls → unused, in both its row and the open detail panel;
    // list_repos is not whitelisted → disabled).
    expect((await screen.findAllByText('unused')).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByRole('combobox', { name: /audit lookback window/i })).toHaveValue('30d');
  });
});

describe('ToolsWorkspace — URL writers', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('writes tool, audit, and window params, omitting defaults', () => {
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: new Date().toISOString(),
      servers: {},
    });
    renderWithProbe('/tools?server=github');
    const loc = () => screen.getByTestId('loc').textContent ?? '';

    // Selecting a row writes ?tool=.
    fireEvent.click(screen.getByRole('option', { name: /create_issue details/i }));
    expect(loc()).toContain('tool=create_issue');

    // Toggling Audit writes ?audit=1; the default 7d window stays omitted.
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));
    expect(loc()).toContain('audit=1');
    expect(loc()).not.toContain('window=');

    // A non-default window is written; returning to the default drops it.
    const windowSelect = () =>
      screen.getByRole('combobox', { name: /audit lookback window/i });
    fireEvent.change(windowSelect(), { target: { value: '30d' } });
    expect(loc()).toContain('window=30d');
    fireEvent.change(windowSelect(), { target: { value: '7d' } });
    expect(loc()).not.toContain('window=');

    // Toggling Audit off drops audit (and any window) from the URL.
    fireEvent.change(windowSelect(), { target: { value: '24h' } });
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));
    expect(loc()).not.toContain('audit=');
    expect(loc()).not.toContain('window=');

    // Switching servers clears the tool selection from the URL.
    fireEvent.click(screen.getByRole('option', { name: /create_issue details/i }));
    expect(loc()).toContain('tool=create_issue');
    fireEvent.click(screen.getByRole('button', { name: /atlassian/i }));
    expect(loc()).toContain('server=atlassian');
    expect(loc()).not.toContain('tool=');
  });

  it('self-heals a ?tool= that does not resolve on the active server', async () => {
    renderWithProbe('/tools?server=github&tool=no_such_tool');
    await waitFor(() =>
      expect(screen.getByTestId('loc').textContent).not.toContain('tool='),
    );
  });
});

describe('ToolsWorkspace — empty stack', () => {
  it('renders the empty state without crashing when there are no servers and the catalog is null', () => {
    // Regression: in stackless mode the catalog API returns {"tools": null}.
    // That null used to reach new Fuse(null) via useFuzzySearch and throw
    // during render, unmounting the whole app to a blank screen.
    useStackStore.setState({
      isLoading: false,
      mcpServers: [],
      tools: null as unknown as Tool[],
      toolCatalog: null as unknown as Tool[],
    });
    expect(() => renderAt('/tools')).not.toThrow();
    expect(screen.getByText(/no mcp servers yet/i)).toBeInTheDocument();
  });
});

describe('ToolsWorkspace — Audit Mode', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  const recent = () => new Date(Date.now() - 60 * 60 * 1000).toISOString(); // 1h ago
  const observedSince = () => new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString();

  it('toggling Audit Mode fetches usage and annotates rows by state', async () => {
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: observedSince(),
      servers: { github: { create_issue: { calls: 5, lastCalledAt: recent() } } },
    });

    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));

    // create_issue is exposed + recently used → "used".
    expect(await screen.findByText('used')).toBeInTheDocument();
    // list_repos is advertised but not whitelisted → "disabled".
    expect(screen.getByText('disabled')).toBeInTheDocument();
  });

  it('shows an unused-count rail badge for servers with idle exposed tools', async () => {
    // github's only exposed tool (create_issue) is recently used → 0 unused;
    // atlassian exposes get_page with no calls → 1 unused. Only atlassian
    // should carry the badge.
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: observedSince(),
      servers: { github: { create_issue: { calls: 5, lastCalledAt: recent() } } },
    });

    renderAt('/tools');
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));

    expect(await screen.findByText('1 unused')).toBeInTheDocument();
  });

  it('shows a visible error when the usage fetch fails', async () => {
    vi.spyOn(api, 'fetchToolUsage').mockRejectedValue(new Error('accumulator not configured'));

    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));

    expect(
      await screen.findByText(/tool usage unavailable: accumulator not configured/i),
    ).toBeInTheDocument();
  });

  it('shows a loading indicator until the first usage snapshot lands', () => {
    // A never-settling fetch pins the pre-snapshot state.
    vi.spyOn(api, 'fetchToolUsage').mockReturnValue(new Promise(() => {}));

    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));

    expect(screen.getByText(/loading usage/i)).toBeInTheDocument();
  });

  it('annotates global search results with audit state', async () => {
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: observedSince(),
      servers: { github: { create_issue: { calls: 5, lastCalledAt: recent() } } },
    });

    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));
    await screen.findByText('used');

    const input = screen.getByPlaceholderText(/search tools across all/i);
    fireEvent.change(input, { target: { value: 'create_issue' } });

    const result = screen.getByText('create_issue').closest('button')!;
    expect(within(result).getByText('used')).toBeInTheDocument();
  });

  it('remediation disables idle exposed tools through a single-server save', async () => {
    // gitlab exposes a + b (whitelist), advertises a third disabled tool c.
    const gitlabCatalog = [
      tool(`gitlab${TOOL_NAME_DELIMITER}a`),
      tool(`gitlab${TOOL_NAME_DELIMITER}b`),
      tool(`gitlab${TOOL_NAME_DELIMITER}c`),
    ];
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server('gitlab', ['a', 'b', 'c'], ['a', 'b'])],
      tools: gitlabCatalog,
      toolCatalog: gitlabCatalog,
    });
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: observedSince(),
      // a used recently; b exposed but idle → the remediation target.
      servers: { gitlab: { a: { calls: 9, lastCalledAt: recent() } } },
    });
    const saveSpy = vi
      .spyOn(api, 'setServerTools')
      .mockResolvedValue({ server: 'gitlab', tools: ['a'], reloaded: true });
    vi.spyOn(api, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'x', version: '1' },
      'mcp-servers': [],
    });
    vi.spyOn(api, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(api, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    renderAt('/tools?server=gitlab');
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));

    // The remediation affordance appears once usage loads.
    const disableBtn = await screen.findByRole('button', { name: /disable 1 unused tools/i });
    fireEvent.click(disableBtn);

    // Consequence-stating confirmation, then commit.
    const confirm = await screen.findByRole('button', { name: /disable & reload/i });
    fireEvent.click(confirm);

    // The idle tool (b) is dropped; the used tool (a) persists as the whitelist.
    await waitFor(() => expect(saveSpy).toHaveBeenCalledWith('gitlab', ['a']));
  });
});

describe('ToolsWorkspace — annotation chips', () => {
  it('renders chips for declared hints and nothing for undeclared tools', () => {
    const catalog = [
      {
        ...tool(`${GITHUB}${TOOL_NAME_DELIMITER}create_issue`, 'Create a GitHub issue'),
        annotations: { readOnlyHint: false, destructiveHint: true },
      },
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}list_repos`, 'List repositories'),
    ];
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, ['create_issue', 'list_repos'], [])],
      tools: catalog,
      toolCatalog: catalog,
    });

    renderAt('/tools?server=github');
    // Declared hints render as compact chips on the row.
    expect(screen.getByText('RW')).toBeInTheDocument();
    expect(screen.getByText('DESTR')).toBeInTheDocument();
    // The unannotated tool renders no chip vocabulary.
    const listRepos = screen.getByRole('option', { name: /list_repos details/i });
    expect(within(listRepos).queryByText('RO')).not.toBeInTheDocument();
    expect(within(listRepos).queryByText('DESTR')).not.toBeInTheDocument();
  });

  it('the Destructive facet narrows the list without Audit and writes ?risk=', () => {
    const catalog = [
      {
        ...tool(`${GITHUB}${TOOL_NAME_DELIMITER}create_issue`, 'Create a GitHub issue'),
        annotations: { destructiveHint: true },
      },
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}list_repos`, 'List repositories'),
    ];
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, ['create_issue', 'list_repos'], [])],
      tools: catalog,
      toolCatalog: catalog,
    });

    renderWithProbe('/tools?server=github');
    const loc = () => screen.getByTestId('loc').textContent ?? '';

    fireEvent.click(screen.getByRole('button', { name: 'Destructive 1' }));
    expect(loc()).toContain('risk=destructive');
    expect(screen.getByRole('option', { name: /create_issue details/i })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /list_repos details/i })).not.toBeInTheDocument();
    expect(screen.getByText('1 of 2 shown')).toBeInTheDocument();

    // Toggling it off restores the list and drops the param.
    fireEvent.click(screen.getByRole('button', { name: 'Destructive 1' }));
    expect(loc()).not.toContain('risk=');
    expect(screen.getByRole('option', { name: /list_repos details/i })).toBeInTheDocument();
  });
});

describe('ToolsWorkspace — audit filters and sort', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  const recent = () => new Date(Date.now() - 60 * 60 * 1000).toISOString();

  function seedTwoExposed() {
    const catalog = [
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}create_issue`, 'Create a GitHub issue'),
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}list_repos`, 'List repositories'),
    ];
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, ['create_issue', 'list_repos'], [])],
      tools: catalog,
      toolCatalog: catalog,
    });
  }

  it('filter chips narrow rows by audit state and write ?filter=', async () => {
    seedTwoExposed();
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: new Date().toISOString(),
      servers: { github: { create_issue: { calls: 5, lastCalledAt: recent() } } },
    });

    renderWithProbe('/tools?server=github&audit=1');
    const loc = () => screen.getByTestId('loc').textContent ?? '';

    // Chips carry per-state counts once classification lands.
    const unusedChip = await screen.findByRole('button', { name: 'Unused 1' });
    fireEvent.click(unusedChip);
    expect(loc()).toContain('filter=unused');
    expect(screen.getByRole('option', { name: /list_repos details/i })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /create_issue details/i })).not.toBeInTheDocument();

    // Back to All drops the param.
    fireEvent.click(screen.getByRole('button', { name: 'All 2' }));
    expect(loc()).not.toContain('filter=');
    expect(screen.getByRole('option', { name: /create_issue details/i })).toBeInTheDocument();
  });

  it('name sort orders rows alphabetically and writes ?sort=; default restores server order', () => {
    const catalog = [
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}zeta_tool`),
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}alpha_tool`),
    ];
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, ['zeta_tool', 'alpha_tool'], [])],
      tools: catalog,
      toolCatalog: catalog,
    });

    renderWithProbe('/tools?server=github');
    const loc = () => screen.getByTestId('loc').textContent ?? '';
    const list = () => screen.getByRole('listbox');
    const names = () =>
      within(list()).getAllByRole('option').map((el) => el.textContent?.match(/\w+_tool/)?.[0]);

    // Server-advertised order by default.
    expect(names()).toEqual(['zeta_tool', 'alpha_tool']);

    fireEvent.change(screen.getByRole('combobox', { name: /sort tools/i }), {
      target: { value: 'name' },
    });
    expect(loc()).toContain('sort=name');
    expect(names()).toEqual(['alpha_tool', 'zeta_tool']);

    fireEvent.change(screen.getByRole('combobox', { name: /sort tools/i }), {
      target: { value: 'default' },
    });
    expect(loc()).not.toContain('sort=');
    expect(names()).toEqual(['zeta_tool', 'alpha_tool']);
  });

  it('a usage sort fetches usage even with Audit off', async () => {
    seedTwoExposed();
    const usageSpy = vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: new Date().toISOString(),
      servers: { github: { list_repos: { calls: 12, lastCalledAt: recent() } } },
    });

    renderAt('/tools?server=github');
    expect(usageSpy).not.toHaveBeenCalled();

    fireEvent.change(screen.getByRole('combobox', { name: /sort tools/i }), {
      target: { value: 'calls' },
    });
    await waitFor(() => expect(usageSpy).toHaveBeenCalled());

    // Once the snapshot lands, the most-called tool floats to the top.
    await waitFor(() => {
      const list = screen.getByRole('listbox');
      const names = within(list)
        .getAllByRole('option')
        .map((el) => el.textContent?.match(/create_issue|list_repos/)?.[0]);
      expect(names).toEqual(['list_repos', 'create_issue']);
    });
  });

  it('applies a persisted sort preference when the URL carries no params', () => {
    const catalog = [
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}zeta_tool`),
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}alpha_tool`),
    ];
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, ['zeta_tool', 'alpha_tool'], [])],
      tools: catalog,
      toolCatalog: catalog,
    });
    useUIStore.setState({ toolsPrefs: { filter: 'all', sort: 'name', destructiveOnly: false } });

    renderAt('/tools?server=github');
    const list = screen.getByRole('listbox');
    const names = within(list)
      .getAllByRole('option')
      .map((el) => el.textContent?.match(/\w+_tool/)?.[0]);
    expect(names).toEqual(['alpha_tool', 'zeta_tool']);
  });
});

describe('ToolsWorkspace — optimize convergence line', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  const finding = (id: string) => ({
    id,
    heuristic: 'unused_tool',
    severity: 'info' as const,
    title: 'Unused tool',
    summary: 's',
    server: 'github',
    tool: id,
    remediation: '',
    detected_at: '2026-07-26T00:00:00Z',
  });

  it('shows the unused-tool count with a jump into Audit at 7d', async () => {
    vi.spyOn(api, 'fetchOptimizeReport').mockResolvedValue({
      findings: [finding('a'), finding('b')],
      health_score: 90,
      generated_at: '2026-07-26T00:00:00Z',
    });
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: new Date().toISOString(),
      servers: {},
    });

    renderWithProbe('/tools?server=github');
    expect(await screen.findByText(/unused tools \(7d\)/i)).toHaveTextContent(
      'Optimize suggests 2 unused tools (7d)',
    );

    fireEvent.click(screen.getByRole('button', { name: /review in audit/i }));
    const loc = screen.getByTestId('loc').textContent ?? '';
    expect(loc).toContain('audit=1');
    expect(loc).not.toContain('window=');
  });

  it('renders nothing (never "0 unused") for a need_more_data report', async () => {
    vi.spyOn(api, 'fetchOptimizeReport').mockResolvedValue({
      findings: [
        {
          id: 'info-need-more-data',
          heuristic: 'need_more_data',
          severity: 'info' as const,
          title: 'Need more data',
          summary: 'young gateway',
                remediation: '',
          detected_at: '2026-07-26T00:00:00Z',
        },
      ],
      health_score: 100,
      generated_at: '2026-07-26T00:00:00Z',
    });

    renderAt('/tools?server=github');
    await waitFor(() => expect(api.fetchOptimizeReport).toHaveBeenCalled());
    expect(screen.queryByText(/optimize suggests/i)).not.toBeInTheDocument();
  });
});

describe('ToolsWorkspace — draft rail counts', () => {
  it('the dirty server badge shows the draft selection instead of the live count', () => {
    renderAt('/tools?server=github');
    // Live: 1 of 2 whitelisted.
    expect(screen.getByText('1/2')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    // Draft: both selected; the badge follows the draft while dirty.
    expect(screen.getByText('2/2')).toBeInTheDocument();
    expect(screen.queryByText('1/2')).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /discard unsaved tool changes/i }));
    expect(screen.getByText('1/2')).toBeInTheDocument();
  });
});
