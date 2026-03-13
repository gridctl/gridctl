import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MCPServerForm } from '../components/wizard/steps/MCPServerForm';
import type { MCPServerFormData } from '../lib/yaml-builder';

function defaultData(overrides?: Partial<MCPServerFormData>): MCPServerFormData {
  return { name: '', serverType: 'container', ...overrides };
}

describe('MCPServerForm', () => {
  let onChange: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    onChange = vi.fn();
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
  const onChange = vi.fn();

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
