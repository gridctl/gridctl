import { describe, expect, it } from 'vitest';
import type { PolicyToolStatus } from '../lib/api';
import { policyChip, policyDetailText } from '../lib/groups';

interface ChipCase {
  name: string;
  verdict?: PolicyToolStatus;
  label: string | null;
  title: string | null;
  detail: string | null;
}

const cases: ChipCase[] = [
  {
    name: 'allow',
    verdict: { action: 'allow', source: 'rule', rule_index: 1, rule: 'vault__list_paths', exposed: true },
    label: null,
    title: null,
    detail: null,
  },
  {
    name: 'absent verdict',
    label: null,
    title: null,
    detail: null,
  },
  {
    name: 'approval rule',
    verdict: {
      action: 'require_approval',
      source: 'rule',
      rule_index: 0,
      rule: 'vault__*',
      exposed: true,
    },
    label: 'APPROVAL',
    title: 'blocked by policy rule 1: vault__*',
    detail: 'blocked by policy rule 1: vault__* (requires approval, enforced as block)',
  },
  {
    name: 'block_destructive',
    verdict: { action: 'block', source: 'block_destructive', exposed: true },
    label: 'BLOCKED',
    title: 'blocked by policy: block_destructive',
    detail: 'blocked by policy: block_destructive',
  },
  {
    name: 'default block',
    verdict: { action: 'block', source: 'default', exposed: false },
    label: 'BLOCKED',
    title: 'blocked by policy default',
    detail: 'blocked by policy default',
  },
  {
    name: 'rule without a pattern',
    verdict: { action: 'block', source: 'rule', rule_index: 2, exposed: true },
    label: 'BLOCKED',
    title: 'blocked by policy rule 3',
    detail: 'blocked by policy rule 3',
  },
];

describe('policy chip helpers', () => {
  it.each(cases)('$name', ({ verdict, label, title, detail }) => {
    const chip = policyChip(verdict);
    expect(chip?.label ?? null).toBe(label);
    expect(chip?.title ?? null).toBe(title);
    if (chip) {
      expect(chip.tone).toBe('danger');
    }
    expect(policyDetailText(verdict)).toBe(detail);
  });
});
