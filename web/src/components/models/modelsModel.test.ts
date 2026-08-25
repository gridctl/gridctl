import { describe, expect, it } from 'vitest';
import type { ModelsSyncResult, ModelsTargetStatus } from '../../types';
import {
  ADOPTABLE_TARGETS,
  MODELS_ATTENTION,
  describeModelsSyncResults,
  driftedTargets,
  hasAdoptableDrift,
  modelsTargetLabel,
} from './modelsModel';

function result(overrides: Partial<ModelsSyncResult> & { target: string; action: string }): ModelsSyncResult {
  return { client: 'litellm', path: '/x', ...overrides };
}

describe('describeModelsSyncResults', () => {
  it('classifies errors above skips above updates', () => {
    const { kind } = describeModelsSyncResults(
      [
        result({ target: 'litellm-fragment', action: 'error', error: 'disk full' }),
        result({ target: 'opencode', action: 'skipped-drift' }),
      ],
      false,
    );
    expect(kind).toBe('error');
  });

  it('reports skips as warnings, never success', () => {
    // A skip carries no error field; an error-keyed classification would
    // announce success while nothing was written.
    const { kind, message } = describeModelsSyncResults(
      [
        result({ target: 'litellm-fragment', action: 'skipped-drift' }),
        result({ target: 'opencode', action: 'updated' }),
      ],
      false,
    );
    expect(kind).toBe('warning');
    expect(message).toContain('LiteLLM router fragment');
  });

  it('names the restart consequence when the fragment was written', () => {
    const { kind, message } = describeModelsSyncResults(
      [result({ target: 'litellm-fragment', action: 'updated' })],
      false,
    );
    expect(kind).toBe('success');
    expect(message).toContain('restart LiteLLM');
  });

  it('summarizes dry runs by would-change count', () => {
    expect(
      describeModelsSyncResults([result({ target: 'opencode', action: 'unchanged' })], true).message,
    ).toContain('Nothing to change');
    expect(
      describeModelsSyncResults([result({ target: 'opencode', action: 'would-update' })], true)
        .message,
    ).toContain('1 target would change');
  });
});

describe('drift helpers', () => {
  const rows: ModelsTargetStatus[] = [
    { target: 'litellm-fragment', client: 'litellm', state: 'in-sync', restart_pending: true },
    { target: 'litellm-include', client: 'litellm', state: 'drifted' },
    { target: 'opencode', client: 'opencode', state: 'stale' },
  ];

  it('collects drifted rows only', () => {
    expect(driftedTargets(rows).map((t) => t.target)).toEqual(['litellm-include']);
  });

  it('include-only drift is not adoptable', () => {
    // Adopt records on-disk bytes for the fragment and OpenCode only; a
    // removed include line has nothing to record and resolves via force
    // sync.
    expect(hasAdoptableDrift(rows)).toBe(false);
    expect(
      hasAdoptableDrift([{ target: 'opencode', client: 'opencode', state: 'drifted' }]),
    ).toBe(true);
    expect(ADOPTABLE_TARGETS.has('litellm-include')).toBe(false);
  });

  it('attention mirrors the engine: restart-pending and never-synced excluded', () => {
    expect(MODELS_ATTENTION.has('stale')).toBe(true);
    expect(MODELS_ATTENTION.has('never-synced')).toBe(false);
  });
});

describe('modelsTargetLabel', () => {
  it('labels the three targets and passes unknowns through', () => {
    expect(modelsTargetLabel('litellm-fragment')).toBe('LiteLLM router fragment');
    expect(modelsTargetLabel('opencode')).toBe('OpenCode provider');
    expect(modelsTargetLabel('future-target')).toBe('future-target');
  });
});
