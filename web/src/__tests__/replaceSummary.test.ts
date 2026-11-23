import { describe, it, expect } from 'vitest';
import { describeReplace } from '../components/vault/replaceSummary';

const existing = { key: 'API_KEY', type: 'json' as const, is_secret: true, set: 'prod' };

describe('describeReplace', () => {
  it('always states the value loss and kept metadata', () => {
    expect(describeReplace(existing, { type: 'json', isSecret: true })).toEqual([
      'The current value is replaced and cannot be recovered.',
      'The description and other metadata are kept.',
    ]);
  });

  it('lists only the fields that differ', () => {
    expect(
      describeReplace(existing, { type: 'string', isSecret: false, set: 'staging' }),
    ).toEqual([
      'The current value is replaced and cannot be recovered.',
      'Type: json → string',
      'Visibility: secret → plaintext',
      'Set: prod → staging',
      'The description and other metadata are kept.',
    ]);
  });

  it('omits the set line when no set is chosen or it matches', () => {
    expect(describeReplace(existing, { type: 'json', isSecret: true, set: 'prod' })).not.toContain(
      'Set: prod → prod',
    );
    expect(
      describeReplace({ ...existing, set: undefined }, { type: 'json', isSecret: true, set: 'dev' }),
    ).toContain('Set: no set → dev');
  });
});
