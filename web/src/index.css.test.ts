/// <reference types="node" />
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const css = readFileSync(join(import.meta.dirname, 'index.css'), 'utf8');

function fontFaces(source: string): string[] {
  const faces: string[] = [];
  const marker = '@font-face';
  let from = 0;
  while (from < source.length) {
    const start = source.indexOf(marker, from);
    if (start === -1) break;
    const open = source.indexOf('{', start);
    if (open === -1) break;
    let depth = 0;
    let end = open;
    for (; end < source.length; end += 1) {
      if (source[end] === '{') depth += 1;
      else if (source[end] === '}') {
        depth -= 1;
        if (depth === 0) {
          end += 1;
          break;
        }
      }
    }
    faces.push(source.slice(start, end));
    from = end;
  }
  return faces;
}

describe('index.css typography', () => {
  it('does not import Google Fonts', () => {
    expect(css).not.toContain('googleapis');
  });

  it('declares Outfit and IBM Plex Mono with three font faces', () => {
    const faces = fontFaces(css);
    expect(faces).toHaveLength(3);
    expect(faces.filter((face) => face.includes("font-family: 'Outfit'"))).toHaveLength(1);
    expect(faces.filter((face) => face.includes("font-family: 'IBM Plex Mono'"))).toHaveLength(2);
  });

  it('declares the micro steps without line-height companions', () => {
    expect(css).toContain('--text-2xs: 0.6875rem');
    expect(css).toContain('--text-3xs: 0.625rem');
    expect(css).not.toContain('--text-2xs--line-height');
    expect(css).not.toContain('--text-3xs--line-height');
  });
});
