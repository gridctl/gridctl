import { useMemo } from 'react';
import { marked } from 'marked';
import { cn } from '../../lib/cn';

/** Render SKILL.md markdown to HTML with GFM + line breaks. */
export function renderMarkdown(content: string): string {
  return marked.parse(content, { breaks: true, gfm: true }) as string;
}

interface MarkdownPreviewProps {
  content: string;
  /** Placeholder shown when content is empty. */
  emptyHint?: string;
}

/**
 * Read-only markdown renderer shared by the SkillEditor preview pane and the
 * Library inspector's Instructions tab, so both render SKILL.md bodies with the
 * same `prose` treatment.
 */
export function MarkdownPreview({
  content,
  emptyHint = 'Preview will appear here as you type...',
}: MarkdownPreviewProps) {
  const html = useMemo(() => (content ? renderMarkdown(content) : ''), [content]);

  if (!content) {
    return (
      <div className="flex items-center justify-center h-full min-h-[200px]">
        <p className="text-text-muted/40 text-sm italic">{emptyHint}</p>
      </div>
    );
  }

  return (
    <div
      className={cn(
        'prose prose-invert prose-sm max-w-none',
        // Headings
        'prose-headings:text-text-primary prose-headings:font-semibold prose-headings:tracking-tight',
        'prose-h1:text-lg prose-h1:border-b prose-h1:border-border/30 prose-h1:pb-2 prose-h1:mb-4',
        'prose-h2:text-base prose-h2:mt-6 prose-h2:mb-2',
        'prose-h3:text-sm prose-h3:mt-4 prose-h3:mb-1',
        // Body text
        'prose-p:text-text-secondary prose-p:text-sm prose-p:leading-relaxed',
        // Code
        'prose-code:text-primary prose-code:bg-surface-highlight prose-code:px-1.5 prose-code:py-0.5 prose-code:rounded prose-code:text-xs prose-code:font-mono prose-code:before:content-none prose-code:after:content-none',
        'prose-pre:bg-background/60 prose-pre:border prose-pre:border-border/30 prose-pre:rounded-lg prose-pre:text-xs',
        // Links
        'prose-a:text-primary prose-a:no-underline hover:prose-a:underline',
        // Lists
        'prose-li:text-text-secondary prose-li:text-sm prose-li:marker:text-text-muted',
        'prose-ul:my-2 prose-ol:my-2',
        // Strong / emphasis
        'prose-strong:text-text-primary',
        'prose-em:text-text-secondary',
        // Blockquotes
        'prose-blockquote:border-primary/40 prose-blockquote:text-text-muted prose-blockquote:not-italic',
        // Tables
        'prose-th:text-text-primary prose-th:text-xs prose-th:uppercase prose-th:tracking-wider prose-th:font-medium',
        'prose-td:text-text-secondary prose-td:text-sm',
        // Horizontal rules
        'prose-hr:border-border/30',
      )}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
