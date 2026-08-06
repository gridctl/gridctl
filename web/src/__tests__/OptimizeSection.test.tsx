import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { OptimizeSection } from '../components/sidebar/OptimizeSection';
import type { OptimizeFinding } from '../types';

const findings: OptimizeFinding[] = vi.hoisted(() => [
  {
    id: 'unused-tool-github-list_repos',
    heuristic: 'unused_tool',
    severity: 'info',
    title: 'Unused tool: github/list_repos',
    summary: 'Not called in the lookback window.',
    server: 'github',
    tool: 'list_repos',
    remediation: '# tools: filter',
    detected_at: '2026-07-13T00:00:00Z',
  },
  {
    id: 'schema-overhead-github',
    heuristic: 'schema_overhead',
    severity: 'info',
    title: 'Schema overhead exceeds tool value: github',
    summary: 'Schemas outweigh recorded output tokens.',
    server: 'github',
    remediation: '# prune tools',
    detected_at: '2026-07-13T00:00:00Z',
  },
]);

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchOptimizeReport: vi.fn().mockResolvedValue({
      findings,
      health_score: 88,
      generated_at: '2026-07-13T00:00:00Z',
    }),
  };
});

describe('OptimizeSection', () => {
  it('lists findings with the health score and no dollar framing', async () => {
    render(<OptimizeSection />);
    expect(await screen.findByText('Unused tool: github/list_repos')).toBeInTheDocument();
    expect(screen.getByText('Schema overhead exceeds tool value: github')).toBeInTheDocument();
    expect(screen.getByText('88/100')).toBeInTheDocument();
    expect(screen.queryByText(/\/wk/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\$/)).not.toBeInTheDocument();
  });

  it('expands a finding to its remediation snippet', async () => {
    render(<OptimizeSection />);
    fireEvent.click(await screen.findByText('Unused tool: github/list_repos'));
    expect(await screen.findByText('# tools: filter')).toBeInTheDocument();
  });
});
