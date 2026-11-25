import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import type { ReactNode } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import { useStackStore } from '../stores/useStackStore';
import * as apiModule from '../lib/api';

const recorded = vi.hoisted(() => ({
  panels: [] as Array<Record<string, unknown>>,
}));

vi.mock('react-resizable-panels', () => ({
  Group: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  Panel: ({ children, ...props }: { children?: ReactNode } & Record<string, unknown>) => {
    recorded.panels.push(props);
    return <div>{children}</div>;
  },
  Separator: () => <div />,
}));

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

beforeEach(() => {
  cleanup();
  vi.restoreAllMocks();
  recorded.panels.length = 0;
  useWizardStore.getState().reset();
  useWizardStore.getState().setAppendInFlight(false);
  useWizardStore.getState().setAppendedResource(null);
  useStackStore.setState({ mcpServers: [], resources: [] });
  vi.spyOn(apiModule, 'fetchWizardDrafts').mockResolvedValue([]);
  vi.spyOn(apiModule, 'validateStackResource').mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
  vi.spyOn(apiModule, 'validateStackSpec').mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
});

function openOnResourceReview() {
  useWizardStore.setState({
    isOpen: true,
    currentStep: 'review',
    selectedType: 'resource',
    formData: {
      ...useWizardStore.getState().formData,
      resource: { name: 'cache', image: 'redis:7' },
    },
  });
}

async function clickDeploy() {
  const deploy = await screen.findByRole('button', { name: 'Deploy' });
  await waitFor(() => expect(deploy).not.toBeDisabled());
  fireEvent.click(deploy);
}

describe('CreationWizard after a successful deploy', () => {
  it('resets to the type step with default form data', async () => {
    vi.spyOn(apiModule, 'appendToStack').mockResolvedValue({ success: true, resourceType: 'resource', resourceName: 'cache' });
    const onDeploy = vi.fn();
    openOnResourceReview();
    render(<CreationWizard onDeploy={onDeploy} />);

    await clickDeploy();
    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1));

    const state = useWizardStore.getState();
    expect(state.isOpen).toBe(false);
    expect(state.currentStep).toBe('type');
    expect(state.selectedType).toBeNull();
    expect(state.formData.resource.name).toBe('');

    act(() => { useWizardStore.getState().open(); });
    expect(await screen.findByText('What would you like to create?')).toBeInTheDocument();
  });

  it('leaves the form intact when the append fails', async () => {
    vi.spyOn(apiModule, 'appendToStack').mockRejectedValue(new Error('append failed'));
    const onDeploy = vi.fn();
    openOnResourceReview();
    render(<CreationWizard onDeploy={onDeploy} />);

    await clickDeploy();
    await waitFor(() => expect(apiModule.appendToStack).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(useWizardStore.getState().appendInFlight).toBe(false));

    expect(onDeploy).not.toHaveBeenCalled();
    const state = useWizardStore.getState();
    expect(state.isOpen).toBe(true);
    expect(state.currentStep).toBe('review');
    expect(state.selectedType).toBe('resource');
    expect(state.formData.resource).toEqual({ name: 'cache', image: 'redis:7' });
  });
});

describe('CreationWizard panel sizes', () => {
  it('passes percentage strings to both panels', () => {
    useWizardStore.setState({
      isOpen: true,
      currentStep: 'form',
      selectedType: 'mcp-server',
      formData: {
        ...useWizardStore.getState().formData,
        'mcp-server': { name: 'db', serverType: 'container', image: 'mcp/postgres' },
      },
    });
    render(<CreationWizard />);

    expect(recorded.panels.length).toBeGreaterThanOrEqual(2);
    const [form, preview] = recorded.panels.slice(-2);
    expect(form).toMatchObject({ defaultSize: '55%', minSize: '40%' });
    expect(preview).toMatchObject({ defaultSize: '45%', minSize: '30%' });
    for (const props of recorded.panels) {
      expect(typeof props.defaultSize).not.toBe('number');
      expect(typeof props.minSize).not.toBe('number');
    }
  });
});
