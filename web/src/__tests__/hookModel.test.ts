import { describe, it, expect } from 'vitest';
import { describeHookUnsync, joinProse, planHookUnsync } from '../components/registry/hooks/hookModel';
import type { HookProjectionStatus, HookUnsupportedClient, HookUnsyncResult } from '../types';

function row(client: string, action: string, extra: Partial<HookUnsyncResult> = {}): HookUnsyncResult {
  return { hook: 'block-force-push', client, target: `/tmp/${client}`, action, ...extra };
}

function status(client: string, state: HookProjectionStatus['state']): HookProjectionStatus {
  return {
    hook: 'block-force-push',
    client,
    channel: 'merge-key',
    render: 'identity',
    state,
    target: `/tmp/${client}`,
  };
}

const cursor: HookUnsupportedClient = {
  slug: 'cursor',
  name: 'Cursor',
  location: 'reads ~/.claude/settings.json hooks',
  covered_by: 'claude-code',
  detected: true,
};
const windsurf: HookUnsupportedClient = {
  slug: 'windsurf',
  name: 'Windsurf (Devin Local)',
  location: 'reads ~/.claude/settings.json hooks',
  covered_by: 'claude-code',
  detected: false,
};

describe('describeHookUnsync', () => {
  it('reports nothing to unsync for an empty result', () => {
    expect(describeHookUnsync([])).toEqual({ kind: 'success', message: 'Nothing to unsync' });
  });

  it('counts every removed client', () => {
    expect(describeHookUnsync([row('claude-code', 'removed'), row('opencode', 'removed')])).toEqual({
      kind: 'success',
      message: 'Unsynced block-force-push from 2 clients',
    });
    expect(describeHookUnsync([row('claude-code', 'removed')])).toEqual({
      kind: 'success',
      message: 'Unsynced block-force-push from 1 client',
    });
  });

  it('counts already-gone as success', () => {
    expect(describeHookUnsync([row('claude-code', 'removed'), row('opencode', 'already-gone')])).toEqual({
      kind: 'success',
      message: 'Unsynced block-force-push from 2 clients',
    });
  });

  it('names a drift skip with its detail', () => {
    expect(describeHookUnsync([row('opencode', 'skipped-drift', { detail: 'plugin file was edited' })])).toEqual({
      kind: 'error',
      message: 'block-force-push → OpenCode: plugin file was edited',
    });
  });

  it('names an error with only the error field set', () => {
    expect(describeHookUnsync([row('claude-code', 'error', { error: 'permission denied' })])).toEqual({
      kind: 'error',
      message: 'block-force-push → Claude Code: permission denied',
    });
  });

  it('falls back to the action when no reason is given', () => {
    expect(describeHookUnsync([row('copilot', 'skipped-foreign')]).message).toBe(
      'block-force-push → GitHub Copilot CLI: skipped-foreign',
    );
  });

  it('appends the other removed clients in singular and plural', () => {
    expect(
      describeHookUnsync([row('claude-code', 'removed'), row('opencode', 'skipped-drift', { detail: 'edited' })]),
    ).toEqual({ kind: 'error', message: 'block-force-push → OpenCode: edited; removed from 1 other client' });
    expect(
      describeHookUnsync([
        row('claude-code', 'removed'),
        row('copilot', 'removed'),
        row('opencode', 'error', { error: 'busy' }),
      ]),
    ).toEqual({ kind: 'error', message: 'block-force-push → OpenCode: busy; removed from 2 other clients' });
  });
});

describe('joinProse', () => {
  it('joins with an Oxford comma', () => {
    expect(joinProse([])).toBe('');
    expect(joinProse(['A'])).toBe('A');
    expect(joinProse(['A', 'B'])).toBe('A and B');
    expect(joinProse(['A', 'B', 'C'])).toBe('A, B, and C');
  });
});

describe('planHookUnsync', () => {
  it('splits removable and drifted clients in row order without duplicates', () => {
    const plan = planHookUnsync(
      [status('opencode', 'in-sync'), status('copilot', 'drifted'), status('claude-code', 'stale'), status('opencode', 'in-sync')],
      [],
    );
    expect(plan.removable).toEqual(['opencode', 'claude-code']);
    expect(plan.kept).toEqual(['copilot']);
    expect(plan.covered).toEqual([]);
  });

  it('names only detected clients covered by a removable projection', () => {
    const plan = planHookUnsync([status('claude-code', 'in-sync')], [cursor, windsurf]);
    expect(plan.covered).toEqual([{ names: ['Cursor'], by: 'claude-code' }]);
  });

  it('names no covered client when the covering projection is kept or absent', () => {
    expect(planHookUnsync([status('claude-code', 'drifted')], [cursor]).covered).toEqual([]);
    expect(planHookUnsync([status('opencode', 'in-sync')], [cursor]).covered).toEqual([]);
  });
});
