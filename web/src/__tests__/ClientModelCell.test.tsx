import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ClientModelCell } from '../components/pricing/ClientModelCell';
import { resetPricingModelsCacheForTests } from '../hooks/usePricingModels';
import * as apiModule from '../lib/api';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

beforeEach(() => {
  resetPricingModelsCacheForTests();
  vi.restoreAllMocks();
  vi.spyOn(apiModule, 'fetchPricingModels').mockResolvedValue({
    source: 'litellm',
    models: ['claude-haiku-4-5', 'claude-opus-4-7'],
  });
});

describe('ClientModelCell', () => {
  it('renders the declared model as a pill with client provenance', () => {
    render(
      <ClientModelCell
        client="claude-code"
        declaredModel="claude-opus-4-7"
        costAttribution
        onSaved={vi.fn()}
      />,
    );
    expect(screen.getByText('claude-opus-4-7')).toBeInTheDocument();
    expect(screen.getByText('· client')).toBeInTheDocument();
  });

  it('renders per-server for undeclared clients when attribution exists', () => {
    render(
      <ClientModelCell client="goose" costAttribution onSaved={vi.fn()} />,
    );
    expect(screen.getByText('per-server')).toBeInTheDocument();
  });

  it('saves a committed model and reports it to the host', async () => {
    const onSaved = vi.fn();
    const update = vi.spyOn(apiModule, 'updateClientModel').mockResolvedValue({
      client: 'claude-code',
      profileKey: 'claude-code',
      model: 'claude-haiku-4-5',
      reloaded: true,
    });
    render(
      <ClientModelCell client="claude-code" costAttribution={false} onSaved={onSaved} />,
    );

    fireEvent.click(screen.getByText('set model'));
    const input = await screen.findByRole('combobox', { name: 'Pricing model' });
    fireEvent.change(input, { target: { value: 'claude-haiku-4-5' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith('claude-code', 'claude-haiku-4-5');
      expect(onSaved).toHaveBeenCalledWith('claude-code', 'claude-haiku-4-5');
    });
    // Editor closes back to the pill state on success.
    await waitFor(() => {
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    });
  });

  it('keeps the editor open and surfaces the message on save failure', async () => {
    const onSaved = vi.fn();
    vi.spyOn(apiModule, 'updateClientModel').mockRejectedValue(
      new Error('The stack file was modified outside the canvas.'),
    );
    render(
      <ClientModelCell client="claude-code" costAttribution={false} onSaved={onSaved} />,
    );

    fireEvent.click(screen.getByText('set model'));
    const input = await screen.findByRole('combobox', { name: 'Pricing model' });
    fireEvent.change(input, { target: { value: 'claude-haiku-4-5' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument();
    });
    expect(onSaved).not.toHaveBeenCalled();
  });

  it('does not call the API when the committed value is unchanged', async () => {
    const update = vi.spyOn(apiModule, 'updateClientModel');
    render(
      <ClientModelCell
        client="claude-code"
        declaredModel="claude-opus-4-7"
        costAttribution
        onSaved={vi.fn()}
      />,
    );

    fireEvent.click(screen.getByText('claude-opus-4-7'));
    const input = await screen.findByRole('combobox', { name: 'Pricing model' });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    });
    expect(update).not.toHaveBeenCalled();
  });
});
