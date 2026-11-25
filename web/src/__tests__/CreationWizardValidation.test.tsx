import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import { useStackStore } from '../stores/useStackStore';
import type { MCPServerStatus, ResourceStatus } from '../types';
import type { MCPServerFormData, ResourceFormData, StackFormData } from '../lib/yaml-builder';

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchWizardDrafts: vi.fn().mockResolvedValue([]),
    validateStackSpec: vi.fn().mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] }),
    validateStackResource: vi.fn().mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] }),
    resolvePythonSource: vi.fn().mockResolvedValue({}),
    fetchStatus: vi.fn().mockResolvedValue({}),
  };
});

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

beforeEach(() => {
  cleanup();
  useWizardStore.getState().reset();
  useStackStore.setState({ stackName: 'demo', mcpServers: [], resources: [], clients: [] });
});

afterEach(() => {
  cleanup();
  useWizardStore.getState().reset();
  useStackStore.setState({ stackName: '', mcpServers: [], resources: [], clients: [] });
});

function openServerForm(server: Partial<MCPServerFormData> = {}) {
  useWizardStore.setState({
    isOpen: true,
    selectedType: 'mcp-server',
    selectedTemplate: 'blank',
    currentStep: 'form',
    formData: {
      ...useWizardStore.getState().formData,
      'mcp-server': { name: '', serverType: 'container', ...server },
    },
  });
  render(<CreationWizard />);
}

function openResourceForm(resource: ResourceFormData) {
  useWizardStore.setState({
    isOpen: true,
    selectedType: 'resource',
    selectedTemplate: 'blank',
    currentStep: 'form',
    formData: { ...useWizardStore.getState().formData, resource },
  });
  render(<CreationWizard />);
}

function openStackForm(stack: StackFormData) {
  useWizardStore.setState({
    isOpen: true,
    selectedType: 'stack',
    selectedTemplate: 'blank',
    currentStep: 'form',
    formData: { ...useWizardStore.getState().formData, stack },
  });
  render(<CreationWizard />);
}

function nextButton(): HTMLElement {
  return screen.getByRole('button', { name: /^Next/ });
}

function nameInput(): HTMLElement {
  return screen.getByRole('textbox', { name: /^Name/ });
}

describe('CreationWizard Configure step validation', () => {
  it('gates Next on an empty form with a reason and no inline errors', () => {
    openServerForm();

    const next = nextButton();
    expect(next).toHaveAttribute('aria-disabled', 'true');
    expect(next).not.toBeDisabled();
    expect(next).toHaveAccessibleDescription('3 fields need attention: Name, Image, Port');
    expect(screen.getByText('3 fields need attention: Name, Image, Port')).toBeVisible();

    expect(screen.queryByText('Name is required')).not.toBeInTheDocument();
    expect(document.querySelector('[aria-invalid="true"]')).toBeNull();
  });

  it('stays on the form when the gated Next is clicked, shows every error, and focuses the first field', () => {
    openServerForm();

    fireEvent.click(nextButton());

    expect(useWizardStore.getState().currentStep).toBe('form');
    expect(screen.getByText('Name is required')).toBeInTheDocument();
    expect(screen.getByText('Image is required')).toBeInTheDocument();
    expect(screen.getByText('Port is required')).toBeInTheDocument();
    expect(nameInput()).toHaveAttribute('aria-invalid', 'true');
    expect(nameInput()).toHaveAccessibleDescription(expect.stringContaining('Name is required'));
    expect(document.activeElement).toBe(nameInput());
  });

  it('shows only the blurred field error', () => {
    openServerForm();

    fireEvent.blur(nameInput());

    expect(screen.getByText('Name is required')).toBeInTheDocument();
    expect(screen.queryByText('Image is required')).not.toBeInTheDocument();
    expect(screen.queryByText('Port is required')).not.toBeInTheDocument();
  });

  it('flags a name present in the stack store and clears it when the name changes', () => {
    useStackStore.setState({ mcpServers: [{ name: 'demo' }] as MCPServerStatus[] });
    openServerForm({ image: 'mcp/fetch:latest', port: 8080 });

    fireEvent.change(nameInput(), { target: { value: 'demo' } });
    fireEvent.blur(nameInput());

    const message = "An MCP server named 'demo' already exists in this stack";
    expect(screen.getAllByText(message).length).toBeGreaterThan(0);
    expect(nameInput()).toHaveAttribute('aria-invalid', 'true');
    expect(nextButton()).toHaveAttribute('aria-disabled', 'true');

    fireEvent.change(nameInput(), { target: { value: 'fresh' } });
    expect(screen.queryByText(message)).not.toBeInTheDocument();
    expect(nextButton()).not.toHaveAttribute('aria-disabled');
  });

  it('flags a new MCP server named like a resource in the stack store', () => {
    useStackStore.setState({ resources: [{ name: 'db' }] as ResourceStatus[] });
    openServerForm({ name: 'db', image: 'mcp/fetch:latest', port: 8080 });

    fireEvent.click(nextButton());

    expect(useWizardStore.getState().currentStep).toBe('form');
    expect(screen.getAllByText("A resource named 'db' already exists in this stack").length).toBeGreaterThan(0);
  });

  it('advances to Review once required fields are filled', async () => {
    openServerForm();

    fireEvent.change(nameInput(), { target: { value: 'fetch' } });
    fireEvent.change(screen.getByRole('combobox', { name: /^Image/ }), { target: { value: 'mcp/fetch:latest' } });
    fireEvent.change(screen.getByRole('spinbutton', { name: /^Port/ }), { target: { value: '8080' } });

    const next = nextButton();
    expect(next).not.toHaveAttribute('aria-disabled');
    expect(next).not.toBeDisabled();
    expect(next).not.toHaveAttribute('aria-describedby');

    fireEvent.click(next);
    await waitFor(() => expect(useWizardStore.getState().currentStep).toBe('review'));
  });

  it('shows the command error for a local process and focuses the add button when empty', () => {
    openServerForm({ name: 'local', serverType: 'local', transport: 'stdio' });

    fireEvent.click(nextButton());

    const addArgument = screen.getByRole('button', { name: /Add argument/ });
    expect(addArgument).toHaveAccessibleDescription('Command is required');
    expect(document.activeElement).toBe(screen.getByRole('button', { name: /Add argument/ }));
  });

  it('expands the collapsed Authentication section and focuses the bearer token', () => {
    openServerForm({ name: 'remote', serverType: 'external', transport: 'http', url: 'https://x.example/mcp', auth: { type: 'bearer' } });

    expect(screen.queryByRole('textbox', { name: /^Token/ })).not.toBeInTheDocument();
    expect(nextButton()).toHaveAccessibleDescription('Token is required');

    fireEvent.click(nextButton());

    const token = screen.getByRole('textbox', { name: /^Token/ });
    expect(token).toHaveAttribute('aria-invalid', 'true');
    expect(token).toHaveAccessibleDescription(expect.stringContaining('Token is required'));
    expect(document.activeElement).toBe(token);
    expect(screen.getByRole('button', { name: /^Authentication, has errors/ })).toBeInTheDocument();
  });

  it('gives a reason for a custom header failure', () => {
    openServerForm({
      name: 'remote',
      serverType: 'external',
      transport: 'http',
      url: 'https://x.example/mcp',
      headers: { 'Mcp-Session-Id': 'x' },
    });

    expect(nextButton()).toHaveAttribute('aria-disabled', 'true');
    expect(nextButton()).toHaveAccessibleDescription('Fix the custom header errors');
    fireEvent.click(nextButton());
    expect(useWizardStore.getState().currentStep).toBe('form');
  });

  it('gates Next for a resource with an empty image', () => {
    openResourceForm({ name: 'cache', image: '' });

    expect(nextButton()).toHaveAttribute('aria-disabled', 'true');
    expect(nextButton()).toHaveAccessibleDescription('Image is required');
    fireEvent.click(nextButton());
    expect(useWizardStore.getState().currentStep).toBe('form');
    expect(screen.getByRole('textbox', { name: /^Image/ })).toHaveAttribute('aria-invalid', 'true');
  });

  it('flags a nested stack server missing its image on the item header', () => {
    openStackForm({
      name: 'team',
      version: '1',
      mcpServers: [{ name: 'github', serverType: 'container', transport: 'http', port: 3000 }],
    });

    expect(nextButton()).toHaveAttribute('aria-disabled', 'true');
    expect(nextButton()).toHaveAccessibleDescription('MCP server 1: Image is required');
    expect(screen.queryByRole('button', { name: /has errors/ })).not.toBeInTheDocument();

    fireEvent.click(nextButton());

    expect(useWizardStore.getState().currentStep).toBe('form');
    const header = screen.getByRole('button', { name: 'github, has errors' });
    expect(screen.getByRole('button', { name: /^MCP Servers, has errors/ })).toBeInTheDocument();
    const image = screen.getByRole('combobox', { name: /^Image/ });
    expect(image).toHaveAttribute('aria-invalid', 'true');
    expect(document.activeElement).toBe(image);

    fireEvent.click(header);
    expect(screen.queryByRole('combobox', { name: /^Image/ })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'github, has errors' })).toBeInTheDocument();
  });

  it('flags the later of two nested resources sharing a name', () => {
    openStackForm({
      name: 'team',
      version: '1',
      resources: [{ name: 'db', image: 'postgres:16' }, { name: 'db', image: 'postgres:16' }],
    });

    expect(nextButton()).toHaveAccessibleDescription("Resource 2: Another resource in this stack is named 'db'");
    fireEvent.click(nextButton());

    expect(screen.getByRole('button', { name: 'db' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'db, has errors' })).toBeInTheDocument();
    expect(screen.getByText("Another resource in this stack is named 'db'")).toBeInTheDocument();
  });

  it('clears touched errors under a list when an item is removed', () => {
    openStackForm({
      name: 'team',
      version: '1',
      resources: [{ name: 'one', image: '' }, { name: 'two', image: '' }, { name: 'three', image: '' }],
    });
    fireEvent.click(screen.getByRole('button', { name: /^Resources/ }));
    fireEvent.click(screen.getByRole('button', { name: 'two' }));
    fireEvent.blur(screen.getByRole('textbox', { name: /^Image/ }));
    expect(screen.getByRole('button', { name: 'two, has errors' })).toBeInTheDocument();

    // "three" moves into the removed item's index and must not inherit its touched state.
    fireEvent.click(screen.getByRole('button', { name: 'Remove one' }));

    expect(screen.getByRole('button', { name: 'two' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'three' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /has errors/ })).not.toBeInTheDocument();
  });

  it('flags a missing gateway token in the stack form', () => {
    openStackForm({ name: 'team', version: '1', gateway: { auth: { type: 'bearer', token: '' } } });

    expect(nextButton()).toHaveAccessibleDescription('Gateway token is required');
    fireEvent.click(nextButton());

    const token = screen.getByRole('textbox', { name: /^Token/ });
    expect(token).toHaveAttribute('aria-invalid', 'true');
    expect(document.activeElement).toBe(token);
  });

  it('does not gate Next with the form validator in YAML mode', async () => {
    useWizardStore.setState({
      isOpen: true,
      selectedType: 'mcp-server',
      selectedTemplate: 'blank',
      currentStep: 'form',
      expertMode: true,
      yamlContent: 'name: x\nurl: https://x\n',
    });
    render(<CreationWizard />);

    const next = nextButton();
    expect(next).not.toHaveAttribute('aria-disabled');
    expect(next).not.toBeDisabled();
    expect(screen.queryByText(/fields need attention/)).not.toBeInTheDocument();

    fireEvent.click(next);
    await waitFor(() => expect(useWizardStore.getState().currentStep).toBe('review'));
  });

  it('keeps native disabled on the type step', () => {
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(<CreationWizard />);

    expect(nextButton()).toBeDisabled();
    expect(nextButton()).not.toHaveAttribute('aria-disabled');
  });

  it('clears touched errors when the selected type changes', () => {
    openServerForm();
    fireEvent.blur(nameInput());
    expect(screen.getByText('Name is required')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /Back/ }));
    fireEvent.click(screen.getByRole('button', { name: /Back/ }));
    fireEvent.click(screen.getByRole('button', { name: /^Resource Database/ }));
    act(() => useWizardStore.getState().setSelectedTemplate('blank'));

    expect(useWizardStore.getState().selectedType).toBe('resource');
    expect(screen.queryByText('Name is required')).not.toBeInTheDocument();
  });
});
