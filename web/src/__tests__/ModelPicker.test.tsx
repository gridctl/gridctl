import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ModelPicker } from '../components/pricing/ModelPicker';
import { resetPricingModelsCacheForTests } from '../hooks/usePricingModels';
import * as apiModule from '../lib/api';

const MODELS = [
  'claude-haiku-4-5',
  'claude-opus-4-7',
  'claude-sonnet-4-6',
  'gpt-4o',
  'anthropic/claude-opus-4-7',
  'groq/llama-3.3-70b',
];

beforeEach(() => {
  resetPricingModelsCacheForTests();
  vi.restoreAllMocks();
  vi.spyOn(apiModule, 'fetchPricingModels').mockResolvedValue({
    source: 'litellm',
    models: MODELS,
  });
});

function getInput(): HTMLInputElement {
  return screen.getByRole('combobox', { name: 'Pricing model' });
}

describe('ModelPicker', () => {
  it('filters options live on substring input', async () => {
    render(<ModelPicker value="" onCommit={vi.fn()} autoFocus />);
    await waitFor(() => expect(apiModule.fetchPricingModels).toHaveBeenCalled());

    fireEvent.change(getInput(), { target: { value: 'opus' } });
    await waitFor(() => {
      const options = screen.getAllByRole('option');
      expect(options.map((o) => o.textContent)).toEqual([
        'claude-opus-4-7',
        'anthropic/claude-opus-4-7',
      ]);
    });
  });

  it('groups provider-prefixed IDs under their provider', async () => {
    render(<ModelPicker value="" onCommit={vi.fn()} autoFocus />);
    await waitFor(() => expect(screen.getAllByRole('option').length).toBe(MODELS.length));
    // Bare IDs lead under "models"; prefixed IDs group alphabetically.
    expect(screen.getByText('models')).toBeInTheDocument();
    expect(screen.getByText('anthropic')).toBeInTheDocument();
    expect(screen.getByText('groq')).toBeInTheDocument();
  });

  it('commits the active option on Enter after arrow navigation', async () => {
    const onCommit = vi.fn();
    render(<ModelPicker value="" onCommit={onCommit} autoFocus />);
    await waitFor(() => expect(screen.getAllByRole('option').length).toBe(MODELS.length));

    const input = getInput();
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onCommit).toHaveBeenCalledWith('claude-opus-4-7');
  });

  it('passes free text through on Enter when nothing is highlighted', async () => {
    const onCommit = vi.fn();
    render(<ModelPicker value="" onCommit={onCommit} autoFocus />);
    await waitFor(() => expect(apiModule.fetchPricingModels).toHaveBeenCalled());

    fireEvent.change(getInput(), { target: { value: 'my-custom-model' } });
    fireEvent.keyDown(getInput(), { key: 'Enter' });
    expect(onCommit).toHaveBeenCalledWith('my-custom-model');
  });

  it('shows the soft unknown-model note for IDs outside the snapshot', async () => {
    render(<ModelPicker value="" onCommit={vi.fn()} autoFocus />);
    await waitFor(() => expect(apiModule.fetchPricingModels).toHaveBeenCalled());

    fireEvent.change(getInput(), { target: { value: 'my-custom-model' } });
    await waitFor(() => {
      expect(screen.getByText(/prices as \$0/)).toBeInTheDocument();
    });
  });

  it('cancels on Escape without committing', async () => {
    const onCommit = vi.fn();
    const onCancel = vi.fn();
    render(<ModelPicker value="claude-opus-4-7" onCommit={onCommit} onCancel={onCancel} autoFocus />);
    await waitFor(() => expect(apiModule.fetchPricingModels).toHaveBeenCalled());

    fireEvent.keyDown(getInput(), { key: 'Escape' });
    expect(onCancel).toHaveBeenCalled();
    expect(onCommit).not.toHaveBeenCalled();
  });

  it('commits an option on mouse selection', async () => {
    const onCommit = vi.fn();
    render(<ModelPicker value="" onCommit={onCommit} autoFocus />);
    await waitFor(() => expect(screen.getAllByRole('option').length).toBe(MODELS.length));

    fireEvent.mouseDown(screen.getByRole('option', { name: 'gpt-4o' }));
    expect(onCommit).toHaveBeenCalledWith('gpt-4o');
  });

  it('clears the draft via the clear affordance', async () => {
    render(<ModelPicker value="claude-opus-4-7" onCommit={vi.fn()} autoFocus />);
    await waitFor(() => expect(apiModule.fetchPricingModels).toHaveBeenCalled());

    fireEvent.mouseDown(screen.getByRole('button', { name: 'Clear model' }));
    expect(getInput().value).toBe('');
  });
});
