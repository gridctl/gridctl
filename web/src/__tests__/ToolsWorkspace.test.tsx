import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import '@testing-library/jest-dom';
import { ToolsWorkspace } from '../components/workspaces/ToolsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
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
  useStackStore.setState({
    isLoading: false,
    mcpServers: [
      // github: 1 of 2 whitelisted
      server(GITHUB, ['create_issue', 'list_repos'], ['create_issue']),
      // atlassian: empty whitelist = all exposed (1/1)
      server(ATLAS, ['get_page'], []),
    ],
    tools: [
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}create_issue`, 'Create a GitHub issue', {
        type: 'object',
        properties: { title: { type: 'string' } },
      }),
      tool(`${GITHUB}${TOOL_NAME_DELIMITER}list_repos`, 'List repositories'),
      tool(`${ATLAS}${TOOL_NAME_DELIMITER}get_page`, 'Get a Confluence page'),
    ],
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

  it('deep-links to ?server= and shows that server\'s tools, seeded from its whitelist', () => {
    renderAt('/tools?server=github');
    // github advertises create_issue + list_repos; whitelist is [create_issue].
    expect(screen.getByRole('option', { name: 'create_issue' })).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(screen.getByRole('option', { name: 'list_repos' })).toHaveAttribute(
      'aria-checked',
      'false',
    );
  });

  it('defaults to the first server (alphabetical) when no ?server= is given', () => {
    renderAt('/tools');
    // atlassian sorts before github → its tool is shown by default.
    expect(screen.getByRole('option', { name: 'get_page' })).toBeInTheDocument();
  });

  it('selecting a server in the rail switches the detail pane', () => {
    renderAt('/tools');
    fireEvent.click(screen.getByRole('button', { name: /github/i }));
    expect(screen.getByRole('option', { name: 'create_issue' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'get_page' })).not.toBeInTheDocument();
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
    expect(screen.getByRole('option', { name: 'create_issue' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'list_repos' })).toBeInTheDocument();
  });

  it('previews a tool input schema on expand', () => {
    renderAt('/tools?server=github');
    fireEvent.click(screen.getByRole('button', { name: /show create_issue schema/i }));
    // The CodeViewer renders the JSON schema content.
    expect(screen.getByLabelText('create_issue input schema')).toBeInTheDocument();
  });
});
