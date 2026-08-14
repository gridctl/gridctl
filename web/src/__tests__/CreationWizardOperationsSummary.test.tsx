import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import * as apiModule from '../lib/api';
import type { OpenAPIOperation } from '../lib/api';

// The picker publishes counts from a component the wizard unmounts on the way
// to Review. These tests walk that transition, which is the only place the
// "12 of 517" wording the feature exists for is actually rendered.

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function operation(partial: Partial<OpenAPIOperation> & { operation_id: string }): OpenAPIOperation {
  return {
    tool_name: partial.operation_id,
    method: 'GET',
    path: `/${partial.operation_id}`,
    ...partial,
  };
}

const OPERATIONS: OpenAPIOperation[] = [
  operation({ operation_id: 'listPets', method: 'GET', path: '/pets' }),
  operation({ operation_id: 'createPet', method: 'POST', path: '/pets' }),
  operation({ operation_id: 'deletePet', method: 'DELETE', path: '/pets/{id}' }),
  operation({ operation_id: 'listStores', method: 'GET', path: '/stores' }),
];

const SPEC = 'https://example.com/openapi.json';

beforeEach(() => {
  cleanup();
  vi.restoreAllMocks();
  useWizardStore.getState().reset();
  vi.spyOn(apiModule, 'previewOpenAPIOperations').mockResolvedValue({
    operations: OPERATIONS,
    skipped_count: 0,
    loaded_at: new Date().toISOString(),
    cached: false,
  });
  vi.spyOn(apiModule, 'validateStackSpec').mockResolvedValue({
    valid: true,
    errorCount: 0,
    warningCount: 0,
    issues: [],
  });
});

function openOpenAPIForm(operations?: { include?: string[]; exclude?: string[] }) {
  useWizardStore.setState({
    isOpen: true,
    currentStep: 'form',
    selectedType: 'mcp-server',
    formData: {
      ...useWizardStore.getState().formData,
      'mcp-server': {
        name: 'petstore',
        serverType: 'openapi',
        openapi: { spec: SPEC, operations },
      },
    },
  });
}

async function loadOperations() {
  // A pre-existing selection with no loaded list opens in manual entry, so the
  // picker has to be reopened before the spec can be loaded.
  const backToPicker = screen.queryByRole('button', { name: /back to picker/i });
  if (backToPicker) fireEvent.click(backToPicker);

  fireEvent.click(screen.getByRole('button', { name: /load operations from the openapi spec/i }));
  await waitFor(() => expect(screen.getByText(/^Showing \d+ of/)).toBeInTheDocument());
}

function goToReview() {
  useWizardStore.getState().setStep('review');
}

describe('CreationWizard OpenAPI operations summary', () => {
  it('keeps the spec total on the Review step after the form unmounts', async () => {
    openOpenAPIForm({ include: ['listPets', 'createPet'] });
    render(<CreationWizard />);
    await loadOperations();

    goToReview();

    // The denominator has to survive the form unmounting; without it the
    // summary degrades to a bare count and the operator loses the spec size at
    // exactly the point they decide to deploy.
    await waitFor(() => expect(screen.getByText('2 of 4 (include)')).toBeInTheDocument());
  });

  it('states the full count in All mode on Review', async () => {
    openOpenAPIForm();
    render(<CreationWizard />);
    await loadOperations();

    goToReview();

    await waitFor(() => expect(screen.getByText(/^All 4/)).toBeInTheDocument());
  });

  it('repeats the destructive-operation count on Review', async () => {
    openOpenAPIForm({ include: ['listPets', 'deletePet'] });
    render(<CreationWizard />);
    await loadOperations();

    goToReview();

    await waitFor(() => expect(screen.getByText('2 of 4 (include) · 1 DELETE')).toBeInTheDocument());
  });

  it('degrades to a bare count when no spec was ever loaded', async () => {
    openOpenAPIForm({ include: ['listPets', 'createPet'] });
    render(<CreationWizard />);

    goToReview();

    await waitFor(() => expect(screen.getByText('2 selected (include)')).toBeInTheDocument());
  });

  it('shows no operations row for a non-OpenAPI server', async () => {
    useWizardStore.setState({
      isOpen: true,
      currentStep: 'review',
      selectedType: 'mcp-server',
      formData: {
        ...useWizardStore.getState().formData,
        'mcp-server': { name: 'db', serverType: 'container', image: 'mcp/postgres' },
      },
    });
    render(<CreationWizard />);

    await waitFor(() => expect(screen.getByText('Summary')).toBeInTheDocument());
    expect(screen.queryByText('Operations')).not.toBeInTheDocument();
  });
});
