import { describe, it, expect } from 'vitest';
import { toolRowLabel, type ToolRowLabelInput } from '../lib/toolRowLabel';
import { policyDetailText } from '../lib/groups';

function input(over: Partial<ToolRowLabelInput> = {}): ToolRowLabelInput {
  return {
    name: 'list_repos',
    isEnabled: true,
    groups: [],
    policyText: '',
    auditState: null,
    lastUsed: '',
    ...over,
  };
}

describe('toolRowLabel', () => {
  it('starts with the name and the enabled state', () => {
    expect(toolRowLabel(input())).toBe('list_repos, enabled');
    expect(toolRowLabel(input({ isEnabled: false }))).toBe('list_repos, disabled');
  });

  it('never appends "details"', () => {
    expect(toolRowLabel(input({ groups: ['core'] }))).not.toMatch(/details/);
  });

  it('names each group', () => {
    expect(toolRowLabel(input({ groups: ['core', 'review'] }))).toBe(
      'list_repos, enabled, group core, group review',
    );
  });

  it('spells out every annotation chip in full words', () => {
    expect(
      toolRowLabel(input({ annotations: { readOnlyHint: true, destructiveHint: false } })),
    ).toBe('list_repos, enabled, read-only, non-destructive');
    expect(
      toolRowLabel(
        input({
          annotations: {
            readOnlyHint: false,
            destructiveHint: true,
            idempotentHint: true,
            openWorldHint: true,
          },
        }),
      ),
    ).toBe('list_repos, enabled, read-write, destructive, idempotent, open world');
    expect(toolRowLabel(input({ annotations: { openWorldHint: false } }))).toBe(
      'list_repos, enabled, closed world',
    );
  });

  it('announces a require_approval verdict as requiring approval', () => {
    const policyText = policyDetailText({ action: 'require_approval', source: 'default', exposed: true }) ?? '';
    const label = toolRowLabel(input({ policyText }));
    expect(label).toContain('requires approval');
    expect(label).toBe(`list_repos, enabled, ${policyText}`);
  });

  it('announces a block verdict without the approval wording', () => {
    const policyText =
      policyDetailText({ action: 'block', source: 'rule', rule_index: 0, rule: 'github__*', exposed: true }) ?? '';
    const label = toolRowLabel(input({ policyText }));
    expect(label).toContain('blocked by policy rule 1: github__*');
    expect(label).not.toContain('requires approval');
  });

  it('includes used and unused audit states with last-used text', () => {
    expect(toolRowLabel(input({ auditState: 'used', lastUsed: '2m ago' }))).toBe(
      'list_repos, enabled, used, last used 2m ago',
    );
    expect(toolRowLabel(input({ auditState: 'unused' }))).toBe('list_repos, enabled, unused');
  });

  it('does not repeat a disabled audit state', () => {
    expect(toolRowLabel(input({ isEnabled: false, auditState: 'disabled', lastUsed: 'x' }))).toBe(
      'list_repos, disabled',
    );
  });

  it('orders the parts like the prompt example', () => {
    expect(
      toolRowLabel(
        input({
          groups: ['core'],
          annotations: { readOnlyHint: true, destructiveHint: false },
          auditState: 'unused',
        }),
      ),
    ).toBe('list_repos, enabled, group core, read-only, non-destructive, unused');
  });
});
