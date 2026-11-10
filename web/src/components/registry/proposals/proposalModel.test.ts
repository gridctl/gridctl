import { describe, expect, it } from 'vitest';
import type { Proposal } from '../../../types';
import {
  approveBlock,
  classifyUnifiedDiff,
  countPending,
  filterProposals,
  pathExplanation,
  provenanceLabel,
  sortProposals,
} from './proposalModel';

function proposal(overrides: Partial<Proposal> = {}): Proposal {
  return {
    id: 'p-000000000001',
    version: '1',
    kind: 'skill',
    target: 'incident-triage',
    base_hash: 'h2:abc',
    base_current: true,
    diff: '',
    classification: 'knowledge',
    prevalence: { sessions: 1, runs: 0 },
    evidence: [],
    created_at: '2026-11-09T14:02:00Z',
    status: 'pending',
    path: 'apply',
    provenance: { kind: 'local' },
    findings: [],
    verdict: null,
    files: ['SKILL.md'],
    pin_status: 'pinned',
    ...overrides,
  };
}

const finding = { code: 'imperative', severity: 'warn' as const, confidence: 'high' as const, field: 'body', message: 'm' };

describe('sortProposals', () => {
  it('puts pending first, then newest first, with the id as a tie-break', () => {
    const list = [
      proposal({ id: 'p-a', status: 'approved', created_at: '2026-11-09T12:00:00Z' }),
      proposal({ id: 'p-b', status: 'pending', created_at: '2026-11-08T12:00:00Z' }),
      proposal({ id: 'p-c', status: 'rejected', created_at: '2026-11-07T12:00:00Z' }),
      proposal({ id: 'p-d', status: 'pending', created_at: '2026-11-09T09:00:00Z' }),
      proposal({ id: 'p-e', status: 'pending', created_at: '2026-11-09T09:00:00Z' }),
    ];
    expect(sortProposals(list).map((p) => p.id)).toEqual(['p-d', 'p-e', 'p-b', 'p-a', 'p-c']);
    expect(list[0].id).toBe('p-a');
  });

  it('filters by status and counts pending', () => {
    const list = [proposal({ id: 'p-1' }), proposal({ id: 'p-2', status: 'approved' })];
    expect(filterProposals(list, 'pending').map((p) => p.id)).toEqual(['p-1']);
    expect(filterProposals(list, 'all')).toHaveLength(2);
    expect(countPending(list)).toBe(1);
    expect(countPending(null)).toBe(0);
  });
});

describe('approveBlock', () => {
  it('allows a current, pinned, finding-free proposal', () => {
    expect(approveBlock(proposal(), '')).toBeNull();
  });

  it('blocks a stale base', () => {
    expect(approveBlock(proposal({ base_current: false }), 'why')).toMatch(/stale base/);
  });

  it('blocks pin drift on the apply path only', () => {
    expect(approveBlock(proposal({ pin_status: 'drift' }), 'why')).toMatch(/Pins workspace/);
    expect(approveBlock(proposal({ pin_status: 'drift', path: 'record' }), 'why')).toBeNull();
  });

  it('requires a reason when findings exist', () => {
    const p = proposal({ findings: [finding] });
    expect(approveBlock(p, '   ')).toMatch(/requires a reason/);
    expect(approveBlock(p, 'reviewed')).toBeNull();
  });

  it('blocks a proposal that is not pending', () => {
    expect(approveBlock(proposal({ status: 'rejected' }), 'why')).toMatch(/already rejected/);
  });
});

describe('pathExplanation and provenanceLabel', () => {
  it('explains the apply path', () => {
    expect(pathExplanation(proposal())).toMatch(/applies the diff/);
    expect(provenanceLabel(proposal())).toBe('local');
  });

  it('names the arrival command on the record path', () => {
    const pack = proposal({ path: 'record', provenance: { kind: 'pack', pack: 'portable-pack' } });
    expect(pathExplanation(pack)).toMatch(/gridctl pack add/);
    expect(pathExplanation(pack)).toMatch(/proposal:p-000000000001/);
    expect(provenanceLabel(pack)).toBe('pack portable-pack');

    const imported = proposal({ path: 'record', provenance: { kind: 'local-import', source_dir: '/src/triage' } });
    expect(pathExplanation(imported)).toMatch(/Edit \/src\/triage and run 'gridctl skill update'/);

    const git = proposal({ path: 'record', provenance: { kind: 'git' } });
    expect(pathExplanation(git)).toMatch(/'gridctl skill update'/);

    const hook = proposal({ kind: 'hook', path: 'record', provenance: { kind: 'git' } });
    expect(pathExplanation(hook)).toMatch(/gridctl pack add/);
  });
});

describe('classifyUnifiedDiff', () => {
  it('classifies headers, hunks, added, removed, context, and notes', () => {
    const diff = [
      '--- a/SKILL.md',
      '+++ b/SKILL.md',
      '@@ -1,3 +1,3 @@',
      ' keep',
      '-old',
      '--- looks like a header',
      '+new',
      '+++ looks like a header too',
      '\\ No newline at end of file',
      '--- /dev/null',
      '+++ b/notes.md',
      '@@ -0,0 +1 @@',
      '+added',
      '',
    ].join('\n');
    expect(classifyUnifiedDiff(diff).map((l) => l.kind)).toEqual([
      'header',
      'header',
      'hunk',
      'context',
      'removed',
      'removed',
      'added',
      'added',
      'note',
      'header',
      'header',
      'hunk',
      'added',
    ]);
  });

  it('normalizes CRLF line endings', () => {
    expect(classifyUnifiedDiff('@@ -1 +1 @@\r\n-a\r\n+b\r\n').map((l) => l.text)).toEqual(['@@ -1 +1 @@', '-a', '+b']);
  });
});
