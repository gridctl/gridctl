/// <reference types="node" />
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../index.css'), 'utf8');

const steps = ['raised', 'sticky', 'header', 'overlay', 'modal', 'popover', 'toast'];

describe('z-index scale', () => {
  it('declares the seven steps in ascending order', () => {
    const values = steps.map((name) => {
      const match = css.match(new RegExp(`--z-index-${name}:\\s*(\\d+)`));
      expect(match, name).not.toBeNull();
      return Number(match?.[1]);
    });
    const positions = steps.map((name) => css.indexOf(`--z-index-${name}:`));
    expect(values).toEqual([10, 20, 30, 40, 50, 60, 70]);
    expect(positions.every((position, index) => index === 0 || position > positions[index - 1])).toBe(true);
    for (let i = 1; i < values.length; i += 1) {
      expect(values[i]).toBeGreaterThan(values[i - 1]);
    }
  });
});
