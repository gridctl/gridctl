import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, act, waitFor } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import { fetchCatalog } from '../lib/api';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return { ...actual, fetchCatalog: vi.fn() };
});

const mockFetchCatalog = vi.mocked(fetchCatalog);

function openOnTemplateStep(selectedType: 'stack' | 'resource' | 'mcp-server') {
  useWizardStore.setState({ isOpen: true, currentStep: 'template', selectedType });
}

beforeEach(() => {
  cleanup();
  mockFetchCatalog.mockReset();
  mockFetchCatalog.mockResolvedValue({ query: '', source: 'all', servers: [] });
  useWizardStore.getState().reset();
});

describe('CreationWizard template cards', () => {
  it('fills the resource form from a database card without a Preset section', async () => {
    openOnTemplateStep('resource');
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('PostgreSQL'));

    expect(useWizardStore.getState().currentStep).toBe('form');
    expect(screen.getByPlaceholderText('my-resource')).toHaveValue('postgres');
    expect(screen.getByDisplayValue('postgres:16')).toBeInTheDocument();
    expect(screen.queryByText('Preset')).not.toBeInTheDocument();
    await waitFor(() => expect(useWizardStore.getState().yamlContent).toContain('5432:5432'));
    expect(useWizardStore.getState().yamlContent).toContain('POSTGRES_PASSWORD');
  });

  it('applies Basic Stack with one visible server and Blank Stack with none', () => {
    openOnTemplateStep('stack');
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('Basic Stack'));
    expect(useWizardStore.getState().formData.stack.mcpServers).toHaveLength(1);
    expect(useWizardStore.getState().formData.stack.mcpServers![0].serverType).toBe('container');
    // The server's own form is open without expanding a section.
    expect(screen.getByRole('spinbutton', { name: /^Port/ })).toBeInTheDocument();

    fireEvent.click(screen.getByText('Back'));
    fireEvent.click(screen.getByText('Blank Stack'));
    expect(useWizardStore.getState().formData.stack.mcpServers).toBeUndefined();
  });

  it('reapplies the selected card when clicked again', () => {
    openOnTemplateStep('resource');
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('Redis'));
    act(() => {
      useWizardStore.getState().updateFormData('resource', { name: 'cache', volumes: ['./d:/data'] });
    });
    fireEvent.click(screen.getByText('Back'));
    fireEvent.click(screen.getByText('Redis'));

    const resource = useWizardStore.getState().formData.resource;
    expect(resource.name).toBe('redis');
    expect(resource.volumes).toBeUndefined();
  });

  it('keeps the form data on Back followed by Next', () => {
    openOnTemplateStep('resource');
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('MySQL'));
    act(() => {
      useWizardStore.getState().updateFormData('resource', { name: 'orders-db' });
    });
    fireEvent.click(screen.getByText('Back'));
    expect(useWizardStore.getState().currentStep).toBe('template');
    fireEvent.click(screen.getByText('Next'));

    const state = useWizardStore.getState();
    expect(state.currentStep).toBe('form');
    expect(state.formData.resource.name).toBe('orders-db');
    expect(state.formData.resource.image).toBe('mysql:8');
  });

  it('leaves YAML mode and shows the template data when a card is picked', () => {
    openOnTemplateStep('resource');
    useWizardStore.setState({ expertMode: true, yamlContent: 'name: stale\nimage: old:1\n' });
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('MongoDB'));

    const state = useWizardStore.getState();
    expect(state.expertMode).toBe(false);
    expect(screen.queryByLabelText('YAML')).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('my-resource')).toHaveValue('mongodb');
    expect(screen.getByDisplayValue('mongo:7')).toBeInTheDocument();
  });

  it('does not touch another type when an MCP server card is picked', () => {
    act(() => {
      useWizardStore.getState().updateFormData('resource', { name: 'kept', image: 'kept:1' });
    });
    openOnTemplateStep('mcp-server');
    render(<CreationWizard />);

    fireEvent.click(screen.getByText('External URL'));

    const state = useWizardStore.getState();
    expect(state.formData['mcp-server']).toEqual({ name: '', serverType: 'external', transport: 'sse' });
    expect(state.formData.resource).toEqual({ name: 'kept', image: 'kept:1' });
  });
});
