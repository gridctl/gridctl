import { useEffect, useMemo, useRef } from 'react';
import { cn } from '../../lib/cn';
import { renderMarkdown } from '../../lib/markdown';

interface MarkdownPreviewProps {
  content: string;
  /** Placeholder shown when content is empty. */
  emptyHint?: string;
}

/**
 * Read-only markdown renderer shared by the SkillEditor preview pane and the
 * Library inspector's Instructions tab, so both render SKILL.md bodies the same
 * way: sanitized HTML with syntax-highlighted, copyable code blocks and
 * GitHub-style alert callouts. Styling lives under the `.skill-md` scope in
 * index.css. Copy buttons are wired via one delegated listener rather than by
 * re-injecting HTML.
 */
export function MarkdownPreview({
  content,
  emptyHint = 'Preview will appear here as you type...',
}: MarkdownPreviewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const html = useMemo(() => (content ? renderMarkdown(content) : ''), [content]);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const timers = new Set<number>();

    function onClick(e: MouseEvent) {
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
            timers.delete(t);
          }, 1500);
          timers.add(t);
        })
        .catch(() => {
          /* clipboard may be unavailable; ignore */
        });
    }

    el.addEventListener('click', onClick);
    return () => {
      el.removeEventListener('click', onClick);
      timers.forEach((t) => window.clearTimeout(t));
    };
  }, [html]);

  if (!content) {
    return (
      <div className="flex items-center justify-center h-full min-h-[200px]">
        <p className="text-text-muted/40 text-sm italic">{emptyHint}</p>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className={cn('skill-md max-w-none')}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
