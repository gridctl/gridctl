import type { PaletteCommand } from '../types/palette';

// Match tiers, best first. A command's tier is the best one it qualifies for.
export const PALETTE_TIER = {
  exact: 0,
  prefix: 1,
  wordStart: 2,
  keywordPrefix: 3,
  substring: 4,
  subsequence: 5,
} as const;

export type PaletteTier = (typeof PALETTE_TIER)[keyof typeof PALETTE_TIER];

export function normalizePaletteQuery(query: string): string {
  return query.trim().toLowerCase();
}

const WORD_CHAR = /[\p{L}\p{N}]/u;

function hasWordStart(text: string, q: string): boolean {
  let i = text.indexOf(q);
  while (i !== -1) {
    if (i === 0 || !WORD_CHAR.test(text[i - 1])) return true;
    i = text.indexOf(q, i + 1);
  }
  return false;
}

function isSubsequence(q: string, haystack: string): boolean {
  let pos = 0;
  for (const ch of q) {
    const found = haystack.indexOf(ch, pos);
    if (found === -1) return false;
    pos = found + 1;
  }
  return true;
}

// Returns the match tier of `command` for a normalized query, or null when it
// does not match. The result set is exactly the subsequence rule over the
// label and keywords; the other tiers only order within that set.
export function scorePaletteCommand(command: PaletteCommand, q: string): PaletteTier | null {
  const label = command.label.toLowerCase();
  const keywords = (command.keywords ?? []).map((k) => k.toLowerCase());
  if (!isSubsequence(q, [label, ...keywords].join(' '))) return null;

  const names = [label, ...(command.aliases ?? []).map((a) => a.toLowerCase())];
  if (names.some((n) => n === q)) return PALETTE_TIER.exact;
  if (names.some((n) => n.startsWith(q))) return PALETTE_TIER.prefix;
  if (names.some((n) => hasWordStart(n, q))) return PALETTE_TIER.wordStart;
  if (keywords.some((k) => k.startsWith(q))) return PALETTE_TIER.keywordPrefix;
  if (names.some((n) => n.includes(q)) || keywords.some((k) => k.includes(q))) {
    return PALETTE_TIER.substring;
  }
  return PALETTE_TIER.subsequence;
}
