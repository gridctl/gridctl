import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const srcRoot = join(dirname(fileURLToPath(import.meta.url)), '..');

// Static value imports only: `import type` and dynamic `import()` are skipped.
const IMPORT_RE = /^\s*(?:import|export)\s+(?!type\b)(?:[^'";]*?\sfrom\s+)?['"]([^'"]+)['"]/gm;

function resolveLocal(from: string, spec: string): string | null {
  const base = join(dirname(from), spec);
  for (const candidate of [base, `${base}.ts`, `${base}.tsx`, join(base, 'index.ts'), join(base, 'index.tsx')]) {
    if (existsSync(candidate) && /\.tsx?$/.test(candidate)) return candidate;
  }
  return null;
}

function staticGraph(entry: string): { files: Set<string>; packages: Map<string, string> } {
  const files = new Set<string>();
  const packages = new Map<string, string>();
  const queue = [entry];
  while (queue.length > 0) {
    const file = queue.pop() as string;
    if (files.has(file)) continue;
    files.add(file);
    const text = readFileSync(file, 'utf8');
    for (const match of text.matchAll(IMPORT_RE)) {
      const spec = match[1];
      if (spec.startsWith('.')) {
        const next = resolveLocal(file, spec);
        if (next) queue.push(next);
      } else if (!packages.has(spec)) {
        packages.set(spec, relative(srcRoot, file));
      }
    }
  }
  return { files, packages };
}

describe('Stack workspace bundle boundary', () => {
  it('reaches no static yaml import from StackWorkspace', () => {
    const { files, packages } = staticGraph(join(srcRoot, 'components/workspaces/StackWorkspace.tsx'));
    expect(files.has(join(srcRoot, 'lib/lineIssues.ts'))).toBe(true);
    expect(packages.get('yaml')).toBeUndefined();
  });

  it('loads yaml in lineIssues through a dynamic import', () => {
    const source = readFileSync(join(srcRoot, 'lib/lineIssues.ts'), 'utf8');
    expect(source).toMatch(/import\('yaml'\)/);
    expect(source).not.toMatch(/^\s*import\s+(?!type\b)[^;]*from\s+['"]yaml['"]/m);
  });
});
