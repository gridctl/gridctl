import type { ReactNode } from 'react';

// Wraps every case-insensitive occurrence of `query` in `text` with a
// theme-token mark. Exact match spans only — no whole-word or fuzzy
// expansion — so the highlight never claims more than the filter matched.
// Returns the plain string untouched when there is nothing to highlight,
// keeping the common render path allocation-free.
export function highlightMatches(text: string, query: string): ReactNode {
  const q = query.trim().toLowerCase();
  if (!q) return text;
  const lower = text.toLowerCase();
  // Rare code points lowercase to a different number of UTF-16 units
  // (e.g. U+0130), desyncing lowercase indices from the original string —
  // skip highlighting rather than slice mid-character.
  if (lower.length !== text.length) return text;
  let idx = lower.indexOf(q);
  if (idx === -1) return text;

  const parts: ReactNode[] = [];
  let cursor = 0;
  let key = 0;
  while (idx !== -1) {
    if (idx > cursor) parts.push(text.slice(cursor, idx));
    parts.push(
      <mark key={key++} className="bg-primary/25 text-text-primary rounded-[2px] px-px">
        {text.slice(idx, idx + q.length)}
      </mark>,
    );
    cursor = idx + q.length;
    idx = lower.indexOf(q, cursor);
  }
  if (cursor < text.length) parts.push(text.slice(cursor));
  return parts;
}
