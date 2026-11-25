import { describe, expect, it } from 'vitest';
import {
  affectedContexts,
  affectedServers,
  affectedSkills,
  driftAlert,
  findingsAlert,
  newlyAffected,
  type NewNames,
} from '../lib/pinAlerts';
import type { ContextPin, ServerPins, SkillPin } from '../lib/api';

function names(partial: Partial<NewNames>): NewNames {
  return { server: [], skill: [], fragment: [], ...partial };
}

describe('newlyAffected', () => {
  it('returns only names absent from the previous set, sorted', () => {
    expect(newlyAffected(new Set(['b']), new Set(['c', 'b', 'a']))).toEqual(['a', 'c']);
    expect(newlyAffected(new Set(['a']), new Set(['a']))).toEqual([]);
    expect(newlyAffected(new Set(['a', 'b']), new Set())).toEqual([]);
  });
});

describe('driftAlert and findingsAlert', () => {
  it('names a single new server', () => {
    expect(driftAlert(names({ server: ['github'] }))).toEqual({
      message: 'Pin drift detected on server github',
      target: '/pins?server=github&view=drift',
    });
  });

  it('names a single new skill', () => {
    expect(findingsAlert(names({ skill: ['deploy'] }))).toEqual({
      message: 'New findings on skill deploy',
      target: '/pins?kind=skill&skill=deploy&view=findings',
    });
  });

  it('names a single new fragment', () => {
    expect(driftAlert(names({ fragment: ['00-default'] }))).toEqual({
      message: 'Pin drift detected on fragment 00-default',
      target: '/pins?kind=context&fragment=00-default&view=drift',
    });
  });

  it('counts two servers and links to the first', () => {
    expect(driftAlert(names({ server: ['a b', 'z'] }))).toEqual({
      message: 'Pin drift detected on 2 servers',
      target: '/pins?server=a%20b&view=drift',
    });
  });

  it('joins two kinds with "and" and prefers a server target', () => {
    expect(findingsAlert(names({ server: ['x'], skill: ['a', 'b'] }))).toEqual({
      message: 'New findings on 1 server and 2 skills',
      target: '/pins?server=x&view=findings',
    });
  });

  it('joins three kinds with an Oxford comma', () => {
    expect(driftAlert(names({ server: ['s'], skill: ['a', 'b'], fragment: ['f'] }))?.message).toBe(
      'Pin drift detected on 1 server, 2 skills, and 1 fragment',
    );
  });

  it('links to the first skill when no server is new, else the first fragment', () => {
    expect(driftAlert(names({ skill: ['k'], fragment: ['f'] }))?.target).toBe(
      '/pins?kind=skill&skill=k&view=drift',
    );
    expect(findingsAlert(names({ fragment: ['f', 'g'] }))?.target).toBe(
      '/pins?kind=context&fragment=f&view=findings',
    );
  });

  it('returns null when nothing is new', () => {
    expect(driftAlert(names({}))).toBeNull();
    expect(findingsAlert(names({}))).toBeNull();
  });
});

describe('affected name extraction', () => {
  it('uses drift status and warn-or-critical findings', () => {
    const servers = {
      a: { status: 'drift', tools: {} },
      b: { status: 'pinned', tools: { t: { findings: [{ severity: 'warn' }] } } },
      c: { status: 'pinned', tools: { t: { findings: [{ severity: 'info' }] } } },
    } as unknown as Record<string, ServerPins>;
    const s = affectedServers(servers);
    expect([...s.drift]).toEqual(['a']);
    expect([...s.findings]).toEqual(['b']);

    const skills = {
      k: { status: 'drift', findings: [{ severity: 'critical' }] },
    } as unknown as Record<string, SkillPin>;
    const k = affectedSkills(skills);
    expect([...k.drift]).toEqual(['k']);
    expect([...k.findings]).toEqual(['k']);

    const contexts = {
      f: { status: 'pinned', findings: [{ severity: 'warn' }] },
    } as unknown as Record<string, ContextPin>;
    const f = affectedContexts(contexts);
    expect([...f.drift]).toEqual([]);
    expect([...f.findings]).toEqual(['f']);
  });
});
