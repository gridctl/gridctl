import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, useLocation, useNavigate } from 'react-router';
import '@testing-library/jest-dom';
import { ToolsWorkspace } from '../components/workspaces/ToolsWorkspace';
import { requestGatewayPoll } from '../hooks/usePolling';
import { useStackStore } from '../stores/useStackStore';
import { useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { usePinsStore } from '../stores/usePinsStore';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';
import { useUIStore } from '../stores/useUIStore';
import { useToolCallHistoryStore } from '../stores/useToolCallHistoryStore';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import * as api from '../lib/api';
import type { MCPServerStatus, Tool } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
vi.mock('../hooks/usePolling', () => ({
  requestGatewayPoll: vi.fn(),
  usePolling: () => ({ refresh: vi.fn() }),
}));

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
  vi.spyOn(api, 'fetchPolicy').mockResolvedValue({ configured: false, rules: [], tools: {} });
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
    error: null,
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
  it('renders a failed state instead of the teaching empty state when the poll failed', () => {
    useStackStore.setState({
      mcpServers: [],
      isLoading: false,
      error: 'Gateway connection unavailable',
    });
    renderAt();
    expect(screen.getByText('Tools unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No MCP servers yet')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(requestGatewayPoll).toHaveBeenCalled();
  });

  it('keeps the editor mounted and shows a banner when a later poll fails', () => {
    useStackStore.setState({ error: 'Gateway connection unavailable' });
    renderAt('/tools?server=github');
    expect(screen.getByRole('alert')).toHaveTextContent('Gateway connection unavailable');
    expect(screen.getByText('create_issue')).toBeInTheDocument();
    expect(screen.queryByText('Tools unavailable')).not.toBeInTheDocument();
  });

  it('renders a BLOCKED chip and detail text from GET /api/policy', async () => {
    vi.mocked(api.fetchPolicy).mockResolvedValue({
      configured: true,
      default: 'allow',
      block_destructive: false,
      rules: [{ index: 0, match: 'github__*', action: 'block' }],
      tools: {
        [`${GITHUB}${TOOL_NAME_DELIMITER}create_issue`]: {
          action: 'block',
          source: 'rule',
          rule_index: 0,
          rule: 'github__*',
          exposed: true,
        },
      },
    });
    renderAt('/tools?server=github');
    const chip = await screen.findByText('BLOCKED');
    expect(chip).toHaveAttribute('title', 'blocked by policy rule 1: github__*');
    fireEvent.click(screen.getByRole('option', { name: /create_issue/i }));
    expect(await screen.findByRole('heading', { name: 'Policy' })).toBeInTheDocument();
    expect(screen.getByText(/blocked by policy rule 1: github__\*/)).toBeInTheDocument();
  });

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
    const result = screen.getByRole('option', { name: /get_page/ });
    expect(within(result).getByText(ATLAS)).toBeInTheDocument();
  });

  it('picking a global result opens the tool and offers Back to results', () => {
    renderWithProbe('/tools?server=atlassian');
    const loc = () => screen.getByTestId('loc').textContent ?? '';
    const input = screen.getByPlaceholderText(/search tools across all/i);
    fireEvent.change(input, { target: { value: 'issue' } });
    // The github create_issue tool matches across servers.
    fireEvent.click(screen.getByRole('option', { name: /create_issue/ }));
    // The query moves to ?back= and the github detail pane is shown.
    const params = new URLSearchParams(loc().split('?')[1]);
    expect(params.get('server')).toBe(GITHUB);
    expect(params.get('tool')).toBe('create_issue');
    expect(params.get('back')).toBe('issue');
    expect(params.has('q')).toBe(false);
    expect(screen.getByRole('option', { name: /list_repos/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /back to results for "issue"/i })).toBeInTheDocument();
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
    fireEvent.click(screen.getByRole('option', { name: /^create_issue, / }));
    // The panel summarizes the schema; the raw JSON sits behind a disclosure.
    expect(screen.getByRole('table', { name: 'create_issue parameters' })).toBeInTheDocument();
    expect(screen.queryByLabelText('create_issue input schema')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Raw schema' }));
    expect(screen.getByLabelText('create_issue input schema')).toBeInTheDocument();
    expect(screen.queryByText(/select a tool to view/i)).not.toBeInTheDocument();
  });
});

describe('ToolsWorkspace — empty-save guard and discard', () => {
  it('Clear disables Save and switches the count line to danger copy', () => {
    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('button', { name: /clear all tools/i }));

    // The neutral count sentence is replaced by a blocking alert, and Save
    // cannot fire.
    expect(screen.getByRole('alert')).toHaveTextContent(/cannot save an empty selection/i);
    expect(
      screen.queryByText(/selected \(unsaved\)|^\d+ of \d+ exposed$|exposed \(no allow-list\)/),
    ).not.toBeInTheDocument();
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

// Stands in for a shell control that navigates through the shared guard.
function GuardedNavProbe({ to }: { to: string }) {
  const navigate = useNavigate();
  return (
    <button
      type="button"
      data-testid={`guarded-nav-${to}`}
      onClick={() => useNavigationGuardStore.getState().guard(to, () => navigate(to))}
    />
  );
}

function renderGuardedAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ToolsWorkspace />
      <NavigationGuardDialog />
      <GuardedNavProbe to="/metrics" />
      <GuardedNavProbe to="/tools?server=atlassian" />
      <LocationProbe />
    </MemoryRouter>,
  );
}

describe('ToolsWorkspace — leave-workspace guard', () => {
  beforeEach(() => {
    useNavigationGuardStore.setState({ guards: [], pending: null });
  });
  afterEach(() => {
    useNavigationGuardStore.setState({ guards: [], pending: null });
  });

  it('registers a guard only while the draft is dirty', () => {
    renderGuardedAt('/tools?server=github');
    expect(useNavigationGuardStore.getState().guards).toHaveLength(0);

    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    expect(useNavigationGuardStore.getState().guards).toHaveLength(1);

    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    expect(useNavigationGuardStore.getState().guards).toHaveLength(0);
  });

  it('blocks leaving Tools until confirmed, then restores the baseline', () => {
    renderGuardedAt('/tools?server=github');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));

    fireEvent.click(screen.getByTestId('guarded-nav-/metrics'));
    expect(screen.getByTestId('loc')).toHaveTextContent('/tools?server=github');
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Tools for github');

    // Cancel keeps the route and the draft.
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByTestId('loc')).toHaveTextContent('/tools?server=github');
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute(
      'aria-checked',
      'true',
    );

    // Confirm discards the draft and lands on the destination.
    fireEvent.click(screen.getByTestId('guarded-nav-/metrics'));
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(screen.getByTestId('loc')).toHaveTextContent('/metrics');
    expect(useNavigationGuardStore.getState().guards).toHaveLength(0);
  });

  it('restores the checkbox baseline when discarding', () => {
    renderGuardedAt('/tools?server=github');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    // Run the guard's discard without leaving, so the editor stays mounted.
    act(() => useNavigationGuardStore.getState().guard('/metrics', () => {}));
    act(() => useNavigationGuardStore.getState().confirm());
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute(
      'aria-checked',
      'false',
    );
  });

  it('does not block a target that stays in Tools', () => {
    renderGuardedAt('/tools?server=github');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));

    expect(useNavigationGuardStore.getState().isBlocked('/tools?server=other')).toBe(false);
    expect(useNavigationGuardStore.getState().isBlocked('/metrics')).toBe(true);

    fireEvent.click(screen.getByTestId('guarded-nav-/tools?server=atlassian'));
    expect(useNavigationGuardStore.getState().pending).toBeNull();
  });
});

// Changes ?server= from outside the workspace's own switch guard, the way a
// cross-workspace deep link or browser history does.
function ExternalNav() {
  const navigate = useNavigate();
  return (
    <button type="button" data-testid="external-nav" onClick={() => navigate('/tools?server=atlassian')} />
  );
}

describe('ToolsWorkspace — active server changes under a dirty draft', () => {
  beforeEach(() => {
    useNavigationGuardStore.setState({ guards: [], pending: null });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    useNavigationGuardStore.setState({ guards: [], pending: null });
  });

  it('offers only Discard when the owner leaves the stack', () => {
    const setSpy = vi.spyOn(api, 'setServerTools');
    const selectSpy = vi.spyOn(useStackStore.getState(), 'selectNode');
    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));

    act(() => {
      useStackStore.setState({ mcpServers: [server(ATLAS, ['get_page'], [])] });
    });

    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Discard unsaved changes to github?');
    expect(within(dialog).queryByRole('button', { name: 'Keep editing' })).not.toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
    expect(setSpy).not.toHaveBeenCalled();
    expect(selectSpy).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: 'Discard' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByRole('option', { name: /get_page/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Saved$/ })).toBeDisabled();
    expect(setSpy).not.toHaveBeenCalled();
    expect(selectSpy).not.toHaveBeenCalled();
  });

  it('holds the discard confirm while the fleet panel is open', () => {
    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Open fleet tool actions' }));
    const fleet = screen.getByRole('dialog', { name: 'Fleet tool actions' });

    act(() => {
      useStackStore.setState({ mcpServers: [server(ATLAS, ['get_page'], [])] });
    });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();

    fireEvent.click(within(fleet).getByRole('button', { name: 'Close' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Discard unsaved changes to github?');
  });

  it('holds the discard confirm behind the leave confirm', () => {
    renderGuardedAt('/tools?server=github');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    fireEvent.click(screen.getByTestId('guarded-nav-/metrics'));

    act(() => {
      useStackStore.setState({ mcpServers: [server(ATLAS, ['get_page'], [])] });
    });
    const dialogs = screen.getAllByRole('alertdialog');
    expect(dialogs).toHaveLength(1);
    expect(dialogs[0]).toHaveTextContent('Tools for github');

    fireEvent.click(within(dialogs[0]).getByRole('button', { name: 'Cancel' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Discard unsaved changes to github?');
  });

  it('Keep editing returns to the owner through the URL', () => {
    const selectSpy = vi.spyOn(useStackStore.getState(), 'selectNode');
    render(
      <MemoryRouter initialEntries={['/tools?server=github']}>
        <ToolsWorkspace />
        <ExternalNav />
        <LocationProbe />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    fireEvent.click(screen.getByTestId('external-nav'));

    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Discard unsaved changes to github?');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep editing' }));

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByTestId('loc')).toHaveTextContent('server=github');
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(screen.getByRole('button', { name: /save 1 change & reload/i })).toBeEnabled();
    expect(selectSpy).not.toHaveBeenCalled();
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

    // Tool selection restored: the detail panel renders the parameters table.
    expect(screen.getByRole('table', { name: 'create_issue parameters' })).toBeInTheDocument();
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
    fireEvent.click(screen.getByRole('option', { name: /^create_issue, / }));
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
    fireEvent.click(screen.getByRole('option', { name: /^create_issue, / }));
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
    const listRepos = screen.getByRole('option', { name: /^list_repos, / });
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
    expect(screen.getByRole('option', { name: /^create_issue, / })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /^list_repos, / })).not.toBeInTheDocument();
    expect(screen.getByText('1 of 2 shown')).toBeInTheDocument();

    // Toggling it off restores the list and drops the param.
    fireEvent.click(screen.getByRole('button', { name: 'Destructive 1' }));
    expect(loc()).not.toContain('risk=');
    expect(screen.getByRole('option', { name: /^list_repos, / })).toBeInTheDocument();
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
    expect(screen.getByRole('option', { name: /^list_repos, / })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /^create_issue, / })).not.toBeInTheDocument();

    // Back to All drops the param.
    fireEvent.click(screen.getByRole('button', { name: 'All 2' }));
    expect(loc()).not.toContain('filter=');
    expect(screen.getByRole('option', { name: /^create_issue, / })).toBeInTheDocument();
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

describe('ToolsWorkspace — count line', () => {
  it('distinguishes no allow-list, a saved allow-list, and an unsaved draft', () => {
    useStackStore.setState({
      mcpServers: [server(GITHUB, ['create_issue', 'list_repos'], ['create_issue']), server(ATLAS, ['get_page', 'get_space'], [])],
    });
    const { unmount } = renderAt('/tools?server=atlassian');
    expect(screen.getByText('All 2 tools exposed (no allow-list)')).toBeInTheDocument();
    unmount();

    renderAt('/tools?server=github');
    expect(screen.getByText('1 of 2 exposed')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    expect(screen.getByText('2 of 2 selected (unsaved)')).toBeInTheDocument();
    expect(screen.queryByText('1 of 2 exposed')).not.toBeInTheDocument();
  });
});

describe('ToolsWorkspace — chip legend', () => {
  it('lists every chip and returns focus on Escape', () => {
    renderAt('/tools?server=github');
    const button = screen.getByRole('button', { name: 'Legend' });
    expect(button).toHaveAttribute('aria-haspopup', 'dialog');
    expect(button).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    const legend = screen.getByRole('dialog', { name: 'Chip legend' });
    for (const [label, name] of [
      ['RO', 'Read-only'],
      ['RW', 'Read-write'],
      ['DESTR', 'Destructive'],
      ['SAFE', 'Non-destructive'],
      ['IDEM', 'Idempotent'],
      ['OPEN', 'Open world'],
      ['CLOSED', 'Closed world'],
    ]) {
      expect(within(legend).getByText(label)).toBeInTheDocument();
      expect(within(legend).getByText(name)).toBeInTheDocument();
    }
    expect(within(legend).getByText('BLOCKED')).toBeInTheDocument();
    expect(within(legend).getByText('Denied by the policy block')).toBeInTheDocument();
    expect(within(legend).getByText('APPROVAL')).toBeInTheDocument();
    expect(within(legend).getByText('Policy requires approval; enforced as a block')).toBeInTheDocument();
    expect(within(legend).getByText('Hints are reported by the server, not verified by gridctl.')).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Chip legend' })).not.toBeInTheDocument();
    expect(button).toHaveFocus();
  });
});

describe('ToolsWorkspace — detail links', () => {
  beforeEach(() => {
    useNavigationGuardStore.setState({ guards: [], pending: null });
  });
  afterEach(() => {
    useNavigationGuardStore.setState({ guards: [], pending: null });
  });

  it('navigates immediately with a clean editor', () => {
    renderGuardedAt('/tools?server=github&tool=create_issue');
    fireEvent.click(screen.getByRole('link', { name: 'Traces' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByTestId('loc')).toHaveTextContent('/traces?q=create_issue&server=github&errors=0');
  });

  it('asks before discarding a dirty draft, then navigates on confirm', () => {
    renderGuardedAt('/tools?server=github&tool=create_issue');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    fireEvent.click(screen.getByRole('link', { name: 'Traces' }));

    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Discard unsaved changes');
    expect(screen.getByTestId('loc')).toHaveTextContent('/tools?server=github&tool=create_issue');

    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.getByTestId('loc')).toHaveTextContent('/tools?server=github&tool=create_issue');
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute('aria-checked', 'true');

    fireEvent.click(screen.getByRole('link', { name: 'Traces' }));
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(screen.getByTestId('loc')).toHaveTextContent('/traces?q=create_issue&server=github&errors=0');
  });
});

describe('ToolsWorkspace — pin chip', () => {
  function pinEntry(tools: string[]) {
    return {
      server_hash: 'h',
      pinned_at: '2026-07-01T00:00:00Z',
      last_verified_at: '2026-07-01T00:00:00Z',
      tool_count: tools.length,
      status: 'pinned' as const,
      tools: Object.fromEntries(tools.map((t) => [t, {} as api.PinRecord])),
    };
  }

  afterEach(() => {
    usePinsStore.setState({ pins: null });
  });

  it('renders no chip before pins load', () => {
    usePinsStore.setState({ pins: null });
    renderAt('/tools?server=github&tool=create_issue');
    expect(screen.queryByRole('link', { name: /pin status/i })).not.toBeInTheDocument();
  });

  it('labels a pinned tool and links to the server in Pins', () => {
    usePinsStore.setState({ pins: { [GITHUB]: pinEntry(['create_issue']) } });
    renderAt('/tools?server=github&tool=create_issue');
    expect(screen.getByRole('link', { name: 'Pin status: Pinned. Open in Pins' })).toHaveAttribute(
      'href',
      '/pins?server=github',
    );
  });

  it('stays silent for a tool the saved allow-list hides, even after a draft toggle', () => {
    usePinsStore.setState({ pins: { [GITHUB]: pinEntry(['create_issue']) } });
    renderAt('/tools?server=github&tool=list_repos');
    expect(screen.getByRole('heading', { name: 'list_repos' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /pin status/i })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    expect(screen.getByRole('checkbox', { name: /list_repos/i })).toHaveAttribute('aria-checked', 'true');
    expect(screen.queryByRole('link', { name: /pin status/i })).not.toBeInTheDocument();
  });

  it('reads Not in pin set for an exposed tool absent from the pin set', () => {
    usePinsStore.setState({ pins: { [ATLAS]: pinEntry([]) } });
    renderAt('/tools?server=atlassian&tool=get_page');
    expect(screen.getByRole('link', { name: 'Pin status: Not in pin set. Open in Pins' })).toBeInTheDocument();
  });
});

describe('ToolsWorkspace — Try it gating', () => {
  beforeEach(() => {
    useToolCallHistoryStore.setState({ byTool: {}, tryItOpen: true });
  });

  it('gates Run on the saved allow-list, not the draft', () => {
    renderAt('/tools?server=github&tool=list_repos');
    const run = screen.getByRole('button', { name: 'Run' });
    expect(run).toBeDisabled();
    const reason = document.getElementById(run.getAttribute('aria-describedby') ?? '');
    expect(reason).toHaveTextContent('Not exposed. This tool is hidden by the saved allow-list for github');
    // Checking the draft box without saving does not enable Run.
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    expect(screen.getByRole('button', { name: 'Run' })).toBeDisabled();
    expect(
      screen.getByText('Unsaved allow-list changes are not applied. Calls use the saved allow-list.'),
    ).toBeInTheDocument();
  });

  it('keeps Run enabled with a note when the draft hides an exposed tool', () => {
    renderAt('/tools?server=github&tool=create_issue');
    expect(screen.getByRole('button', { name: 'Run' })).toBeEnabled();
    expect(
      screen.queryByText('Unsaved allow-list changes are not applied. Calls use the saved allow-list.'),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: /create_issue/i }));
    expect(screen.getByRole('button', { name: 'Run' })).toBeEnabled();
    expect(
      screen.getByText('Unsaved allow-list changes are not applied. Calls use the saved allow-list.'),
    ).toBeInTheDocument();
  });
});

describe('ToolsWorkspace — global search navigation', () => {
  const LATE = `${'Long preamble about the page tool and its behavior. '.repeat(3)}Emits a zephyrine event.`;

  beforeEach(() => {
    const catalog = [
      ...useStackStore.getState().toolCatalog!,
      tool(`${ATLAS}${TOOL_NAME_DELIMITER}watch_page`, LATE),
    ];
    useStackStore.setState({
      mcpServers: [
        server(GITHUB, ['create_issue', 'list_repos'], ['create_issue']),
        server(ATLAS, ['get_page', 'watch_page'], []),
      ],
      tools: catalog,
      toolCatalog: catalog,
    });
  });

  const params = () =>
    new URLSearchParams((screen.getByTestId('loc').textContent ?? '').split('?')[1] ?? '');
  const searchInput = () => screen.getByPlaceholderText(/search (all tools|tools across all)/i);
  const resultOptions = () =>
    within(screen.getByRole('listbox', { name: /search results/i })).getAllByRole('option');

  it('finds a term late in a description and highlights it in an excerpt', () => {
    expect(LATE.indexOf('zephyrine')).toBeGreaterThanOrEqual(120);
    renderAt('/tools');
    fireEvent.change(searchInput(), { target: { value: 'zephyrine' } });
    const row = screen.getByRole('option', { name: /watch_page/ });
    const mark = row.querySelector('mark');
    expect(mark).toHaveTextContent('zephyrine');
    expect(row.textContent).toContain('…');
  });

  it('highlights exact matches in the server and tool names', () => {
    renderAt('/tools');
    fireEvent.change(searchInput(), { target: { value: 'git' } });
    const row = screen.getByRole('option', { name: /create_issue/ });
    expect([...row.querySelectorAll('mark')].map((m) => m.textContent)).toContain('git');
  });

  it('returns to the same results, focused, with the picked row active', () => {
    renderWithProbe('/tools?server=github');
    fireEvent.change(searchInput(), { target: { value: 'page' } });
    const before = resultOptions().map((o) => o.textContent);
    expect(before.length).toBeGreaterThan(1);
    fireEvent.click(resultOptions()[1]);
    expect(params().get('back')).toBe('page');
    fireEvent.click(screen.getByRole('button', { name: /back to results for "page"/i }));
    expect(params().get('q')).toBe('page');
    expect(params().has('back')).toBe(false);
    expect(resultOptions().map((o) => o.textContent)).toEqual(before);
    expect(searchInput()).toHaveValue('page');
    expect(searchInput()).toHaveFocus();
    const options = resultOptions();
    expect(options[1]).toHaveAttribute('aria-selected', 'true');
    expect(searchInput()).toHaveAttribute('aria-activedescendant', options[1].id);
    expect(screen.queryByRole('button', { name: /back to results/i })).not.toBeInTheDocument();
  });

  it('moves through results with ArrowDown, ArrowUp, and Enter', () => {
    renderWithProbe('/tools?server=github');
    const input = searchInput();
    fireEvent.change(input, { target: { value: 'page' } });
    expect(input).toHaveAttribute('role', 'combobox');
    expect(input).toHaveAttribute('aria-expanded', 'true');
    const listbox = screen.getByRole('listbox', { name: /search results/i });
    expect(input).toHaveAttribute('aria-controls', listbox.id);
    const options = within(listbox).getAllByRole('option');
    expect(input).toHaveAttribute('aria-activedescendant', options[0].id);

    fireEvent.keyDown(input, { key: 'ArrowUp' });
    expect(input).toHaveAttribute('aria-activedescendant', options[options.length - 1].id);
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    expect(input).toHaveAttribute('aria-activedescendant', options[0].id);
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    expect(input).toHaveAttribute('aria-activedescendant', options[1].id);
    expect(options[1]).toHaveAttribute('aria-selected', 'true');
    expect(document.getElementById(options[1].id)).toBe(options[1]);

    const picked = options[1].textContent ?? '';
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(params().get('server')).toBe(ATLAS);
    expect(picked).toContain(params().get('tool'));
    expect(params().get('back')).toBe('page');
    expect(params().has('q')).toBe(false);
  });

  it('ignores keys while composing and with no results', () => {
    renderWithProbe('/tools?server=github');
    const input = searchInput();
    fireEvent.change(input, { target: { value: 'page' } });
    fireEvent.keyDown(input, { key: 'Enter', isComposing: true });
    expect(params().get('q')).toBe('page');
    fireEvent.change(input, { target: { value: 'nothingmatchesthis' } });
    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(input).not.toHaveAttribute('aria-activedescendant');
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(params().get('q')).toBe('nothingmatchesthis');
  });

  it('a rail click removes back', () => {
    renderWithProbe('/tools?server=github&tool=create_issue&back=issue');
    expect(screen.getByRole('button', { name: /back to results for "issue"/i })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: new RegExp(ATLAS) }));
    expect(params().has('back')).toBe(false);
    expect(screen.queryByRole('button', { name: /back to results/i })).not.toBeInTheDocument();
  });

  it('typing a new query removes back', () => {
    renderWithProbe('/tools?server=github&tool=create_issue&back=issue');
    fireEvent.change(searchInput(), { target: { value: 'repo' } });
    expect(params().has('back')).toBe(false);
    expect(params().get('q')).toBe('repo');
  });

  it('dismiss removes back only', () => {
    renderWithProbe('/tools?server=github&tool=create_issue&back=issue');
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
    expect(params().has('back')).toBe(false);
    expect(params().get('server')).toBe(GITHUB);
    expect(params().get('tool')).toBe('create_issue');
  });

  it('renders no chip while a query is active', () => {
    renderAt('/tools?server=github&q=repo&back=issue');
    expect(screen.queryByRole('button', { name: /back to results/i })).not.toBeInTheDocument();
    expect(screen.getByRole('option', { name: /list_repos/ })).toBeInTheDocument();
  });

  it('keeps back through the discard dialog when picking on another server', () => {
    renderWithProbe('/tools?server=github');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    fireEvent.change(searchInput(), { target: { value: 'get_page' } });
    fireEvent.click(screen.getByRole('option', { name: /get_page/ }));
    const dialog = screen.getByRole('alertdialog');
    expect(params().get('q')).toBe('get_page');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Discard & switch' }));
    expect(params().get('server')).toBe(ATLAS);
    expect(params().get('tool')).toBe('get_page');
    expect(params().get('back')).toBe('get_page');
    expect(params().has('q')).toBe(false);
  });

  it('keeps results and q when the discard dialog is cancelled', () => {
    renderWithProbe('/tools?server=github');
    fireEvent.click(screen.getByRole('checkbox', { name: /list_repos/i }));
    fireEvent.change(searchInput(), { target: { value: 'get_page' } });
    fireEvent.click(screen.getByRole('option', { name: /get_page/ }));
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Cancel' }));
    expect(params().get('q')).toBe('get_page');
    expect(params().has('back')).toBe(false);
    expect(screen.getByRole('option', { name: /get_page/ })).toBeInTheDocument();
  });

  it('lists tool-name hits before description-only hits', () => {
    const catalog = [
      tool(`${ATLAS}${TOOL_NAME_DELIMITER}list_things`, 'List boards for a project with their columns and owners'),
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}get_board_items_by_name`, 'Fetch items'),
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}move_board`, 'Move an item'),
    ];
    useStackStore.setState({
      mcpServers: [
        server(GITHUB, ['get_board_items_by_name', 'move_board'], []),
        server(ATLAS, ['list_things'], []),
      ],
      tools: catalog,
      toolCatalog: catalog,
    });
    renderAt('/tools');
    fireEvent.change(searchInput(), { target: { value: 'board' } });
    const order = resultOptions().map((o) =>
      ['list_things', 'get_board_items_by_name', 'move_board'].find((t) => o.textContent?.includes(t)),
    );
    expect(order).toHaveLength(3);
    expect(order[2]).toBe('list_things');
  });

  it('renders the empty message outside the listbox', () => {
    renderAt('/tools');
    fireEvent.change(searchInput(), { target: { value: 'nothingmatchesthis' } });
    const listbox = screen.getByRole('listbox', { name: /search results/i });
    expect(listbox).toBeEmptyDOMElement();
    expect(screen.getByText(/no tools match "nothingmatchesthis" across any server/i)).toBeInTheDocument();
  });

  it('resolves aria-activedescendant for names containing whitespace', () => {
    const catalog = [tool(`my server${TOOL_NAME_DELIMITER}do thing`, 'Has spaces')];
    useStackStore.setState({
      mcpServers: [server('my server', ['do thing'], [])],
      tools: catalog,
      toolCatalog: catalog,
    });
    renderAt('/tools');
    const input = searchInput();
    fireEvent.change(input, { target: { value: 'thing' } });
    const id = input.getAttribute('aria-activedescendant')!;
    expect(id).toBeTruthy();
    expect(document.getElementById(id)).toBe(screen.getByRole('option', { name: /do thing/ }));
  });
});

describe('ToolsWorkspace — row keyboard and labels', () => {
  const options = () =>
    within(screen.getByPlaceholderText(/^filter github tools/i).closest('[cmdk-root]') as HTMLElement).getAllByRole('option');

  it('gives the list one tab stop on the cursor row and none on checkboxes', () => {
    renderAt('/tools?server=github');
    const tabbable = options().filter((o) => o.tabIndex === 0);
    expect(tabbable).toHaveLength(1);
    expect(tabbable[0]).toHaveAttribute('aria-selected', 'true');
    for (const cb of screen.getAllByRole('checkbox')) expect(cb.tabIndex).toBe(-1);
  });

  it('Space on a focused row toggles it without opening the detail rail', () => {
    renderWithProbe('/tools?server=github');
    const row = options()[0];
    expect(row).toHaveAttribute('tabindex', '0');
    row.focus();
    expect(screen.getByRole('checkbox', { name: /create_issue/i })).toHaveAttribute('aria-checked', 'true');
    fireEvent.keyDown(row, { key: ' ' });
    expect(screen.getByRole('checkbox', { name: /create_issue/i })).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByTestId('loc').textContent).not.toContain('tool=');
    expect(screen.getByText(/select a tool to view/i)).toBeInTheDocument();
  });

  it('Space on a focused checkbox does not toggle through the row handler', () => {
    renderAt('/tools?server=github');
    const cb = screen.getByRole('checkbox', { name: /create_issue/i });
    fireEvent.keyDown(cb, { key: ' ' });
    expect(screen.getByRole('checkbox', { name: /create_issue/i })).toHaveAttribute('aria-checked', 'true');
  });

  it('Enter on a focused row opens the detail rail', () => {
    renderWithProbe('/tools?server=github');
    const row = options()[0];
    row.focus();
    fireEvent.keyDown(row, { key: 'Enter' });
    expect(screen.getByTestId('loc').textContent).toContain('tool=create_issue');
  });

  it('Space in the filter input changes no checkbox', () => {
    renderAt('/tools?server=github');
    const before = screen.getAllByRole('checkbox').map((c) => c.getAttribute('aria-checked'));
    const input = screen.getByPlaceholderText(/^filter github tools/i);
    input.focus();
    const ev = fireEvent.keyDown(input, { key: ' ' });
    expect(ev).toBe(true);
    expect(screen.getAllByRole('checkbox').map((c) => c.getAttribute('aria-checked'))).toEqual(before);
  });

  it('arrow keys, Home, and End from a focused row move focus with the cursor', () => {
    renderAt('/tools?server=github');
    const [first, second] = options();
    first.focus();
    fireEvent.keyDown(first, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(second);
    expect(second).toHaveAttribute('tabindex', '0');
    expect(first).toHaveAttribute('tabindex', '-1');
    fireEvent.keyDown(second, { key: 'ArrowUp' });
    expect(document.activeElement).toBe(first);
    fireEvent.keyDown(first, { key: 'End' });
    expect(document.activeElement).toBe(second);
    fireEvent.keyDown(second, { key: 'Home' });
    expect(document.activeElement).toBe(first);
  });

  it('Ctrl+N and Ctrl+P move focus with the cursor; a plain n does not', () => {
    renderAt('/tools?server=github');
    const [first, second] = options();
    first.focus();
    fireEvent.keyDown(first, { key: 'n' });
    expect(document.activeElement).toBe(first);
    fireEvent.keyDown(first, { key: 'n', ctrlKey: true });
    expect(document.activeElement).toBe(second);
    fireEvent.keyDown(second, { key: 'p', ctrlKey: true });
    expect(document.activeElement).toBe(first);
    fireEvent.keyDown(first, { key: 'j', ctrlKey: true });
    expect(document.activeElement).toBe(second);
    fireEvent.keyDown(second, { key: 'k', ctrlKey: true });
    expect(document.activeElement).toBe(first);
  });

  it('hovering another row does not move focus off a focused row', () => {
    renderAt('/tools?server=github');
    const [first, second] = options();
    first.focus();
    fireEvent.pointerMove(second);
    expect(second).toHaveAttribute('aria-selected', 'true');
    expect(document.activeElement).toBe(first);
  });

  it('hovering a row while typing keeps focus in the filter input', () => {
    renderAt('/tools?server=github');
    const input = screen.getByPlaceholderText(/^filter github tools/i);
    input.focus();
    fireEvent.pointerMove(options()[1]);
    expect(document.activeElement).toBe(input);
  });

  it('names each option from its state and chips, with the description as its description', async () => {
    const catalog = [
      {
        ...tool(`${GITHUB}${TOOL_NAME_DELIMITER}create_issue`, 'Create a GitHub issue'),
        annotations: { readOnlyHint: false, destructiveHint: true },
      },
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}list_repos`, 'List repositories'),
    ];
    useStackStore.setState({
      mcpServers: [server(GITHUB, ['create_issue', 'list_repos'], ['create_issue'])],
      tools: catalog,
      toolCatalog: catalog,
    });
    vi.mocked(api.fetchPolicy).mockResolvedValue({
      configured: true,
      default: 'allow',
      block_destructive: false,
      rules: [{ index: 0, match: 'github__create_issue', action: 'require_approval' }],
      tools: {
        [`${GITHUB}${TOOL_NAME_DELIMITER}create_issue`]: {
          action: 'require_approval',
          source: 'rule',
          rule_index: 0,
          rule: 'github__create_issue',
          exposed: true,
        },
      },
    });
    renderAt('/tools?server=github');
    await screen.findByText('APPROVAL');
    const enabled = screen.getByRole('option', {
      name: 'create_issue, enabled, read-write, destructive, blocked by policy rule 1: github__create_issue (requires approval, enforced as block)',
    });
    expect(enabled).toHaveAccessibleDescription('Create a GitHub issue');
    const disabled = screen.getByRole('option', { name: 'list_repos, disabled' });
    expect(disabled).toHaveAccessibleDescription('List repositories');
    for (const o of options()) expect(o.getAttribute('aria-label')).not.toMatch(/details/);
  });

  it('attaches no description when a tool has none', () => {
    const catalog = [tool(`${GITHUB}${TOOL_NAME_DELIMITER}bare`)];
    useStackStore.setState({
      mcpServers: [server(GITHUB, ['bare'], [])],
      tools: catalog,
      toolCatalog: catalog,
    });
    renderAt('/tools?server=github');
    expect(screen.getByRole('option', { name: 'bare, enabled' })).not.toHaveAttribute('aria-describedby');
  });
});

describe('ToolsWorkspace — sort while filtering', () => {
  it('disables Sort with a described reason while the filter has text', () => {
    renderWithProbe('/tools?server=github&sort=name');
    const sort = () => screen.getByRole('combobox', { name: /sort tools/i });
    expect(sort()).toBeEnabled();
    expect(screen.queryByText('Relevance order')).not.toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText(/^filter github tools/i), { target: { value: 'repo' } });
    expect(sort()).toBeDisabled();
    expect(sort()).toHaveValue('name');
    expect(sort()).toHaveAttribute('title', 'Filtered results are in relevance order. Clear the filter to sort.');
    expect(screen.getByText('Relevance order')).toBeInTheDocument();
    expect(sort()).toHaveAccessibleDescription('Relevance order');
    expect(screen.getByTestId('loc').textContent).toContain('sort=name');

    fireEvent.change(screen.getByPlaceholderText(/^filter github tools/i), { target: { value: '' } });
    expect(sort()).toBeEnabled();
    expect(sort()).not.toHaveAttribute('aria-describedby');
    expect(sort()).not.toHaveAttribute('title');
    expect(screen.queryByText('Relevance order')).not.toBeInTheDocument();
    expect(screen.getByTestId('loc').textContent).toContain('sort=name');
  });
});

describe('ToolsWorkspace — Disable unused with other draft edits', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('saves only the unused removal and leaves other toggles unsaved', async () => {
    const recent = new Date(Date.now() - 60 * 60 * 1000).toISOString();
    const catalog = [
      tool(`gitlab${TOOL_NAME_DELIMITER}a`),
      tool(`gitlab${TOOL_NAME_DELIMITER}b`),
      tool(`gitlab${TOOL_NAME_DELIMITER}c`),
      tool(`gitlab${TOOL_NAME_DELIMITER}d`),
    ];
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server('gitlab', ['a', 'b', 'c', 'd'], ['a', 'b'])],
      tools: catalog,
      toolCatalog: catalog,
    });
    vi.spyOn(api, 'fetchToolUsage').mockResolvedValue({
      observedSince: new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString(),
      servers: { gitlab: { a: { calls: 9, lastCalledAt: recent }, c: { calls: 1, lastCalledAt: recent } } },
    });
    const saveSpy = vi
      .spyOn(api, 'setServerTools')
      .mockResolvedValue({ server: 'gitlab', tools: ['a'], reloaded: true });
    vi.spyOn(api, 'fetchStatus').mockResolvedValue({ gateway: { name: 'x', version: '1' }, 'mcp-servers': [] });
    vi.spyOn(api, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(api, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    renderAt('/tools?server=gitlab');
    fireEvent.click(screen.getByRole('button', { name: /toggle audit mode/i }));
    // c is a draft-only enable (used, so not an unused target).
    fireEvent.click(screen.getByRole('checkbox', { name: 'Enable c' }));

    fireEvent.click(await screen.findByRole('button', { name: /disable 1 unused tools/i }));
    fireEvent.click(await screen.findByRole('button', { name: /disable & reload/i }));

    await waitFor(() => expect(saveSpy).toHaveBeenCalledWith('gitlab', ['a']));
    expect(saveSpy).toHaveBeenCalledTimes(1);
    // The poll adopts the saved list; the draft-only enable stays unsaved.
    act(() => {
      useStackStore.setState({ mcpServers: [server('gitlab', ['a', 'b', 'c', 'd'], ['a'])] });
    });
    expect(screen.getByRole('checkbox', { name: 'Disable c' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('checkbox', { name: 'Enable b' })).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('button', { name: 'Save 1 change & Reload' })).toBeEnabled();
  });
});

describe('ToolsWorkspace — long lists', () => {
  function seed(count: number) {
    const names = Array.from({ length: count }, (_, i) => `t${String(i).padStart(3, '0')}`);
    const catalog = names.map((n) => tool(`${GITHUB}${TOOL_NAME_DELIMITER}${n}`, `Tool ${n}`));
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, names, [])],
      tools: catalog,
      toolCatalog: catalog,
    });
    return names;
  }

  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(480);
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(640);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  const options = () =>
    within(screen.getByPlaceholderText(/^filter github tools/i).closest('[cmdk-root]') as HTMLElement).getAllByRole('option');

  it('renders every row at the 200-row threshold', () => {
    seed(200);
    renderAt('/tools?server=github');
    expect(options()).toHaveLength(200);
    expect(options()[0]).not.toHaveAttribute('aria-setsize');
  });

  it('does not scroll back to an unchanged selection when a filter crosses the threshold', () => {
    seed(300);
    const intoView = vi.fn();
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: intoView });
    try {
      renderAt('/tools?server=github&tool=t001');
      const input = screen.getByPlaceholderText(/^filter github tools/i);
      fireEvent.change(input, { target: { value: 't00' } });
      fireEvent.change(input, { target: { value: '' } });
      fireEvent.change(input, { target: { value: 't00' } });
      // cmdk scrolls its cursor into view on filter changes (no behavior);
      // only the reveal effect asks for smooth scrolling.
      const reveals = intoView.mock.calls.filter(([arg]) => arg?.behavior === 'smooth');
      expect(reveals).toHaveLength(0);
    } finally {
      delete (HTMLElement.prototype as { scrollIntoView?: unknown }).scrollIntoView;
    }
  });

  it('renders a window above the threshold that keeps the first and last rows', () => {
    const names = seed(300);
    renderAt('/tools?server=github');
    const rendered = options();
    expect(rendered.length).toBeLessThan(100);
    expect(screen.getByRole('option', { name: new RegExp(`^${names[0]}, `) })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: new RegExp(`^${names[299]}, `) })).toBeInTheDocument();
    expect(screen.queryByText(/no tools match/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/no tools discovered/i)).not.toBeInTheDocument();
  });

  it('keeps the cursor row and its neighbors mounted', () => {
    const names = seed(300);
    renderAt('/tools?server=github');
    const last = screen.getByRole('option', { name: new RegExp(`^${names[299]}, `) });
    // The cursor starts on the first row; End moves it to the last.
    last.focus();
    fireEvent.keyDown(last, { key: 'End' });
    expect(last).toHaveAttribute('aria-selected', 'true');
    expect(document.activeElement).toBe(last);
    fireEvent.keyDown(last, { key: 'ArrowUp' });
    const prev = screen.getByRole('option', { name: new RegExp(`^${names[298]}, `) });
    expect(prev).toHaveAttribute('aria-selected', 'true');
    expect(document.activeElement).toBe(prev);
    expect(screen.getByRole('option', { name: new RegExp(`^${names[297]}, `) })).toBeInTheDocument();
    fireEvent.keyDown(prev, { key: 'Home' });
    expect(document.activeElement).toBe(screen.getByRole('option', { name: new RegExp(`^${names[0]}, `) }));
  });

  it('reveals a deep-linked tool far down the list without throwing', () => {
    seed(300);
    const scrollToSpy = vi.fn();
    // getMaxScrollOffset reads scrollHeight - clientHeight, both 0 in jsdom.
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(300 * 48);
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(480);
    Object.defineProperty(HTMLElement.prototype, 'scrollTo', { configurable: true, value: scrollToSpy });
    try {
      renderAt('/tools?server=github&tool=t250');
      expect(screen.getByRole('heading', { name: 't250' })).toBeInTheDocument();
      expect(scrollToSpy).toHaveBeenCalled();
      const top = scrollToSpy.mock.calls.at(-1)?.[0]?.top;
      expect(top).toBeGreaterThan(0);
    } finally {
      delete (HTMLElement.prototype as { scrollTo?: unknown }).scrollTo;
    }
  });
});

describe('ToolsWorkspace — long lists keep the focused row', () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(480);
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(640);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('keeps a focused row mounted after a hover moves the cursor and the list scrolls', () => {
    const names = Array.from({ length: 300 }, (_, i) => `t${String(i).padStart(3, '0')}`);
    const catalog = names.map((n) => tool(`${GITHUB}${TOOL_NAME_DELIMITER}${n}`, `Tool ${n}`));
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, names, [])],
      tools: catalog,
      toolCatalog: catalog,
    });
    renderAt('/tools?server=github');
    const option = (n: string) => screen.queryByRole('option', { name: new RegExp(`^${n}, `) });
    const focused = option('t005')!;
    focused.focus();
    fireEvent.pointerMove(option('t000')!);
    expect(option('t000')).toHaveAttribute('aria-selected', 'true');
    const list = screen.getByPlaceholderText(/^filter github tools/i)
      .closest('[cmdk-root]')!.querySelector('[cmdk-list]') as HTMLElement;
    act(() => {
      list.scrollTop = 6000;
      fireEvent.scroll(list);
    });
    expect(option('t005')).toBe(focused);
    expect(document.activeElement).toBe(focused);
  });

  it('keeps a row mounted when its checkbox holds focus and the list scrolls', () => {
    const names = Array.from({ length: 300 }, (_, i) => `t${String(i).padStart(3, '0')}`);
    const catalog = names.map((n) => tool(`${GITHUB}${TOOL_NAME_DELIMITER}${n}`, `Tool ${n}`));
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, names, [])],
      tools: catalog,
      toolCatalog: catalog,
    });
    renderAt('/tools?server=github');
    const option = (n: string) => screen.queryByRole('option', { name: new RegExp(`^${n}, `) });
    const box = screen.getByRole('checkbox', { name: /^(Enable|Disable) t005$/ });
    box.focus();
    fireEvent.pointerMove(option('t000')!);
    const list = screen.getByPlaceholderText(/^filter github tools/i)
      .closest('[cmdk-root]')!.querySelector('[cmdk-list]') as HTMLElement;
    act(() => {
      list.scrollTop = 6000;
      fireEvent.scroll(list);
    });
    expect(option('t005')).toBeInTheDocument();
    expect(document.activeElement).toBe(box);
  });

  it('gives windowed options their list size and position', () => {
    const names = Array.from({ length: 300 }, (_, i) => `t${String(i).padStart(3, '0')}`);
    const catalog = names.map((n) => tool(`${GITHUB}${TOOL_NAME_DELIMITER}${n}`, `Tool ${n}`));
    useStackStore.setState({
      isLoading: false,
      mcpServers: [server(GITHUB, names, [])],
      tools: catalog,
      toolCatalog: catalog,
    });
    renderAt('/tools?server=github');
    const last = screen.getByRole('option', { name: /^t299, / });
    expect(last).toHaveAttribute('aria-setsize', '300');
    expect(last).toHaveAttribute('aria-posinset', '300');
    expect(screen.getByRole('option', { name: /^t000, / })).toHaveAttribute('aria-posinset', '1');
  });
});
