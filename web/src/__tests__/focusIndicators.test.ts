import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

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

function hexToken(block: string, name: string): string {
  const match = new RegExp(`--color-${name}:\\s*(#[0-9a-fA-F]{6})\\s*;`).exec(block);
  if (!match) throw new Error(`missing --color-${name}`);
  return match[1];
}

function luminance(hex: string): number {
  const channels = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
  const [r, g, b] = channels.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

function offenders(pattern: RegExp): string[] {
  const found: string[] = [];
  for (const path of walkSources(srcRoot)) {
    const text = readFileSync(path, 'utf8');
    const rel = relative(srcRoot, path);
    for (const match of text.matchAll(pattern)) found.push(`${rel}: ${match[0].trim()}`);
  }
  return found;
}

const SURFACES = ['background', 'surface', 'surface-elevated', 'surface-highlight'];

describe('focus indicators', () => {
  const css = readFileSync(join(srcRoot, 'index.css'), 'utf8');

  it('declares the global 2px primary focus-visible outline', () => {
    const rule = extractBlock(css, /(?:^|\s):focus-visible\s*\{/);
    expect(rule).toMatch(/outline:\s*2px solid var\(--color-primary\)\s*;/);
    expect(rule).toMatch(/outline-offset:\s*2px\s*;/);
  });

  it('keeps the primary indicator at 3:1 against every surface in both themes', () => {
    const themes = {
      dark: extractBlock(css, /@theme\b/),
      light: extractBlock(css, /:root\[data-theme='light'\]/),
    };
    const failing: string[] = [];
    for (const [theme, block] of Object.entries(themes)) {
      const primary = hexToken(block, 'primary');
      for (const surface of SURFACES) {
        const ratio = contrast(primary, hexToken(block, surface));
        if (ratio < 3) failing.push(`${theme} ${surface}: ${ratio.toFixed(2)}`);
      }
    }
    expect(failing).toEqual([]);
  });

  it('never suppresses the outline', () => {
    expect(offenders(/(?:^|[\s"'`:])outline-(?:none|hidden|0)\b/gm)).toEqual([]);
  });

  it('adds no substitute ring, border, or fill indicator on focus', () => {
    expect(offenders(/\bfocus(?:-visible)?:(?:ring|border|bg)-/g)).toEqual([]);
    expect(offenders(/\bfocus-within:ring-/g)).toEqual([]);
  });

  it('uses no translucent outline color', () => {
    expect(offenders(/\boutline-[a-z-]+\/(?:\d|\[)/g)).toEqual([]);
  });

  it('keeps an outline on the focused CodeMirror editor', () => {
    const theme = readFileSync(join(srcRoot, 'themes', 'codemirror.ts'), 'utf8');
    expect(theme).not.toMatch(/outline:\s*['"]none['"]/);
  });
});
