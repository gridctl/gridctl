import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { GroupsPanel } from '../components/workspaces/GroupsPanel';
import { ToolsWorkspace } from '../components/workspaces/ToolsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { groupsForTool, annotationChips } from '../lib/groups';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import * as api from '../lib/api';
import type { GroupsReport } from '../lib/api';
import type { MCPServerStatus, Tool } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

const releaseReport: GroupsReport = {
  configured: true,
  groups: [
    {
      name: 'release',
      description: 'Release engineering bundle',
      endpoint: '/groups/release/mcp',
      member_count: 2,
      tools: ['github__search_code', 'shout'],
      members: [
        {
          name: 'shout',
          canonical: 'github__create_issue',
          description: 'File a release-blocking issue.',
          annotations: { destructiveHint: true },
          renamed: true,
          rewritten: true,
        },
        {
          name: 'github__search_code',
          canonical: 'github__search_code',
          description: 'Searches code.',
          annotations: { readOnlyHint: true },
        },
      ],
      overrides: { github__create_issue: 'shout' },
    },
  ],
};

const catalog: Tool[] = [
  { name: 'github__create_issue', description: 'Creates an issue in a repository.', inputSchema: {} } as Tool,
  { name: 'github__search_code', description: 'Searches code.', inputSchema: {} } as Tool,
];

beforeEach(() => {
  cleanup();
});

describe('GroupsPanel', () => {
  it('shows the empty hint when unconfigured', () => {
    render(
      <GroupsPanel isOpen onClose={() => {}} report={{ configured: false, groups: [] }} toolCatalog={[]} />,
    );
    expect(screen.getByText('No tool groups configured.')).toBeInTheDocument();
  });

  it('lists groups with member counts and the link hint', () => {
    render(<GroupsPanel isOpen onClose={() => {}} report={releaseReport} toolCatalog={catalog} />);
    // "release" appears in both the list and the detail header.
    expect(screen.getAllByText('release').length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText('2 tools')).toBeInTheDocument();
    expect(screen.getByText(/gridctl link <client> --group release/)).toBeInTheDocument();
  });

  it('shows rename origin, rewritten-beside-original, and annotation chips', () => {
    render(<GroupsPanel isOpen onClose={() => {}} report={releaseReport} toolCatalog={catalog} />);

    // Exposed name plus canonical origin for the renamed member.
    expect(screen.getByText('shout')).toBeInTheDocument();
    expect(screen.getByText('github__create_issue')).toBeInTheDocument();

    // Rewritten description labeled, original shown beside it.
    expect(screen.getByText('rewritten')).toBeInTheDocument();
    expect(screen.getByText('File a release-blocking issue.')).toBeInTheDocument();
    expect(screen.getByText('original')).toBeInTheDocument();
    expect(screen.getByText('Creates an issue in a repository.')).toBeInTheDocument();

    // Annotation chips: injected destructive on the rename, read-only on the other.
    expect(screen.getByText('DESTR')).toBeInTheDocument();
    expect(screen.getByText('RO')).toBeInTheDocument();
  });

  it('copies the absolute endpoint URL', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });

    render(<GroupsPanel isOpen onClose={() => {}} report={releaseReport} toolCatalog={catalog} />);
    fireEvent.click(screen.getByRole('button', { name: /Copy endpoint URL/ }));

    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/groups/release/mcp`),
    );
  });
});

describe('groups helpers', () => {
  it('resolves group membership by canonical name', () => {
    expect(groupsForTool(releaseReport, 'github__create_issue')).toEqual(['release']);
    expect(groupsForTool(releaseReport, 'github__search_code')).toEqual(['release']);
    expect(groupsForTool(releaseReport, 'github__delete_repo')).toEqual([]);
    expect(groupsForTool({ configured: false, groups: [] }, 'github__create_issue')).toEqual([]);
    expect(groupsForTool(null, 'github__create_issue')).toEqual([]);
  });

  it('renders only declared annotation hints as chips', () => {
    expect(annotationChips(undefined)).toEqual([]);
    const labels = annotationChips({ readOnlyHint: true, destructiveHint: false }).map((c) => c.label);
    expect(labels).toEqual(['RO', 'SAFE']);
  });
});

describe('ToolsWorkspace groups integration', () => {
  function seedStore() {
    const github: MCPServerStatus = {
      name: 'github',
      transport: 'stdio',
      initialized: true,
      toolCount: 2,
      tools: ['create_issue', 'search_code'],
      healthy: true,
    } as unknown as MCPServerStatus;
    useStackStore.setState({
      mcpServers: [github],
      toolCatalog: catalog.map((t) => ({ ...t })),
      isLoading: false,
    });
  }

  it('hides the Groups button when unconfigured', async () => {
    vi.spyOn(api, 'fetchGroups').mockResolvedValue({ configured: false, groups: [] });
    seedStore();
    render(<ToolsWorkspace />, { wrapper: MemoryRouter });
    await waitFor(() => expect(api.fetchGroups).toHaveBeenCalled());
    expect(screen.queryByRole('button', { name: 'Open tool groups' })).not.toBeInTheDocument();
  });

  it('shows the Groups button and membership badges when configured', async () => {
    vi.spyOn(api, 'fetchGroups').mockResolvedValue(releaseReport);
    seedStore();
    render(<ToolsWorkspace />, { wrapper: MemoryRouter });

    await screen.findByRole('button', { name: 'Open tool groups' });

    // Both github tools are members; their rows carry the release badge.
    const badges = await screen.findAllByTitle('In tool group "release"');
    expect(badges.length).toBe(2);

    // The delimiter sanity: badges keyed on canonical server__tool names.
    expect(`github${TOOL_NAME_DELIMITER}create_issue`).toBe('github__create_issue');
  });
});
