import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

type RGB = [number, number, number];
type Theme = 'dark' | 'light';

const srcRoot = join(dirname(fileURLToPath(import.meta.url)), '..');

function extractBlock(css: string, start: RegExp): string {
  const match = start.exec(css);
  if (!match || match.index === undefined) {
    throw new Error(`missing block ${start}`);
  }
  const open = css.indexOf('{', match.index);
  let depth = 0;
  for (let i = open; i < css.length; i++) {
    if (css[i] === '{') depth += 1;
    else if (css[i] === '}') {
      depth -= 1;
      if (depth === 0) return css.slice(match.index, i + 1);
    }
  }
  throw new Error(`unclosed block ${start}`);
}

function declarations(block: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const match of block.matchAll(/--color-([a-z0-9-]+)\s*:\s*([^;]+);/g)) {
    out.set(match[1], match[2].trim());
  }
  return out;
}

function parseHex(value: string): RGB {
  const match = /^#([0-9a-f]{6})$/i.exec(value);
  if (!match) throw new Error(`not a six-digit hex color: ${value}`);
  const n = parseInt(match[1], 16);
  return [(n >> 16) & 0xff, (n >> 8) & 0xff, n & 0xff];
}

function linearize(channel: number): number {
  const c = channel / 255;
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

function luminance([r, g, b]: RGB): number {
  return 0.2126 * linearize(r) + 0.7152 * linearize(g) + 0.0722 * linearize(b);
}

function contrast(a: RGB, b: RGB): number {
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

// A Tailwind /NN modifier keeps the color and lowers alpha; the browser
// composites that over the surface per channel in sRGB.
function tint(fg: RGB, bg: RGB, alpha: number): RGB {
  return [0, 1, 2].map((i) => fg[i] * alpha + bg[i] * (1 - alpha)) as RGB;
}

const css = readFileSync(join(srcRoot, 'index.css'), 'utf8');
const dark = declarations(extractBlock(css, /@theme\b/));
const light = declarations(extractBlock(css, /:root\[data-theme='light'\]/));

function token(theme: Theme, name: string): RGB {
  const value = theme === 'light' ? (light.get(name) ?? dark.get(name)) : dark.get(name);
  if (value === undefined) throw new Error(`missing token --color-${name} (${theme})`);
  return parseHex(value);
}

const WHITE: RGB = [255, 255, 255];
const THEMES: Theme[] = ['dark', 'light'];
const ALL_SURFACES = ['background', 'surface', 'surface-elevated', 'surface-highlight'];
const BASE_SURFACES = ['background', 'surface', 'surface-elevated'];

function cases(surfaces: string[], fgs: string[]): [Theme, string, string][] {
  return THEMES.flatMap((theme) =>
    fgs.flatMap((fg) => surfaces.map((surface): [Theme, string, string] => [theme, fg, surface])),
  );
}

function expectRatio(ratio: number, min: number, label: string) {
  expect(ratio, `${label}: ${ratio.toFixed(2)}:1, needs ${min}:1`).toBeGreaterThanOrEqual(min);
}

describe('contrast math', () => {
  it('matches the WCAG reference points', () => {
    expect(contrast([0, 0, 0], WHITE)).toBeCloseTo(21, 5);
    expect(contrast(parseHex('#777777'), WHITE)).toBeLessThan(4.5);
    expect(contrast(parseHex('#767676'), WHITE)).toBeGreaterThanOrEqual(4.5);
  });

  it('refuses a token that is missing or not a hex color', () => {
    expect(() => token('dark', 'no-such-token')).toThrow(/missing token/);
    expect(() => token('dark', 'border-subtle')).toThrow(/six-digit hex/);
  });
});

describe('token contrast', () => {
  it.each(cases(ALL_SURFACES, ['text-muted', 'text-secondary', 'text-primary']))(
    '%s %s on %s is at least 4.5:1',
    (theme, fg, surface) => {
      expectRatio(contrast(token(theme, fg), token(theme, surface)), 4.5, `${theme} ${fg} on ${surface}`);
    },
  );

  it.each(THEMES.flatMap((theme) => ['danger', 'danger-hover'].map((fill) => [theme, fill])))(
    '%s white on %s is at least 4.5:1',
    (theme, fill) => {
      expectRatio(contrast(WHITE, token(theme as Theme, fill)), 4.5, `${theme} white on ${fill}`);
    },
  );

  it.each(cases(ALL_SURFACES, ['border-input']))(
    '%s %s against %s is at least 3:1',
    (theme, fg, surface) => {
      expectRatio(contrast(token(theme, fg), token(theme, surface)), 3, `${theme} ${fg} against ${surface}`);
    },
  );

  it.each(cases(BASE_SURFACES, ['status-error']))(
    '%s %s on its own 10 percent tint over %s is at least 4.5:1',
    (theme, fg, surface) => {
      const color = token(theme, fg);
      const bg = tint(color, token(theme, surface), 0.1);
      expectRatio(contrast(color, bg), 4.5, `${theme} ${fg} on its 10% tint over ${surface}`);
    },
  );

  it.each(cases(BASE_SURFACES, ['tertiary']))(
    '%s %s on %s is at least 4.5:1',
    (theme, fg, surface) => {
      expectRatio(contrast(token(theme, fg), token(theme, surface)), 4.5, `${theme} ${fg} on ${surface}`);
    },
  );
});

function walkSources(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (name === '__tests__' || name === 'test') continue;
      walkSources(path, out);
      continue;
    }
    if (!/\.tsx?$/.test(name) || /\.test\.tsx?$/.test(name)) continue;
    out.push(path);
  }
  return out;
}

describe('danger token usage', () => {
  const sources = walkSources(srcRoot).map((path) => ({
    rel: relative(srcRoot, path),
    text: readFileSync(path, 'utf8'),
  }));

  it('keeps white text off solid status-error fills and gradients within one class string', () => {
    const offenders = sources.flatMap(({ rel, text }) => [
      ...[...text.matchAll(/from-status-error|to-status-error\/80/g)].map((match) => `${rel}: ${match[0]}`),
      ...[...text.matchAll(/(['"`])((?:(?!\1)[^\\]|\\.)*?)\1/g)]
        .map((match) => match[2])
        .filter((value) => {
          const classes = value.split(/\s+/);
          return (
            classes.includes('text-white') &&
            classes.some((cls) => /^(?:[\w-]+:)*bg-status-error(?!\/)/.test(cls))
          );
        })
        .map((value) => `${rel}: ${value}`),
    ]);
    expect(offenders).toEqual([]);
  });
});
