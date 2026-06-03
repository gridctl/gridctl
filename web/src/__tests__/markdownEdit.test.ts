import { describe, it, expect } from 'vitest';
import { applyMarkdownAction } from '../lib/markdownEdit';

describe('applyMarkdownAction', () => {
  it('wraps the selection in bold markers and reselects the inner text', () => {
    const r = applyMarkdownAction('hello world', 0, 5, 'bold');
    expect(r.value).toBe('**hello** world');
    expect(r.value.slice(r.selStart, r.selEnd)).toBe('hello');
  });

  it('uses placeholder text for an empty bold selection', () => {
    const r = applyMarkdownAction('', 0, 0, 'bold');
    expect(r.value).toBe('**bold text**');
  });

  it('prefixes the current line with a heading marker', () => {
    const r = applyMarkdownAction('line one\nline two', 9, 9, 'heading');
    expect(r.value).toBe('line one\n## line two');
  });

  it('prefixes the current line with a list marker', () => {
    const r = applyMarkdownAction('item', 0, 0, 'list');
    expect(r.value).toBe('- item');
  });

  it('wraps the selection in a fenced code block', () => {
    const r = applyMarkdownAction('x = 1', 0, 5, 'code');
    expect(r.value).toBe('```\nx = 1\n```');
    expect(r.value.slice(r.selStart, r.selEnd)).toBe('x = 1');
  });
});
