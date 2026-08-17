import { describe, it, expect, vi, afterEach } from 'vitest';
import { AuthError, HTTPError, previewPack, addPack } from '../lib/api';
import {
  authBannerFor,
  httpsEquivalentOf,
  isSSHAgentError,
  isSSHUrl,
  shouldOpenAuthCard,
} from '../lib/gitAuthErrors';

describe('isSSHUrl', () => {
  it.each([
    ['git@github.com:acme/pack.git', true],
    ['ssh://git@github.com/acme/pack.git', true],
    // Any SCP user, matching the server's pattern. A narrower rule would show
    // token fields for these and then fail with a protocol mismatch.
    ['deploy@git.internal:acme/pack.git', true],
    ['gitolite@host:acme/pack', true],
    ['git-mirror@host.example:acme/pack.git', true],
    ['https://github.com/acme/pack', false],
    ['http://github.com/acme/pack', false],
    ['/srv/packs/local', false],
    ['', false],
    // An email-looking string with no colon path is not a git URL.
    ['someone@example.com', false],
  ])('isSSHUrl(%j) === %s', (url, want) => {
    expect(isSSHUrl(url as string)).toBe(want);
  });

  it('tolerates surrounding whitespace, as the inputs are pasted', () => {
    expect(isSSHUrl('  git@github.com:acme/pack.git  ')).toBe(true);
  });
});

describe('classifiers', () => {
  it('treats a 401 AuthError as auth-fixable even with no status', () => {
    expect(shouldOpenAuthCard(new AuthError('Authentication required'))).toBe(true);
  });

  it('treats 401 and 404 HTTPErrors as auth-fixable', () => {
    expect(shouldOpenAuthCard(new HTTPError(401, 'nope'))).toBe(true);
    expect(shouldOpenAuthCard(new HTTPError(404, 'repository not found'))).toBe(true);
  });

  it('does NOT treat a missing ssh-agent as auth-fixable', () => {
    const err = new HTTPError(422, 'ssh agent not available', {
      code: 'ssh_agent_unavailable',
    });
    expect(isSSHAgentError(err)).toBe(true);
    // Credentials cannot fix it, so the card must stay shut.
    expect(shouldOpenAuthCard(err)).toBe(false);
  });

  it('ignores an unrelated server fault', () => {
    expect(shouldOpenAuthCard(new HTTPError(500, 'disk on fire'))).toBe(false);
    expect(isSSHAgentError(new HTTPError(500, 'disk on fire'))).toBe(false);
  });

  it('matches on code, never on message text', () => {
    // Same prose, no code: prose changes, codes do not.
    const prosey = new HTTPError(422, 'ssh agent not available: SSH_AUTH_SOCK is unset');
    expect(isSSHAgentError(prosey)).toBe(false);
  });

  it('exposes the https equivalent only when the server sent one', () => {
    expect(
      httpsEquivalentOf(
        new HTTPError(422, 'x', {
          code: 'ssh_agent_unavailable',
          httpsEquivalent: 'https://github.com/acme/pack',
        }),
      ),
    ).toBe('https://github.com/acme/pack');
    expect(httpsEquivalentOf(new HTTPError(422, 'x', { code: 'ssh_agent_unavailable' }))).toBeUndefined();
    expect(httpsEquivalentOf(new AuthError('x'))).toBeUndefined();
  });

  it('words the banner by cause', () => {
    expect(authBannerFor(new HTTPError(404, 'x'))).toMatch(/private repository/i);
    expect(authBannerFor(new AuthError('x'))).toMatch(/requires authentication/i);
  });
});

/**
 * The fetch layer is the seam that made the ssh-agent banner dead before it was
 * fixed: `packFetch` built a bare HTTPError and dropped the structured fields.
 * Every wizard test constructs its own HTTPError, so if this plumbing regressed
 * they would all still pass while "Try HTTPS instead" silently disappeared.
 * These tests go through the real fetch path on purpose.
 */
describe('pack fetch error contract', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  // vi.stubGlobal is the pattern the other fetch-level suites here use.
  function mockResponse(status: number, body: unknown) {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: status >= 200 && status < 300,
        status,
        json: async () => body,
      }),
    );
  }

  it('carries code and httpsEquivalent off a 422 body', async () => {
    mockResponse(422, {
      error: 'Pack preview failed: ssh agent not available',
      code: 'ssh_agent_unavailable',
      httpsEquivalent: 'https://github.com/acme/pack',
    });

    await expect(previewPack({ repo: 'git@github.com:acme/pack.git' })).rejects.toSatisfy(
      (err: unknown) =>
        err instanceof HTTPError &&
        err.status === 422 &&
        err.code === 'ssh_agent_unavailable' &&
        err.httpsEquivalent === 'https://github.com/acme/pack',
    );
  });

  it('makes that error satisfy the classifiers end to end', async () => {
    mockResponse(422, {
      error: 'Pack import failed: ssh agent not available',
      code: 'ssh_agent_unavailable',
      httpsEquivalent: 'https://github.com/acme/pack',
    });

    const err = await addPack({ repo: 'git@github.com:acme/pack.git' }).catch((e) => e);
    expect(isSSHAgentError(err)).toBe(true);
    expect(httpsEquivalentOf(err)).toBe('https://github.com/acme/pack');
    expect(shouldOpenAuthCard(err)).toBe(false);
  });

  it('turns a 401 into an AuthError, which carries no status', async () => {
    mockResponse(401, { error: 'Authentication required' });

    const err = await previewPack({ repo: 'https://github.com/acme/pack' }).catch((e) => e);
    expect(err).toBeInstanceOf(AuthError);
    expect((err as { status?: number }).status).toBeUndefined();
    // Which is exactly why the classifier cannot rely on status alone.
    expect(shouldOpenAuthCard(err)).toBe(true);
  });

  it('leaves code undefined when the body has none', async () => {
    mockResponse(404, { error: 'Pack preview failed: repository not found' });

    const err = await previewPack({ repo: 'https://github.com/acme/pack' }).catch((e) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect((err as HTTPError).code).toBeUndefined();
    expect((err as HTTPError).httpsEquivalent).toBeUndefined();
    expect(shouldOpenAuthCard(err)).toBe(true);
  });

  it('ignores non-string code and httpsEquivalent values', async () => {
    mockResponse(422, { error: 'x', code: 42, httpsEquivalent: { nope: true } });

    const err = await previewPack({ repo: 'git@github.com:acme/pack.git' }).catch((e) => e);
    expect((err as HTTPError).code).toBeUndefined();
    expect((err as HTTPError).httpsEquivalent).toBeUndefined();
  });
});
