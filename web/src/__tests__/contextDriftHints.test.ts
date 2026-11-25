import { describe, expect, it } from 'vitest';
import { contextDriftCliHints } from '../lib/contextDriftHints';

describe('contextDriftCliHints', () => {
  it('names a bare adopt only for a single-file client', () => {
    expect(contextDriftCliHints('opencode', 'single-file')).toEqual([
      'gridctl ctx diff opencode',
      'gridctl ctx adopt opencode',
      'gridctl ctx sync opencode --force',
    ]);
  });

  it('scopes diff and adopt to the selected fragment on an identity render', () => {
    expect(contextDriftCliHints('claude-code', 'multi-file-identity', '10-style')).toEqual([
      'gridctl ctx diff claude-code 10-style',
      'gridctl ctx adopt claude-code 10-style',
      'gridctl ctx sync claude-code --force',
    ]);
  });

  it('names no adopt on a lossy render', () => {
    const hints = contextDriftCliHints('vscode', 'multi-file-lossy', '00-default');
    expect(hints).toEqual(['gridctl ctx diff vscode 00-default', 'gridctl ctx sync vscode --force']);
    expect(hints.some((h) => h.includes('adopt'))).toBe(false);
  });

  it('names the --into capture with a literal placeholder on a compiled client', () => {
    expect(contextDriftCliHints('opencode', 'compiled', 'ignored')).toEqual([
      'gridctl ctx diff opencode',
      'gridctl ctx adopt opencode --into <fragment>',
      'gridctl ctx sync opencode --force',
    ]);
  });
});
