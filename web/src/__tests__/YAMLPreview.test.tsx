import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { YAMLPreview } from '../components/wizard/YAMLPreview';
import { validateStackResource, validateStackSpec } from '../lib/api';
import type { ValidationResult } from '../types';

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  validateStackSpec: vi.fn().mockResolvedValue({ issues: [] }),
  validateStackResource: vi.fn().mockResolvedValue({ issues: [] }),
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

describe('YAMLPreview validation', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(validateStackSpec).mockClear();
    vi.mocked(validateStackResource).mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  const valid: ValidationResult = { valid: true, errorCount: 0, warningCount: 0, issues: [] };

  it('validates a server fragment with the fragment validator, as Review does', async () => {
    vi.mocked(validateStackResource).mockResolvedValue(valid);
    render(<YAMLPreview yaml={'name: github\nimage: ghcr.io/github/github-mcp-server\n'} resourceType="mcp-server" />);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });

    expect(validateStackResource).toHaveBeenCalledWith('name: github\nimage: ghcr.io/github/github-mcp-server\n', 'mcp-server');
    expect(validateStackSpec).not.toHaveBeenCalled();
    expect(screen.getByText('Valid')).toBeInTheDocument();
  });

  it('keeps the whole-stack validator for stack YAML', async () => {
    render(<YAMLPreview yaml={'version: "1"\nname: daily\n'} resourceType="stack" />);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });

    expect(validateStackSpec).toHaveBeenCalledTimes(1);
    expect(validateStackResource).not.toHaveBeenCalled();
  });

  it('ignores a late response for older YAML', async () => {
    let resolveFirst: (value: ValidationResult) => void = () => {};
    let resolveSecond: (value: ValidationResult) => void = () => {};
    vi.mocked(validateStackResource)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveSecond = resolve; }));
    const { rerender } = render(<YAMLPreview yaml={'name: old\nimagee: x\n'} resourceType="mcp-server" />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    rerender(<YAMLPreview yaml={'name: new\nimage: x\n'} resourceType="mcp-server" />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });

    await act(async () => {
      resolveSecond({ valid: false, errorCount: 0, warningCount: 1, issues: [{ field: 'image', message: 'second response', severity: 'warning' }] });
    });
    await act(async () => {
      resolveFirst({ valid: false, errorCount: 1, warningCount: 0, issues: [{ field: 'imagee', message: 'first response', severity: 'error' }] });
    });

    expect(screen.getByText('second response')).toBeInTheDocument();
    expect(screen.queryByText('first response')).not.toBeInTheDocument();
  });

  it('keeps the spinner while the current request is pending after a stale one resolves', async () => {
    let resolveFirst: (value: ValidationResult) => void = () => {};
    vi.mocked(validateStackResource)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
      .mockImplementationOnce(() => new Promise(() => {}));
    const { rerender, container } = render(<YAMLPreview yaml={'name: old\n'} resourceType="resource" />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    rerender(<YAMLPreview yaml={'name: new\n'} resourceType="resource" />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    await act(async () => {
      resolveFirst(valid);
    });

    expect(container.querySelector('.animate-spin')).not.toBeNull();
    expect(screen.queryByText('Valid')).not.toBeInTheDocument();
  });
});

describe('YAMLPreview secret masking', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(validateStackSpec).mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  const yaml = 'version: "1"\nname: demo\ngateway:\n  auth:\n    type: bearer\n    token: sk-literal-123\n';

  it('masks literals by default, reveals them on toggle, and validates the real YAML', async () => {
    const { container } = render(<YAMLPreview yaml={yaml} resourceType="stack" />);
    expect(container.textContent).not.toContain('sk-literal-123');
    const lines = Array.from(container.querySelectorAll('span.whitespace-pre')).map((s) => s.textContent);
    expect(lines).toHaveLength(yaml.split('\n').length);
    expect(lines[5]).toBe('    token: ••••••••');

    const toggle = screen.getByRole('button', { name: 'Show secrets' });
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    act(() => toggle.click());
    expect(container.textContent).toContain('sk-literal-123');
    expect(screen.getByRole('button', { name: 'Hide secrets' })).toHaveAttribute('aria-pressed', 'true');

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    expect(validateStackSpec).toHaveBeenCalledWith(yaml);
  });

  it('shows no toggle for references', () => {
    const { container } = render(<YAMLPreview yaml={'name: demo\ngateway:\n  auth:\n    token: "${var:GATEWAY_TOKEN}"\n'} resourceType="stack" />);
    expect(screen.queryByRole('button', { name: 'Show secrets' })).not.toBeInTheDocument();
    expect(container.textContent).toContain('${var:GATEWAY_TOKEN}');
  });
});
