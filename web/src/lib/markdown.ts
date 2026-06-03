import { Marked } from 'marked';
import markedAlert from 'marked-alert';
import DOMPurify from 'dompurify';
import Prism from 'prismjs';
// Register only the grammars we surface in skill instructions. Imported
// individually (not via the autoloader) so the bundler can tree-shake the rest.
// `javascript`/`markup`/`css` ship in the Prism core import above.
import 'prismjs/components/prism-bash';
import 'prismjs/components/prism-python';
import 'prismjs/components/prism-json';
import 'prismjs/components/prism-yaml';
import 'prismjs/components/prism-typescript';

/** Fence labels we recognize, mapped to the Prism grammar we ship. */
const LANG_ALIAS: Record<string, string> = {
  sh: 'bash',
  shell: 'bash',
  bash: 'bash',
  py: 'python',
  python: 'python',
  js: 'javascript',
  javascript: 'javascript',
  ts: 'typescript',
  typescript: 'typescript',
  json: 'json',
  yml: 'yaml',
  yaml: 'yaml',
};

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// A single configured instance: GFM + line breaks (preserving the prior
// behavior), GitHub-style alert callouts, and a custom fenced-code renderer
// that adds a language/copy header and syntax highlighting.
const marked = new Marked({ breaks: true, gfm: true });
marked.use(markedAlert());
marked.use({
  renderer: {
    code({ text, lang }) {
      const raw = text ?? '';
      const key = LANG_ALIAS[(lang ?? '').trim().toLowerCase()];
      const grammar = key ? Prism.languages[key] : undefined;
      const body = grammar ? Prism.highlight(raw, grammar, key) : escapeHtml(raw);
      const label = key ?? (lang ? lang.trim() : 'text');
      const cls = key ? `language-${key}` : '';
      // Stash the original (decoded) source for the copy button.
      // encodeURIComponent keeps it attribute-safe and round-trips
      // whitespace and newlines exactly, so copy yields the literal source
      // rather than the highlighted DOM text.
      const encoded = encodeURIComponent(raw);
      return (
        `<figure class="md-code" data-lang="${escapeHtml(label)}">` +
        `<figcaption class="md-code__bar">` +
        `<span class="md-code__lang">${escapeHtml(label)}</span>` +
        `<button type="button" class="md-code__copy" data-copy data-code="${encoded}" aria-label="Copy code">Copy</button>` +
        `</figcaption>` +
        `<pre class="${cls}"><code class="${cls}">${body}</code></pre>` +
        `</figure>`
      );
    },
  },
});

/**
 * Render SKILL.md markdown to sanitized HTML.
 *
 * The pipeline is parse-then-sanitize: marked produces HTML, then DOMPurify
 * strips anything dangerous. Sanitizing the *output* (never the input markdown)
 * is deliberate — sanitizing before the markdown pass is a known XSS bypass.
 * Skills can be imported from remote git, so the body is untrusted input.
 */
export function renderMarkdown(content: string): string {
  const html = marked.parse(content, { breaks: true, gfm: true }) as string;
  return DOMPurify.sanitize(html);
}
