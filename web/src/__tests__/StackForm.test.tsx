import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { StackForm } from '../components/wizard/steps/StackForm';
import type { StackFormData } from '../lib/yaml-builder';

// Mock SecretsPopover to avoid vault API calls
vi.mock('../components/wizard/SecretsPopover', () => ({
  SecretsPopover: ({ onSelect }: { onSelect: (ref: string) => void }) => (
    <button data-testid="secrets-popover" onClick={() => onSelect('${vault:TEST}')}>
      vault
    </button>
  ),
}));

function defaultData(overrides?: Partial<StackFormData>): StackFormData {
  return { name: '', version: '1', ...overrides };
}

const noop = (_data: Partial<StackFormData>) => {};

describe('StackForm', () => {
  let onChange: typeof noop;

  beforeEach(() => {
    onChange = vi.fn<typeof noop>();
  });

  it('renders all 7 accordion sections', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByText('Identity')).toBeInTheDocument();
    expect(screen.getByText('Gateway')).toBeInTheDocument();
    expect(screen.getByText('Network')).toBeInTheDocument();
    expect(screen.getByText('Secrets')).toBeInTheDocument();
    expect(screen.getByText('MCP Servers')).toBeInTheDocument();
    expect(screen.getByText('Agents')).toBeInTheDocument();
    expect(screen.getByText('Resources')).toBeInTheDocument();
  });

  it('renders identity section with name and version', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByPlaceholderText('my-stack')).toBeInTheDocument();
    // Version is locked and displayed as text
    expect(screen.getByText('1')).toBeInTheDocument();
    expect(screen.getByText('locked')).toBeInTheDocument();
  });

  it('enforces kebab-case on name input', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    const nameInput = screen.getByPlaceholderText('my-stack');
    fireEvent.change(nameInput, { target: { value: 'My Stack Name' } });
    expect(onChange).toHaveBeenCalledWith({ name: 'my-stack-name' });
  });

  it('shows gateway section with auth fields when expanded', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    // Click to expand Gateway section
    fireEvent.click(screen.getByText('Gateway'));
    expect(screen.getByText('Authentication')).toBeInTheDocument();
    expect(screen.getByText('Code Mode')).toBeInTheDocument();
    expect(screen.getByText('Output Format')).toBeInTheDocument();
    expect(screen.getByText('Allowed Origins')).toBeInTheDocument();
  });

  it('shows network section with name and driver', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Network'));
    expect(screen.getByPlaceholderText('gridctl-net')).toBeInTheDocument();
    // Driver select should have bridge option
    expect(screen.getByText('bridge')).toBeInTheDocument();
  });

  it('shows secrets section and allows adding set names', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Secrets'));
    expect(screen.getByText('Variable Sets')).toBeInTheDocument();
    expect(screen.getByText('Add set')).toBeInTheDocument();
  });

  it('can add MCP servers', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('MCP Servers'));
    fireEvent.click(screen.getByText('Add MCP Server'));
    expect(onChange).toHaveBeenCalledWith({
      mcpServers: [{ name: '', serverType: 'container' }],
    });
  });

  it('can remove MCP servers', () => {
    const data = defaultData({
      mcpServers: [
        { name: 'server-1', serverType: 'container' },
        { name: 'server-2', serverType: 'external' },
      ],
    });
    render(<StackForm data={data} onChange={onChange} />);
    fireEvent.click(screen.getByText('MCP Servers'));

    // Both servers should appear
    expect(screen.getByText('server-1')).toBeInTheDocument();
    expect(screen.getByText('server-2')).toBeInTheDocument();

    // Click remove on the first server
    const removeButtons = screen.getAllByTitle('Remove');
    fireEvent.click(removeButtons[0]);
    expect(onChange).toHaveBeenCalledWith({
      mcpServers: [{ name: 'server-2', serverType: 'external' }],
    });
  });

  it('can add agents', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Agents'));
    fireEvent.click(screen.getByText('Add Agent'));
    expect(onChange).toHaveBeenCalledWith({
      agents: [{ name: '', agentType: 'container' }],
    });
  });

  it('shows available servers in agent uses selection', () => {
    const data = defaultData({
      mcpServers: [
        { name: 'filesystem', serverType: 'container' },
        { name: 'github', serverType: 'container' },
      ],
      agents: [{ name: 'my-agent', agentType: 'container' }],
    });
    render(<StackForm data={data} onChange={onChange} />);
    fireEvent.click(screen.getByText('Agents'));

    // Expand the agent
    fireEvent.click(screen.getByText('my-agent'));

    // Server names should appear as selectable options
    expect(screen.getByText('filesystem')).toBeInTheDocument();
    expect(screen.getByText('github')).toBeInTheDocument();
  });

  it('can add resources with presets', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Resources'));
    // Click Add Resource to show presets
    fireEvent.click(screen.getByText('Add Resource'));
    // Preset cards should appear
    expect(screen.getByText('PostgreSQL')).toBeInTheDocument();
    expect(screen.getByText('Redis')).toBeInTheDocument();
    expect(screen.getByText('MySQL')).toBeInTheDocument();
    expect(screen.getByText('MongoDB')).toBeInTheDocument();
    expect(screen.getByText('Custom')).toBeInTheDocument();
  });

  it('applies preset data when selecting a resource preset', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Resources'));
    fireEvent.click(screen.getByText('Add Resource'));
    fireEvent.click(screen.getByText('PostgreSQL'));
    expect(onChange).toHaveBeenCalledWith({
      resources: [
        expect.objectContaining({
          name: 'postgres',
          image: 'postgres:16',
          ports: ['5432:5432'],
        }),
      ],
    });
  });

  it('shows badge counts for populated sections', () => {
    const data = defaultData({
      mcpServers: [{ name: 's1', serverType: 'container' }, { name: 's2', serverType: 'container' }],
      agents: [{ name: 'a1', agentType: 'container' }, { name: 'a2', agentType: 'headless' }, { name: 'a3', agentType: 'container' }],
    });
    render(<StackForm data={data} onChange={onChange} />);
    // Server count badge should show "2", agent count badge should show "3"
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
  });

  it('displays gateway auth token field when bearer is selected', () => {
    const data = defaultData({
      gateway: {
        auth: { type: 'bearer', token: '' },
      },
    });
    render(<StackForm data={data} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway'));
    expect(screen.getByPlaceholderText('auth-token-value')).toBeInTheDocument();
  });

  it('displays header name field when header auth is selected', () => {
    const data = defaultData({
      gateway: {
        auth: { type: 'header', token: '', header: '' },
      },
    });
    render(<StackForm data={data} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway'));
    expect(screen.getByPlaceholderText('X-API-Key')).toBeInTheDocument();
  });

  it('can remove agents', () => {
    const data = defaultData({
      agents: [
        { name: 'agent-1', agentType: 'container' },
        { name: 'agent-2', agentType: 'headless' },
      ],
    });
    render(<StackForm data={data} onChange={onChange} />);
    fireEvent.click(screen.getByText('Agents'));

    const removeButtons = screen.getAllByTitle('Remove');
    fireEvent.click(removeButtons[0]);
    expect(onChange).toHaveBeenCalledWith({
      agents: [{ name: 'agent-2', agentType: 'headless' }],
    });
  });

  it('can remove resources', () => {
    const data = defaultData({
      resources: [
        { name: 'pg', image: 'postgres:16' },
        { name: 'redis', image: 'redis:7-alpine' },
      ],
    });
    render(<StackForm data={data} onChange={onChange} />);
    fireEvent.click(screen.getByText('Resources'));

    const removeButtons = screen.getAllByTitle('Remove');
    fireEvent.click(removeButtons[0]);
    expect(onChange).toHaveBeenCalledWith({
      resources: [{ name: 'redis', image: 'redis:7-alpine' }],
    });
  });
});
