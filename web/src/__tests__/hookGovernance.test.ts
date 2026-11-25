import { describe, expect, it } from 'vitest';
import { hookPinsUrl } from '../lib/hookGovernance';

describe('hookPinsUrl', () => {
  it('links to the hook kind without a name', () => {
    expect(hookPinsUrl()).toBe('/pins?kind=hook');
    expect(hookPinsUrl(null, 'drift')).toBe('/pins?kind=hook');
  });

  it('encodes the name and appends the view', () => {
    expect(hookPinsUrl('block force/push')).toBe('/pins?kind=hook&hook=block%20force%2Fpush');
    expect(hookPinsUrl('a', 'drift')).toBe('/pins?kind=hook&hook=a&view=drift');
    expect(hookPinsUrl('a', 'findings')).toBe('/pins?kind=hook&hook=a&view=findings');
  });
});
