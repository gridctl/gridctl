import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { CatalogPicker } from '../components/wizard/CatalogPicker';
import { fetchCatalog, type CatalogEntry, type CatalogResponse } from '../lib/api';
import { catalogEntryToFormData, catalogServerName } from '../lib/catalog';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return { ...actual, fetchCatalog: vi.fn() };
});

const mockFetchCatalog = vi.mocked(fetchCatalog);

const githubEntry: CatalogEntry = {
  name: 'github',
  title: 'GitHub',
  description: 'GitHub repositories, issues, and pull requests over MCP',
  tier: 'curated',
  status: 'active',
  install: { type: 'image', transport: 'http', image: 'ghcr.io/github/github-mcp-server:latest', port: 8080 },
  inputs: [
    { name: 'GITHUB_PERSONAL_ACCESS_TOKEN', description: 'PAT with repo scope', required: true, secret: true },
  ],
};

const registryEntry: CatalogEntry = {
  name: 'io.example/weather',
  description: 'Community weather server',
  tier: 'registry',
  status: 'active',
  install: { type: 'command', transport: 'stdio', command: ['npx', '-y', 'weather-mcp@1.0.0'] },
};

const unsupportedEntry: CatalogEntry = {
  name: 'io.example/desktop-ext',
  description: 'Ships as a desktop extension bundle',
  tier: 'registry',
  status: 'active',
  install: { type: 'command', transport: 'stdio' },
  unsupported: 'mcpb',
};

function respond(servers: CatalogEntry[], extra: Partial<CatalogResponse> = {}): CatalogResponse {
  return { query: '', source: 'all', servers, ...extra };
}

beforeEach(() => {
  cleanup();
  mockFetchCatalog.mockReset();
});

describe('CatalogPicker', () => {
  it('lists entries with source-tier badges', async () => {
    mockFetchCatalog.mockResolvedValue(respond([githubEntry, registryEntry]));
    render(<CatalogPicker onSelect={vi.fn()} />);

    expect(await screen.findByText('GitHub')).toBeInTheDocument();
    expect(screen.getByText('io.example/weather')).toBeInTheDocument();
    expect(screen.getByText('curated')).toBeInTheDocument();
    expect(screen.getByText('registry')).toBeInTheDocument();
  });

  it('shows the preview pane and fires onSelect with the entry', async () => {
    mockFetchCatalog.mockResolvedValue(respond([githubEntry]));
    const onSelect = vi.fn();
    render(<CatalogPicker onSelect={onSelect} />);

    fireEvent.click(await screen.findByText('GitHub'));
    expect(screen.getByText('GITHUB_PERSONAL_ACCESS_TOKEN')).toBeInTheDocument();

    fireEvent.click(screen.getByText('Use this server'));
    expect(onSelect).toHaveBeenCalledWith(githubEntry);
  });

  it('searches with the typed query', async () => {
    mockFetchCatalog.mockResolvedValue(respond([githubEntry, registryEntry]));
    render(<CatalogPicker onSelect={vi.fn()} />);
    await screen.findByText('GitHub');

    mockFetchCatalog.mockResolvedValue(respond([registryEntry], { query: 'weather' }));
    fireEvent.change(screen.getByPlaceholderText('Search servers, e.g. postgres...'), {
      target: { value: 'weather' },
    });

    await waitFor(() => expect(mockFetchCatalog).toHaveBeenLastCalledWith('weather'));
    expect(await screen.findByText('io.example/weather')).toBeInTheDocument();
  });

  it('surfaces a degraded-registry notice', async () => {
    mockFetchCatalog.mockResolvedValue(
      respond([githubEntry], { registry_error: 'connection refused' }),
    );
    render(<CatalogPicker onSelect={vi.fn()} />);

    expect(
      await screen.findByText('MCP Registry unavailable; showing curated results only'),
    ).toBeInTheDocument();
  });

  it('blocks selection of unsupported entries', async () => {
    mockFetchCatalog.mockResolvedValue(respond([unsupportedEntry]));
    render(<CatalogPicker onSelect={vi.fn()} />);

    fireEvent.click(await screen.findByText('io.example/desktop-ext'));
    expect(screen.queryByText('Use this server')).not.toBeInTheDocument();
    expect(screen.getByText(/Unsupported package type \(mcpb\)/)).toBeInTheDocument();
  });
});

describe('catalogServerName', () => {
  it('keeps short curated names', () => {
    expect(catalogServerName(githubEntry)).toBe('github');
  });

  it('strips the registry namespace and sanitizes', () => {
    expect(catalogServerName(registryEntry)).toBe('weather');
    expect(
      catalogServerName({ ...registryEntry, name: 'io.example/My Weather!' }),
    ).toBe('My-Weather');
  });
});

describe('catalogEntryToFormData', () => {
  it('maps an image install to a container server with vault-ref secrets', () => {
    const data = catalogEntryToFormData(githubEntry);
    expect(data).toMatchObject({
      serverType: 'container',
      name: 'github',
      image: 'ghcr.io/github/github-mcp-server:latest',
      port: 8080,
      transport: 'http',
      env: { GITHUB_PERSONAL_ACCESS_TOKEN: '${var:GITHUB_PERSONAL_ACCESS_TOKEN}' },
    });
    expect(data.command).toBeUndefined();
    expect(data.url).toBeUndefined();
  });

  it('maps a command install to a local server and appends arg defaults', () => {
    const entry: CatalogEntry = {
      ...registryEntry,
      inputs: [
        { name: 'ROOT', arg: true, default: '/workspace' },
        { name: 'LOG_LEVEL', default: 'info' },
        { name: 'OPTIONAL_HINT' },
      ],
    };
    const data = catalogEntryToFormData(entry);
    expect(data).toMatchObject({
      serverType: 'local',
      command: ['npx', '-y', 'weather-mcp@1.0.0', '/workspace'],
      transport: 'stdio',
      env: { LOG_LEVEL: 'info' },
    });
    expect(data.env).not.toHaveProperty('OPTIONAL_HINT');
  });

  it('maps a url install with bearer auth and drops env inputs', () => {
    const entry: CatalogEntry = {
      name: 'io.example/remote',
      description: 'remote',
      tier: 'registry',
      install: { type: 'url', transport: 'http', url: 'https://mcp.example.com/mcp', auth_type: 'bearer' },
      inputs: [
        { name: 'API_TOKEN', secret: true, auth: true, required: true },
        { name: 'IGNORED_ENV', default: 'x' },
      ],
    };
    const data = catalogEntryToFormData(entry);
    expect(data).toMatchObject({
      serverType: 'external',
      url: 'https://mcp.example.com/mcp',
      transport: 'http',
      auth: { type: 'bearer', token: '${var:API_TOKEN}' },
    });
    expect(data.env).toBeUndefined();
  });

  it('clears fields left over from a previous selection', () => {
    const data = catalogEntryToFormData(registryEntry);
    expect(data).toHaveProperty('image', undefined);
    expect(data).toHaveProperty('url', undefined);
    expect(data).toHaveProperty('auth', undefined);
  });
});
