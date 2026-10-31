import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { fireEvent, render } from '@testing-library/react';
import { MarkdownPreview } from '../components/registry/MarkdownPreview';

const THREE = '# One\n\nMiddle\n\n# Three';

describe('MarkdownPreview blocks', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.querySelectorAll('style[data-md-margin]').forEach((node) => node.remove());
  });

  it('keeps untouched block nodes when the middle block changes', () => {
    const { container, rerender } = render(<MarkdownPreview content={THREE} />);
    const before = container.querySelectorAll('[data-md-block]');
    expect(before).toHaveLength(3);
    const first = before[0];
    const third = before[2];

    rerender(<MarkdownPreview content={'# One\n\nMiddle edited\n\n# Three'} />);
    const after = container.querySelectorAll('[data-md-block]');
    expect(after[0]).toBe(first);
    expect(after[2]).toBe(third);
    expect(after[1].textContent).toContain('Middle edited');
    expect(after[0].textContent).toContain('One');
    expect(after[2].textContent).toContain('Three');
  });

  it('keeps existing block nodes when a block is inserted at the top', () => {
    const { container, rerender } = render(<MarkdownPreview content={'# Keep\n\nParagraph'} />);
    const before = [...container.querySelectorAll('[data-md-block]')];
    expect(before).toHaveLength(2);

    rerender(<MarkdownPreview content={'# New\n\n# Keep\n\nParagraph'} />);
    const after = [...container.querySelectorAll('[data-md-block]')];
    expect(after).toHaveLength(3);
    expect(after[1]).toBe(before[0]);
    expect(after[2]).toBe(before[1]);
    expect(after[0].textContent).toContain('New');
  });

  it('strips onerror from an image inside a list item and a blockquote', () => {
    const { container } = render(
      <MarkdownPreview
        content={'- item <img src=x onerror=alert(1)>\n\n> quote <img src=x onerror=alert(1)>'}
      />,
    );
    expect(container.querySelector('[data-md-block]')).not.toBeNull();
    expect(container.querySelector('li')).not.toBeNull();
    expect(container.querySelector('blockquote')).not.toBeNull();
    expect(container.innerHTML).not.toContain('onerror');
    expect(container.innerHTML).not.toContain('alert(1)');
  });

  it('keeps details nesting on the whole-document path and adds no block wrappers', () => {
    const { container } = render(
      <MarkdownPreview content={'<details><summary>x</summary>\n\n## Inside\n\n</details>'} />,
    );
    const heading = container.querySelector('h2');
    expect(heading).not.toBeNull();
    expect(heading?.parentElement?.tagName).toBe('DETAILS');
    expect(heading?.textContent).toContain('Inside');
    expect(container.querySelector('[data-md-block]')).toBeNull();
  });

  it('binds the copy listener once and keeps it on the fallback path', () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });
    const add = vi.spyOn(HTMLElement.prototype, 'addEventListener');
    const { container, rerender } = render(<MarkdownPreview content={'```bash\necho hi\n```'} />);
    const clicks = () => add.mock.calls.filter(([type]) => type === 'click').length;
    const bound = clicks();
    expect(bound).toBeGreaterThan(0);

    rerender(<MarkdownPreview content={'```bash\necho hi\n```\n\nmore'} />);
    expect(clicks()).toBe(bound);
    fireEvent.click(container.querySelector('button[data-copy]')!);
    expect(writeText).toHaveBeenCalledWith('echo hi');

    rerender(<MarkdownPreview content={'<div>box</div>\n\n```bash\necho there\n```'} />);
    expect(container.querySelector('[data-md-block]')).toBeNull();
    fireEvent.click(container.querySelector('button[data-copy]')!);
    expect(writeText).toHaveBeenCalledWith('echo there');
  });

  it('drops fallback HTML when the same container switches to blocks', () => {
    const { container, rerender } = render(
      <MarkdownPreview content={'<div>box</div>\n\n# Top'} />,
    );
    expect(container.textContent).toContain('box');
    expect(container.textContent).toContain('Top');
    expect(container.querySelector('[data-md-block]')).toBeNull();

    rerender(<MarkdownPreview content={'# Alpha\n\nBeta'} />);
    const blocks = container.querySelectorAll('[data-md-block]');
    expect(blocks).toHaveLength(2);
    expect(container.textContent).not.toContain('box');
    expect(container.textContent).not.toContain('Top');
    const root = container.querySelector('.skill-md');
    expect(root).not.toBeNull();
    expect(root!.childNodes.length).toBe(blocks.length);

    rerender(<MarkdownPreview content={'<!-- c -->\n\nGamma'} />);
    expect(container.querySelector('[data-md-block]')).toBeNull();
    expect(container.textContent).not.toContain('Alpha');
    expect(container.textContent).toContain('Gamma');
  });

  it('keeps a copied label when another block changes', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });
    const { container, rerender } = render(
      <MarkdownPreview content={'```bash\necho hi\n```\n\nAfter'} />,
    );
    const button = container.querySelector<HTMLButtonElement>('button[data-copy]');
    expect(button).not.toBeNull();
    fireEvent.click(button!);
    await vi.waitFor(() => expect(button!.textContent).toBe('Copied'));

    rerender(<MarkdownPreview content={'```bash\necho hi\n```\n\nAfter edited'} />);
    const same = container.querySelector('button[data-copy]');
    expect(same).toBe(button);
    expect(same?.textContent).toBe('Copied');
    expect(container.textContent).toContain('After edited');
  });

  it('zeros the first and last margins on both paths', () => {
    const style = document.createElement('style');
    style.setAttribute('data-md-margin', '');
    style.textContent = [
      '.skill-md h1, .skill-md h2 { margin: 1.4em 0 0.5em; }',
      '.skill-md p { margin: 0 0 0.7em; }',
      '.skill-md > :first-child { margin-top: 0; }',
      '.skill-md > :last-child { margin-bottom: 0; }',
      '.skill-md > [data-md-block]:first-child > :first-child { margin-top: 0; }',
      '.skill-md > [data-md-block]:last-child > :last-child { margin-bottom: 0; }',
    ].join('\n');
    document.head.appendChild(style);

    const blocks = render(<MarkdownPreview content={'# Top\n\nBottom paragraph'} />);
    const first = blocks.container.querySelector('h1');
    const last = blocks.container.querySelector('p');
    expect(first).not.toBeNull();
    expect(last).not.toBeNull();
    expect(getComputedStyle(first!).marginTop).toBe('0px');
    expect(getComputedStyle(last!).marginBottom).toBe('0px');
    blocks.unmount();

    const fallback = render(
      <MarkdownPreview content={'<div>box</div>\n\n# Top\n\nBottom paragraph'} />,
    );
    expect(fallback.container.querySelector('[data-md-block]')).toBeNull();
    const fallbackFirst = fallback.container.querySelector('div.skill-md > :first-child');
    const fallbackLast = fallback.container.querySelector('div.skill-md > :last-child');
    expect(fallbackFirst).not.toBeNull();
    expect(fallbackLast?.tagName).toBe('P');
    expect(getComputedStyle(fallbackFirst!).marginTop).toBe('0px');
    expect(getComputedStyle(fallbackLast!).marginBottom).toBe('0px');
  });

  it('keeps the wrapper-aware margin rules in the stylesheet', () => {
    const css = readFileSync(resolve(import.meta.dirname, '../index.css'), 'utf8');
    expect(css).toContain('.skill-md > :first-child { margin-top: 0; }');
    expect(css).toContain('.skill-md > :last-child { margin-bottom: 0; }');
    expect(css).toContain('.skill-md > [data-md-block]:first-child > :first-child { margin-top: 0; }');
    expect(css).toContain('.skill-md > [data-md-block]:last-child > :last-child { margin-bottom: 0; }');
  });
});
