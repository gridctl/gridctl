export type UnifiedLineKind = 'header' | 'hunk' | 'added' | 'removed' | 'context' | 'note';

export interface UnifiedLine {
  kind: UnifiedLineKind;
  text: string;
}

const HUNK_HEADER = /^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@/;

// classifyUnifiedDiff labels each line of a stored unified diff by its
// prefix. It tracks hunk line counts so a removed line whose content starts
// with "-- " is not mistaken for a "--- " file header.
export function classifyUnifiedDiff(diff: string): UnifiedLine[] {
  const lines = diff.replace(/\r\n/g, '\n').split('\n');
  if (lines.length > 0 && lines[lines.length - 1] === '') lines.pop();
  const out: UnifiedLine[] = [];
  let oldLeft = 0;
  let newLeft = 0;
  for (const text of lines) {
    if (oldLeft > 0 || newLeft > 0) {
      if (text.startsWith('+')) {
        newLeft--;
        out.push({ kind: 'added', text });
        continue;
      }
      if (text.startsWith('-')) {
        oldLeft--;
        out.push({ kind: 'removed', text });
        continue;
      }
      if (text.startsWith(' ') || text === '') {
        oldLeft--;
        newLeft--;
        out.push({ kind: 'context', text });
        continue;
      }
      if (text.startsWith('\\')) {
        out.push({ kind: 'note', text });
        continue;
      }
      oldLeft = 0;
      newLeft = 0;
    }
    const hunk = HUNK_HEADER.exec(text);
    if (hunk) {
      oldLeft = hunk[1] === undefined ? 1 : Number(hunk[1]);
      newLeft = hunk[2] === undefined ? 1 : Number(hunk[2]);
      out.push({ kind: 'hunk', text });
      continue;
    }
    if (text.startsWith('\\')) {
      out.push({ kind: 'note', text });
      continue;
    }
    out.push({ kind: 'header', text });
  }
  return out;
}
