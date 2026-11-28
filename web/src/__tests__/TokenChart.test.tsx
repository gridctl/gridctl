import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { TokenChart } from '../components/metrics/metricsShared';
import { FLEET_TOKEN_CATEGORY } from '../components/metrics/metricsData';
import type { TokenMetricsResponse } from '../types';

const metricsFixture: TokenMetricsResponse = {
  range: '7d',
  interval: '1h',
  data_points: [
    { timestamp: '2026-11-20T09:00:00Z', input_tokens: 1400, output_tokens: 600, total_tokens: 2000 },
    { timestamp: '2026-11-21T09:00:00Z', input_tokens: 250, output_tokens: 12500, total_tokens: 12750 },
  ],
  per_server: {},
};

const fleetRows = [
  { time: '2026-11-20T09:00:00Z', 'Input Tokens': 1400, 'Output Tokens': 600 },
  { time: '2026-11-21T09:00:00Z', 'Input Tokens': 250, 'Output Tokens': 12500 },
];

describe('TokenChart', () => {
  it('toggles between the chart and an accessible exact-value series table', () => {
    render(<TokenChart data={fleetRows} metricsData={metricsFixture} />);

    // Chart view is active by default: toggle has no aria-pressed and no table is mounted.
    const toTable = screen.getByRole('button', { name: 'View as table' });
    expect(toTable).toHaveAttribute('type', 'button');
    expect(toTable).not.toHaveAttribute('aria-pressed');
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    expect(screen.getByText(/2 points · 1h interval/)).toBeInTheDocument();

    // Switch to table view.
    fireEvent.click(toTable);

    const toChart = screen.getByRole('button', { name: 'View as chart' });
    expect(toChart).not.toHaveAttribute('aria-pressed');
    expect(screen.queryByRole('button', { name: 'View as table' })).not.toBeInTheDocument();

    const table = screen.getByRole('table', { name: 'Token Usage Over Time' });
    const headers = within(table).getAllByRole('columnheader');
    expect(headers.map((h) => h.textContent)).toEqual(['Time', 'Input Tokens', 'Output Tokens']);
    for (const h of headers) {
      expect(h).toHaveAttribute('scope', 'col');
      expect(h).toHaveClass('sticky', 'top-0', 'bg-surface-elevated');
      expect(h.className).not.toMatch(/\bbg-\S+\//);
    }

    const rowHeaders = within(table).getAllByRole('rowheader');
    expect(rowHeaders).toHaveLength(2);
    for (const rh of rowHeaders) {
      expect(rh).toHaveAttribute('scope', 'row');
      expect(rh.textContent).toContain('2026');
      expect(rh.textContent).toContain('–');
      expect(rh.textContent).not.toContain('interval');
    }
    expect(rowHeaders[0].textContent).not.toBe(rowHeaders[1].textContent);

    const bodyRows = within(table).getAllByRole('row').slice(1);
    expect(bodyRows).toHaveLength(2);
    const firstCells = within(bodyRows[0]).getAllByRole('cell');
    expect(firstCells.map((c) => c.textContent)).toEqual([(1400).toLocaleString(), (600).toLocaleString()]);
    for (const cell of firstCells) {
      expect(cell.className).toContain('text-right');
      expect(cell.className).toContain('tabular-nums');
    }
    const secondCells = within(bodyRows[1]).getAllByRole('cell');
    expect(secondCells.map((c) => c.textContent)).toEqual([(250).toLocaleString(), (12500).toLocaleString()]);

    // Switch back to chart view.
    fireEvent.click(toChart);
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'View as table' })).not.toHaveAttribute('aria-pressed');
  });

  it('includes the Fleet Total column for focused-server charts', () => {
    const focusedRows = [
      {
        time: '2026-11-20T09:00:00Z',
        'Input Tokens': 300,
        'Output Tokens': 150,
        [FLEET_TOKEN_CATEGORY]: 2000,
      },
    ];
    render(
      <TokenChart
        data={focusedRows}
        metricsData={metricsFixture}
        subject="github"
        categories={['Input Tokens', 'Output Tokens', FLEET_TOKEN_CATEGORY]}
        colors={['teal', 'amber', 'gray']}
        chartType="default"
        dashedCategories={[FLEET_TOKEN_CATEGORY]}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'View as table' }));

    const table = screen.getByRole('table', { name: 'github · Token Usage' });
    expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual([
      'Time',
      'Input Tokens',
      'Output Tokens',
      'Fleet Total',
    ]);
    const cells = within(within(table).getAllByRole('row')[1]).getAllByRole('cell');
    expect(cells.map((c) => c.textContent)).toEqual([
      (300).toLocaleString(),
      (150).toLocaleString(),
      (2000).toLocaleString(),
    ]);
  });

  it('shows headers and an empty-window row when data is empty', () => {
    render(
      <TokenChart
        data={[]}
        metricsData={{ range: '30m', interval: '1m', data_points: [], per_server: {} }}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'View as table' }));

    const table = screen.getByRole('table', { name: 'Token Usage Over Time' });
    expect(within(table).getAllByRole('columnheader').map((h) => h.textContent)).toEqual([
      'Time',
      'Input Tokens',
      'Output Tokens',
    ]);
    const emptyCell = within(table).getByRole('cell', { name: 'No data in this window.' });
    expect(emptyCell).toHaveAttribute('colspan', '3');
  });
});
