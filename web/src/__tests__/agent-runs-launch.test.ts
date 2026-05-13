import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  fetchAgentRun,
  inputFromRunDetail,
  launchRun,
  LaunchRunError,
} from '../lib/agent-runs';

function mockResponse(body: unknown, init: { status?: number; statusText?: string } = {}) {
  const status = init.status ?? 200;
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: init.statusText ?? (status === 200 ? 'OK' : 'ERR'),
    json: async () => body,
    text: async () => (typeof body === 'string' ? body : JSON.stringify(body)),
  } as unknown as Response;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('launchRun', () => {
  it('POSTs skill_name and input and returns the run id', async () => {
    const fetchStub = vi.fn().mockResolvedValue(
      mockResponse({ run_id: 'run-123', started_at: '2026-05-13T17:00:00Z' }),
    );
    vi.stubGlobal('fetch', fetchStub);

    const res = await launchRun({ skill_name: 'repo-audit', input: { url: 'x' } });

    expect(res.run_id).toBe('run-123');
    expect(res.started_at).toBe('2026-05-13T17:00:00Z');
    expect(fetchStub).toHaveBeenCalledTimes(1);
    const [url, init] = fetchStub.mock.calls[0];
    expect(url).toBe('/api/agent/runs');
    expect(init.method).toBe('POST');
    const body = JSON.parse(init.body as string);
    expect(body).toEqual({ skill_name: 'repo-audit', input: { url: 'x' } });
  });

  it('throws LaunchRunError with the server message on 4xx', async () => {
    const fetchStub = vi.fn().mockResolvedValue(
      mockResponse({ error: 'skill "ghost" not found' }, { status: 404, statusText: 'Not Found' }),
    );
    vi.stubGlobal('fetch', fetchStub);

    await expect(launchRun({ skill_name: 'ghost', input: {} })).rejects.toMatchObject({
      name: 'LaunchRunError',
      status: 404,
      message: 'skill "ghost" not found',
    });
  });

  it('throws LaunchRunError with the status text on 5xx with no error body', async () => {
    const fetchStub = vi.fn().mockResolvedValue(
      // simulate a body where json parse fails — body.error is missing
      {
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
        json: async () => {
          throw new Error('no body');
        },
        text: async () => '',
      } as unknown as Response,
    );
    vi.stubGlobal('fetch', fetchStub);

    const err = await launchRun({ skill_name: 'x', input: {} }).catch((e) => e);
    expect(err).toBeInstanceOf(LaunchRunError);
    expect(err.status).toBe(500);
    expect(err.message).toBe('500 Internal Server Error');
  });
});

describe('fetchAgentRun', () => {
  it('returns the run detail on 200', async () => {
    const detail = {
      run: { run_id: 'r1', status: 'completed', event_count: 1 },
      events: [
        {
          run_id: 'r1',
          seq: 1,
          time: '2026-05-13T17:00:00Z',
          type: 'run_started',
          payload: { skill: 'x', input: { url: 'https://example.com' } },
        },
      ],
    };
    const fetchStub = vi.fn().mockResolvedValue(mockResponse(detail));
    vi.stubGlobal('fetch', fetchStub);

    const got = await fetchAgentRun('r1');
    expect(got?.events[0].type).toBe('run_started');
  });

  it('returns null on 404', async () => {
    const fetchStub = vi.fn().mockResolvedValue(mockResponse({}, { status: 404 }));
    vi.stubGlobal('fetch', fetchStub);
    expect(await fetchAgentRun('missing')).toBeNull();
  });
});

describe('inputFromRunDetail', () => {
  it('extracts the input object from the run_started event', () => {
    expect(
      inputFromRunDetail({
        run: { run_id: 'r', status: 'ok', event_count: 1 },
        events: [
          {
            run_id: 'r',
            seq: 1,
            time: 't',
            type: 'run_started',
            payload: { input: { url: 'https://example.com' } },
          },
        ],
      }),
    ).toEqual({ url: 'https://example.com' });
  });

  it('returns {} when there is no run_started event', () => {
    expect(
      inputFromRunDetail({
        run: { run_id: 'r', status: 'ok', event_count: 0 },
        events: [],
      }),
    ).toEqual({});
  });

  it('returns {} for a null detail', () => {
    expect(inputFromRunDetail(null)).toEqual({});
  });

  it('returns {} when the input is a non-object', () => {
    expect(
      inputFromRunDetail({
        run: { run_id: 'r', status: 'ok', event_count: 1 },
        events: [
          {
            run_id: 'r',
            seq: 1,
            time: 't',
            type: 'run_started',
            payload: { input: ['bad'] },
          },
        ],
      }),
    ).toEqual({});
  });
});
