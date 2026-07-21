import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import '@testing-library/jest-dom';
import { ToolsWorkspace } from '../components/workspaces/ToolsWorkspace';
import { useStackStore } from '../stores/useStackStore';
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
