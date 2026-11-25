import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import { useStackStore } from '../stores/useStackStore';
import * as apiModule from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

type AppendResult = Awaited<ReturnType<typeof apiModule.appendToStack>>;

describe('CreationWizard while a deploy append is in flight', () => {
  let resolveAppend: (value: AppendResult) => void;

  beforeEach(() => {
    cleanup();
    vi.restoreAllMocks();
    useWizardStore.getState().reset();
    useWizardStore.getState().setAppendInFlight(false);
    useStackStore.setState({ mcpServers: [], resources: [] });
    vi.spyOn(apiModule, 'validateStackResource').mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({ 'mcp-servers': [] } as never);
    vi.spyOn(apiModule, 'appendToStack').mockImplementation(
      () => new Promise((resolve) => { resolveAppend = resolve; }),
    );
    useWizardStore.setState({
      isOpen: true,
      currentStep: 'review',
      selectedType: 'mcp-server',
      formData: {
        ...useWizardStore.getState().formData,
        'mcp-server': { name: 'db', serverType: 'container', image: 'mcp/postgres' },
      },
    });
  });

  it('disables Back and Start over until the append settles', async () => {
    render(<CreationWizard />);
    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    expect(screen.getByRole('button', { name: 'Back' })).not.toBeDisabled();
    expect(screen.getByRole('button', { name: 'Start over' })).not.toBeDisabled();

    fireEvent.click(deploy);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Back' })).toBeDisabled());
    expect(screen.getByRole('button', { name: 'Start over' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Adding to stack...' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Close' })).not.toBeDisabled();

    await act(async () => { resolveAppend({ success: true, resourceType: 'mcp-server', resourceName: 'db' }); });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Back' })).not.toBeDisabled());
    expect(screen.getByRole('button', { name: 'Start over' })).not.toBeDisabled();
  });

  it('cannot append twice after closing and reopening during the request', async () => {
    const view = render(<CreationWizard />);
    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);
    await waitFor(() => expect(useWizardStore.getState().appendInFlight).toBe(true));

    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(useWizardStore.getState().isOpen).toBe(false);
    view.unmount();

    act(() => { useWizardStore.getState().open(); });
    render(<CreationWizard />);
    const busy = await screen.findByRole('button', { name: 'Adding to stack...' });
    expect(busy).toBeDisabled();
    fireEvent.click(busy);
    expect(apiModule.appendToStack).toHaveBeenCalledTimes(1);

    await act(async () => { resolveAppend({ success: true, resourceType: 'mcp-server', resourceName: 'db' }); });
    expect(await screen.findByRole('button', { name: 'Check deployment' })).toBeInTheDocument();
    expect(useWizardStore.getState().isOpen).toBe(true);
    expect(apiModule.appendToStack).toHaveBeenCalledTimes(1);
  });
});
