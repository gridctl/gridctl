import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest';
import { credentialHeaders, credentialKey, legacyTokenKey, persistCredential, readCredential } from '../lib/credentials';
import { AuthError, gatewayRequest, verifyCredential } from '../lib/gatewayRequest';
import { useAuthStore } from '../stores/useAuthStore';
import { fetchStatus, triggerReload, initializeStack, RestartRequiredError } from '../lib/api';
import { useTracesStore } from '../stores/useTracesStore';

beforeEach(() => {
  localStorage.clear();
  useAuthStore.setState({ authRequired: false, credential: null, generation: Symbol(), attempt: null });
  vi.stubGlobal('fetch', vi.fn());
});
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });
const candidate = () => ({ mode: 'api_key' as const, header: 'X-Credential', token: crypto.randomUUID() });

describe('credential storage and headers', () => {
  it('treats legacy JSON-looking tokens as opaque bearer text', () => {
    const raw = JSON.stringify({ token: crypto.randomUUID() });
    localStorage.setItem(legacyTokenKey, raw);
    const stored = readCredential().credential;
    expect(stored?.mode).toBe('bearer');
    expect(stored?.token === raw).toBe(true);
  });
  it('round-trips one atomic versioned entry', () => {
    const value = candidate();
    expect(persistCredential(value)).toBeNull();
    expect(readCredential().credential?.token === value.token).toBe(true);
    expect(readCredential().credential?.header).toBe(value.header);
  });
  it('preserves unknown data in a supported version and refuses newer writes', () => {
    localStorage.setItem(credentialKey, JSON.stringify({ version: 1, ...candidate(), extension: { preserve: true } }));
    expect(persistCredential(candidate())).toBeNull();
    expect(JSON.parse(localStorage.getItem(credentialKey)!).extension.preserve).toBe(true);
    const newer = JSON.stringify({ version: 2, opaque: true });
    localStorage.setItem(credentialKey, newer);
    expect(persistCredential(candidate())).not.toBeNull();
    expect(localStorage.getItem(credentialKey)).toBe(newer);
  });
  it('verifies explicitly and retains window-only state when storage reads fail', async () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('unavailable'); });
    expect(readCredential().credential).toBeNull();
    expect(readCredential().notice).not.toBeNull();
    const value = candidate();
    vi.mocked(fetch).mockResolvedValue(Response.json({ gateway: { name: 'fixture', version: 'test' }, 'mcp-servers': [] }));
    await verifyCredential(value);
    expect(useAuthStore.getState().commitAttempt(useAuthStore.getState().beginAttempt(), value)).toBe(true);
    expect(useAuthStore.getState().credential?.token === value.token).toBe(true);
    expect(useAuthStore.getState().notice).toMatch(/window only/);
  });
  it('does not pair a new header with an old saved token after a failed replacement', () => {
    persistCredential(candidate());
    const prior = localStorage.getItem(credentialKey);
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('unavailable'); });
    expect(persistCredential({ ...candidate(), header: 'X-Replacement' })).not.toBeNull();
    expect(localStorage.getItem(credentialKey) === prior).toBe(true);
  });
  it('requests re-entry for unusable stored headers without deleting the entry', () => {
    const entry = JSON.stringify({ version: 1, ...candidate(), header: 'Host' });
    localStorage.setItem(credentialKey, entry);
    expect(readCredential().credential).toBeNull();
    expect(readCredential().notice).toMatch(/browser Fetch/);
    expect(localStorage.getItem(credentialKey) === entry).toBe(true);
  });
  it.each(['broken', '{}', '{"version":99}', '{"version":1,"mode":"unknown"}'])('refuses corrupt or unknown metadata', entry => {
    localStorage.setItem(legacyTokenKey, crypto.randomUUID());
    localStorage.setItem(credentialKey, entry);
    expect(readCredential().credential).toBeNull();
    expect(readCredential().notice).not.toBeNull();
  });
  it.each(['bad header', 'Host', 'Cookie', 'sEc-Test', 'Proxy-Authorization', 'Content-Type', 'Accept', 'mCp-SeSsIoN-Id'])('refuses unsupported or owned header %s', header => {
    expect(() => credentialHeaders({ ...candidate(), header })).toThrow();
  });
  it('rejects a case-insensitive request collision', () => {
    expect(() => credentialHeaders(candidate(), { 'x-credential': 'request-owned' })).toThrow(/conflicts/);
  });
  it.each([0, 9, 10, 13, 31, 127])('refuses value control code %s without disclosure', code => {
    const value = candidate();
    value.token += String.fromCharCode(code);
    try { credentialHeaders(value); throw new Error('accepted'); } catch (error) {
      expect((error as Error).message).toMatch(/control characters/);
      expect((error as Error).message.includes(value.token)).toBe(false);
    }
  });
  it('preserves raw API-key and bearer bytes', () => {
    const value = candidate();
    const raw = credentialHeaders({ ...value, header: '' });
    expect(raw.get('Authorization') === value.token).toBe(true);
    const bearer = credentialHeaders({ ...value, mode: 'bearer' });
    expect(bearer.get('Authorization') === `Bearer ${value.token}`).toBe(true);
    const prefixed = credentialHeaders({ ...value, mode: 'bearer', token: ' ' + value.token });
    expect(prefixed.get('Authorization') === `Bearer  ${value.token}`).toBe(true);
  });
});

describe('request lifecycle', () => {
  it('releases generation listeners when an HTTP failure body is not consumed', async () => {
    let listeners = 0;
    const subscribe = useAuthStore.subscribe;
    vi.spyOn(useAuthStore, 'subscribe').mockImplementation(listener => {
      listeners++;
      const stop = subscribe(listener);
      return () => { listeners--; stop(); };
    });
    vi.mocked(fetch).mockResolvedValue(new Response('Server unavailable', { status: 500 }));
    await expect(fetchStatus()).rejects.toThrow();
    expect(listeners).toBe(0);
  });
  it('does not replace newer trace state with a stale polling failure', async () => {
    useTracesStore.setState({ traces: [], error: null, total: 0 });
    let resolve!: (response: Response) => void;
    vi.mocked(fetch).mockReturnValueOnce(new Promise(r => { resolve = r; }));
    const old = useTracesStore.getState().loadTraces();
    useAuthStore.getState().commitAttempt(useAuthStore.getState().beginAttempt(), candidate());
    vi.mocked(fetch).mockResolvedValueOnce(Response.json({ traces: [], total: 7, tracingEnabled: true, bufferSize: 7, bufferCapacity: 100 }));
    await useTracesStore.getState().loadTraces();
    resolve(new Response(null, { status: 401, headers: { 'Gridctl-Auth-Rejected': '1' } }));
    await old;
    expect(useTracesStore.getState().error).toBeNull();
    expect(useTracesStore.getState().total).toBe(7);
    expect(useAuthStore.getState().authRequired).toBe(false);
  });
  it('only marks middleware-proven gateway rejection as auth-required', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 401 }));
    await expect(fetchStatus()).rejects.toMatchObject({ status: 401 });
    expect(useAuthStore.getState().authRequired).toBe(false);
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 401, headers: { 'Gridctl-Auth-Rejected': '1' } }));
    await expect(fetchStatus()).rejects.toBeInstanceOf(AuthError);
    expect(useAuthStore.getState().authRequired).toBe(true);
    await expect(triggerReload()).rejects.toMatchObject({ kind: 'stale' });
    expect(fetch).toHaveBeenCalledTimes(2);
  });
  it.each([200, 401])('ignores late status %s after newer verification', async status => {
    let resolve!: (response: Response) => void;
    vi.mocked(fetch).mockReturnValue(new Promise(r => { resolve = r; }));
    const pending = gatewayRequest('/api/status');
    const newer = candidate();
    useAuthStore.getState().commitAttempt(useAuthStore.getState().beginAttempt(), newer);
    resolve(new Response(null, { status, headers: { 'Gridctl-Auth-Rejected': '1' } }));
    await expect(pending).rejects.toMatchObject({ kind: 'stale' });
    expect(useAuthStore.getState().authRequired).toBe(false);
    expect(useAuthStore.getState().credential?.token === newer.token).toBe(true);
  });
  it('does not commit superseded attempts or storage-observed credentials', () => {
    const first = useAuthStore.getState().beginAttempt();
    const second = useAuthStore.getState().beginAttempt();
    expect(useAuthStore.getState().commitAttempt(first, candidate())).toBe(false);
    expect(useAuthStore.getState().commitAttempt(second, candidate())).toBe(true);
    const prior = useAuthStore.getState().credential;
    useAuthStore.getState().storageChanged();
    expect(useAuthStore.getState().credential === prior).toBe(true);
    expect(useAuthStore.getState().authRequired).toBe(true);
  });
  it('rejects off-origin URLs before fetch', async () => {
    await expect(gatewayRequest('https://other.invalid/api/status')).rejects.toMatchObject({ kind: 'origin' });
    expect(fetch).not.toHaveBeenCalled();
  });
  it.each([301, 302, 307, 308])('rejects visible redirects %s', async status => {
    vi.mocked(fetch).mockResolvedValue(new Response(null, { status }));
    await expect(verifyCredential(candidate())).rejects.toMatchObject({ kind: 'redirect' });
    expect(fetch).toHaveBeenCalledOnce();
  });
  it('classifies opaque browser redirects before parsing', async () => {
    vi.mocked(fetch).mockResolvedValue({ type: 'opaqueredirect', status: 0 } as Response);
    await expect(gatewayRequest('/sse')).rejects.toMatchObject({ kind: 'redirect' });
  });
  it('distinguishes connection, abort, and malformed responses', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('opaque'));
    await expect(gatewayRequest('/api/status')).rejects.toMatchObject({ kind: 'connection' });
    const controller = new AbortController(); controller.abort();
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('opaque'));
    await expect(gatewayRequest('/api/status', { signal: controller.signal })).rejects.toMatchObject({ kind: 'abort' });
    vi.mocked(fetch).mockResolvedValueOnce(new Response('<html>'));
    await expect(verifyCredential(candidate())).rejects.toMatchObject({ kind: 'malformed' });
  });
  it.each([triggerReload, () => initializeStack('fixture')])('keeps restart refusal distinguishable from stack conflicts', async operation => {
    vi.mocked(fetch).mockResolvedValue(Response.json({ code: 'restart_required', changed_fields: ['gateway.auth.token'] }, { status: 409 }));
    await expect(operation()).rejects.toBeInstanceOf(RestartRequiredError);
  });
});
