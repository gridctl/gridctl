import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import '@testing-library/jest-dom';

type TooltipRenderFn = (args: {
  active?: boolean;
  label?: string;
  payload?: Array<{ dataKey: string; value: number; payload: Record<string, unknown>; type?: string }>;
}) => React.ReactNode;

let latestTooltipContent: TooltipRenderFn | undefined;

// ResponsiveContainer relies on ResizeObserver measurements that jsdom does
// not compute. Replace only ResponsiveContainer so the real Recharts axes and
// portaled tick labels render at a fixed size in jsdom, and observe Tooltip's
// content render prop so formatter/callback contracts can be asserted directly.
vi.mock('recharts', async (importActual) => {
  const actual = await importActual<typeof import('recharts')>();
  const ActualTooltip = actual.Tooltip;
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactElement }) =>
      React.isValidElement(children)
        ? React.cloneElement(children as React.ReactElement<{ width?: number; height?: number }>, {
            width: 600,
            height: 240,
          })
        : children,
    Tooltip: (props: React.ComponentProps<typeof ActualTooltip>) => {
      if (typeof props.content === 'function') {
        latestTooltipContent = props.content as unknown as TooltipRenderFn;
      }
      return <ActualTooltip {...props} />;
    },
  };
});

import { AreaChart, Legend } from '../components/chart/AreaChart';
import { MetricsInspector } from '../components/metrics/MetricsInspector';
import { formatChartTick, formatChartTooltipTitle } from '../components/metrics/metricsData';

beforeEach(() => {
  latestTooltipContent = undefined;
});

describe('AreaChart Legend', () => {
  it('renders interactive categories as buttons with aria-pressed and stopPropagation', () => {
    const onClick = vi.fn();
    const parentClick = vi.fn();
    render(
      <div onClick={parentClick}>
        <Legend
          categories={['Input Tokens', 'Output Tokens']}
          colors={['teal', 'amber']}
          onClickLegendItem={onClick}
          activeLegend="Input Tokens"
        />
      </div>,
    );

    const inputBtn = screen.getByRole('button', { name: 'Input Tokens' });
    const outputBtn = screen.getByRole('button', { name: 'Output Tokens' });
    expect(inputBtn).toHaveAttribute('type', 'button');
    expect(inputBtn).toHaveAttribute('aria-pressed', 'true');
    expect(outputBtn).toHaveAttribute('aria-pressed', 'false');
    expect(inputBtn.className).toContain('focus-visible:outline');

    fireEvent.click(outputBtn);
    expect(onClick).toHaveBeenCalledWith('Output Tokens', 'amber');
    expect(parentClick).not.toHaveBeenCalled();
  });

  it('renders static legend items without button roles when no click handler is passed', () => {
    render(
      <Legend
        categories={['Input Tokens', 'Output Tokens']}
        colors={['teal', 'amber']}
      />,
    );

    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(screen.getByText('Input Tokens')).toBeInTheDocument();
    expect(screen.getByText('Output Tokens')).toBeInTheDocument();
  });
});

describe('AreaChart ticks and formatters', () => {
  const sampleData = [
    { time: '2026-11-20T09:00:00Z', 'Input Tokens': 100, 'Output Tokens': 50 },
    { time: '2026-11-21T09:00:00Z', 'Input Tokens': 200, 'Output Tokens': 80 },
  ];

  it('places text-xs directly on every axis tick text element and applies indexFormatter', () => {
    const { container } = render(
      <AreaChart
        data={sampleData}
        index="time"
        categories={['Input Tokens', 'Output Tokens']}
        colors={['teal', 'amber']}
        indexFormatter={(v) => formatChartTick(v, '7d', 'en-US', 'UTC')}
      />,
    );

    const ticks = Array.from(container.querySelectorAll('.recharts-cartesian-axis-tick-value'));
    expect(ticks.length).toBeGreaterThan(0);
    for (const tick of ticks) {
      expect(tick.getAttribute('class')).toContain('text-xs');
    }

    const xTicks = Array.from(container.querySelectorAll('.recharts-xAxis-tick-labels .recharts-cartesian-axis-tick-value'));
    expect(xTicks.length).toBe(2);
    expect(xTicks[0].textContent).toContain('Nov 20');
    expect(xTicks[1].textContent).toContain('Nov 21');
    // Static legend stays button-free when onValueChange is omitted.
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('leaves ticks and tooltip labels unchanged when no formatter props are passed', () => {
    const { container } = render(
      <AreaChart
        data={sampleData}
        index="time"
        categories={['Input Tokens', 'Output Tokens']}
        colors={['teal', 'amber']}
      />,
    );

    const xTicks = Array.from(container.querySelectorAll('.recharts-xAxis-tick-labels .recharts-cartesian-axis-tick-value'));
    expect(xTicks.map((t) => t.textContent)).toEqual(['2026-11-20T09:00:00Z', '2026-11-21T09:00:00Z']);

    expect(latestTooltipContent).toBeTypeOf('function');
    const tip = render(
      <>{latestTooltipContent!({
        active: true,
        label: '2026-11-20T09:00:00Z',
        payload: [{ dataKey: 'Input Tokens', value: 100, payload: sampleData[0] }],
      })}</>,
    );
    expect(within(tip.container).getByText('2026-11-20T09:00:00Z')).toBeInTheDocument();
  });

  it('formats ChartTooltip and customTooltip labels while keeping tooltipCallback on the raw label', () => {
    const tooltipCallback = vi.fn();
    render(
      <AreaChart
        data={sampleData}
        index="time"
        categories={['Input Tokens', 'Output Tokens']}
        colors={['teal', 'amber']}
        tooltipLabelFormatter={(v) => formatChartTooltipTitle(v, '1h', 'en-US', 'UTC')}
        tooltipCallback={tooltipCallback}
      />,
    );

    tooltipCallback.mockClear();
    const payload = [{ dataKey: 'Input Tokens', value: 100, payload: sampleData[0] }];
    const rendered = render(
      <>{latestTooltipContent!({ active: true, label: '2026-11-20T09:00:00Z', payload })}</>,
    );
    expect(within(rendered.container).getByText(/Fri, Nov 20, 2026.*09:00.*10:00.*1h interval/)).toBeInTheDocument();
    expect(tooltipCallback).toHaveBeenCalledTimes(1);
    expect(tooltipCallback).toHaveBeenCalledWith(
      expect.objectContaining({ active: true, label: '2026-11-20T09:00:00Z' }),
    );

    // Repeating the same raw label does not re-invoke tooltipCallback.
    latestTooltipContent!({ active: true, label: '2026-11-20T09:00:00Z', payload });
    expect(tooltipCallback).toHaveBeenCalledTimes(1);

    // Custom tooltip also receives the formatted label.
    const seenLabels: string[] = [];
    const CustomTip = ({ label }: { active: boolean | undefined; payload: unknown[]; label: string }) => {
      seenLabels.push(label);
      return <div>{label}</div>;
    };
    render(
      <AreaChart
        data={sampleData}
        index="time"
        categories={['Input Tokens', 'Output Tokens']}
        colors={['teal', 'amber']}
        tooltipLabelFormatter={(v) => formatChartTooltipTitle(v, '1m', 'en-US', 'UTC')}
        customTooltip={CustomTip}
      />,
    );
    render(<>{latestTooltipContent!({ active: true, label: '2026-11-20T09:00:00Z', payload })}</>);
    expect(seenLabels[0]).toMatch(/Fri, Nov 20, 2026.*09:00.*09:01.*1m interval/);
  });

  it('formats MetricsInspector sparkline ticks with the passed range', () => {
    const { container } = render(
      <MemoryRouter>
        <MetricsInspector
          scope="servers"
          row={{ name: 'github', input: 300, output: 130, total: 430 }}
          onClose={vi.fn()}
          range="7d"
          interval="1h"
          tokenPoints={[
            { timestamp: '2026-11-20T09:00:00Z', input_tokens: 100, output_tokens: 50, total_tokens: 150 },
            { timestamp: '2026-11-21T09:00:00Z', input_tokens: 200, output_tokens: 80, total_tokens: 280 },
          ]}
        />
      </MemoryRouter>,
    );

    const ticks = Array.from(container.querySelectorAll('.recharts-cartesian-axis-tick-value'));
    expect(ticks.length).toBeGreaterThan(0);
    for (const tick of ticks) {
      expect(tick.getAttribute('class')).toContain('text-xs');
    }
    // Raw ISO timestamps must not appear as tick labels.
    expect(ticks.every((t) => !t.textContent?.includes('T09:00:00Z'))).toBe(true);

    // Sparkline tooltip also formats the full bucket and interval suffix.
    expect(latestTooltipContent).toBeTypeOf('function');
    const tip = render(
      <>{latestTooltipContent!({
        active: true,
        label: '2026-11-20T09:00:00Z',
        payload: [
          {
            dataKey: 'Input Tokens',
            value: 100,
            payload: { time: '2026-11-20T09:00:00Z', 'Input Tokens': 100, 'Output Tokens': 50 },
          },
        ],
      })}</>,
    );
    expect(within(tip.container).getByText(/2026.*–.*1h interval/)).toBeInTheDocument();
  });
});
