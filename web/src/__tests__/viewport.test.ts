import { describe, it, expect } from 'vitest';
import { isRectVisible, revealViewport, unionRect } from '../lib/graph/viewport';

const size = { width: 1000, height: 600 };
const origin = { x: 0, y: 0, zoom: 1 };

describe('isRectVisible', () => {
  it('is true for a rect fully inside the canvas', () => {
    expect(isRectVisible({ x: 100, y: 100, width: 200, height: 60 }, origin, size)).toBe(true);
  });

  it('is false for a rect crossing an edge', () => {
    expect(isRectVisible({ x: 900, y: 100, width: 200, height: 60 }, origin, size)).toBe(false);
    expect(isRectVisible({ x: -10, y: 100, width: 200, height: 60 }, origin, size)).toBe(false);
  });

  it('honors the margin', () => {
    const rect = { x: 5, y: 100, width: 200, height: 60 };
    expect(isRectVisible(rect, origin, size)).toBe(true);
    expect(isRectVisible(rect, origin, size, 10)).toBe(false);
  });

  it('is false for a rect larger than the canvas', () => {
    expect(isRectVisible({ x: 0, y: 0, width: 1200, height: 60 }, origin, size)).toBe(false);
  });

  it('applies zoom and translation', () => {
    // Screen left = 300 * 2 - 200 = 400, right = 800; at x = 450 the right edge is 1100.
    const vp = { x: -200, y: 0, zoom: 2 };
    expect(isRectVisible({ x: 300, y: 50, width: 200, height: 60 }, vp, size)).toBe(true);
    expect(isRectVisible({ x: 450, y: 50, width: 200, height: 60 }, vp, size)).toBe(false);
  });
});

describe('revealViewport', () => {
  it('returns null for a visible rect', () => {
    expect(revealViewport({ x: 100, y: 100, width: 200, height: 60 }, origin, size)).toBeNull();
  });

  it('shifts left by the overrun plus margin for a rect past the right edge', () => {
    const next = revealViewport({ x: 900, y: 100, width: 200, height: 60 }, origin, size, 24);
    expect(next).toEqual({ x: -(1100 - 1000) - 24, y: 0, zoom: 1 });
  });

  it('shifts right and down for a rect past the top-left', () => {
    const next = revealViewport({ x: -50, y: -30, width: 200, height: 60 }, origin, size, 24);
    expect(next).toEqual({ x: 74, y: 54, zoom: 1 });
  });

  it('returns null when the rect cannot fit at this zoom', () => {
    expect(revealViewport({ x: 0, y: 0, width: 1200, height: 60 }, origin, size)).toBeNull();
    expect(revealViewport({ x: 0, y: 0, width: 600, height: 60 }, { x: 0, y: 0, zoom: 2 }, size)).toBeNull();
  });

  it('handles a zoom other than 1 and keeps the zoom', () => {
    // At zoom 0.5 a rect at flow 1200..1400 is screen 600..700: visible.
    const vp = { x: 0, y: 0, zoom: 0.5 };
    expect(revealViewport({ x: 1200, y: 100, width: 200, height: 60 }, vp, size, 20)).toBeNull();
    // Flow 2400..2600 is screen 1200..1300, so shift by 1000 - 20 - 1300.
    const far = revealViewport({ x: 2400, y: 100, width: 200, height: 60 }, vp, size, 20);
    expect(far).toEqual({ x: 1000 - 20 - 1300, y: 0, zoom: 0.5 });
  });

  it('shrinks the margin when the rect barely fits', () => {
    // 990 px wide leaves 5 px either side.
    const next = revealViewport({ x: 100, y: 100, width: 990, height: 60 }, origin, size, 24);
    expect(next).toEqual({ x: -95, y: 0, zoom: 1 });
  });
});

describe('unionRect', () => {
  it('returns null for no rects', () => {
    expect(unionRect([])).toBeNull();
  });

  it('bounds every rect', () => {
    expect(
      unionRect([
        { x: 0, y: 10, width: 100, height: 50 },
        { x: 200, y: -20, width: 50, height: 20 },
      ]),
    ).toEqual({ x: 0, y: -20, width: 250, height: 80 });
  });
});
