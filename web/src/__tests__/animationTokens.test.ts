import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const srcRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const webRoot = join(srcRoot, '..');

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

// Tailwind 4 ships these without any theme declaration.
const BUILT_IN = new Set(['spin', 'ping', 'pulse', 'bounce', 'none']);

describe('animation tokens', () => {
  const css = readFileSync(join(srcRoot, 'index.css'), 'utf8');
  const theme = extractBlock(css, /@theme\b/);

  it('defines every animate-* utility the UI references', () => {
    const undefinedNames: string[] = [];
    for (const path of walkSources(srcRoot)) {
      const text = readFileSync(path, 'utf8');
      for (const match of text.matchAll(/\banimate-([a-z][a-z-]*[a-z])/g)) {
        const name = match[1];
        if (BUILT_IN.has(name)) continue;
        const declared = theme.includes(`--animate-${name}:`) || css.includes(`.animate-${name} `) || css.includes(`.animate-${name}{`);
        if (!declared) undefinedNames.push(`${relative(srcRoot, path)}: animate-${name}`);
      }
    }
    expect(undefinedNames).toEqual([]);
  });

  it('declares the pulse-glow animation and keyframes in @theme', () => {
    expect(theme).toMatch(/--animate-pulse-glow:\s*pulse-glow\b/);
    expect(theme).toMatch(/@keyframes pulse-glow\s*\{/);
  });

  it('loads no JavaScript Tailwind config', () => {
    expect(existsSync(join(webRoot, 'tailwind.config.js'))).toBe(false);
    expect(css).not.toMatch(/@config\b/);
  });
});
