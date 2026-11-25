import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { StackForm } from '../components/wizard/steps/StackForm';
import { MCPServerForm } from '../components/wizard/steps/MCPServerForm';
import type { MCPServerFormData, StackFormData } from '../lib/yaml-builder';
import { buildYAML } from '../lib/yaml-builder';
import { fetchPythonPackageVersions, fetchVariableSets } from '../lib/api';

// Mock VariablesPopover to avoid variable store API calls.
vi.mock('../components/wizard/VariablesPopover', () => ({
  VariablesPopover: ({ onSelect }: { onSelect: (ref: string) => void }) => (
    <button data-testid="variables-popover" onClick={() => onSelect('${var:TEST}')}>
      var
    </button>
  ),
}));

// Mock vault API — default to empty sets; individual tests override as needed
vi.mock('../lib/api', () => ({
  fetchVariableSets: vi.fn().mockResolvedValue([]),
  fetchPythonPackageVersions: vi.fn().mockResolvedValue({ versions: ['1.0.0'], latest: '1.0.0' }),
}));

function defaultData(overrides?: Partial<StackFormData>): StackFormData {
  return { name: '', version: '1', ...overrides };
}

type OnChange = (data: Partial<StackFormData>) => void;

describe('StackForm', () => {
  let onChange: OnChange;

  beforeEach(() => {
    onChange = vi.fn<OnChange>();
  });

  it('renders all 8 accordion sections', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByText('Identity')).toBeInTheDocument();
    expect(screen.getByText('Gateway')).toBeInTheDocument();
    expect(screen.getByText('Gateway Advanced')).toBeInTheDocument();
    expect(screen.getByText('Network')).toBeInTheDocument();
    expect(screen.getByText('Logging')).toBeInTheDocument();
    expect(screen.getByText('Variable sets')).toBeInTheDocument();
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

  it('preserves trailing hyphen mid-type', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    const nameInput = screen.getByPlaceholderText('my-stack');
    fireEvent.change(nameInput, { target: { value: 'test-' } });
    expect(onChange).toHaveBeenCalledWith({ name: 'test-' });
  });

  it('strips trailing hyphen on blur', () => {
    render(<StackForm data={defaultData({ name: 'test-' })} onChange={onChange} />);
    const nameInput = screen.getByPlaceholderText('my-stack');
    fireEvent.blur(nameInput);
    expect(onChange).toHaveBeenCalledWith({ name: 'test' });
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
    fireEvent.click(screen.getByText('Variable sets'));
    expect(screen.getByText('Variable Sets')).toBeInTheDocument();
    expect(screen.getByText('Add set')).toBeInTheDocument();
  });

  it('shows vault set dropdown with existing sets when Add set is clicked', async () => {
    vi.mocked(fetchVariableSets).mockResolvedValueOnce([
      { name: 'dev', count: 2 },
      { name: 'prod', count: 5 },
    ]);
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Variable sets'));
    fireEvent.click(screen.getByText('Add set'));
    await waitFor(() => {
      expect(screen.getByText('dev')).toBeInTheDocument();
      expect(screen.getByText('prod')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('dev'));
    expect(onChange).toHaveBeenCalledWith({ secrets: { sets: ['dev'] } });
  });

  it('associates the stack Name label with its input', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByLabelText(/^Name/)).toHaveAttribute('placeholder', 'my-stack');
  });

  it('names the server remove button after the row', () => {
    render(
      <StackForm
        data={defaultData({ mcpServers: [{ name: 'alpha', serverType: 'container' }] })}
        onChange={onChange}
      />,
    );
    expect(screen.getByRole('button', { name: 'Remove alpha' })).toBeInTheDocument();
  });

  it('shows the first server form on mount when the stack has servers', () => {
    render(
      <StackForm
        data={defaultData({ mcpServers: [{ name: 'alpha', serverType: 'container' }] })}
        onChange={onChange}
      />,
    );
    expect(screen.getByDisplayValue('alpha')).toBeInTheDocument();
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
  let onChange: OnChange;
  beforeEach(() => { onChange = vi.fn<OnChange>(); });

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
  let onChange: OnChange;
  beforeEach(() => { onChange = vi.fn<OnChange>(); });

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

describe('StackForm Logging section', () => {
  let onChange: OnChange;
  beforeEach(() => { onChange = vi.fn<OnChange>(); });

  it('renders Logging accordion collapsed by default', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByText('Logging')).toBeInTheDocument();
    expect(screen.queryByPlaceholderText('/var/log/gridctl.log')).not.toBeInTheDocument();
  });

  it('reveals all four fields when expanded', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Logging'));
    expect(screen.getByPlaceholderText('/var/log/gridctl.log')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('100')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('7')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('3')).toBeInTheDocument();
  });

  it('shows configured badge when file is set', () => {
    render(
      <StackForm
        data={defaultData({ logging: { file: '/var/log/gridctl.log' } })}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('configured')).toBeInTheDocument();
  });

  it('shows no badge when file is not set', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    expect(screen.queryByText('configured')).not.toBeInTheDocument();
  });
});

describe('YAML serialization — logging fields', () => {
  it('emits logging block when file is set', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: { name: 'my-stack', logging: { file: '/var/log/gridctl.log' } },
    });
    expect(yaml).toContain('logging:');
    expect(yaml).toContain('file: /var/log/gridctl.log');
  });

  it('omits logging block when file is not set', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: { name: 'my-stack', logging: { maxSizeMB: 200 } },
    });
    expect(yaml).not.toContain('logging:');
  });

  it('includes rotation fields when set alongside file', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: {
        name: 'my-stack',
        logging: { file: '/var/log/gridctl.log', maxSizeMB: 200, maxAgeDays: 14, maxBackups: 5 },
      },
    });
    expect(yaml).toContain('maxSizeMB: 200');
    expect(yaml).toContain('maxAgeDays: 14');
    expect(yaml).toContain('maxBackups: 5');
  });

  it('omits rotation fields when not set', () => {
    const yaml = buildYAML({
      type: 'stack',
      data: { name: 'my-stack', logging: { file: '/var/log/gridctl.log' } },
    });
    expect(yaml).not.toContain('maxSizeMB');
    expect(yaml).not.toContain('maxAgeDays');
    expect(yaml).not.toContain('maxBackups');
  });
});

describe('StackForm tracing select', () => {
  let onChange: OnChange;

  beforeEach(() => {
    onChange = vi.fn<OnChange>();
  });

  function tracingSelect(): HTMLSelectElement {
    fireEvent.click(screen.getByText('Gateway Advanced'));
    return screen.getByLabelText('Tracing enabled') as HTMLSelectElement;
  }

  it('offers Default (on), On, and Off and defaults to Default', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    const select = tracingSelect();
    expect(select.tagName).toBe('SELECT');
    expect(select.value).toBe('');
    expect(Array.from(select.options).map((o) => o.text)).toEqual(['Default (on)', 'On', 'Off']);
    expect(screen.queryByText('Default: on')).not.toBeInTheDocument();
  });

  it('writes false, true, and undefined', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    const select = tracingSelect();
    const enabledAfter = (value: string) => {
      fireEvent.change(select, { target: { value } });
      const last = vi.mocked(onChange).mock.lastCall![0];
      return last.gateway?.tracing && 'enabled' in last.gateway.tracing ? last.gateway.tracing.enabled : 'missing';
    };
    expect(enabledAfter('false')).toBe(false);
    expect(enabledAfter('true')).toBe(true);
    expect(enabledAfter('')).toBeUndefined();
  });

  it('shows stored values', () => {
    const { unmount } = render(
      <StackForm data={defaultData({ gateway: { tracing: { enabled: true } } })} onChange={onChange} />,
    );
    expect(tracingSelect().value).toBe('true');
    unmount();
    render(<StackForm data={defaultData({ gateway: { tracing: { enabled: false } } })} onChange={onChange} />);
    expect(tracingSelect().value).toBe('false');
  });

  it('leaves the schema pinning checkbox as the only Enabled label', () => {
    render(<StackForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('Gateway Advanced'));
    const pinning = screen.getByLabelText('Enabled') as HTMLInputElement;
    expect(pinning.type).toBe('checkbox');
    fireEvent.click(pinning);
    expect(vi.mocked(onChange).mock.lastCall![0].gateway?.security?.schemaPinning?.enabled).toBe(true);
  });

  it('serializes enabled: false and omits an empty tracing block', () => {
    const off = buildYAML({
      type: 'stack',
      data: { name: 'my-stack', version: '1', gateway: { tracing: { enabled: false } } },
    });
    expect(off).toContain('tracing:');
    expect(off).toContain('enabled: false');
    const empty = buildYAML({ type: 'stack', data: { name: 'my-stack', version: '1', gateway: { tracing: {} } } });
    expect(empty).not.toContain('tracing:');
    expect(empty).not.toContain('enabled:');
  });
});

function expectUniqueIdsAndResolvedRefs(root: HTMLElement) {
  const ids = Array.from(root.querySelectorAll('[id]')).map((el) => el.id);
  const duplicates = ids.filter((id, i) => ids.indexOf(id) !== i);
  expect(duplicates).toEqual([]);
  const count = (id: string) => ids.filter((x) => x === id).length;
  root.querySelectorAll('label[for]').forEach((label) => {
    expect(count(label.getAttribute('for')!)).toBe(1);
  });
  root.querySelectorAll('[list]').forEach((el) => {
    expect(count(el.getAttribute('list')!)).toBe(1);
  });
  root.querySelectorAll('[aria-describedby]').forEach((el) => {
    for (const token of el.getAttribute('aria-describedby')!.split(/\s+/).filter(Boolean)) {
      expect(count(token)).toBe(1);
    }
  });
}

describe('StackForm element ids across servers', () => {
  const servers: MCPServerFormData[] = [
    {
      name: 'srv-container',
      serverType: 'container',
      image: 'mcp/fetch:latest',
      autoscale: { min: 0, max: 3, targetInFlight: 4, idleToZero: true },
    },
    { name: 'srv-pypi', serverType: 'source', transport: 'stdio', source: { type: 'pypi', runtime: 'python', package: 'mcp-server-fetch', ref: '1.0.0' } },
    { name: 'srv-git', serverType: 'source', transport: 'stdio', source: { type: 'git', url: 'https://github.com/o/r.git', ref: 'main', runtime: 'python' } },
    { name: 'srv-local', serverType: 'source', transport: 'http', source: { type: 'local', path: './server', dockerfile: 'Dockerfile' } },
    { name: 'srv-api-one', serverType: 'openapi', transport: '', openapi: { spec: 'https://a.example.com/spec.yaml', tls: {} } },
    { name: 'srv-api-two', serverType: 'openapi', transport: '', openapi: { spec: 'https://b.example.com/spec.yaml', tls: {} } },
  ];

  it('renders unique ids whose references resolve, and the second TLS label targets the second server', async () => {
    const onChange = vi.fn<OnChange>();
    const { container } = render(<StackForm data={defaultData({ mcpServers: servers })} onChange={onChange} />);
    // The section and the first server start expanded when servers are present.
    for (const server of servers.slice(1)) fireEvent.click(screen.getByText(server.name));
    for (const advanced of screen.getAllByText('Advanced', { exact: true })) fireEvent.click(advanced);
    for (const tls of screen.getAllByText('TLS / mTLS')) fireEvent.click(tls);
    await waitFor(() => expect(fetchPythonPackageVersions).toHaveBeenCalledWith('mcp-server-fetch'));

    // getByLabelText scans every element of this large tree per call and
    // exceeded the test timeout on CI under coverage; resolve labels directly.
    const labeled = (matcher: string | RegExp) =>
      Array.from(container.querySelectorAll('label'))
        .filter((label) => {
          const text = (label.textContent ?? '').replace(/\s+/g, ' ').trim();
          return typeof matcher === 'string' ? text === matcher : matcher.test(text);
        })
        .map((label) => label.control);
    for (const controls of [
      labeled('Scale to zero when idle'),
      labeled(/^Project name/),
      labeled(/^Repository URL/),
      labeled('Ref'),
      labeled(/^Local root/),
      labeled('Project subdirectory'),
    ]) {
      expect(controls).toHaveLength(1);
      expect(controls[0]).toBeInstanceOf(HTMLElement);
    }
    expect(labeled('Dockerfile')).toHaveLength(2);
    expect(labeled('Volumes').length).toBeGreaterThanOrEqual(1);
    expect(labeled('Execution configuration')).toHaveLength(servers.length);
    for (const control of [...labeled('Dockerfile'), ...labeled('Volumes'), ...labeled('Execution configuration')]) {
      expect(control).toBeInstanceOf(HTMLElement);
    }

    expectUniqueIdsAndResolvedRefs(container);

    vi.mocked(onChange).mockClear();
    const skipLabels = screen.getAllByText('Skip TLS Verification');
    expect(skipLabels).toHaveLength(2);
    fireEvent.click(skipLabels[1]);
    expect(onChange).toHaveBeenCalledTimes(1);
    const updated = vi.mocked(onChange).mock.calls[0][0].mcpServers!;
    expect(updated[5].openapi?.tls?.insecureSkipVerify).toBe(true);
    expect(updated[4].openapi?.tls?.insecureSkipVerify).toBeUndefined();
  });
});

describe('MCPServerForm error ids across forms', () => {
  function Pair({ a, b, errors }: { a: MCPServerFormData; b: MCPServerFormData; errors: Record<string, string> }) {
    return (
      <>
        <div data-testid="form-a"><MCPServerForm data={a} onChange={vi.fn()} errors={errors} /></div>
        <div data-testid="form-b"><MCPServerForm data={b} onChange={vi.fn()} errors={errors} /></div>
      </>
    );
  }

  function expectDescribedByOwnError(form: HTMLElement, label: RegExp, message: string) {
    const input = within(form).getByLabelText(label);
    const ids = (input.getAttribute('aria-describedby') ?? '').split(/\s+/).filter(Boolean);
    expect(ids.length).toBeGreaterThan(0);
    const targets = ids.map((id) => document.getElementById(id));
    expect(targets.every((t) => t !== null && form.contains(t))).toBe(true);
    expect(targets.some((t) => t?.textContent?.includes(message))).toBe(true);
  }

  it('keeps PyPI package and version errors in their own form', async () => {
    const pypi: MCPServerFormData = { name: 'p', serverType: 'source', transport: 'stdio', source: { type: 'pypi', runtime: 'python', package: 'pkg', ref: '1.0.0' } };
    const { container } = render(
      <Pair a={pypi} b={{ ...pypi, name: 'q' }} errors={{ 'source.package': 'Package is required', 'source.ref': 'Version is required' }} />,
    );
    await waitFor(() => expect(fetchPythonPackageVersions).toHaveBeenCalledWith('pkg'));
    expectUniqueIdsAndResolvedRefs(container);
    for (const form of [screen.getByTestId('form-a'), screen.getByTestId('form-b')]) {
      expectDescribedByOwnError(form, /^Project name/, 'Package is required');
      expectDescribedByOwnError(form, /^Exact version/, 'Version is required');
      const version = within(form).getByLabelText(/^Exact version/);
      const list = document.getElementById(version.getAttribute('list')!);
      expect(list?.tagName).toBe('DATALIST');
      expect(form.contains(list)).toBe(true);
    }
  });

  it('keeps Git URL and local path errors in their own form', () => {
    const git: MCPServerFormData = { name: 'g', serverType: 'source', transport: 'http', source: { type: 'git', url: '', dockerfile: 'Dockerfile' } };
    const local: MCPServerFormData = { name: 'l', serverType: 'source', transport: 'http', source: { type: 'local', path: '', dockerfile: 'Dockerfile' } };
    const errors = { 'source.url': 'URL is required', 'source.path': 'Path is required' };
    const { container, unmount } = render(<Pair a={git} b={{ ...git, name: 'h' }} errors={errors} />);
    expectUniqueIdsAndResolvedRefs(container);
    for (const form of [screen.getByTestId('form-a'), screen.getByTestId('form-b')]) {
      expectDescribedByOwnError(form, /^Repository URL/, 'URL is required');
    }
    unmount();
    const second = render(<Pair a={local} b={{ ...local, name: 'm' }} errors={errors} />);
    expectUniqueIdsAndResolvedRefs(second.container);
    for (const form of [screen.getByTestId('form-a'), screen.getByTestId('form-b')]) {
      expectDescribedByOwnError(form, /^Local root/, 'Path is required');
    }
  });
});
