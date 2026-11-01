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

const COLOR_REF =
  /(?:bg|text|border|ring|from|to|via|fill|stroke|outline|decoration|divide|placeholder|accent|caret)-(?:surface|border|primary|secondary|tertiary|status|text|background|chart)[a-z0-9-]*/g;
const SHADOW_REF = /shadow-(?:glow|node|pane)[a-z-]*/g;
const UTILITY_PREFIX =
  /^(?:bg|text|border|ring|from|to|via|fill|stroke|outline|decoration|divide|placeholder|accent|caret)-/;

describe('theme tokens', () => {
  const css = readFileSync(join(srcRoot, 'index.css'), 'utf8');
  const theme = extractBlock(css, /@theme\b/);
  const light = extractBlock(css, /:root\[data-theme='light'\]/);
  const colorTokens = new Set(
    [...theme.matchAll(/--color-([a-z0-9-]+)/g)].map((match) => match[1]),
  );
  const shadowNames = new Set(
    [...theme.matchAll(/--shadow-([a-z0-9-]+)/g)].map((match) => `shadow-${match[1]}`),
  );
  for (const match of css.matchAll(/\.shadow-([a-z0-9-]+)/g)) {
    shadowNames.add(`shadow-${match[1]}`);
  }

  it('declares every semantic color and shadow class the UI references', () => {
    const undeclared: string[] = [];
    for (const path of walkSources(srcRoot)) {
      const text = readFileSync(path, 'utf8');
      const rel = relative(srcRoot, path);
      for (const match of text.matchAll(COLOR_REF)) {
        const token = match[0].replace(UTILITY_PREFIX, '');
        if (!colorTokens.has(token)) undeclared.push(`${rel}: ${match[0]}`);
      }
      for (const match of text.matchAll(SHADOW_REF)) {
        if (!shadowNames.has(match[0])) undeclared.push(`${rel}: ${match[0]}`);
      }
    }
    expect(undeclared).toEqual([]);
  });

  it('re-keys every @theme color in the light palette', () => {
    const missing = [...colorTokens].filter((token) => !light.includes(`--color-${token}`));
    expect(missing).toEqual([]);
  });
});
