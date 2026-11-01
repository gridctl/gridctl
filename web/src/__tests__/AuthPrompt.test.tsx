import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import '@testing-library/jest-dom';
import { AuthPrompt } from '../components/auth/AuthPrompt';
import { useAuthStore } from '../stores/useAuthStore';
import { credentialKey, windowOnlyNotice } from '../lib/credentials';
import { showToast } from '../components/ui/Toast';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

const status = () => Response.json({ gateway: { name: 'gridctl', version: 'test' }, 'mcp-servers': [] });
beforeEach(() => {
  localStorage.clear();
  useAuthStore.setState({ authRequired: true, isAuthenticated: false, credential: null, generation: Symbol(), attempt: null, notice: null });
  vi.stubGlobal('fetch', vi.fn());
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function submit() {
  const input = screen.getByLabelText('Credential');
  fireEvent.change(input, { target: { value: crypto.randomUUID() } });
  fireEvent.submit(input.closest('form')!);
}

describe('AuthPrompt', () => {
  it('explains the stack key, the CLI command, and the mode select', () => {
    render(<AuthPrompt />);
    expect(screen.getByText(/gateway\.auth/)).toBeInTheDocument();
    expect(screen.getByText(/gridctl var get/)).toBeInTheDocument();
    const docs = screen.getByRole('link', { name: 'Gateway authentication docs' });
    expect(docs).toHaveAttribute('href', 'https://github.com/gridctl/gridctl/blob/main/docs/config-schema.md#auth');
    expect(docs).toHaveAttribute('target', '_blank');
    expect(screen.getByLabelText('Authentication mode')).toHaveAccessibleDescription(/Bearer sends Authorization: Bearer <token>/);
  });

  it('labels inputs, disables empty submission, and names Show/Hide', () => {
    render(<AuthPrompt />);
    expect(screen.getByRole('heading', { name: 'Authentication Required' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Authenticate' })).toBeDisabled();
    const input = screen.getByLabelText('Credential');
    expect(input).toHaveAttribute('type', 'password');
    fireEvent.click(screen.getByRole('button', { name: 'Show credential' }));
    expect(screen.getByRole('button', { name: 'Hide credential' })).toHaveAttribute('aria-pressed', 'true');
    expect(input).toHaveAttribute('type', 'text');
  });

  it('verifies an explicit draft before replacing active state or storage', async () => {
    let resolve!: (response: Response) => void;
    vi.mocked(fetch).mockReturnValue(new Promise(r => { resolve = r; }));
    render(<AuthPrompt />);
    submit();
    expect(localStorage.getItem(credentialKey)).toBeNull();
    expect(useAuthStore.getState().credential).toBeNull();
    const [, init] = vi.mocked(fetch).mock.calls[0];
    expect(new Headers(init?.headers).has('Authorization')).toBe(true);
    expect(init?.redirect).toBe('manual');
    resolve(status());
    await waitFor(() => expect(useAuthStore.getState().isAuthenticated).toBe(true));
    expect(localStorage.getItem(credentialKey) !== null).toBe(true);
    expect(showToast).toHaveBeenCalledWith('success', 'Credential verified and saved in this browser for this origin.', { duration: 6000 });
  });

  it('preserves intentional input across mode and header changes', () => {
    render(<AuthPrompt />);
    const input = screen.getByLabelText('Credential') as HTMLInputElement;
    const token = crypto.randomUUID();
    fireEvent.change(input, { target: { value: token } });
    fireEvent.change(screen.getByLabelText('Authentication mode'), { target: { value: 'api_key' } });
    expect(screen.getByLabelText('Credential header')).toHaveValue('Authorization');
    fireEvent.change(screen.getByLabelText('Credential header'), { target: { value: 'X-Credential' } });
    fireEvent.change(screen.getByLabelText('Authentication mode'), { target: { value: 'bearer' } });
    expect(input.value === token).toBe(true);
  });

  it('uses distinct alert text for rejected, forbidden, and gateway errors', async () => {
    render(<AuthPrompt />);
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 401, headers: { 'Gridctl-Auth-Rejected': '1' } }));
    submit();
    expect(await screen.findByRole('alert')).toHaveTextContent('rejected this credential');
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 401 }));
    submit();
    expect(await screen.findByRole('alert')).toHaveTextContent('rejected this credential');
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 403 }));
    submit();
    const forbidden = await screen.findByRole('alert');
    expect(forbidden).toHaveTextContent('Host header');
    expect(forbidden).not.toHaveTextContent('Origin');
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 500 }));
    submit();
    const server = await screen.findByRole('alert');
    expect(server).toHaveTextContent('server error (HTTP 500)');
    expect(server.textContent).not.toBe(forbidden.textContent);
  });

  it.each([401, 403, 500])('preserves the active credential on HTTP %s', async code => {
    const credential = { mode: 'bearer' as const, header: 'Authorization', token: crypto.randomUUID() };
    useAuthStore.setState({ credential });
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status: code, headers: code === 401 ? { 'Gridctl-Auth-Rejected': '1' } : {} }));
    render(<AuthPrompt />);
    submit();
    await screen.findByRole('alert');
    expect(useAuthStore.getState().credential === credential).toBe(true);
    expect(screen.getByLabelText('Credential')).toHaveFocus();
    expect(localStorage.getItem(credentialKey)).toBeNull();
  });

  it('allows verified in-memory use when persistence fails', async () => {
    vi.mocked(fetch).mockResolvedValue(status());
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('unavailable'); });
    render(<AuthPrompt />);
    submit();
    await waitFor(() => expect(useAuthStore.getState().isAuthenticated).toBe(true));
    expect(useAuthStore.getState().notice).toBe(windowOnlyNotice);
    expect(showToast).not.toHaveBeenCalledWith('success', expect.any(String), expect.anything());
  });

  it('rejects a malformed successful status response', async () => {
    vi.mocked(fetch).mockResolvedValue(Response.json({}));
    render(<AuthPrompt />);
    submit();
    await screen.findByRole('alert');
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });
});
