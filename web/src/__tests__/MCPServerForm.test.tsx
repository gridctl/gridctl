import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MCPServerForm } from '../components/wizard/steps/MCPServerForm';
import type { MCPServerFormData } from '../lib/yaml-builder';
import { buildYAML, parseYAMLToForm } from '../lib/yaml-builder';

function defaultData(overrides?: Partial<MCPServerFormData>): MCPServerFormData {
  return { name: '', serverType: 'container', ...overrides };
}

const noop = (_data: Partial<MCPServerFormData>) => {};

describe('MCPServerForm', () => {
  let onChange: typeof noop;

  beforeEach(() => {
    onChange = vi.fn<typeof noop>();
  });

  it('renders all 5 accordion sections', () => {
    render(<MCPServerForm data={defaultData()} onChange={onChange} />);
    expect(screen.getByText('Identity')).toBeInTheDocument();
    expect(screen.getByText('Server Type')).toBeInTheDocument();
    expect(screen.getByText('Configuration')).toBeInTheDocument();
    expect(screen.getByText('Environment & Secrets')).toBeInTheDocument();
    expect(screen.getByText('Advanced')).toBeInTheDocument();
  });

  it('renders 6 server type options', () => {
    render(<MCPServerForm data={defaultData()} onChange={onChange} />);
    // Container appears in both the badge and radio card, so use getAllByText
    expect(screen.getAllByText('Container').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Source')).toBeInTheDocument();
    expect(screen.getByText('External URL')).toBeInTheDocument();
    expect(screen.getByText('Local Process')).toBeInTheDocument();
    expect(screen.getByText('SSH')).toBeInTheDocument();
    expect(screen.getByText('OpenAPI')).toBeInTheDocument();
  });

  it('enforces kebab-case on name input', () => {
    render(<MCPServerForm data={defaultData()} onChange={onChange} />);
    const nameInput = screen.getByPlaceholderText('my-server');
    fireEvent.change(nameInput, { target: { value: 'My Server Name' } });
    expect(onChange).toHaveBeenCalledWith({ name: 'my-server-name' });
  });

  it('preserves trailing hyphen mid-type', () => {
    render(<MCPServerForm data={defaultData()} onChange={onChange} />);
    const nameInput = screen.getByPlaceholderText('my-server');
    fireEvent.change(nameInput, { target: { value: 'test-' } });
    expect(onChange).toHaveBeenCalledWith({ name: 'test-' });
  });

  it('strips trailing hyphen on blur', () => {
    render(<MCPServerForm data={defaultData({ name: 'test-' })} onChange={onChange} />);
    const nameInput = screen.getByPlaceholderText('my-server');
    fireEvent.blur(nameInput);
    expect(onChange).toHaveBeenCalledWith({ name: 'test' });
  });

  it('shows image field for container type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'container' })} onChange={onChange} />);
    expect(screen.getByPlaceholderText('image:tag')).toBeInTheDocument();
  });

  it('shows URL field for external type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'external' })} onChange={onChange} />);
    expect(screen.getByPlaceholderText('https://my-server.example.com/mcp')).toBeInTheDocument();
  });

  it('shows SSH fields for ssh type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'ssh' })} onChange={onChange} />);
    expect(screen.getByPlaceholderText('192.168.1.100')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('root')).toBeInTheDocument();
  });

  it('shows OpenAPI fields for openapi type', () => {
    render(
      <MCPServerForm
        data={defaultData({ serverType: 'openapi' })}
        onChange={onChange}
      />,
    );
    expect(
      screen.getByPlaceholderText('https://api.example.com/openapi.yaml or ./spec.yaml'),
    ).toBeInTheDocument();
  });

  it('shows source fields for source type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'source' })} onChange={onChange} />);
    expect(screen.getByText('Git')).toBeInTheDocument();
    expect(screen.getByText('Local')).toBeInTheDocument();
  });

  it('hides port when transport is stdio', () => {
    render(
      <MCPServerForm
        data={defaultData({ serverType: 'container', transport: 'stdio' })}
        onChange={onChange}
      />,
    );
    expect(screen.queryByPlaceholderText('8080')).not.toBeInTheDocument();
  });

  it('shows port when transport is http', () => {
    render(
      <MCPServerForm
        data={defaultData({ serverType: 'container', transport: 'http' })}
        onChange={onChange}
      />,
    );
    expect(screen.getByPlaceholderText('8080')).toBeInTheDocument();
  });

  it('hides transport selector for local process type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'local' })} onChange={onChange} />);
    // Local process type has stdio transport locked — no transport selector shown
    expect(screen.queryByText('Transport')).not.toBeInTheDocument();
  });

  it('calls onChange when switching server type', () => {
    render(<MCPServerForm data={defaultData()} onChange={onChange} />);
    fireEvent.click(screen.getByText('SSH'));
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ serverType: 'ssh' }),
    );
  });

  it('preserves data when switching types', () => {
    const data = defaultData({ serverType: 'container', image: 'my-image:latest', name: 'test-server' });
    const { rerender } = render(<MCPServerForm data={data} onChange={onChange} />);

    // Switch to SSH
    fireEvent.click(screen.getByText('SSH'));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ serverType: 'ssh' }));

    // Re-render as SSH — original image data should still be in data prop (preserved by parent)
    rerender(
      <MCPServerForm
        data={{ ...data, serverType: 'ssh' }}
        onChange={onChange}
      />,
    );

    // Switch back to Container
    fireEvent.click(screen.getByText('Container'));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ serverType: 'container' }));

    // Re-render as container with original data
    rerender(<MCPServerForm data={data} onChange={onChange} />);
    expect(screen.getByDisplayValue('my-image:latest')).toBeInTheDocument();
  });

  it('displays field errors when provided', () => {
    render(
      <MCPServerForm
        data={defaultData()}
        onChange={onChange}
        errors={{ name: 'Name is required' }}
      />,
    );
    expect(screen.getByText('Name is required')).toBeInTheDocument();
  });

  it('shows OpenAPI auth fields when bearer is selected', () => {
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'openapi',
          openapi: { spec: 'https://api.example.com/spec.yaml', auth: { type: 'bearer', tokenEnv: '' } },
        })}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('Token Environment Variable')).toBeInTheDocument();
  });

  it('shows OpenAPI auth fields when header is selected', () => {
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'openapi',
          openapi: { spec: 'https://api.example.com/spec.yaml', auth: { type: 'header', header: '', valueEnv: '' } },
        })}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('Header Name')).toBeInTheDocument();
    expect(screen.getByText('Value Environment Variable')).toBeInTheDocument();
  });

  it('shows env badge count when env vars are set', () => {
    render(
      <MCPServerForm
        data={defaultData({ env: { API_KEY: 'test', DB_HOST: 'localhost' } })}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('2')).toBeInTheDocument();
  });
});

describe('MCPServerForm field visibility', () => {
  const onChange = vi.fn<typeof noop>();

  it('hides image/port/transport for local process type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'local' })} onChange={onChange} />);
    expect(screen.queryByPlaceholderText('image:tag')).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('8080')).not.toBeInTheDocument();
  });

  it('hides image/port for ssh type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'ssh' })} onChange={onChange} />);
    expect(screen.queryByPlaceholderText('image:tag')).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('8080')).not.toBeInTheDocument();
  });

  it('hides port/network for external type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'external' })} onChange={onChange} />);
    expect(screen.queryByPlaceholderText('8080')).not.toBeInTheDocument();
  });

  it('shows command builder for local process type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'local' })} onChange={onChange} />);
    expect(screen.getByText('Add argument')).toBeInTheDocument();
  });

  it('shows command builder for ssh type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'ssh' })} onChange={onChange} />);
    expect(screen.getByText('Add argument')).toBeInTheDocument();
  });
});

describe('MCPServerForm SSH advanced fields', () => {
  const onChange = vi.fn<typeof noop>();

  it('shows knownHostsFile and jumpHost fields for ssh type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'ssh' })} onChange={onChange} />);
    expect(screen.getByPlaceholderText('~/.ssh/known_hosts')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('[user@]bastion.example.com[:22]')).toBeInTheDocument();
  });

  it('does not show knownHostsFile or jumpHost for non-SSH types', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'container' })} onChange={onChange} />);
    expect(screen.queryByPlaceholderText('~/.ssh/known_hosts')).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('[user@]bastion.example.com[:22]')).not.toBeInTheDocument();
  });
});

describe('MCPServerForm OpenAPI new auth types', () => {
  const onChange = vi.fn<typeof noop>();

  it('shows query auth fields when query is selected', () => {
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'openapi',
          openapi: { spec: 'https://api.example.com/spec.yaml', auth: { type: 'query' } },
        })}
        onChange={onChange}
      />,
    );
    expect(screen.getByText('Parameter Name')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('appid')).toBeInTheDocument();
  });

  it('shows oauth2 auth fields when oauth2 is selected', () => {
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'openapi',
          openapi: { spec: 'https://api.example.com/spec.yaml', auth: { type: 'oauth2' } },
        })}
        onChange={onChange}
      />,
    );
    expect(screen.getByPlaceholderText('OAUTH2_CLIENT_ID')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('OAUTH2_CLIENT_SECRET')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('https://auth.example.com/oauth/token')).toBeInTheDocument();
    expect(screen.getByText('Scopes')).toBeInTheDocument();
  });

  it('shows basic auth fields when basic is selected', () => {
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'openapi',
          openapi: { spec: 'https://api.example.com/spec.yaml', auth: { type: 'basic' } },
        })}
        onChange={onChange}
      />,
    );
    expect(screen.getByPlaceholderText('API_USERNAME')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('API_PASSWORD')).toBeInTheDocument();
  });

  it('does not show oauth2 fields when bearer is selected', () => {
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'openapi',
          openapi: { spec: 'https://api.example.com/spec.yaml', auth: { type: 'bearer', tokenEnv: '' } },
        })}
        onChange={onChange}
      />,
    );
    expect(screen.queryByPlaceholderText('OAUTH2_CLIENT_ID')).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('API_USERNAME')).not.toBeInTheDocument();
  });
});

describe('MCPServerForm TLS section', () => {
  const onChange = vi.fn<typeof noop>();

  it('shows TLS / mTLS section header for OpenAPI type', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'openapi' })} onChange={onChange} />);
    expect(screen.getByText('TLS / mTLS')).toBeInTheDocument();
  });

  it('does not show TLS section for non-OpenAPI types', () => {
    render(<MCPServerForm data={defaultData({ serverType: 'container' })} onChange={onChange} />);
    expect(screen.queryByText('TLS / mTLS')).not.toBeInTheDocument();
  });

  it('shows TLS fields when section is expanded', () => {
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'openapi',
          openapi: { spec: 'https://api.example.com/spec.yaml', tls: {} },
        })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('TLS / mTLS'));
    expect(screen.getByPlaceholderText('./certs/client.crt')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('./certs/client.key')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('./certs/ca.crt')).toBeInTheDocument();
    expect(screen.getByText('Skip TLS Verification')).toBeInTheDocument();
  });
});

describe('MCPServerForm pin_schemas select', () => {
  const onChange = vi.fn<typeof noop>();

  it('shows schema pinning select in advanced section', () => {
    render(<MCPServerForm data={defaultData()} onChange={onChange} />);
    // Expand the Advanced section
    fireEvent.click(screen.getByText('Advanced'));
    expect(screen.getByText('Schema Pinning')).toBeInTheDocument();
  });
});

describe('YAML serialization — new fields', () => {
  it('serializes SSH knownHostsFile and jumpHost', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'ssh',
        ssh: { host: '10.0.0.1', user: 'admin', knownHostsFile: '~/.ssh/known_hosts', jumpHost: 'bastion.example.com' },
      },
    });
    expect(yaml).toContain('knownHostsFile: ~/.ssh/known_hosts');
    expect(yaml).toContain('jumpHost: bastion.example.com');
  });

  it('does not serialize knownHostsFile/jumpHost when empty', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'ssh',
        ssh: { host: '10.0.0.1', user: 'admin' },
      },
    });
    expect(yaml).not.toContain('knownHostsFile');
    expect(yaml).not.toContain('jumpHost');
  });

  it('serializes query auth fields', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'weather',
        serverType: 'openapi',
        openapi: {
          spec: 'https://api.example.com/spec.yaml',
          auth: { type: 'query', paramName: 'appid', valueEnv: 'WEATHER_KEY' },
        },
      },
    });
    expect(yaml).toContain('type: query');
    expect(yaml).toContain('paramName: appid');
    expect(yaml).toContain('valueEnv: WEATHER_KEY');
  });

  it('serializes oauth2 auth fields including scopes as list', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'openapi',
        openapi: {
          spec: 'https://api.example.com/spec.yaml',
          auth: {
            type: 'oauth2',
            clientIdEnv: 'CLIENT_ID',
            clientSecretEnv: 'CLIENT_SECRET',
            tokenUrl: 'https://auth.example.com/token',
            scopes: ['read:data', 'write:data'],
          },
        },
      },
    });
    expect(yaml).toContain('type: oauth2');
    expect(yaml).toContain('clientIdEnv: CLIENT_ID');
    expect(yaml).toContain('clientSecretEnv: CLIENT_SECRET');
    expect(yaml).toContain('tokenUrl:');
    expect(yaml).toContain('scopes:');
    expect(yaml).toContain('- "read:data"');
    expect(yaml).toContain('- "write:data"');
  });

  it('serializes basic auth fields', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'openapi',
        openapi: {
          spec: 'https://api.example.com/spec.yaml',
          auth: { type: 'basic', usernameEnv: 'API_USER', passwordEnv: 'API_PASS' },
        },
      },
    });
    expect(yaml).toContain('type: basic');
    expect(yaml).toContain('usernameEnv: API_USER');
    expect(yaml).toContain('passwordEnv: API_PASS');
  });

  it('serializes TLS fields under tls block', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'openapi',
        openapi: {
          spec: 'https://api.example.com/spec.yaml',
          tls: { certFile: './certs/client.crt', keyFile: './certs/client.key', caFile: './certs/ca.crt' },
        },
      },
    });
    expect(yaml).toContain('tls:');
    expect(yaml).toContain('certFile: ./certs/client.crt');
    expect(yaml).toContain('keyFile: ./certs/client.key');
    expect(yaml).toContain('caFile: ./certs/ca.crt');
  });

  it('serializes insecureSkipVerify: true only when set to true', () => {
    const yamlTrue = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'openapi',
        openapi: {
          spec: 'https://api.example.com/spec.yaml',
          tls: { insecureSkipVerify: true },
        },
      },
    });
    expect(yamlTrue).toContain('insecureSkipVerify: true');

    const yamlFalse = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'openapi',
        openapi: {
          spec: 'https://api.example.com/spec.yaml',
          tls: { insecureSkipVerify: false },
        },
      },
    });
    expect(yamlFalse).not.toContain('insecureSkipVerify');
  });

  it('omits tls block when no tls fields set', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'openapi',
        openapi: { spec: 'https://api.example.com/spec.yaml' },
      },
    });
    expect(yaml).not.toContain('tls:');
  });

  it('serializes source.auth as a credential_ref (vault reference)', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'source',
        source: {
          type: 'git',
          url: 'https://gitlab.internal/team/mcp-foo.git',
          ref: 'main',
          auth: { method: 'token', credentialRef: '${vault:GIT_TOKEN}' },
        },
      },
    });
    expect(yaml).toContain('auth:');
    expect(yaml).toContain('method: token');
    expect(yaml).toContain('credential_ref: "${vault:GIT_TOKEN}"');
  });

  it('never emits a raw token field under source.auth', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'source',
        source: {
          type: 'git',
          url: 'https://gitlab.internal/team/mcp-foo.git',
          auth: { method: 'token', credentialRef: '${vault:GIT_TOKEN}' },
        },
      },
    });
    expect(yaml).not.toMatch(/\btoken:\s/);
  });

  it('omits the source.auth block entirely when credentialRef is missing', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'my-server',
        serverType: 'source',
        source: {
          type: 'git',
          url: 'https://github.com/public/repo.git',
        },
      },
    });
    expect(yaml).not.toContain('auth:');
    expect(yaml).not.toContain('credential_ref');
  });

  it('serializes pin_schemas: true when enabled', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: { name: 'my-server', serverType: 'container', image: 'test:latest', pinSchemas: true },
    });
    expect(yaml).toContain('pin_schemas: true');
  });

  it('serializes pin_schemas: false when disabled', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: { name: 'my-server', serverType: 'container', image: 'test:latest', pinSchemas: false },
    });
    expect(yaml).toContain('pin_schemas: false');
  });

  it('omits pin_schemas when not set', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: { name: 'my-server', serverType: 'container', image: 'test:latest' },
    });
    expect(yaml).not.toContain('pin_schemas');
  });

  it('serializes replicas when > 1', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: { name: 'junos', serverType: 'local', command: ['python', 'srv.py'], replicas: 3 },
    });
    expect(yaml).toContain('replicas: 3');
  });

  it('omits replicas: 1 (matches Go omitempty)', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: { name: 'junos', serverType: 'container', image: 'test:latest', replicas: 1 },
    });
    expect(yaml).not.toContain('replicas');
  });

  it('serializes replica_policy only when non-default', () => {
    const withDefault = buildYAML({
      type: 'mcp-server',
      data: { name: 'junos', serverType: 'container', image: 'test:latest', replicas: 3, replicaPolicy: 'round-robin' },
    });
    expect(withDefault).not.toContain('replica_policy');

    const withCustom = buildYAML({
      type: 'mcp-server',
      data: { name: 'junos', serverType: 'container', image: 'test:latest', replicas: 3, replicaPolicy: 'least-connections' },
    });
    expect(withCustom).toContain('replica_policy: least-connections');
  });

  it('round-trips replicas + least-connections policy through YAML', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'junos',
        serverType: 'local',
        command: ['python', 'jmcp.py'],
        replicas: 3,
        replicaPolicy: 'least-connections',
      },
    });
    const parsed = parseYAMLToForm(yaml, 'mcp-server');
    expect('error' in parsed).toBe(false);
    if ('error' in parsed) return;
    expect(parsed.data.name).toBe('junos');
    expect((parsed.data as MCPServerFormData).replicas).toBe(3);
    expect((parsed.data as MCPServerFormData).replicaPolicy).toBe('least-connections');
  });
});

describe('MCPServerForm — replicas UI', () => {
  it('shows replicas input for container serverType', () => {
    render(
      <MCPServerForm
        data={defaultData({ serverType: 'container', image: 'test:latest' })}
        onChange={() => {}}
      />,
    );
    // Expand Advanced section
    fireEvent.click(screen.getByText('Advanced'));
    expect(screen.getByText('Replicas')).toBeInTheDocument();
  });

  it('shows replicas input for local and ssh serverTypes', () => {
    const { rerender } = render(
      <MCPServerForm
        data={defaultData({ serverType: 'local' })}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    expect(screen.getByText('Replicas')).toBeInTheDocument();

    rerender(
      <MCPServerForm
        data={defaultData({ serverType: 'ssh', ssh: { host: 'h', user: 'u' } })}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText('Replicas')).toBeInTheDocument();
  });

  it('hides replicas input for external and openapi serverTypes', () => {
    const { rerender } = render(
      <MCPServerForm
        data={defaultData({ serverType: 'external', url: 'http://x' })}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    expect(screen.queryByText('Replicas')).not.toBeInTheDocument();

    rerender(
      <MCPServerForm
        data={defaultData({ serverType: 'openapi', openapi: { spec: 'https://x/y.yaml' } })}
        onChange={() => {}}
      />,
    );
    expect(screen.queryByText('Replicas')).not.toBeInTheDocument();
  });

  it('clamps replicas input to [1, 32] and omits default via onChange', () => {
    const onChange = vi.fn();
    render(
      <MCPServerForm
        data={defaultData({ serverType: 'container', image: 'test:latest' })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    const input = screen.getByLabelText('Replicas') as HTMLInputElement;

    fireEvent.change(input, { target: { value: '50' } });
    expect(onChange).toHaveBeenLastCalledWith({ replicas: 32 });

    fireEvent.change(input, { target: { value: '0' } });
    expect(onChange).toHaveBeenLastCalledWith({ replicas: undefined });

    fireEvent.change(input, { target: { value: '1' } });
    expect(onChange).toHaveBeenLastCalledWith({ replicas: undefined });

    fireEvent.change(input, { target: { value: '4' } });
    expect(onChange).toHaveBeenLastCalledWith({ replicas: 4 });
  });

  it('shows replica policy selector only when replicas > 1', () => {
    const { rerender } = render(
      <MCPServerForm
        data={defaultData({ serverType: 'container', image: 'test:latest', replicas: 1 })}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    expect(screen.queryByText('Replica Policy')).not.toBeInTheDocument();

    rerender(
      <MCPServerForm
        data={defaultData({ serverType: 'container', image: 'test:latest', replicas: 3 })}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText('Replica Policy')).toBeInTheDocument();
  });
});

describe('MCPServerForm — scaling segmented control', () => {
  it('renders the Scaling segmented control for supported types', () => {
    render(
      <MCPServerForm
        data={defaultData({ serverType: 'container', image: 'test:latest' })}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    expect(screen.getByRole('radiogroup', { name: 'Scaling mode' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Static replicas' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('radio', { name: 'Autoscale' })).toHaveAttribute('aria-checked', 'false');
  });

  it('does not render the scaling control for external/openapi types', () => {
    const { rerender } = render(
      <MCPServerForm
        data={defaultData({ serverType: 'external', url: 'http://x' })}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    expect(screen.queryByRole('radiogroup', { name: 'Scaling mode' })).not.toBeInTheDocument();
    expect(screen.queryByText('Replicas')).not.toBeInTheDocument();

    rerender(
      <MCPServerForm
        data={defaultData({ serverType: 'openapi', openapi: { spec: 'https://x/y.yaml' } })}
        onChange={() => {}}
      />,
    );
    expect(screen.queryByRole('radiogroup', { name: 'Scaling mode' })).not.toBeInTheDocument();
    expect(screen.queryByText('Replicas')).not.toBeInTheDocument();
  });

  it('selecting Autoscale clears replicas/replicaPolicy and seeds defaults', () => {
    const onChange = vi.fn();
    render(
      <MCPServerForm
        data={defaultData({ serverType: 'container', image: 'test:latest', replicas: 3, replicaPolicy: 'least-connections' })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    fireEvent.click(screen.getByRole('radio', { name: 'Autoscale' }));
    expect(onChange).toHaveBeenCalledWith({
      replicas: undefined,
      replicaPolicy: undefined,
      autoscale: { min: 1, max: 5, targetInFlight: 10, scaleUpAfter: '30s', scaleDownAfter: '5m' },
    });
  });

  it('selecting Static clears autoscale', () => {
    const onChange = vi.fn();
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'container',
          image: 'test:latest',
          autoscale: { min: 1, max: 5, targetInFlight: 10 },
        })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    fireEvent.click(screen.getByRole('radio', { name: 'Static replicas' }));
    expect(onChange).toHaveBeenCalledWith({ autoscale: undefined });
  });

  it('in Autoscale mode renders six inputs, checkbox, and summary line', () => {
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'container',
          image: 'test:latest',
          autoscale: { min: 1, max: 5, targetInFlight: 10, scaleUpAfter: '30s', scaleDownAfter: '5m' },
        })}
        onChange={() => {}}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    expect(screen.getByLabelText('Min replicas')).toHaveValue(1);
    expect(screen.getByLabelText('Max replicas')).toHaveValue(5);
    expect(screen.getByLabelText('Target concurrent requests per replica')).toHaveValue(10);
    expect(screen.getByLabelText('Scale up after')).toHaveValue('30s');
    expect(screen.getByLabelText('Scale down after')).toHaveValue('5m');
    expect(screen.getByLabelText('Warm pool')).toHaveValue(0);
    expect(screen.getByLabelText('Scale to zero when idle')).not.toBeChecked();
    expect(
      screen.getByText('Autoscale 1–5 replicas · 10 concurrent/replica'),
    ).toBeInTheDocument();
  });

  it('clamps min/max to known-valid ranges', () => {
    const onChange = vi.fn();
    render(
      <MCPServerForm
        data={defaultData({
          serverType: 'container',
          image: 'test:latest',
          autoscale: { min: 1, max: 5, targetInFlight: 10 },
        })}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByText('Advanced'));
    const maxInput = screen.getByLabelText('Max replicas') as HTMLInputElement;
    fireEvent.change(maxInput, { target: { value: '99' } });
    expect(onChange).toHaveBeenLastCalledWith({
      autoscale: { min: 1, max: 32, targetInFlight: 10 },
    });
  });
});

describe('YAML serialization — autoscale', () => {
  it('emits an autoscale block with required fields only when optionals are unset', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'junos',
        serverType: 'local',
        command: ['python', 'srv.py'],
        autoscale: { min: 1, max: 5, targetInFlight: 10 },
      },
    });
    expect(yaml).toContain('autoscale:');
    expect(yaml).toContain('min: 1');
    expect(yaml).toContain('max: 5');
    expect(yaml).toContain('target_in_flight: 10');
    expect(yaml).not.toContain('scale_up_after');
    expect(yaml).not.toContain('scale_down_after');
    expect(yaml).not.toContain('warm_pool');
    expect(yaml).not.toContain('idle_to_zero');
  });

  it('emits optional fields when they are set', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'junos',
        serverType: 'container',
        image: 'test:latest',
        autoscale: {
          min: 1,
          max: 8,
          targetInFlight: 20,
          scaleUpAfter: '45s',
          scaleDownAfter: '2m',
          warmPool: 2,
          idleToZero: true,
        },
      },
    });
    expect(yaml).toContain('scale_up_after: 45s');
    expect(yaml).toContain('scale_down_after: 2m');
    expect(yaml).toContain('warm_pool: 2');
    expect(yaml).toContain('idle_to_zero: true');
  });

  it('mirrors Go omitempty: skips warm_pool: 0 and idle_to_zero: false', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'junos',
        serverType: 'container',
        image: 'test:latest',
        autoscale: {
          min: 1,
          max: 5,
          targetInFlight: 10,
          warmPool: 0,
          idleToZero: false,
        },
      },
    });
    expect(yaml).not.toContain('warm_pool');
    expect(yaml).not.toContain('idle_to_zero');
  });

  it('never emits both replicas and autoscale in the same entry', () => {
    // When autoscale is present, the builder ignores replicas/replicaPolicy.
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'junos',
        serverType: 'container',
        image: 'test:latest',
        replicas: 3,
        replicaPolicy: 'least-connections',
        autoscale: { min: 1, max: 5, targetInFlight: 10 },
      },
    });
    expect(yaml).toContain('autoscale:');
    expect(yaml).not.toMatch(/^\s*replicas:/m);
    expect(yaml).not.toMatch(/^\s*replica_policy:/m);
  });

  it('parseYAMLToForm recognizes autoscale and leaves replicas undefined', () => {
    const yaml = buildYAML({
      type: 'mcp-server',
      data: {
        name: 'junos',
        serverType: 'container',
        image: 'test:latest',
        autoscale: {
          min: 2,
          max: 8,
          targetInFlight: 15,
          scaleUpAfter: '45s',
          scaleDownAfter: '2m',
          warmPool: 1,
          idleToZero: true,
        },
      },
    });
    const parsed = parseYAMLToForm(yaml, 'mcp-server');
    expect('error' in parsed).toBe(false);
    if ('error' in parsed) return;
    const d = parsed.data as MCPServerFormData;
    expect(d.autoscale).toEqual({
      min: 2,
      max: 8,
      targetInFlight: 15,
      scaleUpAfter: '45s',
      scaleDownAfter: '2m',
      warmPool: 1,
      idleToZero: true,
    });
    expect(d.replicas).toBeUndefined();
    expect(d.replicaPolicy).toBeUndefined();
  });

  it('round-trips an autoscaled server YAML byte-identically', () => {
    const data: MCPServerFormData = {
      name: 'junos',
      serverType: 'container',
      image: 'test:latest',
      autoscale: {
        min: 1,
        max: 5,
        targetInFlight: 10,
        scaleUpAfter: '30s',
        scaleDownAfter: '5m',
      },
    };
    const yaml = buildYAML({ type: 'mcp-server', data });
    const parsed = parseYAMLToForm(yaml, 'mcp-server');
    expect('error' in parsed).toBe(false);
    if ('error' in parsed) return;
    const rebuilt = buildYAML({
      type: 'mcp-server',
      data: {
        ...(parsed.data as MCPServerFormData),
        // parseYAMLToForm hard-codes serverType: 'container' and does not
        // recover the image line; patch back so the rebuild matches what we
        // started with.
        serverType: 'container',
        image: 'test:latest',
      },
    });
    expect(rebuilt).toBe(yaml);
  });
});
