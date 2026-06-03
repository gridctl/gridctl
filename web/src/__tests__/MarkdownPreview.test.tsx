import { describe, it, expect, vi, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, fireEvent } from '@testing-library/react';
import { MarkdownPreview } from '../components/registry/MarkdownPreview';

describe('MarkdownPreview', () => {
  describe('sanitization', () => {
    it('neutralizes an inline <img onerror> XSS payload', () => {
      const { container } = render(
        <MarkdownPreview content={'Hello <img src=x onerror="alert(1)">'} />,
      );
      // DOMPurify strips the event handler; the rest survives.
      expect(container.innerHTML).not.toContain('onerror');
      expect(container.innerHTML).not.toContain('alert(1)');
    });

    it('strips <script> tags from the rendered body', () => {
      const { container } = render(
        <MarkdownPreview content={'<script>alert(2)</script>\n\nSafe text'} />,
      );
      expect(container.querySelector('script')).toBeNull();
      expect(container.textContent).toContain('Safe text');
    });
  });

  describe('alert callouts', () => {
    it('renders a GitHub [!WARNING] blockquote as a styled callout', () => {
      const { container } = render(
        <MarkdownPreview content={'> [!WARNING]\n> Be careful here'} />,
      );
      const alert = container.querySelector('.markdown-alert.markdown-alert-warning');
      expect(alert).not.toBeNull();
      expect(alert?.textContent).toContain('Be careful here');
    });

    it('leaves an ordinary blockquote unchanged', () => {
      const { container } = render(<MarkdownPreview content={'> just a quote'} />);
      expect(container.querySelector('.markdown-alert')).toBeNull();
      expect(container.querySelector('blockquote')?.textContent).toContain('just a quote');
    });
  });

  describe('code blocks', () => {
    it('renders a language label and highlights known languages', () => {
      const { container } = render(
        <MarkdownPreview content={'```bash\necho "hello"\n```'} />,
      );
      const figure = container.querySelector('figure.md-code');
      expect(figure).not.toBeNull();
      expect(figure?.getAttribute('data-lang')).toBe('bash');
      expect(container.querySelector('pre.language-bash')).not.toBeNull();
      // Prism emits token spans for highlighted code.
      expect(container.querySelector('.token')).not.toBeNull();
    });

    it('renders an unlabeled fence as plain text without throwing', () => {
      const { container } = render(
        <MarkdownPreview content={'```\nplain text\n```'} />,
      );
      const figure = container.querySelector('figure.md-code');
      expect(figure?.getAttribute('data-lang')).toBe('text');
      expect(container.textContent).toContain('plain text');
    });

    it('copies the original source text, not the highlighted DOM', async () => {
      const writeText = vi.fn().mockResolvedValue(undefined);
      Object.defineProperty(navigator, 'clipboard', {
        value: { writeText },
        configurable: true,
      });

      const source = 'echo "hello $USER"';
      const { container } = render(
        <MarkdownPreview content={'```bash\n' + source + '\n```'} />,
      );
      const button = container.querySelector<HTMLButtonElement>('button[data-copy]');
      expect(button).not.toBeNull();
      fireEvent.click(button!);
      expect(writeText).toHaveBeenCalledWith(source);
    });
  });

  it('shows the empty hint when content is blank', () => {
    const { getByText } = render(
      <MarkdownPreview content="" emptyHint="Nothing here yet." />,
    );
    expect(getByText('Nothing here yet.')).toBeInTheDocument();
  });
});

beforeEach(() => {
  vi.restoreAllMocks();
});
