import { describe, it, expect } from 'vitest';
import {
  consumerCount,
  consumerLabel,
  describeSetInjection,
  describeConsumer,
  groupConsumers,
  isNavigable,
  navigationTarget,
} from '../components/vault/consumerHelpers';
import type { Consumer } from '../lib/api';

const scoped = (target: string, targetKind: Consumer['kind'] = 'mcp-server'): Consumer => ({
  kind: 'secrets-set',
  name: 'dev',
  field: 'secrets.sets',
  target,
  targetKind,
});

const unscoped: Consumer = {
  kind: 'secrets-set',
  name: 'dev',
  field: 'secrets.sets',
};

describe('isNavigable', () => {
  it('accepts explicit server and resource sites', () => {
    expect(isNavigable({ kind: 'mcp-server', name: 'a', field: 'env.X' })).toBe(
      true,
    );
    expect(isNavigable({ kind: 'resource', name: 'r', field: 'env.X' })).toBe(
      true,
    );
  });

  it('rejects stack-level sites', () => {
    expect(isNavigable({ kind: 'gateway', field: 'auth.token' })).toBe(false);
    expect(isNavigable({ kind: 'network', name: 'n', field: 'name' })).toBe(
      false,
    );
  });

  it('accepts a scoped set injection, which names one workload', () => {
    expect(isNavigable(scoped('github'))).toBe(true);
    expect(isNavigable(scoped('pg', 'resource'))).toBe(true);
  });

  it('rejects an unscoped set injection, which reaches everything at once', () => {
    expect(isNavigable(unscoped)).toBe(false);
  });
});

describe('navigationTarget', () => {
  it('resolves an explicit site to its own node', () => {
    expect(
      navigationTarget({ kind: 'mcp-server', name: 'github', field: 'env.X' }),
    ).toEqual({ kind: 'mcp-server', name: 'github' });
  });

  it('resolves a scoped set injection to the workload it injects into', () => {
    expect(navigationTarget(scoped('github'))).toEqual({
      kind: 'mcp-server',
      name: 'github',
    });
    expect(navigationTarget(scoped('pg', 'resource'))).toEqual({
      kind: 'resource',
      name: 'pg',
    });
  });

  it('returns null when there is no single node to open', () => {
    expect(navigationTarget(unscoped)).toBeNull();
    expect(navigationTarget({ kind: 'gateway', field: 'auth.token' })).toBeNull();
  });
});

describe('consumerLabel', () => {
  it('names the workload for a scoped injection', () => {
    expect(consumerLabel(scoped('github'))).toBe('set: dev · into github');
  });

  it('keeps the fan-out wording for an unscoped injection', () => {
    expect(consumerLabel(unscoped)).toBe('set: dev · injected into server env');
  });

  it('keeps the raw YAML path for explicit references', () => {
    expect(
      consumerLabel({ kind: 'mcp-server', name: 'github', field: 'command[4]' }),
    ).toBe('github · command[4]');
  });
});

describe('describeConsumer', () => {
  it('says "every" only for an unscoped injection', () => {
    expect(describeConsumer(unscoped)).toContain('every MCP server');
    expect(describeConsumer(scoped('github'))).not.toContain('every');
  });

  it('names the scoped workload and its kind', () => {
    expect(describeConsumer(scoped('github'))).toContain('server "github"');
    expect(describeConsumer(scoped('pg', 'resource'))).toContain(
      'resource "pg"',
    );
  });
});

describe('groupConsumers', () => {
  it('leaves explicit references alone', () => {
    const c: Consumer = { kind: 'mcp-server', name: 'a', field: 'env.X' };
    expect(groupConsumers([c])).toEqual([{ kind: 'one', consumer: c }]);
  });

  it('leaves an unscoped injection alone', () => {
    expect(groupConsumers([unscoped])).toEqual([
      { kind: 'one', consumer: unscoped },
    ]);
  });

  it('folds several targets of one scoped set into a single group', () => {
    const entries = groupConsumers([scoped('a'), scoped('b'), scoped('c')]);
    expect(entries).toHaveLength(1);
    expect(entries[0].kind).toBe('setGroup');
    if (entries[0].kind === 'setGroup') {
      expect(entries[0].setName).toBe('dev');
      expect(entries[0].consumers).toHaveLength(3);
    }
  });

  it('keeps a one-workload scope as a plain row (nothing to expand)', () => {
    const entries = groupConsumers([scoped('a')]);
    expect(entries).toHaveLength(1);
    expect(entries[0].kind).toBe('one');
  });

  it('groups per set name, not across sets', () => {
    const other: Consumer = { ...scoped('z'), name: 'prod' };
    const entries = groupConsumers([scoped('a'), scoped('b'), other]);
    expect(entries).toHaveLength(2);
    expect(entries[0].kind).toBe('setGroup');
    expect(entries[1].kind).toBe('one');
  });

  it('preserves explicit references alongside a scoped group', () => {
    const explicit: Consumer = {
      kind: 'mcp-server',
      name: 'a',
      field: 'env.X',
    };
    const entries = groupConsumers([explicit, scoped('a'), scoped('b')]);
    expect(entries).toHaveLength(2);
    expect(entries[0]).toEqual({ kind: 'one', consumer: explicit });
    expect(entries[1].kind).toBe('setGroup');
  });
});

describe('consumerCount', () => {
  it('counts a scoped set once, not once per workload it reaches', () => {
    // Tightening a set's scope must never make a variable look more heavily
    // used than leaving it to fan out.
    const scopedToFive = ['a', 'b', 'c', 'd', 'e'].map((n) => scoped(n));
    expect(consumerCount(scopedToFive)).toBe(1);
    expect(consumerCount([unscoped])).toBe(1);
  });

  it('still counts explicit sites individually', () => {
    expect(
      consumerCount([
        { kind: 'mcp-server', name: 'a', field: 'env.X' },
        { kind: 'resource', name: 'r', field: 'env.X' },
      ]),
    ).toBe(2);
  });

  it('is zero for no consumers and tolerates undefined', () => {
    expect(consumerCount([])).toBe(0);
    expect(consumerCount(undefined)).toBe(0);
  });
});

describe('describeSetInjection', () => {
  it('is null when no set injects the variable', () => {
    expect(describeSetInjection([])).toBeNull();
    expect(
      describeSetInjection([{ kind: 'mcp-server', name: 'a', field: 'env.X' }]),
    ).toBeNull();
  });

  it('claims fan-out only for an unscoped set', () => {
    expect(describeSetInjection([unscoped])).toContain('every server env');
  });

  it('names the single workload a scoped set reaches', () => {
    const phrase = describeSetInjection([scoped('github')]);
    expect(phrase).toContain('github');
    expect(phrase).not.toContain('every');
  });

  it('counts workloads when a scoped set reaches several', () => {
    const phrase = describeSetInjection([scoped('a'), scoped('b')]);
    expect(phrase).toContain('2 workloads');
    expect(phrase).not.toContain('every');
  });

  it('carries no preposition, since the caller appends one', () => {
    // The delete dialog renders "<phrase> via secrets.sets."
    for (const c of [[unscoped], [scoped('a')], [scoped('a'), scoped('b')]]) {
      expect(describeSetInjection(c)?.endsWith('via')).toBe(false);
    }
  });
});
