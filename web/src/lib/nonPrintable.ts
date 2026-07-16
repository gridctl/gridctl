// Tool descriptions are instructions to the model, and poisoned ones hide
// payloads in control, zero-width, and bidi-override characters. Anything a
// reviewer is asked to approve must render every character visibly.

// Backslash (so real escapes cannot be spoofed by literal escape-looking
// text), C0/C1 controls, soft hyphen, ALM, zero-width and bidi controls
// (including the U+2066-U+2069 isolates), line/paragraph separators, word
// joiners, BOM, and astral Unicode tag characters (an ASCII-smuggling
// channel). Deliberately excludes plain spaces and printable text.
// eslint-disable-next-line no-control-regex
const NON_PRINTABLE = /[\\\u0000-\u001f\u007f-\u009f\u00ad\u061c\u200b-\u200f\u2028-\u202e\u2060-\u2064\u2066-\u206f\ufeff\u{e0000}-\u{e007f}]/gu;

const NAMED: Record<string, string> = {
  '\\': '\\\\',
  '\n': '\\n',
  '\r': '\\r',
  '\t': '\\t',
};

/**
 * Replaces non-printable characters with visible escape sequences (\\n,
 * \\u202e, ...) so hidden instructions cannot survive review unseen.
 */
export function escapeNonPrintable(s: string): string {
  return s.replace(NON_PRINTABLE, (ch) => {
    const named = NAMED[ch];
    if (named) return named;
    const cp = ch.codePointAt(0)!;
    return cp > 0xffff
      ? `\\u{${cp.toString(16)}}`
      : `\\u${cp.toString(16).padStart(4, '0')}`;
  });
}

/** Abbreviates a pin hash for display, keeping any scheme prefix (h2:). */
export function shortPinHash(hash: string): string {
  const idx = hash.indexOf(':');
  const prefix = idx >= 0 ? hash.slice(0, idx + 1) : '';
  const rest = idx >= 0 ? hash.slice(idx + 1) : hash;
  return prefix + (rest.length > 12 ? rest.slice(0, 12) : rest);
}
