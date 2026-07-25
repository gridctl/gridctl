// Hand-rolled diff primitives for review surfaces (Pins schema panels and
// drift description rows). Deliberately dependency-free: the inputs are
// small, attacker-influenced review artifacts, not arbitrary documents, so a
// bounded LCS beats pulling in a diff library.

export type DiffKind = 'same' | 'removed' | 'added';

export interface DiffToken {
  kind: DiffKind;
  text: string;
  /** 1-based source line numbers; set by diffLines only. */
  oldLine?: number;
  newLine?: number;
}

// The DP table is quadratic; past this cell count the diff degrades rather
// than stalling the tab (a pathological upstream schema is attacker-timed
// work on the review path).
const MAX_DP_CELLS = 250_000;

// Render cap for line-diff consumers: one DOM node per line hangs the tab on
// a pathological schema, during exactly the review the panel enables.
export const MAX_DIFF_LINES = 2000;

// lcsTokens is the shared LCS walk: bottom-up table, then a forward pass
// preferring removals on ties so output order is stable.
function lcsTokens(a: string[], b: string[]): DiffToken[] {
  const m = a.length;
  const n = b.length;
  const dp: number[][] = Array.from({ length: m + 1 }, () => new Array<number>(n + 1).fill(0));
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }
  const out: DiffToken[] = [];
  let i = 0;
  let j = 0;
  while (i < m && j < n) {
    if (a[i] === b[j]) {
      out.push({ kind: 'same', text: a[i] });
      i++;
      j++;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      out.push({ kind: 'removed', text: a[i] });
      i++;
    } else {
      out.push({ kind: 'added', text: b[j] });
      j++;
    }
  }
  for (; i < m; i++) out.push({ kind: 'removed', text: a[i] });
  for (; j < n; j++) out.push({ kind: 'added', text: b[j] });
  return out;
}

/**
 * Pretty-prints a canonical schema string for review; a value that fails to
 * parse renders raw (still escaped downstream). Lives here rather than in a
 * component module so fast refresh keeps working.
 */
export function prettySchema(canonical: string): string {
  if (!canonical) return '';
  try {
    return JSON.stringify(JSON.parse(canonical), null, 2);
  } catch {
    return canonical;
  }
}

/**
 * Line-level diff with 1-based source line numbers on every token. Oversized
 * inputs fall back to plain removed/added blocks so the quadratic table
 * stays bounded.
 */
export function diffLines(oldText: string, newText: string): DiffToken[] {
  const a = oldText.split('\n');
  const b = newText.split('\n');
  const out: DiffToken[] =
    a.length * b.length > MAX_DP_CELLS
      ? [
          ...a.map((text) => ({ kind: 'removed' as const, text })),
          ...b.map((text) => ({ kind: 'added' as const, text })),
        ]
      : lcsTokens(a, b);
  let oldLine = 0;
  let newLine = 0;
  for (const t of out) {
    if (t.kind !== 'added') t.oldLine = ++oldLine;
    if (t.kind !== 'removed') t.newLine = ++newLine;
  }
  return out;
}

// Whitespace runs are kept as tokens so concatenating a side reproduces the
// original string exactly; a review surface must never silently reflow the
// text it asks a human to approve.
function tokenizeWords(text: string): string[] {
  return text.split(/(\s+)/).filter((t) => t !== '');
}

/**
 * Word-level diff for prose (descriptions). Returns null when the inputs are
 * too large to diff within the DP budget; callers render plain text instead
 * of a misleading whole-string removed/added pair.
 */
export function diffWords(oldText: string, newText: string): DiffToken[] | null {
  const a = tokenizeWords(oldText);
  const b = tokenizeWords(newText);
  if (a.length * b.length > MAX_DP_CELLS) return null;
  return lcsTokens(a, b);
}

/**
 * Projects a word diff onto one side of an old/new pair: the old side keeps
 * same + removed tokens, the new side same + added, each reproducing that
 * side's original text exactly.
 */
export function diffSide(tokens: DiffToken[], side: 'old' | 'new'): DiffToken[] {
  const keep: DiffKind = side === 'old' ? 'removed' : 'added';
  return tokens.filter((t) => t.kind === 'same' || t.kind === keep);
}
