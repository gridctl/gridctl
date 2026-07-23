import { describe, it, expect } from 'vitest';
import { horizontalOverflow } from '../lib/graph/popoverPlacement';

const rect = (left: number, width: number) => ({ right: left + width, width });

describe('horizontalOverflow', () => {
  it('reports no overrun when the card fits with room to spare', () => {
    expect(horizontalOverflow(rect(100, 288), rect(0, 1200))).toBe(0);
  });

  it('reports the distance past the container right edge plus margin', () => {
    // Card right edge at 2000 against a 1900px container: 100 past plus 8.
    expect(horizontalOverflow(rect(1700, 300), rect(0, 1900))).toBe(108);
  });

  it('reports the margin shortfall when the card ends inside the margin', () => {
    // Right edge at 1898 with an 8px margin against a 1900px container.
    expect(horizontalOverflow(rect(1610, 288), rect(0, 1900), 8)).toBe(6);
  });

  it('reports no overrun when the card ends exactly at the margin', () => {
    expect(horizontalOverflow(rect(1604, 288), rect(0, 1900), 8)).toBe(0);
  });

  it('ignores an unmeasured zero-size card rect (jsdom default)', () => {
    expect(horizontalOverflow(rect(0, 0), rect(0, 1200))).toBe(0);
  });

  it('ignores an unmeasured zero-size container rect', () => {
    expect(horizontalOverflow(rect(500, 288), rect(0, 0))).toBe(0);
  });
});
