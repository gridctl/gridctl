import { describe, it, expect } from 'vitest';
import {
  formatManagedDirs,
  installedManagedDirs,
  missingManagedDirs,
  referencedManagedDirs,
} from '../lib/skillPackage';
import type { SkillFile } from '../types';

const file = (path: string): SkillFile => ({ path, size: 1, isDir: false });

describe('referencedManagedDirs', () => {
  it('matches a plain path reference', () => {
    expect(referencedManagedDirs('Run scripts/build.sh to start.')).toEqual(['scripts']);
  });

  it('matches a relative path reference', () => {
    expect(referencedManagedDirs('Execute ./scripts/setup.py first.')).toEqual(['scripts']);
    expect(referencedManagedDirs('See ../references/api.md')).toEqual(['references']);
  });

  it('matches inside an inline code span, the usual way a file is named', () => {
    expect(referencedManagedDirs('Run `scripts/lint.sh` before committing.')).toEqual(['scripts']);
  });

  it('matches a directory reference with no file extension', () => {
    expect(referencedManagedDirs('Everything under references/api is generated.')).toEqual(['references']);
  });

  it('matches a path at the very start of the body', () => {
    expect(referencedManagedDirs('scripts/entry.sh is the entry point.')).toEqual(['scripts']);
  });

  it('reports each referenced directory once, in a stable order', () => {
    const body = 'Use assets/logo.png, then scripts/a.sh, then scripts/b.sh, then references/x.md.';
    expect(referencedManagedDirs(body)).toEqual(['scripts', 'references', 'assets']);
  });

  // These are the failure mode that makes the whole feature worthless: a banner
  // that fires on ordinary prose gets ignored, and then it is worse than absent.
  describe('does not fire on prose', () => {
    it.each([
      ['run the build scripts', 'You should run the build scripts before shipping.'],
      ['see the references above', 'See the references above for the full list.'],
      ['assets provided separately', 'Brand assets are provided separately by design.'],
      ['plural noun at end of sentence', 'This skill has no scripts.'],
      ['word followed by punctuation', 'Consider the references, then decide.'],
      ['slash with spaces', 'The scripts / assets split is historical.'],
      ['bare directory with nothing after', 'Everything lives in scripts/'],
    ])('%s', (_label, body) => {
      expect(referencedManagedDirs(body)).toEqual([]);
    });

    it('does not match a longer path segment that merely ends in the word', () => {
      expect(referencedManagedDirs('Look in vendor/scripts/x.sh')).toEqual([]);
      expect(referencedManagedDirs('Look in myscripts/x.sh')).toEqual([]);
    });

    it('ignores fenced code blocks, which often quote another project', () => {
      const body = ['Nothing is bundled here.', '', '```bash', 'cd scripts/', './scripts/deploy.sh', '```'].join('\n');
      expect(referencedManagedDirs(body)).toEqual([]);
    });

    it('still matches a real reference outside a fence', () => {
      const body = ['Run `scripts/real.sh`.', '', '```bash', './scripts/quoted.sh', '```'].join('\n');
      expect(referencedManagedDirs(body)).toEqual(['scripts']);
    });

    it('returns nothing for an empty body', () => {
      expect(referencedManagedDirs('')).toEqual([]);
    });
  });
});

describe('installedManagedDirs', () => {
  it('reports the managed directories that ship files', () => {
    const files = [file('scripts/a.sh'), file('references/b.md'), file('README.md')];
    expect([...installedManagedDirs(files)].sort()).toEqual(['references', 'scripts']);
  });

  it('ignores directory entries, which carry no content', () => {
    expect([...installedManagedDirs([{ path: 'scripts', size: 0, isDir: true }])]).toEqual([]);
  });

  it('ignores unmanaged top-level directories', () => {
    expect([...installedManagedDirs([file('docs/x.md')])]).toEqual([]);
  });
});

describe('missingManagedDirs', () => {
  it('reports a referenced directory that ships nothing', () => {
    expect(missingManagedDirs('Run scripts/build.sh', [])).toEqual(['scripts']);
  });

  it('reports nothing when the referenced directory is installed', () => {
    expect(missingManagedDirs('Run scripts/build.sh', [file('scripts/build.sh')])).toEqual([]);
  });

  // Per-directory, not all-or-nothing: a partially complete package should name
  // only the part that is actually absent.
  it('reports only the absent directory when one of two is installed', () => {
    const body = 'Run scripts/build.sh and read references/guide.md.';
    expect(missingManagedDirs(body, [file('scripts/build.sh')])).toEqual(['references']);
  });

  // A skill whose files have not loaded is not a skill with no files.
  it('reports nothing while the file list is unknown', () => {
    expect(missingManagedDirs('Run scripts/build.sh', null)).toEqual([]);
  });

  it('reports nothing while the body is unknown', () => {
    expect(missingManagedDirs(null, [])).toEqual([]);
  });
});

describe('formatManagedDirs', () => {
  it('formats one, two, and three directories', () => {
    expect(formatManagedDirs([])).toBe('');
    expect(formatManagedDirs(['scripts'])).toBe('scripts/');
    expect(formatManagedDirs(['scripts', 'references'])).toBe('scripts/ and references/');
    expect(formatManagedDirs(['scripts', 'references', 'assets'])).toBe(
      'scripts/, references/, and assets/',
    );
  });
});
