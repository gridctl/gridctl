import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import { beforeAll, describe, expect, it } from 'vitest';

const webRoot = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const srcRoot = join(webRoot, 'src');
const FIXTURE = 'src/fixture.tsx';
const MESSAGE_PREFIX = 'Opacity modifier on a text token.';

let eslint: ESLint;

beforeAll(() => {
  eslint = new ESLint({
    cwd: webRoot,
    overrideConfigFile: join(webRoot, 'eslint.config.js'),
  });
});

async function textOpacityReports(code: string): Promise<number> {
  const [result] = await eslint.lintText(code, { filePath: FIXTURE });
  return result.messages.filter(
    (m) => m.ruleId === 'no-restricted-syntax' && m.message.startsWith(MESSAGE_PREFIX),
  ).length;
}

const reported: string[] = [
  '<span className="text-xs text-text-muted/60">x</span>',
  '<input className="placeholder:text-text-muted/40" />',
  '<a className="text-primary hover:text-primary/80">x</a>',
  '<span className="text-text-secondary/80">x</span>',
  '<li className="before:text-text-muted/60">x</li>',
  "const header = 'text-3xs text-text-muted/50';",
  "<p className={cn('a', on ? 'text-primary' : 'text-text-muted/60')}>x</p>",
  '<p className={`x text-text-muted/60 ${b}`}>x</p>',
  '<Search size={12} className="text-text-muted/50" />',
  '<div aria-hidden="true"><span className="text-text-muted/40">x</span></div>',
  '<span aria-hidden="false" className="text-text-muted/40">x</span>',
  '<span aria-hidden={false} className="text-text-muted/40">x</span>',
];

const allowed: string[] = [
  '<Search size={12} className="text-text-muted/50" aria-hidden="true" />',
  "<Search aria-hidden className={cn('a', on ? 'text-primary' : 'text-text-muted/40')} />",
  '<Search aria-hidden={true} className={`x text-text-muted/60 ${b}`} />',
  '<div className="bg-text-primary/10 ring-text-primary/70 via-text-primary/20 border-text-primary/70" />',
  '<div className="from-text-primary/20" />',
  '<span className="text-text-muted text-text-secondary text-primary bg-primary/10 border-primary/20 text-status-error/80">x</span>',
];

describe('text token opacity lint rule', () => {
  it('lints the fixture path rather than ignoring it as a test file', async () => {
    expect(await eslint.isPathIgnored(FIXTURE)).toBe(false);
    const config = await eslint.calculateConfigForFile(FIXTURE);
    const entries = config.rules['no-restricted-syntax'].slice(1) as { message: string }[];
    expect(entries.some((e) => e.message.startsWith(MESSAGE_PREFIX))).toBe(true);
  });

  it.each(reported)('reports %s', async (code) => {
    expect(await textOpacityReports(code)).toBeGreaterThan(0);
  });

  it.each(allowed)('allows %s', async (code) => {
    expect(await textOpacityReports(code)).toBe(0);
  });
});

function walkSources(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (name === '__tests__' || name === 'test') continue;
      walkSources(path, out);
    } else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name)) {
      out.push(path);
    }
  }
  return out;
}

describe('primary hover states', () => {
  it('never pairs hover:text-primary-light with a hover background', () => {
    const offenders: string[] = [];
    for (const file of walkSources(srcRoot)) {
      readFileSync(file, 'utf8')
        .split('\n')
        .forEach((line, i) => {
          if (/hover:text-primary-light\b/.test(line) && /(?<![\w-])hover:bg-/.test(line)) {
            offenders.push(`${relative(srcRoot, file)}:${i + 1}`);
          }
        });
    }
    expect(offenders).toEqual([]);
  });
});
