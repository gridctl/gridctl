import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom/vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { SpecTab } from '../components/spec/SpecTab';
import { useSpecStore } from '../stores/useSpecStore';
import { AuthError, fetchStackExport, HTTPError } from '../lib/api';
import { useAuthStore } from '../stores/useAuthStore';

vi.mock('../lib/api', async (importOriginal) => ({
  ...await importOriginal<typeof import('../lib/api')>(),
  fetchStackSpec: vi.fn().mockResolvedValue({ path: '/stack.yaml', content: 'name: raw-authored-spec' }),
  fetchStackHealth: vi.fn().mockResolvedValue({}),
  validateStackSpec: vi.fn().mockResolvedValue({ valid: true, issues: [] }),
}));

beforeEach(() => {
  useAuthStore.setState({ authRequired: false, generation: Symbol(), credential: null });
  vi.stubGlobal('fetch', vi.fn());
  useSpecStore.setState({ spec: null, specError: null, specLoading: false, validation: null });
  vi.stubGlobal('URL', Object.assign(URL, {
    createObjectURL: vi.fn().mockReturnValue('blob:export'),
    revokeObjectURL: vi.fn(),
  }));
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

it('downloads only the API export and keeps raw content unchanged', async () => {
  vi.mocked(fetch).mockResolvedValue(Response.json({ content: 'name: exported', format: 'yaml', notice: 'Review authored literals before sharing.' }));
  render(<SpecTab />);
  const button = await screen.findByRole('button', { name: 'Export YAML' });
  expect(button).toHaveAttribute('aria-describedby', 'stack-export-notice');
  button.focus();
  expect(button).toHaveFocus();
  fireEvent.click(button);
  await waitFor(() => expect(HTMLAnchorElement.prototype.click).toHaveBeenCalledOnce());
  expect(URL.createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
  expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:export');
  expect(screen.getByText(/Review authored literals before sharing/)).toBeInTheDocument();
  expect(useSpecStore.getState().spec?.content).toBe('name: raw-authored-spec');
  expect(screen.getByText(/raw-authored-spec/)).toBeInTheDocument();
});

it('prevents repeated actions and downloads on failure, then permits retry', async () => {
  let resolveExport!: (response: Response) => void;
  vi.mocked(fetch).mockReturnValue(new Promise((resolve) => { resolveExport = resolve; }));
  render(<SpecTab />);
  fireEvent.click(await screen.findByRole('button', { name: 'Export YAML' }));
  const pending = screen.getByRole('button', { name: 'Exporting...' });
  expect(pending).toBeDisabled();
  fireEvent.click(pending);
  expect(fetch).toHaveBeenCalledOnce();
  expect(fetch).toHaveBeenCalledWith(new URL('/api/stack/export', window.location.origin).href, expect.objectContaining({ headers: expect.any(Object), redirect: 'manual' }));
  const message = 'Failed to load stack: export: gateway.auth.token: recognized sensitive field contains an inline literal; use an authored variable reference';
  resolveExport(Response.json({ error: message }, { status: 500, statusText: 'Internal Server Error' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(message);
  expect(URL.createObjectURL).not.toHaveBeenCalled();
  expect(HTMLAnchorElement.prototype.click).not.toHaveBeenCalled();
  expect(screen.getByRole('button', { name: 'Export YAML' })).toBeEnabled();
  expect(useSpecStore.getState().spec?.content).toBe('name: raw-authored-spec');
});

it('preserves authentication errors', async () => {
  vi.mocked(fetch).mockResolvedValue(new Response(null, { status: 401, headers: { 'Gridctl-Auth-Rejected': '1' } }));
  await expect(fetchStackExport()).rejects.toBeInstanceOf(AuthError);
});

it.each(['not JSON', '{}', '{"error":{}}'])('uses a status fallback for invalid error bodies: %s', async (body) => {
  vi.mocked(fetch).mockResolvedValue(new Response(body, { status: 502 }));
  await expect(fetchStackExport()).rejects.toMatchObject({
    name: 'HTTPError',
    status: 502,
    message: 'GET /api/stack/export failed: 502',
  } satisfies Partial<HTTPError>);
});
