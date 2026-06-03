import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  updateSkillSource,
  syncAllSources,
  fetchSkillDiff,
  detachSkill,
  resetSkill,
} from '../lib/api';

afterEach(() => {
  vi.unstubAllGlobals();
});

function mockJSON(payload: unknown) {
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => payload });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('drift-safe sync api', () => {
  it('updateSkillSource omits a body when no options are given', async () => {
    const fetchMock = mockJSON({ source: 'src', results: [] });
    await updateSkillSource('my source');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/skills/sources/my%20source/update');
    expect(init.method).toBe('POST');
    expect(init.body).toBeUndefined();
  });

  it('updateSkillSource sends force/skills when provided', async () => {
    const fetchMock = mockJSON({ source: 'src', results: [{ skill: 'a', backup: 'SKILL.md.pre-abc' }] });
    const res = await updateSkillSource('src', { force: true, skills: ['a'] });
    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({ force: true, skills: ['a'] });
    expect(res.results[0].backup).toBe('SKILL.md.pre-abc');
  });

  it('syncAllSources sends a body only when forcing', async () => {
    const fetchMock = mockJSON({ sources: [], syncedSources: 0, updatedSkills: 0, failedSources: 0, pinnedSources: 0 });
    await syncAllSources();
    expect(fetchMock.mock.calls[0][1].body).toBeUndefined();

    await syncAllSources({ force: true });
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ force: true });
  });

  it('fetchSkillDiff GETs the per-skill diff endpoint', async () => {
    const payload = { skill: 'a', local: 'x', upstream: 'y', unifiedDiff: '@@', drifted: true };
    const fetchMock = mockJSON(payload);
    const res = await fetchSkillDiff('src', 'a');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/skills/sources/src/skills/a/diff');
    // GET requests carry no method override body
    expect(init?.method ?? 'GET').toBe('GET');
    expect(res).toEqual(payload);
  });

  it('detachSkill POSTs to the detach endpoint', async () => {
    const fetchMock = mockJSON({ detached: 'a' });
    const res = await detachSkill('src', 'a');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/skills/sources/src/skills/a/detach');
    expect(init.method).toBe('POST');
    expect(res.detached).toBe('a');
  });

  it('resetSkill POSTs to the reset endpoint', async () => {
    const fetchMock = mockJSON({ skill: 'a', imported: 1, backup: 'SKILL.md.pre-abc' });
    const res = await resetSkill('src', 'a');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/skills/sources/src/skills/a/reset');
    expect(init.method).toBe('POST');
    expect(res.backup).toBe('SKILL.md.pre-abc');
  });
});
