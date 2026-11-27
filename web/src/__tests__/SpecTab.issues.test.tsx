import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { useSpecStore } from '../stores/useSpecStore';
import { useStackStore } from '../stores/useStackStore';
import type { SpecHealth, StackSpec, ValidationResult } from '../types';

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  fetchStackSpec: vi.fn(),
  fetchStackHealth: vi.fn(),
  validateStackSpec: vi.fn(),
}));

import { fetchStackHealth, fetchStackSpec, validateStackSpec } from '../lib/api';
import { SpecTab } from '../components/spec/SpecTab';

const CONTENT = [
  'name: demo', //          1
  'mcp-servers:', //        2
  '  - name: a', //         3
  '    network: x', //      4
  '  - name: b', //         5
  '    network: edge', //   6
].join('\n');

const SPEC: StackSpec = { path: '/tmp/stack.yaml', content: CONTENT };
const HEALTH: SpecHealth = {
  validation: { status: 'errors', errorCount: 2, warningCount: 1 },
  drift: { status: 'in-sync' },
  dependencies: { status: 'resolved' },
} as SpecHealth;
const VALIDATION: ValidationResult = {
  valid: false,
  errorCount: 2,
  warningCount: 1,
  issues: [
    { field: 'mcp-servers[1].network', severity: 'error', message: 'm1' },
    { field: 'stack', severity: 'warning', message: 'm2' },
    { field: 'x', severity: 'error', message: 'unknown field "x" (line 2)', line: 2 },
  ],
};

const never = <T,>() => new Promise<T>(() => {});

function issueRows(): HTMLElement[] {
  const region = screen.getByRole('heading', { name: /Issues/ }).closest('section') as HTMLElement;
  return within(region).getAllByRole('listitem');
}

function yamlRow(line: number): HTMLElement {
  return document.querySelector(`tr[data-line="${line}"]`) as HTMLElement;
}

beforeEach(() => {
  cleanup();
  vi.mocked(fetchStackSpec).mockReset().mockImplementation(() => never());
  vi.mocked(fetchStackHealth).mockReset().mockResolvedValue(HEALTH);
  vi.mocked(validateStackSpec).mockReset().mockResolvedValue(VALIDATION);
  useStackStore.setState({ featureDetails: [] });
  useSpecStore.setState({
    spec: SPEC,
    specLoading: false,
    specError: null,
    validation: VALIDATION,
    validationError: null,
    health: null,
    diffModalOpen: false,
  });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('SpecTab issues list', () => {
  it('lists issues by severity and line, with buttons only for located issues', async () => {
    render(<SpecTab />);
    expect(screen.getByRole('heading', { name: 'Issues (3)' })).toBeInTheDocument();
    expect(await screen.findByText('line 6')).toBeInTheDocument();

    const rows = issueRows();
    expect(rows.map((r) => r.textContent?.match(/^(Error|Warning|Info)/)?.[1])).toEqual(['Error', 'Error', 'Warning']);
    expect(within(rows[0]).getByRole('button')).toHaveTextContent('line 2');
    expect(within(rows[1]).getByRole('button')).toHaveTextContent('line 6');
    expect(within(rows[1]).getByRole('button')).toHaveTextContent('mcp-servers[1].network');
    expect(within(rows[2]).queryByRole('button')).toBeNull();
    expect(rows[2]).toHaveTextContent('stack');
    expect(rows[2]).not.toHaveTextContent(/line \d/);
  });

  it('renders rows before yaml loads and fills in field lines without moving focus', async () => {
    // Fresh modules, so yaml has not been loaded by an earlier test.
    vi.resetModules();
    const fresh = await import('../stores/useSpecStore');
    const { SpecTab: FreshSpecTab } = await import('../components/spec/SpecTab');
    const lineIssues = await import('../lib/lineIssues');
    expect(lineIssues.getLoadedYaml()).toBeNull();
    fresh.useSpecStore.setState({ spec: SPEC, validation: VALIDATION });
    render(<FreshSpecTab />);
    const before = issueRows();
    expect(before[0]).toHaveTextContent('line 2');
    expect(screen.queryByText('line 6')).toBeNull();
    const first = within(before[0]).getByRole('button');
    first.focus();

    expect(await screen.findByText('line 6')).toBeInTheDocument();
    expect(document.activeElement).toBe(first);
  });

  it('scrolls the line into view and moves the active marker between rows', async () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    render(<SpecTab />);
    await screen.findByText('line 6');

    fireEvent.click(screen.getByRole('button', { name: /line 2/ }));
    expect(scroll).toHaveBeenCalledTimes(1);
    expect(scroll.mock.contexts[0]).toBe(yamlRow(2));
    expect(scroll.mock.calls[0][0]).toMatchObject({ block: 'center', behavior: 'smooth' });
    expect(yamlRow(2)).toHaveAttribute('aria-current', 'true');
    expect(screen.getByRole('button', { name: /line 2/ })).toHaveAttribute('aria-current', 'true');

    fireEvent.click(screen.getByRole('button', { name: /line 6/ }));
    expect(scroll.mock.contexts[1]).toBe(yamlRow(6));
    expect(yamlRow(6)).toHaveAttribute('aria-current', 'true');
    expect(yamlRow(2)).not.toHaveAttribute('aria-current');
  });

  it('jumps without smooth scrolling under reduced motion', async () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    const original = window.matchMedia;
    window.matchMedia = vi.fn(() => ({ matches: true }) as MediaQueryList);
    try {
      render(<SpecTab />);
      fireEvent.click(screen.getByRole('button', { name: /line 2/ }));
      expect(scroll.mock.calls[0][0]).toMatchObject({ behavior: 'auto' });
    } finally {
      window.matchMedia = original;
    }
  });

  it('shows no issues region when the spec is valid', () => {
    useSpecStore.setState({ validation: { valid: true, errorCount: 0, warningCount: 0, issues: [] } });
    render(<SpecTab />);
    expect(screen.queryByRole('heading', { name: /Issues/ })).toBeNull();
  });
});

describe('SpecTab gutter', () => {
  it('marks only lines with issues, naming the count and severities', async () => {
    useSpecStore.setState({
      validation: {
        ...VALIDATION,
        issues: [
          ...VALIDATION.issues,
          { field: 'y', severity: 'warning', message: 'w on 2', line: 2 },
        ],
      },
    });
    render(<SpecTab />);
    await screen.findByText('line 6');

    expect(within(yamlRow(1)).queryByRole('img')).toBeNull();
    expect(yamlRow(1).querySelector('.rounded-full')).toBeNull();
    expect(yamlRow(3).querySelector('td:last-child')?.childElementCount).toBe(0);

    const marker = within(yamlRow(2)).getByRole('img', { name: '2 issues on line 2: 1 error, 1 warning' });
    expect(marker).toHaveAttribute('title', 'error: unknown field "x" (line 2)\nwarning: w on 2');
    expect(within(yamlRow(6)).getByRole('img', { name: '1 issue on line 6: 1 error' })).toBeInTheDocument();
  });
});

describe('SpecTab refresh failures', () => {
  async function flush() {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
  }

  it('keeps the last copy on a spec failure, guards concurrent loads, and clears after a successful retry', async () => {
    vi.useFakeTimers();
    let resolveSpec!: (spec: StackSpec) => void;
    vi.mocked(fetchStackSpec)
      .mockRejectedValueOnce(new Error('gateway offline'))
      .mockReturnValueOnce(new Promise<StackSpec>((r) => { resolveSpec = r; }));
    render(<SpecTab />);
    await flush();

    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Could not refresh the spec: gateway offline. Showing the last loaded copy.');
    expect(yamlRow(6)).toHaveTextContent('network: edge');
    expect(fetchStackSpec).toHaveBeenCalledTimes(1);

    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    expect(fetchStackSpec).toHaveBeenCalledTimes(2);
    expect(within(screen.getByRole('alert')).getByRole('button', { name: 'Retry' })).toBeDisabled();
    expect(screen.getByRole('alert')).toBe(alert);

    // The scheduled poll lands while Retry's load is in flight.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(fetchStackSpec).toHaveBeenCalledTimes(2);

    await act(async () => {
      resolveSpec(SPEC);
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.queryByRole('alert')).toBeNull();
    expect(validateStackSpec).toHaveBeenCalledTimes(1);
  });

  it('keeps the previous issues when validation fails after a successful fetch', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchStackSpec).mockResolvedValueOnce(SPEC);
    vi.mocked(validateStackSpec).mockRejectedValueOnce(new Error('validator down'));
    render(<SpecTab />);
    await flush();

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Could not refresh validation: validator down. Issues below may be out of date.',
    );
    expect(useSpecStore.getState().specError).toBeNull();
    expect(screen.getByRole('heading', { name: 'Issues (3)' })).toBeInTheDocument();
    expect(screen.getByText('m1')).toBeInTheDocument();
  });

  it('releases the load guard after a failure so the next poll runs', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchStackSpec)
      .mockRejectedValueOnce(new Error('one'))
      .mockResolvedValueOnce(SPEC);
    render(<SpecTab />);
    await flush();
    expect(screen.getByRole('alert')).toBeInTheDocument();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10000);
    });
    expect(fetchStackSpec).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
