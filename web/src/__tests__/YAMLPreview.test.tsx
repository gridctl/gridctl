import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import '@testing-library/jest-dom';
import { YAMLPreview } from '../components/wizard/YAMLPreview';

vi.mock('../lib/api', () => ({
  validateStackSpec: vi.fn().mockResolvedValue({ issues: [] }),
}));

describe('YAMLPreview', () => {
  it('preserves leading whitespace on rendered lines', () => {
    const yaml = [
      'version: "1"',
      'name: daily',
      'mcp-servers:',
      '  - name: github',
      '    image: "ghcr.io/github/github-mcp-server:latest"',
      '    transport: stdio',
    ].join('\n');

    const { container } = render(<YAMLPreview yaml={yaml} />);

    // Each rendered YAML line is a span.whitespace-pre sibling of the line-number span.
    const contentSpans = Array.from(
      container.querySelectorAll<HTMLSpanElement>('span.whitespace-pre'),
    );
    const lineTexts = contentSpans.map((s) => s.textContent ?? '');

    // Line 4: `  - name: github` — two leading spaces preserved
    expect(lineTexts).toContain('  - name: github');
    // Line 5: `    image: "..."` — four leading spaces preserved
    expect(lineTexts.some((t) => /^ {4}image: "/.test(t))).toBe(true);
    // Line 6: `    transport: stdio` — four leading spaces preserved
    expect(lineTexts).toContain('    transport: stdio');
  });

  it('applies whitespace-pre so the browser does not collapse indentation', () => {
    const yaml = 'secrets:\n  sets:\n    - dev';
    const { container } = render(<YAMLPreview yaml={yaml} />);

    const contentSpans = container.querySelectorAll('span.whitespace-pre');
    expect(contentSpans.length).toBeGreaterThan(0);
  });
});
