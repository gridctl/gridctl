import { describe, it, expect } from 'vitest';
import { DESCRIPTION_MAX_CHARS, DESCRIPTION_MAX_LINES, clampDescription } from '../lib/toolDescription';

// Lengths and hiddenChars are UTF-16 code units (String.prototype.length).
describe('clampDescription (UTF-16 code units)', () => {
  it('exports the limits', () => {
    expect(DESCRIPTION_MAX_CHARS).toBe(480);
    expect(DESCRIPTION_MAX_LINES).toBe(8);
  });

  it('leaves a short two-line description alone', () => {
    const text = `${'a'.repeat(49)}\n${'b'.repeat(50)}`;
    expect(text.length).toBe(100);
    expect(clampDescription(text)).toEqual({ visible: text, hiddenChars: 0 });
  });

  it('leaves text at exactly the character limit alone', () => {
    const text = 'x'.repeat(DESCRIPTION_MAX_CHARS);
    expect(clampDescription(text)).toEqual({ visible: text, hiddenChars: 0 });
  });

  it('leaves exactly eight lines alone', () => {
    const text = Array.from({ length: 8 }, (_, i) => `line ${i}`).join('\n');
    expect(clampDescription(text).hiddenChars).toBe(0);
  });

  it('leaves eight lines with a trailing newline alone', () => {
    const text = `${Array.from({ length: 8 }, (_, i) => `line ${i}`).join('\n')}\n`;
    expect(clampDescription(text)).toEqual({ visible: text, hiddenChars: 0 });
  });

  it('leaves text at the character limit plus trailing whitespace alone', () => {
    const text = `${'x'.repeat(DESCRIPTION_MAX_CHARS)}\n`;
    expect(clampDescription(text)).toEqual({ visible: text, hiddenChars: 0 });
  });

  it('clamps 12 short lines at eight lines', () => {
    const lines = Array.from({ length: 12 }, (_, i) => `line ${i}`);
    const text = lines.join('\n');
    const { visible, hiddenChars } = clampDescription(text);
    expect(visible).toBe(lines.slice(0, 8).join('\n'));
    expect(visible.split('\n')).toHaveLength(8);
    expect(hiddenChars).toBe(text.length - visible.length);
  });

  it('clamps a 2,000-character description to 480 or fewer', () => {
    const text = 'word '.repeat(400);
    expect(text.length).toBe(2000);
    const { visible, hiddenChars } = clampDescription(text);
    expect(visible.length).toBeLessThanOrEqual(DESCRIPTION_MAX_CHARS);
    expect(text.startsWith(visible)).toBe(true);
    expect(hiddenChars).toBe(text.length - visible.length);
  });

  it('backs up to whitespace within the last 40 characters', () => {
    const text = `${'a'.repeat(470)} ${'b'.repeat(100)}`;
    const { visible, hiddenChars } = clampDescription(text);
    expect(visible).toBe('a'.repeat(470));
    expect(hiddenChars).toBe(text.length - 470);
  });

  it('cuts mid-word when no whitespace is near the cut', () => {
    const text = `${'a'.repeat(400)} ${'b'.repeat(200)}`;
    const { visible } = clampDescription(text);
    expect(visible.length).toBe(DESCRIPTION_MAX_CHARS);
  });

  it('never splits a surrogate pair', () => {
    // 479 units, then an emoji (two units) straddling the 480 cut.
    const text = `${'a'.repeat(479)}😀${'b'.repeat(50)}`;
    const { visible, hiddenChars } = clampDescription(text);
    expect(visible).toBe('a'.repeat(479));
    expect(hiddenChars).toBe(text.length - 479);
    const last = visible.charCodeAt(visible.length - 1);
    expect(last >= 0xd800 && last <= 0xdbff).toBe(false);
  });
});
