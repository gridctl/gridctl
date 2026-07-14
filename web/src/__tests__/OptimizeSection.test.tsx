import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
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
    impact_usd_per_week: 4.2,
    remediation: '# tools: filter',
    detected_at: '2026-07-13T00:00:00Z',
  },
  {
    id: 'expensive-model-cheap-task-github',
    heuristic: 'expensive_model_on_cheap_task',
    severity: 'info',
    title: 'Expensive model on cheap task: github',
    summary: 'Simple-lookup pattern on an Opus-tier rate.',
    server: 'github',
    impact_usd_per_week: 0,
    remediation: '# pick a smaller model',
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
  it('renders the weekly-dollar chip for a tool finding with real impact', async () => {
    render(<OptimizeSection />);
    expect(await screen.findByText('Unused tool: github/list_repos')).toBeInTheDocument();
    expect(screen.getByText('$4.20/wk')).toBeInTheDocument();
  });

  it('hides the chip for zero-impact findings', async () => {
    render(<OptimizeSection />);
    expect(await screen.findByText('Expensive model on cheap task: github')).toBeInTheDocument();
    // Exactly one chip: the zero-impact finding renders none.
    expect(screen.getAllByText(/\/wk$/)).toHaveLength(1);
  });
});
