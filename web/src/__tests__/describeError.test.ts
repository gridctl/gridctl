import { describe, expect, it } from 'vitest';
import {
  AgentScanError,
  ClientLinkError,
  ClientScopeError,
  HTTPError,
  PackFindingsError,
  ProbeError,
  RestartRequiredError,
  SetServerToolsError,
  StackAlreadyActiveError,
  StackModifiedError,
} from '../lib/api';
import { AuthError, GatewayRequestError } from '../lib/gatewayRequest';
import { asError, describeError, isCancellation, messageOf } from '../lib/describeError';

describe('messageOf', () => {
  it('reads Error, string, and message objects, and never stringifies the rest', () => {
    expect(messageOf(new Error('disk full'))).toBe('disk full');
    expect(messageOf('plain')).toBe('plain');
    expect(messageOf({ message: 'from field' })).toBe('from field');
    expect(messageOf({ message: 12 })).toBe('');
    expect(messageOf(null)).toBe('');
    expect(messageOf(undefined)).toBe('');
    expect(messageOf(7)).toBe('');
    expect(messageOf({ code: 1 })).toBe('');
    expect(messageOf(new Error('[object Object]'))).toBe('');
    expect(messageOf('[object Object]')).toBe('');
  });
});

describe('asError', () => {
  it('returns an Error without stringifying the value', () => {
    const existing = new Error('kept');
    expect(asError(existing)).toBe(existing);
    expect(asError('text').message).toBe('text');
    expect(asError({ message: 'field' }).message).toBe('field');
    expect(asError({}).message).toBe('');
    expect(asError(null).message).toBe('');
    expect(String(asError({}))).not.toContain('[object Object]');
  });
});

describe('isCancellation', () => {
  it('matches AbortError and superseded gateway requests', () => {
    expect(isCancellation(new DOMException('aborted', 'AbortError'))).toBe(true);
    expect(isCancellation(new GatewayRequestError('abort', 'Gateway request cancelled.'))).toBe(true);
    expect(isCancellation(new GatewayRequestError('stale', 'Superseded credential request.'))).toBe(true);
    expect(isCancellation(new GatewayRequestError('connection', 'down'))).toBe(false);
    expect(isCancellation(new Error('aborted'))).toBe(false);
    expect(isCancellation(new DOMException('no', 'NotAllowedError'))).toBe(false);
  });
});

describe('describeError', () => {
  it('frames a plain Error, a string, and a message object', () => {
    expect(describeError(new Error('disk full'), 'Save')).toEqual({
      summary: 'Save failed',
      detail: 'disk full',
      text: 'Save failed: disk full.',
    });
    expect(describeError('disk full', 'Save')).toEqual({
      summary: 'Save failed',
      detail: 'disk full',
      text: 'Save failed: disk full.',
    });
    expect(describeError({ message: 'disk full' }, 'Save')).toEqual({
      summary: 'Save failed',
      detail: 'disk full',
      text: 'Save failed: disk full.',
    });
  });

  it('omits detail for null, undefined, and plain objects', () => {
    for (const value of [null, undefined, { code: 1 }, 4]) {
      const described = describeError(value, 'Save');
      expect(described).toEqual({ summary: 'Save failed', text: 'Save failed.' });
      expect(described.text).not.toContain('[object Object]');
      expect(JSON.stringify(described)).not.toContain('[object Object]');
    }
  });

  it('pauses on gateway AuthError without echoing a token', () => {
    const described = describeError(new AuthError(), 'Save');
    expect(described.summary).toBe('Save paused');
    expect(described.detail).toBeUndefined();
    expect(described.hint).toBe('Verify your gateway credential to resume.');
    expect(described.text).toBe('Save paused. Verify your gateway credential to resume.');
  });

  it('maps gateway transport kinds', () => {
    expect(describeError(new GatewayRequestError('connection', 'Gateway connection failed.'), 'Reload')).toMatchObject({
      summary: 'Reload failed',
      detail: 'Gateway connection failed.',
      hint: 'Check that the gateway is running and reachable.',
    });
    expect(describeError(new GatewayRequestError('redirect', 'Gateway redirect refused.'), 'Reload').hint).toBe(
      'Open the UI directly on the gateway origin.',
    );
    expect(describeError(new GatewayRequestError('origin', 'Credential-bearing requests must use the gateway origin.'), 'Reload').hint).toBe(
      'Open the UI directly on the gateway origin.',
    );
    expect(describeError(new GatewayRequestError('malformed', 'Gateway returned an invalid JSON response.'), 'Reload').hint).toBe(
      'Refresh and retry. If it persists, check gridctl logs.',
    );
    expect(describeError(new GatewayRequestError('abort', 'Gateway request cancelled.'), 'Reload')).toEqual({
      summary: 'Reload cancelled',
      text: 'Reload cancelled.',
    });
    expect(describeError(new GatewayRequestError('stale', 'Superseded credential request.'), 'Reload').summary).toBe(
      'Reload cancelled',
    );
  });

  it('gives AgentScanError the findings hint rather than the generic 409 hint', () => {
    const described = describeError(new AgentScanError('scan blocked', []), 'Save');
    expect(described.summary).toBe('Save blocked by findings');
    expect(described.detail).toBe('scan blocked');
    expect(described.hint).toBe('Review the security scan findings before saving again.');
    expect(described.hint).not.toContain('Something changed underneath');
  });

  it('keeps a downstream 401 message and does not hint at the gateway credential', () => {
    const described = describeError(
      new HTTPError(401, 'The downstream operation requires authentication.'),
      'Import',
    );
    expect(described.detail).toBe('The downstream operation requires authentication.');
    expect(described.hint).toBeUndefined();
    expect(described.text).not.toMatch(/gateway credential/i);
    expect(described.text).toBe('Import failed: The downstream operation requires authentication.');
  });

  it('selects status hints for 404, 409, and 5xx', () => {
    expect(describeError(new HTTPError(404, 'missing'), 'Delete').hint).toBe(
      'It may have been removed. Refresh and retry.',
    );
    expect(describeError(new HTTPError(404, 'missing'), 'Delete').text).toBe(
      'Delete failed: missing. It may have been removed. Refresh and retry.',
    );
    expect(describeError(new HTTPError(409, 'conflict'), 'Save').hint).toBe(
      'Something changed underneath. Refresh and retry.',
    );
    expect(describeError(new HTTPError(409, 'conflict'), 'Save').text).toBe(
      'Save failed: conflict. Something changed underneath. Refresh and retry.',
    );
    expect(describeError(new HTTPError(503, 'unavailable'), 'Save').hint).toBe(
      'The gateway reported an error. Check gridctl logs.',
    );
    expect(describeError(new HTTPError(400, 'bad request'), 'Save').hint).toBeUndefined();
  });

  it('prefers the ssh-agent remedy over a status hint', () => {
    const described = describeError(
      new HTTPError(400, 'no agent', { code: 'ssh_agent_unavailable' }),
      'Import',
    );
    expect(described.hint).toBe('No ssh-agent is reachable. Use the HTTPS URL with a credential.');
  });

  it('uses structured hints, and the reload_failed sentence for that code', () => {
    expect(
      describeError(new ClientLinkError('link_conflict', 'occupied', 'Choose another name.', 409), 'Link').hint,
    ).toBe('Choose another name.');
    expect(
      describeError(new ClientScopeError('unknown_server', 'gone', 'Refresh the client list.', 422), 'Save').hint,
    ).toBe('Refresh the client list.');
    expect(
      describeError(new SetServerToolsError('reload_failed', 'reload broke', 'server hint', 502), 'Save').hint,
    ).toBe('The file was saved. Use Reload Config or run gridctl reload.');
    expect(describeError(new ProbeError('rate_limited', 'slow down', 'Wait and retry.', 429), 'Probe').hint).toBe(
      'Wait and retry.',
    );
    expect(describeError(new StackModifiedError('changed on disk', 'Reload the file.'), 'Save').hint).toBe(
      'Reload the file.',
    );
    expect(describeError(new ProbeError('internal', 'no hint', undefined, 500), 'Probe').hint).toBeUndefined();
  });

  it('maps restart, pack findings, and an already-active stack', () => {
    const restart = describeError(new RestartRequiredError(['gateway.auth.token']), 'Reload');
    expect(restart.summary).toBe('Reload requires a restart');
    expect(restart.hint).toBe('Restart the gateway with its original startup options.');
    expect(restart.detail).toMatch(/Restart required/);

    const findings = describeError(new PackFindingsError('blocked', 'demo', []), 'Import pack');
    expect(findings.summary).toBe('Import pack blocked by findings');
    expect(findings.hint).toBe('Review the findings and acknowledge trust to continue.');

    const active = describeError(new StackAlreadyActiveError(), 'Initialize');
    expect(active.detail).toBe('A stack is already active.');
    expect(active.hint).toBe('Stop it before initializing another.');
    expect(active.text).toBe('Initialize failed: A stack is already active. Stop it before initializing another.');
  });

  it('applies 404, 409, and 5xx hints to API error messages', () => {
    expect(describeError(new Error('API error: 503 upstream'), 'Refresh').hint).toBe(
      'The gateway reported an error. Check gridctl logs.',
    );
    expect(describeError(new Error('API error: 404 gone'), 'Delete').hint).toBe(
      'It may have been removed. Refresh and retry.',
    );
    expect(describeError(new Error('API error: 409 stale'), 'Save').hint).toBe(
      'Something changed underneath. Refresh and retry.',
    );
    expect(describeError(new Error('API error: 400 bad'), 'Save').hint).toBeUndefined();
  });

  it('strips a leading Error prefix, truncates at 160, and keeps the untrimmed text', () => {
    const described = describeError(new Error('Error: short'), 'Save');
    expect(described.detail).toBe('short');
    expect(described.fullDetail).toBeUndefined();
    expect(described.text).toBe('Save failed: short.');

    const long = `Error: ${'x'.repeat(180)}\nsecond line`;
    const truncated = describeError(new Error(long), 'Save');
    expect(truncated.detail).toHaveLength(160);
    expect(truncated.detail?.endsWith('…')).toBe(true);
    expect(truncated.detail).not.toContain('\n');
    expect(truncated.fullDetail).toBe(`${'x'.repeat(180)}\nsecond line`);
    expect(truncated.text.endsWith('.')).toBe(true);
  });

  it('collapses whitespace on the first line and omits an empty message', () => {
    const described = describeError(new Error('  disk   full  \nmore'), 'Save');
    expect(described.detail).toBe('disk full');
    expect(described.fullDetail).toBe('disk   full  \nmore');
    expect(describeError(new Error(''), 'Save')).toEqual({ summary: 'Save failed', text: 'Save failed.' });
    expect(describeError(new Error('Error: '), 'Save').detail).toBeUndefined();
  });
});
