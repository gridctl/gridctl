import { memo, useCallback, useEffect, useMemo, useRef } from 'react';
import { cn } from '../../lib/cn';
import {
  renderMarkdown,
  renderMarkdownBlocks,
  type MarkdownBlockCache,
} from '../../lib/markdown';

interface MarkdownPreviewProps {
  content: string;
  /** Placeholder shown when content is empty. */
  emptyHint?: string;
}

// memo on the HTML string is what keeps an unchanged block's DOM node.
// An equal __html on a re-rendered parent is not enough: React may still
// write innerHTML. display:contents adds no box, so .skill-md child
// margins still apply once the wrapper-aware rules are in place.
const Block = memo(function Block({ html }: { html: string }) {
  return <div data-md-block="" className="contents" dangerouslySetInnerHTML={{ __html: html }} />;
});

/**
 * Read-only markdown renderer shared by the SkillEditor preview pane and the
 * Library inspector's Instructions tab, so both render SKILL.md bodies the same
 * way: sanitized HTML with syntax-highlighted, copyable code blocks and
 * GitHub-style alert callouts. Styling lives under the `.skill-md` scope in
 * index.css. Copy buttons are wired via one delegated listener rather than by
 * re-injecting HTML.
 *
 * A document without raw block HTML renders one block per top-level token.
 * Unchanged blocks keep their DOM nodes. Raw block HTML falls back to a single
 * renderMarkdown write, the same path as before.
 */
export function MarkdownPreview({
  content,
  emptyHint = 'Preview will appear here as you type...',
}: MarkdownPreviewProps) {
  const cacheRef = useRef<MarkdownBlockCache | null>(null);
  const timersRef = useRef<Set<number>>(new Set());

  const onClick = useCallback((e: MouseEvent) => {
    const btn = (e.target as HTMLElement).closest<HTMLButtonElement>('button[data-copy]');
    if (!btn) return;
    const encoded = btn.getAttribute('data-code');
    if (encoded == null) return;
    void navigator.clipboard
      ?.writeText(decodeURIComponent(encoded))
      .then(() => {
        btn.setAttribute('data-copied', '');
        btn.textContent = 'Copied';
        const t = window.setTimeout(() => {
          btn.removeAttribute('data-copied');
          btn.textContent = 'Copy';
          timersRef.current.delete(t);
        }, 1500);
        timersRef.current.add(t);
      })
      .catch(() => {
        /* clipboard may be unavailable; ignore */
      });
  }, []);

  // Bind once per container mount. Re-binding on every HTML change would
  // drop an in-flight "Copied" timer's button if the node were replaced,
  // and is unnecessary: the listener is delegated.
  const setContainer = useCallback(
    (el: HTMLDivElement | null) => {
      if (!el) return;
      el.addEventListener('click', onClick);
      return () => {
        el.removeEventListener('click', onClick);
      };
    },
    [onClick],
  );

  useEffect(() => {
    const timers = timersRef.current;
    return () => {
      timers.forEach((t) => window.clearTimeout(t));
      timers.clear();
    };
  }, []);

  // The cache is an instance variable, not state: updating it must not
  // re-render, and a StrictMode double render must see the same previous
  // cache. It is written in the effect below, after commit.
  const view = useMemo(() => {
    if (!content) return { kind: 'empty' as const };
    // react-hooks/refs: this read is the cache lookup for the next content
    // value. The cache is not a render input; the effect stores it.
    // eslint-disable-next-line react-hooks/refs
    const rendered = renderMarkdownBlocks(content, cacheRef.current);
    if (!rendered) return { kind: 'fallback' as const, html: renderMarkdown(content) };
    return { kind: 'blocks' as const, blocks: rendered.blocks, cache: rendered.cache };
  }, [content]);

  useEffect(() => {
    if (view.kind === 'blocks') cacheRef.current = view.cache;
  }, [view]);

  if (view.kind === 'empty') {
    return (
      <div className="flex items-center justify-center h-full min-h-[200px]">
        <p className="text-text-muted/40 text-sm italic">{emptyHint}</p>
      </div>
    );
  }

  if (view.kind === 'fallback') {
    return (
      <div
        ref={setContainer}
        className={cn('skill-md max-w-none')}
        dangerouslySetInnerHTML={{ __html: view.html }}
      />
    );
  }

  return (
    <div ref={setContainer} className={cn('skill-md max-w-none')}>
      {view.blocks.map((block) => (
        <Block key={block.key} html={block.html} />
      ))}
    </div>
  );
}
