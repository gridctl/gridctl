export type MarkdownAction = 'bold' | 'list' | 'code' | 'heading';

/**
 * Apply a lightweight markdown transform at a textarea's current selection and
 * return the new value plus the caret/selection to restore. Pure (no DOM) so
 * the SkillEditor toolbar can stay thin and the behavior is unit-testable.
 */
export function applyMarkdownAction(
  value: string,
  selStart: number,
  selEnd: number,
  action: MarkdownAction,
): { value: string; selStart: number; selEnd: number } {
  const selected = value.slice(selStart, selEnd);
  const lineStart = value.lastIndexOf('\n', selStart - 1) + 1;

  switch (action) {
    case 'bold': {
      const text = selected || 'bold text';
      const next = value.slice(0, selStart) + `**${text}**` + value.slice(selEnd);
      return { value: next, selStart: selStart + 2, selEnd: selStart + 2 + text.length };
    }
    case 'code': {
      const text = selected || 'code';
      const block = '```\n' + text + '\n```';
      const next = value.slice(0, selStart) + block + value.slice(selEnd);
      return { value: next, selStart: selStart + 4, selEnd: selStart + 4 + text.length };
    }
    case 'heading': {
      const next = value.slice(0, lineStart) + '## ' + value.slice(lineStart);
      return { value: next, selStart: selStart + 3, selEnd: selEnd + 3 };
    }
    case 'list': {
      const next = value.slice(0, lineStart) + '- ' + value.slice(lineStart);
      return { value: next, selStart: selStart + 2, selEnd: selEnd + 2 };
    }
  }
}
