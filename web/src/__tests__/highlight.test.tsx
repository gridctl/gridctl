import { describe, it, expect } from 'vitest';
import { excerptAround } from '../lib/highlight';

describe('excerptAround', () => {
  const late = `${'x'.repeat(100)} webhook trailing`;

  it('returns the text unchanged when the match is early', () => {
    const text = 'Send a webhook to the target';
    expect(excerptAround(text, 'webhook')).toBe(text);
  });

  it('returns the text unchanged when there is no match', () => {
    expect(excerptAround(late, 'absent')).toBe(late);
  });

  it('returns the text unchanged for a blank query', () => {
    expect(excerptAround(late, '   ')).toBe(late);
  });

  it('starts with an ellipsis and keeps a late match', () => {
    const out = excerptAround(late, 'WEBHOOK');
    expect(out.startsWith('…')).toBe(true);
    expect(out).toContain('webhook');
    expect(out.length).toBeLessThan(late.length);
    expect(out.indexOf('webhook')).toBe(41);
  });

  it('skips text whose lowercase length differs', () => {
    const text = `\u0130${'x'.repeat(100)} webhook`;
    expect(excerptAround(text, 'webhook')).toBe(text);
  });
});
