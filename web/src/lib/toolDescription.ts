// Pure clamp for tool descriptions in the Tools workspace detail rail.

export const DESCRIPTION_MAX_CHARS = 480;
export const DESCRIPTION_MAX_LINES = 8;

// How far back from the cut to look for whitespace to break on.
const WORD_BREAK_WINDOW = 40;

export interface ClampedDescription {
  visible: string;
  // UTF-16 code units not shown: text.length - visible.length.
  hiddenChars: number;
}

// Descriptions are attacker-controlled text the model reads as instructions,
// so a shortened view always reports exactly how much it hides.
export function clampDescription(text: string): ClampedDescription {
  let cut = text.length;
  if (cut > DESCRIPTION_MAX_CHARS) cut = DESCRIPTION_MAX_CHARS;

  let newlines = 0;
  for (let i = 0; i < cut; i++) {
    if (text.charCodeAt(i) === 10) {
      newlines++;
      if (newlines === DESCRIPTION_MAX_LINES) {
        cut = i;
        break;
      }
    }
  }

  // Trailing whitespace past the cut hides nothing a reader would see.
  if (cut >= text.length || text.slice(cut).trim() === '') return { visible: text, hiddenChars: 0 };

  // Back up to the previous whitespace when one sits close to the cut. A cut
  // at a line limit already lands on a newline.
  if (!/\s/.test(text[cut])) {
    const floor = Math.max(0, cut - WORD_BREAK_WINDOW);
    for (let i = cut - 1; i >= floor; i--) {
      if (/\s/.test(text[i])) {
        cut = i;
        break;
      }
    }
  }

  // Never split a surrogate pair.
  if (cut > 0 && cut < text.length) {
    const prev = text.charCodeAt(cut - 1);
    const next = text.charCodeAt(cut);
    if (prev >= 0xd800 && prev <= 0xdbff && next >= 0xdc00 && next <= 0xdfff) cut--;
  }

  const visible = text.slice(0, cut).replace(/\s+$/, '');
  return { visible, hiddenChars: text.length - visible.length };
}
