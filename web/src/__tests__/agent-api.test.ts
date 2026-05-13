import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchSkill, fetchSkills } from '../lib/agent-api';

function mockFetchResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? 'OK' : 'ERR',
    json: async () => body,
  } as unknown as Response;
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('fetchSkill', () => {
  it('coerces a null `nodes` field to an empty array', async () => {
    const stub = vi.fn().mockResolvedValue(
      mockFetchResponse({
        skill: 'blog',
        lang: '',
        file: '',
        nodes: null,
        parse_error: 'no typed handler (skill.go / skill.ts) found',
      }),
    );
    vi.stubGlobal('fetch', stub);

    const g = await fetchSkill('blog');
    expect(g).not.toBeNull();
    expect(g!.nodes).toEqual([]);
    expect(Array.isArray(g!.nodes)).toBe(true);
  });

  it('preserves a populated `nodes` array', async () => {
    const nodes = [
      { id: 'tool:0', kind: 'tool' as const, label: 'x', file: 'skill.ts', line: 1, col: 1 },
    ];
    const stub = vi.fn().mockResolvedValue(
      mockFetchResponse({ skill: 'x', lang: 'ts', file: 'skill.ts', nodes }),
    );
    vi.stubGlobal('fetch', stub);

    const g = await fetchSkill('x');
    expect(g!.nodes).toEqual(nodes);
  });

  it('returns null for 404 / 503', async () => {
    const stub = vi.fn().mockResolvedValue(mockFetchResponse({}, 404));
    vi.stubGlobal('fetch', stub);
    expect(await fetchSkill('missing')).toBeNull();
  });
});

describe('fetchSkills', () => {
  it('returns the skills array', async () => {
    const stub = vi.fn().mockResolvedValue(
      mockFetchResponse({
        skills: [{ name: 'x', lang: 'ts', dir: 'x', node_count: 1 }],
      }),
    );
    vi.stubGlobal('fetch', stub);

    const skills = await fetchSkills();
    expect(skills).toHaveLength(1);
    expect(skills[0].name).toBe('x');
  });

  it('returns [] on 503', async () => {
    const stub = vi.fn().mockResolvedValue(mockFetchResponse({}, 503));
    vi.stubGlobal('fetch', stub);
    expect(await fetchSkills()).toEqual([]);
  });
});
