import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import { fetchCatalog, type CatalogEntry } from '../lib/api';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return { ...actual, fetchCatalog: vi.fn() };
});

const mockFetchCatalog = vi.mocked(fetchCatalog);

const postgresEntry: CatalogEntry = {
  name: 'postgres',
  title: 'PostgreSQL',
  description: 'Query and inspect PostgreSQL databases',
  tier: 'curated',
  status: 'active',
  install: {
    type: 'command',
    transport: 'stdio',
    command: ['npx', '-y', '@modelcontextprotocol/server-postgres'],
  },
  inputs: [
    { name: 'DATABASE_URL', description: 'Connection string', required: true, secret: true, arg: true },
  ],
};

beforeEach(() => {
  cleanup();
  mockFetchCatalog.mockReset();
  mockFetchCatalog.mockResolvedValue({ query: '', source: 'all', servers: [postgresEntry] });
  useWizardStore.getState().reset();
  useWizardStore.setState({
    isOpen: true,
    currentStep: 'template',
    selectedType: 'mcp-server',
  });
});

describe('CreationWizard catalog step', () => {
  it('offers the Templates/Catalog toggle on the mcp-server template step', () => {
    render(<CreationWizard />);
    expect(screen.getByText('Templates')).toBeInTheDocument();
    expect(screen.getByText('Catalog')).toBeInTheDocument();
    // Templates remain the default view.
    expect(screen.getByText('Choose a template')).toBeInTheDocument();
  });

  it('pre-fills the form and advances when a catalog entry is used', async () => {
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('Catalog'));
    fireEvent.click(await screen.findByText('PostgreSQL'));
    fireEvent.click(screen.getByText('Use this server'));

    const state = useWizardStore.getState();
    expect(state.currentStep).toBe('form');
    expect(state.selectedTemplate).toBe('catalog:postgres');
    // Mirrors what `gridctl add postgres --yes` writes: the secret arg
    // input rides the command line as a ${var:KEY} reference.
    expect(state.formData['mcp-server']).toMatchObject({
      name: 'postgres',
      serverType: 'local',
      command: ['npx', '-y', '@modelcontextprotocol/server-postgres', '${var:DATABASE_URL}'],
      transport: 'stdio',
    });
    expect(state.formData['mcp-server'].env).toBeUndefined();
  });

  it('clears catalog data when Blank Server is picked after Back', async () => {
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('Catalog'));
    fireEvent.click(await screen.findByText('PostgreSQL'));
    fireEvent.click(screen.getByText('Use this server'));
    expect(useWizardStore.getState().formData['mcp-server'].command).toBeDefined();

    fireEvent.click(screen.getByText('Back'));
    fireEvent.click(screen.getByText('Blank Server'));

    const state = useWizardStore.getState();
    expect(state.currentStep).toBe('form');
    expect(state.selectedTemplate).toBe('blank');
    expect(state.formData['mcp-server']).toEqual({ name: '', serverType: 'container' });
    expect(state.formData['mcp-server'].command).toBeUndefined();
  });

  it('drops fields the catalog entry does not set', async () => {
    useWizardStore.getState().updateFormData('mcp-server', {
      headers: { 'X-Team': 'core' },
      tools: ['query'],
    });
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('Catalog'));
    fireEvent.click(await screen.findByText('PostgreSQL'));
    fireEvent.click(screen.getByText('Use this server'));

    const server = useWizardStore.getState().formData['mcp-server'];
    expect(server.name).toBe('postgres');
    expect(server).not.toHaveProperty('headers');
    expect(server).not.toHaveProperty('tools');
  });
});
