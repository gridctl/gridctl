import { describe, it, expect, vi, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { ServerToolScopeGroup, type ScopeTool } from '../components/stack/ServerToolScopeGroup';

const TOOLS: ScopeTool[] = [
  { name: 'search-repos', description: 'Search repositories' },
  { name: 'create-issue', description: 'Open an issue' },
];

function noop() {}

function renderGroup(overrides: Partial<React.ComponentProps<typeof ServerToolScopeGroup>> = {}) {
  const props: React.ComponentProps<typeof ServerToolScopeGroup> = {
    serverName: 'github',
    availableTools: TOOLS,
    mode: 'all',
    selected: new Set<string>(),
    onModeChange: noop,
    onToggleTool: noop,
    onSelectAll: noop,
    onClear: noop,
    ...overrides,
  };
  return render(<ServerToolScopeGroup {...props} />);
}

beforeEach(() => cleanup());

describe('ServerToolScopeGroup', () => {
  it('renders a positive "all" state, not a checklist, in all mode', () => {
    renderGroup({ mode: 'all' });
    expect(screen.getByText(/All 2 tools/i)).toBeInTheDocument();
    // No tool checkboxes are shown in all mode.
    expect(screen.queryByRole('checkbox', { name: /search-repos/i })).not.toBeInTheDocument();
  });

  it('switches mode via the segmented toggle', () => {
    const onModeChange = vi.fn();
    renderGroup({ mode: 'all', onModeChange });
    fireEvent.click(screen.getByRole('button', { name: 'Custom' }));
    expect(onModeChange).toHaveBeenCalledWith('custom');
  });

  it('keeps the checklist collapsed by default, behind a count summary', () => {
    renderGroup({ mode: 'custom', selected: new Set(['search-repos']) });
    // The count is always visible in the header...
    expect(screen.getByText('1/2')).toBeInTheDocument();
    // ...but the tool rows are not, until the operator opens the list.
    expect(screen.queryByText('Search repositories')).not.toBeInTheDocument();
    expect(screen.getByText(/Edit tools/i)).toBeInTheDocument();
  });

  it('shows a live count and per-tool checkboxes once expanded', () => {
    renderGroup({ mode: 'custom', selected: new Set(['search-repos']) });
    fireEvent.click(screen.getByText(/Edit tools/i));
    expect(screen.getByText('search-repos')).toBeInTheDocument();
    expect(screen.getByText('Search repositories')).toBeInTheDocument();
  });

  it('toggles a tool on click once expanded', () => {
    const onToggleTool = vi.fn();
    renderGroup({ mode: 'custom', selected: new Set(), onToggleTool });
    fireEvent.click(screen.getByText(/Edit tools/i));
    fireEvent.click(screen.getByText('create-issue'));
    expect(onToggleTool).toHaveBeenCalledWith('create-issue');
  });

  it('parent checkbox is indeterminate when only some tools are selected', () => {
    renderGroup({ mode: 'custom', selected: new Set(['search-repos']) });
    fireEvent.click(screen.getByText(/Edit tools/i));
    const parent = screen.getByRole('checkbox', { name: /Select all|Clear all/ });
    expect(parent).toHaveAttribute('aria-checked', 'mixed');
  });

  it('select-all passes the visible tool names', () => {
    const onSelectAll = vi.fn();
    renderGroup({ mode: 'custom', selected: new Set(), onSelectAll });
    fireEvent.click(screen.getByText(/Edit tools/i));
    fireEvent.click(screen.getByRole('checkbox', { name: /Select all/ }));
    expect(onSelectAll).toHaveBeenCalledWith(['search-repos', 'create-issue']);
  });

  it('flags an empty custom selection in the collapsed summary', () => {
    renderGroup({ mode: 'custom', selected: new Set() });
    expect(screen.getByText(/none yet/i)).toBeInTheDocument();
  });
});
