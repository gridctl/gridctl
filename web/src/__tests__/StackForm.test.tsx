import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { StackForm } from '../components/wizard/steps/StackForm';
import type { StackFormData } from '../lib/yaml-builder';
import { buildYAML } from '../lib/yaml-builder';

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
    expect(screen.getByText('Gateway Advanced')).toBeInTheDocument();
    expect(screen.getByText('Network')).toBeInTheDocument();
    expect(screen.getByText('Secrets')).toBeInTheDocument();
    expect(screen.getByText('MCP Servers')).toBeInTheDocument();
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
    });
    render(<StackForm data={data} onChange={onChange} />);
    // Server count badge should show "2"
    expect(screen.getByText('2')).toBeInTheDocument();
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

describe('StackForm gateway api_key auth', () => {
  let onChange: typeof noop;
  beforeEach(() => { onChange = vi.fn<typeof noop>(); });

  it('shows api_key option in auth dropdown when gateway section is expanded', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway'));
    const select = screen.getByDisplayValue('');
    expect(select.querySelector('option[value="api_key"]') ?? select).toBeTruthy();
  });

  it('shows header name field for api_key auth type', () => {
    render(
      <StackForm
        data={defaultData({ gateway: { auth: { type: 'api_key', token: '' } } })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Gateway'));
    expect(screen.getByPlaceholderText('X-API-Key')).toBeInTheDocument();
    expect(screen.getByText('Header that carries the API key value')).toBeInTheDocument();
  });

  it('does not show header helper text for bearer auth type', () => {
    render(
      <StackForm
        data={defaultData({ gateway: { auth: { type: 'bearer', token: '' } } })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Gateway'));
    expect(screen.queryByText('Header that carries the API key value')).not.toBeInTheDocument();
  });
});

describe('StackForm Gateway Advanced section', () => {
  let onChange: typeof noop;
  beforeEach(() => { onChange = vi.fn<typeof noop>(); });

  it('shows Gateway Advanced section collapsed by default', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByText('Gateway Advanced')).toBeInTheDocument();
    expect(screen.queryByText('Tracing')).not.toBeInTheDocument();
  });

  it('reveals sub-groups when expanded', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway Advanced'));
    expect(screen.getByText('Tracing')).toBeInTheDocument();
    expect(screen.getByText('Schema Pinning')).toBeInTheDocument();
    expect(screen.getByText('Performance')).toBeInTheDocument();
  });

  it('shows tracing fields when expanded', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway Advanced'));
    expect(screen.getByPlaceholderText('1.0')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('24h')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('65536')).toBeInTheDocument();
  });

  it('shows OTLP endpoint only when export is otlp', () => {
    render(
      <StackForm
        data={defaultData({ gateway: { tracing: { export: 'otlp' } } })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Gateway Advanced'));
    expect(screen.getByPlaceholderText('http://localhost:4318')).toBeInTheDocument();
  });

  it('hides OTLP endpoint when export is not set', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway Advanced'));
    expect(screen.queryByPlaceholderText('http://localhost:4318')).not.toBeInTheDocument();
  });

  it('shows schema pinning action dropdown only when schema pinning is enabled', () => {
    render(
      <StackForm
        data={defaultData({ gateway: { security: { schemaPinning: { enabled: true } } } })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Gateway Advanced'));
    expect(screen.getByText('Warn (log and continue)')).toBeInTheDocument();
    expect(screen.getByText('Block (reject tool calls)')).toBeInTheDocument();
  });

  it('hides schema pinning action dropdown when schema pinning is disabled', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway Advanced'));
    expect(screen.queryByText('Warn (log and continue)')).not.toBeInTheDocument();
  });

  it('shows code_mode_timeout only when codeMode is on', () => {
    render(
      <StackForm
        data={defaultData({ gateway: { codeMode: 'on' } })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Gateway Advanced'));
    expect(screen.getByPlaceholderText('30')).toBeInTheDocument();
  });

  it('hides code_mode_timeout when codeMode is not on', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway Advanced'));
    expect(screen.queryByPlaceholderText('30')).not.toBeInTheDocument();
  });

  it('shows badge count when advanced fields are set', () => {
    render(
      <StackForm
        data={defaultData({
          gateway: {
            tracing: { export: 'otlp' },
            maxToolResultBytes: 32768,
          },
        })}
        onChange={onChange}
      />,
    );
    // badge shows count of non-empty advanced fields (export=1, maxToolResultBytes=1 → badge "2")
    expect(screen.getByText('2')).toBeInTheDocument();
  });
});

describe('YAML serialization — gateway advanced fields', () => {
  it('serializes api_key auth with header', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: {
        name: 'my-stack',
        gateway: { auth: { type: 'api_key', token: 'MY_TOKEN', header: 'X-API-Key' } },
      },
    });
    expect(yaml).toContain('type: api_key');
    expect(yaml).toContain('token: MY_TOKEN');
    expect(yaml).toContain('header: X-API-Key');
  });

  it('serializes tracing block with OTLP endpoint', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: {
        name: 'my-stack',
        gateway: {
          tracing: {
            enabled: false,
            export: 'otlp',
            endpoint: 'http://otel-collector:4318',
          },
        },
      },
    });
    expect(yaml).toContain('tracing:');
    expect(yaml).toContain('enabled: false');
    expect(yaml).toContain('export: otlp');
    expect(yaml).toContain('endpoint:');
  });

  it('omits tracing block when no fields set', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: { name: 'my-stack', gateway: { tracing: {} } },
    });
    expect(yaml).not.toContain('tracing:');
  });

  it('omits sampling when set to default 1.0', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: {
        name: 'my-stack',
        gateway: { tracing: { sampling: 1.0 } },
      },
    });
    expect(yaml).not.toContain('sampling');
  });

  it('serializes sampling when non-default', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: {
        name: 'my-stack',
        gateway: { tracing: { sampling: 0.5 } },
      },
    });
    expect(yaml).toContain('sampling: 0.5');
  });

  it('omits retention when set to default 24h', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: {
        name: 'my-stack',
        gateway: { tracing: { retention: '24h' } },
      },
    });
    expect(yaml).not.toContain('retention');
  });

  it('serializes schema_pinning block', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: {
        name: 'my-stack',
        gateway: {
          security: {
            schemaPinning: { enabled: true, action: 'block' },
          },
        },
      },
    });
    expect(yaml).toContain('security:');
    expect(yaml).toContain('schema_pinning:');
    expect(yaml).toContain('enabled: true');
    expect(yaml).toContain('action: block');
  });

  it('omits security block when schema pinning is not enabled', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: { name: 'my-stack', gateway: {} },
    });
    expect(yaml).not.toContain('security:');
  });

  it('serializes codeModeTimeout and maxToolResultBytes', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: {
        name: 'my-stack',
        gateway: { codeMode: 'on', codeModeTimeout: 60, maxToolResultBytes: 32768 },
      },
    });
    expect(yaml).toContain('code_mode_timeout: 60');
    expect(yaml).toContain('maxToolResultBytes: 32768');
  });
});
