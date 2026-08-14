import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { OperationsPicker } from '../components/wizard/steps/OperationsPicker';
import type { OperationsFilter } from '../lib/openapiOperations';
import * as apiModule from '../lib/api';
import { ProbeError, type OpenAPIOperation, type OpenAPIPreviewSuccess } from '../lib/api';

const setStats = vi.fn();
vi.mock('../stores/useWizardStore', () => ({
  useWizardStore: vi.fn((selector: (s: Record<string, unknown>) => unknown) =>
    selector({ setOpenAPIOperationStats: setStats }),
  ),
}));

function operation(partial: Partial<OpenAPIOperation> & { operation_id: string }): OpenAPIOperation {
  return {
    tool_name: partial.operation_id,
    method: 'GET',
    path: `/${partial.operation_id}`,
    ...partial,
  };
}

const OPERATIONS: OpenAPIOperation[] = [
  operation({ operation_id: 'listPets', method: 'GET', path: '/pets', summary: 'List all pets', tags: ['pets'] }),
  operation({ operation_id: 'createPet', method: 'POST', path: '/pets', summary: 'Create a pet', tags: ['pets'] }),
  operation({ operation_id: 'deletePet', method: 'DELETE', path: '/pets/{id}', summary: 'Remove a pet', tags: ['pets'] }),
  operation({ operation_id: 'listStores', method: 'GET', path: '/stores', summary: 'List stores', tags: ['stores'] }),
];

function previewResult(overrides: Partial<OpenAPIPreviewSuccess> = {}): OpenAPIPreviewSuccess {
  return {
    operations: OPERATIONS,
    skipped_count: 0,
    loaded_at: new Date().toISOString(),
    cached: false,
    ...overrides,
  };
}

function Harness({
  initial,
  spec = 'https://example.com/openapi.json',
  onChangeSpy,
}: {
  initial?: OperationsFilter;
  spec?: string;
  onChangeSpy?: (ops: OperationsFilter | undefined) => void;
}) {
  const [ops, setOps] = useState<OperationsFilter | undefined>(initial);
  return (
    <OperationsPicker
      spec={spec}
      operations={ops}
      onChange={(next) => {
        setOps(next);
        onChangeSpy?.(next);
      }}
    />
  );
}

async function loadSpec() {
  // A seeded selection with no loaded list opens in manual entry, so reopen the
  // picker first when that is where we landed.
  const backToPicker = screen.queryByRole('button', { name: /back to picker/i });
  if (backToPicker) fireEvent.click(backToPicker);

  fireEvent.click(screen.getByRole('button', { name: /load operations from the openapi spec/i }));
  await waitFor(() => expect(screen.getByText(/^Showing \d+ of/)).toBeInTheDocument());
}

describe('OperationsPicker', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    setStats.mockClear();
    vi.spyOn(apiModule, 'previewOpenAPIOperations').mockResolvedValue(previewResult());
  });

  it('renders collapsed until the spec is explicitly loaded', () => {
    render(<Harness />);
    expect(screen.getByRole('button', { name: /load operations/i })).toBeInTheDocument();
    expect(screen.queryByText(/^Showing \d+ of/)).not.toBeInTheDocument();
    expect(apiModule.previewOpenAPIOperations).not.toHaveBeenCalled();
  });

  it('disables loading until a spec is entered', () => {
    render(<Harness spec="" />);
    expect(screen.getByRole('button', { name: /load operations/i })).toBeDisabled();
  });

  it('lists operations after loading and states the outcome in All mode', async () => {
    render(<Harness />);
    await loadSpec();
    expect(screen.getByText('All 4 operations will become tools')).toBeInTheDocument();
    expect(screen.getByText('/pets/{id}')).toBeInTheDocument();
  });

  // Acceptance criterion 4: the three modes write exactly no block, include, or exclude.
  it('writes no operations block in All mode', async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ include: ['listPets'] }} onChangeSpy={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: 'All operations' }));
    expect(onChange).toHaveBeenCalledWith(undefined);
  });

  it('writes an include list in Only selected mode', async () => {
    const onChange = vi.fn();
    render(<Harness onChangeSpy={onChange} />);
    await loadSpec();
    fireEvent.click(screen.getByRole('button', { name: 'Only selected' }));
    fireEvent.click(screen.getByRole('option', { name: 'GET /pets — List all pets' }));
    expect(onChange).toHaveBeenLastCalledWith({ include: ['listPets'] });
  });

  it('writes an exclude list in All except selected mode', async () => {
    const onChange = vi.fn();
    render(<Harness onChangeSpy={onChange} />);
    await loadSpec();
    fireEvent.click(screen.getByRole('button', { name: 'All except selected' }));
    fireEvent.click(screen.getByRole('option', { name: 'DELETE /pets/{id} — Remove a pet' }));
    expect(onChange).toHaveBeenLastCalledWith({ exclude: ['deletePet'] });
  });

  // Acceptance criterion 5: deselecting to zero drops the filter entirely.
  it('removes the filter instead of writing an empty include list', async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ include: ['listPets'] }} onChangeSpy={onChange} />);
    await loadSpec();
    fireEvent.click(screen.getByRole('option', { name: 'GET /pets — List all pets' }));

    expect(onChange).toHaveBeenLastCalledWith(undefined);
    const written = onChange.mock.calls.map(([value]) => value);
    expect(written.some((v) => Array.isArray(v?.include) && v.include.length === 0)).toBe(false);
    expect(screen.getByText(/selection cleared/i)).toBeInTheDocument();
  });

  it('drops the filter when Clear is pressed', async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ include: ['listPets', 'createPet'] }} onChangeSpy={onChange} />);
    await loadSpec();
    fireEvent.click(screen.getByRole('button', { name: /clear selected operations/i }));
    expect(onChange).toHaveBeenLastCalledWith(undefined);
  });

  // Acceptance criterion 6: selecting everything offers, but does not force, All mode.
  it('offers conversion to All mode once every operation is selected', async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ include: [] }} onChangeSpy={onChange} />);
    await loadSpec();
    fireEvent.click(screen.getByRole('button', { name: /select all visible operations/i }));

    expect(onChange).toHaveBeenLastCalledWith({
      include: ['listPets', 'createPet', 'deletePet', 'listStores'],
    });
    const offer = screen.getByRole('button', { name: /switch to all operations/i });
    expect(screen.getByText(/pins this server to today's spec/i)).toBeInTheDocument();

    // Offered, not forced: the filter only drops when the operator accepts.
    fireEvent.click(offer);
    expect(onChange).toHaveBeenLastCalledWith(undefined);
  });

  // Acceptance criterion 7: search and chips narrow the list, and select-all
  // respects the narrowing.
  it('searches across operationId, path, summary, and tag', async () => {
    render(<Harness />);
    await loadSpec();
    const search = screen.getByLabelText('Search operations');

    fireEvent.change(search, { target: { value: 'stores' } });
    await waitFor(() => expect(screen.queryByText('/pets/{id}')).not.toBeInTheDocument());
    expect(screen.getByText('/stores')).toBeInTheDocument();

    fireEvent.change(search, { target: { value: 'Remove a pet' } });
    await waitFor(() => expect(screen.getByText('/pets/{id}')).toBeInTheDocument());
  });

  it('narrows by method chip', async () => {
    render(<Harness />);
    await loadSpec();
    fireEvent.click(screen.getByRole('button', { name: 'DELETE' }));

    expect(screen.getByRole('button', { name: 'DELETE' })).toHaveAttribute('aria-pressed', 'true');
    await waitFor(() => expect(screen.queryByText('/stores')).not.toBeInTheDocument());
    expect(screen.getByText('/pets/{id}')).toBeInTheDocument();
  });

  it('selects only the filtered matches when selecting all while filtered', async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ include: [] }} onChangeSpy={onChange} />);
    await loadSpec();
    fireEvent.click(screen.getByRole('button', { name: 'stores' }));
    fireEvent.click(screen.getByRole('button', { name: /select all visible operations/i }));

    expect(onChange).toHaveBeenLastCalledWith({ include: ['listStores'] });
  });

  // The pitfall this whole feature turns on: the raw operationId is persisted,
  // never the sanitized tool name.
  it('persists the raw operationId when it differs from the tool name', async () => {
    vi.spyOn(apiModule, 'previewOpenAPIOperations').mockResolvedValue(
      previewResult({
        operations: [
          operation({ operation_id: 'pets.list', tool_name: 'pets_list', method: 'GET', path: '/pets' }),
        ],
      }),
    );
    const onChange = vi.fn();
    render(<Harness initial={{ include: [] }} onChangeSpy={onChange} />);
    await loadSpec();
    fireEvent.click(screen.getByRole('option', { name: 'GET /pets' }));

    expect(onChange).toHaveBeenLastCalledWith({ include: ['pets.list'] });
    // Both identifiers are shown so a later Tools editor does not look like it
    // is describing different tools.
    expect(screen.getByText(/pets\.list/)).toBeInTheDocument();
    expect(screen.getByText(/pets_list/)).toBeInTheDocument();
  });

  it('reports skipped operations rather than hiding them', async () => {
    vi.spyOn(apiModule, 'previewOpenAPIOperations').mockResolvedValue(
      previewResult({
        operations: [
          ...OPERATIONS,
          operation({ operation_id: '', tool_name: '', method: 'GET', path: '/health', skipped: true, skip_reason: 'no_operation_id' }),
        ],
        skipped_count: 1,
      }),
    );
    render(<Harness />);
    await loadSpec();

    expect(screen.getByText(/1 operation in this spec cannot become tools/i)).toBeInTheDocument();
    expect(screen.getByText(/no operationId in the spec/i)).toBeInTheDocument();
    // Skipped rows are excluded from the count, which must match what deploy produces.
    expect(screen.getByText('All 4 operations will become tools')).toBeInTheDocument();
  });

  it('badges deprecated operations', async () => {
    vi.spyOn(apiModule, 'previewOpenAPIOperations').mockResolvedValue(
      previewResult({
        operations: [operation({ operation_id: 'oldPets', method: 'GET', path: '/v1/pets', deprecated: true })],
      }),
    );
    render(<Harness />);
    await loadSpec();
    expect(screen.getByText('deprecated')).toBeInTheDocument();
  });

  // The warning must describe the deployed surface, not the click history. In
  // exclude mode the destructive operations are the ones left unchecked, and
  // All mode — the default, and the large-spec case — exposes every DELETE.
  it.each([
    ['all mode exposes every DELETE', undefined, /1 operation using DELETE becomes a tool/i],
    ['include mode counts selected DELETEs', { include: ['deletePet', 'listPets'] }, /1 operation using DELETE becomes a tool/i],
    ['exclude mode counts unexcluded DELETEs', { exclude: ['listPets'] }, /1 operation using DELETE becomes a tool/i],
  ])('warns about destructive operations: %s', async (_name, initial, pattern) => {
    render(<Harness initial={initial as OperationsFilter | undefined} />);
    await loadSpec();
    expect(screen.getByText(pattern as RegExp)).toBeInTheDocument();
  });

  it('drops the destructive warning when the DELETE is excluded', async () => {
    render(<Harness initial={{ exclude: ['deletePet'] }} />);
    await loadSpec();
    expect(screen.queryByText(/using DELETE/i)).not.toBeInTheDocument();
  });

  it('drops the destructive warning when no DELETE is selected', async () => {
    render(<Harness initial={{ include: ['listPets'] }} />);
    await loadSpec();
    expect(screen.queryByText(/using DELETE/i)).not.toBeInTheDocument();
  });

  // Returning from Review, restoring a session, or opening a draft remounts the
  // picker with a selection and no loaded list. An empty "Load operations" box
  // there reads as though the selection was lost.
  it('falls back to manual entry when a selection has no loaded list', () => {
    render(<Harness initial={{ include: ['listPets', 'createPet'] }} />);

    expect(screen.getByLabelText('Operation IDs to include')).toHaveValue('listPets\ncreatePet');
    expect(screen.queryByRole('button', { name: /load operations from the openapi spec/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /back to picker/i })).toBeInTheDocument();
  });

  it('still shows the empty state when there is no selection to display', () => {
    render(<Harness />);
    expect(screen.getByRole('button', { name: /load operations from the openapi spec/i })).toBeInTheDocument();
  });

  // Counting is not membership: after a spec change and reload, leftover IDs
  // from the previous document can make the totals line up while none of the
  // new operations are checked.
  it('reaches manual entry from the empty state', () => {
    render(<Harness />);
    fireEvent.click(screen.getAllByRole('button', { name: /enter operation IDs manually/i })[0]);
    expect(screen.getByRole('button', { name: /back to picker/i })).toBeInTheDocument();
  });

  it('does not offer All-mode conversion when the counts match but the sets do not', async () => {
    vi.spyOn(apiModule, 'previewOpenAPIOperations').mockResolvedValue(
      previewResult({
        operations: [
          operation({ operation_id: 'newAlpha', path: '/alpha' }),
          operation({ operation_id: 'newBeta', path: '/beta' }),
        ],
      }),
    );
    render(<Harness initial={{ include: ['staleOne', 'staleTwo'] }} />);
    await loadSpec();

    expect(screen.queryByRole('button', { name: /switch to all operations/i })).not.toBeInTheDocument();
  });

  it('advises against exposing a large spec wholesale', async () => {
    const many = Array.from({ length: 60 }, (_, i) =>
      operation({ operation_id: `op${i}`, path: `/thing/${i}` }),
    );
    vi.spyOn(apiModule, 'previewOpenAPIOperations').mockResolvedValue(
      previewResult({ operations: many }),
    );
    render(<Harness />);
    await loadSpec();
    expect(screen.getByText(/60 operations is a large tool surface/i)).toBeInTheDocument();
  });

  it('flags a spec edited after loading instead of refetching on its own', async () => {
    const { rerender } = render(<Harness />);
    await loadSpec();
    expect(screen.queryByText(/spec changed/i)).not.toBeInTheDocument();

    rerender(<Harness spec="https://example.com/other.json" />);

    expect(screen.getByText(/spec changed since this list was loaded/i)).toBeInTheDocument();
    // Editing the spec must never trigger a fetch on its own.
    expect(apiModule.previewOpenAPIOperations).toHaveBeenCalledTimes(1);
  });

  it('surfaces a structured error with its hint and a manual-entry escape', async () => {
    vi.spyOn(apiModule, 'previewOpenAPIOperations').mockRejectedValue(
      new ProbeError('fetch_failed', 'Could not reach the spec URL.', 'Check the host is reachable.', 422),
    );
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: /load operations from the openapi spec/i }));

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    expect(screen.getByText('Could not reach the spec URL.')).toBeInTheDocument();
    expect(screen.getByText('Check the host is reachable.')).toBeInTheDocument();
    expect(screen.getByText(/enter operation IDs manually instead/i)).toBeInTheDocument();
  });

  it('keeps the loaded list when a reload fails', async () => {
    render(<Harness />);
    await loadSpec();

    vi.spyOn(apiModule, 'previewOpenAPIOperations').mockRejectedValue(
      new ProbeError('fetch_failed', 'Spec host went away.', undefined, 422),
    );
    fireEvent.click(screen.getByRole('button', { name: /reload spec/i }));

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    // The error is surfaced without discarding a list the operator may be
    // midway through selecting from.
    expect(screen.getByText('/pets/{id}')).toBeInTheDocument();
    expect(screen.getByText('All 4 operations will become tools')).toBeInTheDocument();
  });

  it('keeps manual entry available as a fallback and writes raw IDs from it', async () => {
    const onChange = vi.fn();
    render(<Harness initial={{ include: ['seed'] }} onChangeSpy={onChange} />);

    const textarea = screen.getByLabelText('Operation IDs to include');
    fireEvent.change(textarea, { target: { value: 'pets.list\n  getPetById  \n' } });
    expect(onChange).toHaveBeenLastCalledWith({ include: ['pets.list', 'getPetById'] });
  });

  // Acceptance criterion 12.
  it('announces result counts in a live region and labels rows by method and path', async () => {
    render(<Harness />);
    await loadSpec();

    const live = document.querySelector('[aria-live="polite"]');
    expect(live).toHaveTextContent('4 of 4 operations shown');

    fireEvent.change(screen.getByLabelText('Search operations'), { target: { value: 'stores' } });
    await waitFor(() => expect(live).toHaveTextContent('1 of 4 operations shown'));

    expect(screen.getByRole('option', { name: 'GET /stores — List stores' })).toBeInTheDocument();
  });

  it('states that this filter is not reversible from the Tools Whitelist', () => {
    render(<Harness />);
    expect(screen.getByText(/cannot be re-enabled from the Tools Whitelist/i)).toBeInTheDocument();
  });

  it('publishes the selectable total and destructive count for the Review step', async () => {
    render(<Harness />);
    await loadSpec();
    expect(setStats).toHaveBeenLastCalledWith({ total: 4, deleteCount: 1 });
  });
});
