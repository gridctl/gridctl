import { afterEach, describe, expect, it, vi } from 'vitest';
import DOMPurify from 'dompurify';
import { renderMarkdown, renderMarkdownBlocks } from '../lib/markdown';

// renderMarkdown is DOMPurify.sanitize(marked.parse(...)) with the configured
// instance. Block concatenation must match that call byte for byte.
const FIXTURE = [
  '# Title',
  '',
  'A paragraph with **bold**, *italic*, and `code`.',
  '',
  '- item one',
  '- item two',
  '  - nested',
  '',
  '1. first',
  '2. second',
  '',
  '- [ ] todo',
  '- [x] done',
  '',
  '| a | b |',
  '| --- | --- |',
  '| 1 | 2 |',
  '',
  '```python',
  'print("hi")',
  '```',
  '',
  '```',
  'plain text',
  '```',
  '',
  '> a quote',
  '',
  '> [!WARNING]',
  '> Be careful here',
  '',
  'Hello <img src=x onerror="alert(1)">',
  '',
  '---',
  '',
  'See [the docs][ref].',
  '',
  '[ref]: https://example.com/docs',
  '',
].join('\n');

describe('renderMarkdownBlocks', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('matches whole-document sanitized HTML, including an alert and a reference link', () => {
    const result = renderMarkdownBlocks(FIXTURE, null);
    expect(result).not.toBeNull();
    const joined = result!.blocks.map((block) => block.html).join('');
    expect(joined).toBe(renderMarkdown(FIXTURE));
    expect(joined).toContain('markdown-alert-warning');
    expect(joined).toContain('href="https://example.com/docs"');
    expect(joined).not.toContain('onerror');
  });

  it('signals whole-document rendering for raw block HTML', () => {
    expect(
      renderMarkdownBlocks('<details><summary>x</summary>\n\n## Inside\n\n</details>', null),
    ).toBeNull();
    expect(renderMarkdownBlocks('<script>alert(2)</script>\n\nSafe text', null)).toBeNull();
    expect(renderMarkdownBlocks('<!-- note -->\n\nA paragraph', null)).toBeNull();
    expect(renderMarkdownBlocks('<br>', null)).toBeNull();
    expect(renderMarkdownBlocks('<div>box</div>', null)).toBeNull();
  });

  it('emits no block for a space token or a link definition', () => {
    const result = renderMarkdownBlocks(
      'See [the docs][ref].\n\n[ref]: https://example.com/docs\n',
      null,
    );
    expect(result).not.toBeNull();
    expect(result!.blocks).toHaveLength(1);
    expect(result!.blocks[0].html).toContain('href="https://example.com/docs"');
    expect(result!.blocks.every((block) => !block.key.includes('[ref]:'))).toBe(true);
  });

  it('gives identical blocks distinct keys that stay stable', () => {
    const result = renderMarkdownBlocks('---\n\n---', null);
    expect(result).not.toBeNull();
    expect(result!.blocks).toHaveLength(2);
    expect(result!.blocks[0].key).not.toBe(result!.blocks[1].key);
    expect(result!.blocks[0].html).toBe(result!.blocks[1].html);
    const again = renderMarkdownBlocks('---\n\n---', result!.cache);
    expect(again!.blocks.map((block) => block.key)).toEqual(result!.blocks.map((block) => block.key));
  });

  it('reuses sanitized HTML for an unchanged raw block without sanitizing again', () => {
    const spy = vi.spyOn(DOMPurify, 'sanitize');
    const first = renderMarkdownBlocks('# Title\n\nKeep\n\nTail', null);
    expect(first).not.toBeNull();
    expect(spy.mock.calls.length).toBeGreaterThan(0);
    spy.mockClear();

    const second = renderMarkdownBlocks('# Title\n\nKeep\n\nTail', first!.cache);
    expect(spy).not.toHaveBeenCalled();
    expect(second!.blocks.map((block) => block.html)).toEqual(first!.blocks.map((block) => block.html));
    second!.blocks.forEach((block, index) => {
      expect(block.html).toBe(first!.blocks[index].html);
    });
  });

  it('sanitizes only the block whose raw text changed', () => {
    const first = renderMarkdownBlocks('# Title\n\nKeep\n\nTail', null);
    expect(first).not.toBeNull();
    const spy = vi.spyOn(DOMPurify, 'sanitize');
    const second = renderMarkdownBlocks('# Title\n\nChanged\n\nTail', first!.cache);
    expect(second).not.toBeNull();
    expect(spy).toHaveBeenCalledTimes(1);
    expect(second!.blocks[0].html).toBe(first!.blocks[0].html);
    expect(second!.blocks[2].html).toBe(first!.blocks[2].html);
    expect(second!.blocks[1].html).toContain('Changed');
  });

  it('re-renders a reference link when its definition changes', () => {
    const paragraph = 'See [the docs][ref].';
    const first = renderMarkdownBlocks(`${paragraph}\n\n[ref]: https://example.com/a\n`, null);
    expect(first).not.toBeNull();
    expect(first!.blocks).toHaveLength(1);
    expect(first!.blocks[0].html).toContain('https://example.com/a');
    expect(first!.blocks[0].key).toContain(paragraph);

    const spy = vi.spyOn(DOMPurify, 'sanitize');
    const second = renderMarkdownBlocks(`${paragraph}\n\n[ref]: https://example.com/b\n`, first!.cache);
    expect(second).not.toBeNull();
    expect(spy).toHaveBeenCalled();
    expect(second!.blocks).toHaveLength(1);
    expect(second!.blocks[0].key).toContain(paragraph);
    expect(second!.blocks[0].html).toContain('https://example.com/b');
    expect(second!.blocks[0].html).not.toContain('https://example.com/a');
    expect(second!.blocks.every((block) => !block.key.includes('[ref]:'))).toBe(true);
  });

  it('keeps only the current document in the cache', () => {
    const first = renderMarkdownBlocks('# One\n\n# Two', null);
    expect(first).not.toBeNull();
    const second = renderMarkdownBlocks('# One', first!.cache);
    expect(second).not.toBeNull();
    expect(second!.blocks).toHaveLength(1);
    expect([...second!.cache.byRaw.keys()].some((raw) => raw.includes('Two'))).toBe(false);
  });
});
