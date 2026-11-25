import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import * as apiModule from '../lib/api';
import type { StackFormData } from '../lib/yaml-builder';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

beforeEach(() => {
  cleanup();
  vi.restoreAllMocks();
  useWizardStore.getState().reset();
  vi.spyOn(apiModule, 'fetchWizardDrafts').mockResolvedValue([]);
});

function openStackForm(stack: StackFormData) {
  useWizardStore.setState({
    isOpen: true,
    currentStep: 'form',
    selectedType: 'stack',
    selectedTemplate: 'blank',
    formData: { ...useWizardStore.getState().formData, stack },
  });
  render(<CreationWizard />);
}

async function switchToYaml(): Promise<HTMLTextAreaElement> {
  // The generated YAML is debounced; wait for it before entering YAML mode.
  await waitFor(() => expect(useWizardStore.getState().yamlContent).toContain('name:'));
  fireEvent.click(screen.getByRole('button', { name: 'YAML' }));
  const textarea = screen.getByRole('textbox', { name: 'YAML' }) as HTMLTextAreaElement;
  await waitFor(() => expect(textarea.value).toContain('name:'));
  return textarea;
}

describe('CreationWizard YAML mode', () => {
  it('refuses a switch that would drop an added gateway block', async () => {
    openStackForm({ name: 'demo', version: '1' });
    const textarea = await switchToYaml();
    const edited = `${textarea.value}\ngateway:\n  code_mode: on\n`;
    fireEvent.change(textarea, { target: { value: edited } });

    fireEvent.click(screen.getByRole('button', { name: 'Form' }));

    expect(screen.getByRole('alert')).toHaveTextContent(/gateway/);
    expect((screen.getByRole('textbox', { name: 'YAML' }) as HTMLTextAreaElement).value).toBe(edited);
    expect(useWizardStore.getState().expertMode).toBe(true);
  });

  it('switches when only the name changed and carries it into the form', async () => {
    openStackForm({ name: 'demo', version: '1' });
    const textarea = await switchToYaml();
    fireEvent.change(textarea, { target: { value: textarea.value.replace('name: demo', 'name: renamed') } });

    fireEvent.click(screen.getByRole('button', { name: 'Form' }));

    expect(screen.queryByRole('textbox', { name: 'YAML' })).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(useWizardStore.getState().expertMode).toBe(false);
    expect(useWizardStore.getState().formData.stack.name).toBe('renamed');
  });

  it('clears the refusal when the YAML is edited', async () => {
    openStackForm({ name: 'demo', version: '1' });
    const textarea = await switchToYaml();
    fireEvent.change(textarea, { target: { value: `${textarea.value}\ngateway:\n  code_mode: on\n` } });
    fireEvent.click(screen.getByRole('button', { name: 'Form' }));
    expect(screen.getByRole('alert')).toBeInTheDocument();

    fireEvent.change(screen.getByRole('textbox', { name: 'YAML' }), { target: { value: 'name: demo\n' } });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('refuses a switch that would restore a deleted gateway block', async () => {
    openStackForm({ name: 'demo', version: '1', gateway: { codeMode: 'on' } });
    const textarea = await switchToYaml();
    expect(textarea.value).toContain('gateway:');
    const edited = textarea.value.replace(/\ngateway:\n {2}code_mode: on\n/, '\n');
    expect(edited).not.toContain('gateway');
    fireEvent.change(textarea, { target: { value: edited } });

    fireEvent.click(screen.getByRole('button', { name: 'Form' }));

    expect(screen.getByRole('alert')).toHaveTextContent(/gateway/);
    const after = (screen.getByRole('textbox', { name: 'YAML' }) as HTMLTextAreaElement).value;
    expect(after).not.toContain('gateway');
    expect(useWizardStore.getState().expertMode).toBe(true);
  });

  it('switches back with no message when nothing was edited', async () => {
    openStackForm({
      name: 'demo',
      version: '1',
      gateway: { codeMode: 'on' },
      mcpServers: [{ name: 'github', serverType: 'container', image: 'ghcr.io/example/github:1', transport: 'http', port: 3000 }],
      resources: [{ name: 'db', image: 'postgres:16' }],
    });
    await switchToYaml();
    fireEvent.click(screen.getByRole('button', { name: 'Form' }));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(useWizardStore.getState().expertMode).toBe(false);
  });

  it('refuses a switch from empty YAML instead of restoring the form', async () => {
    openStackForm({ name: 'demo', version: '1', gateway: { codeMode: 'on' } });
    const textarea = await switchToYaml();
    fireEvent.change(textarea, { target: { value: '' } });

    fireEvent.click(screen.getByRole('button', { name: 'Form' }));

    expect(screen.getByRole('alert')).toHaveTextContent(/mapping/);
    expect((screen.getByRole('textbox', { name: 'YAML' }) as HTMLTextAreaElement).value).toBe('');
    expect(useWizardStore.getState().expertMode).toBe(true);
  });

  it('notice no longer promises YAML-only settings', async () => {
    openStackForm({ name: 'demo', version: '1' });
    await switchToYaml();
    expect(screen.queryByText(/YAML-only/)).not.toBeInTheDocument();
    expect(screen.getByText(/Switching to Form is refused/)).toBeInTheDocument();
  });
});
