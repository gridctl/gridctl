import { describe, it, expect } from 'vitest';
import { normalizePaletteQuery, PALETTE_TIER, scorePaletteCommand } from '../lib/paletteScore';
import type { PaletteCommand } from '../types/palette';

function cmd(label: string, extra: Partial<PaletteCommand> = {}): PaletteCommand {
  return { id: label, label, section: 'global', onSelect: () => {}, ...extra };
}

// The pre-ranking rule: every query character in order in label + keywords.
function legacyMatches(c: PaletteCommand, query: string): boolean {
  const q = query.toLowerCase();
  const haystack = [c.label, ...(c.keywords ?? [])].join(' ').toLowerCase();
  let pos = 0;
  for (const ch of q) {
    const found = haystack.indexOf(ch, pos);
    if (found === -1) return false;
    pos = found + 1;
  }
  return true;
}

describe('scorePaletteCommand', () => {
  const goTools = cmd('Go to Tools', { aliases: ['tools'], keywords: ['tools', 'whitelist', 'go'] });
  const vault = cmd('Go to Variables', {
    aliases: ['variables', 'vault'],
    keywords: ['variables', 'vault', 'secrets'],
  });

  it.each([
    ['exact on label', cmd('Refresh'), 'refresh', PALETTE_TIER.exact],
    ['exact on alias', goTools, 'tools', PALETTE_TIER.exact],
    ['exact on a second alias', vault, 'vault', PALETTE_TIER.exact],
    ['prefix on label', cmd('Refresh'), 'ref', PALETTE_TIER.prefix],
    ['prefix on alias', vault, 'vari', PALETTE_TIER.prefix],
    ['word start after a space', cmd('Go to Tools'), 'too', PALETTE_TIER.wordStart],
    ['word start after a colon', cmd('Tools:Audit'), 'aud', PALETTE_TIER.wordStart],
    ['word start after a hyphen', cmd('dry-run'), 'run', PALETTE_TIER.wordStart],
    ['keyword prefix', cmd('Zoom to fit', { keywords: ['fitview'] }), 'fitv', PALETTE_TIER.keywordPrefix],
    ['keyword equal', cmd('Zoom to fit', { keywords: ['magnify'] }), 'magnify', PALETTE_TIER.keywordPrefix],
    ['substring in label', cmd('Refresh'), 'fres', PALETTE_TIER.substring],
    ['substring in keyword', cmd('Zoom in', { keywords: ['magnify'] }), 'gnif', PALETTE_TIER.substring],
    ['subsequence', cmd('Open Traces'), 'otr', PALETTE_TIER.subsequence],
    ['subsequence across label and keyword', cmd('Logs', { keywords: ['console'] }), 'lgc', PALETTE_TIER.subsequence],
  ])('%s', (_name, command, query, tier) => {
    expect(scorePaletteCommand(command, query)).toBe(tier);
  });

  it('returns null when the query does not match', () => {
    expect(scorePaletteCommand(cmd('Refresh'), 'xyz')).toBeNull();
    expect(scorePaletteCommand(cmd('Refresh'), 'hr')).toBeNull();
  });

  it('does not treat a mid-word occurrence as a word start', () => {
    expect(scorePaletteCommand(cmd('Bootstrap'), 'tst')).toBe(PALETTE_TIER.substring);
    expect(scorePaletteCommand(cmd('a2b'), 'b')).toBe(PALETTE_TIER.substring);
  });

  it('finds a later word-start occurrence after a mid-word one', () => {
    expect(scorePaletteCommand(cmd('ratio rat'), 'rat')).toBe(PALETTE_TIER.prefix);
    expect(scorePaletteCommand(cmd('xrat rat'), 'rat')).toBe(PALETTE_TIER.wordStart);
  });

  it('does not let an alias add a result the label and keywords do not', () => {
    expect(scorePaletteCommand(cmd('Refresh', { aliases: ['zzz'] }), 'zzz')).toBeNull();
  });

  it('normalizes the query', () => {
    expect(normalizePaletteQuery('  ToOls ')).toBe('tools');
  });

  it('matches exactly the set the legacy subsequence rule matched', () => {
    const commands = [
      goTools,
      vault,
      cmd('Go to Library', { aliases: ['library'], keywords: ['library', 'registry', 'skills'] }),
      cmd('Go to Logs', { aliases: ['logs'], keywords: ['logs', 'output', 'console'] }),
      cmd('Refresh', { keywords: ['refresh', 'reload', 'sync'] }),
      cmd('Variables: Show Not Referenced', { keywords: ['unreferenced', 'filter'] }),
      cmd('Tools: Open Usage Audit', { keywords: ['audit', 'usage'] }),
      cmd('Inspect github', { aliases: ['github'], keywords: ['github', 'node', 'server'] }),
      cmd('Search traces: tools/call - github', { keywords: ['abc123', 'github', 'trace'] }),
    ];
    const queries = ['tools', 'logs', 'ref', 'vault', 'gh', 'o', 'tsc', 'z', 'sync', 'show', 'ab1', 'go to'];
    for (const q of queries) {
      const ranked = commands.filter((c) => scorePaletteCommand(c, normalizePaletteQuery(q)) !== null);
      const legacy = commands.filter((c) => legacyMatches(c, q));
      expect(new Set(ranked.map((c) => c.id)), q).toEqual(new Set(legacy.map((c) => c.id)));
    }
  });
});
