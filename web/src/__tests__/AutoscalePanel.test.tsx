import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';

import { AutoscalePanel, dwellPhrase } from '../components/status/AutoscalePanel';
import type { AutoscaleStatus } from '../types';
import type { AutoscaleSample, AutoscaleDecision } from '../stores/useStackStore';

function makeStatus(overrides: Partial<AutoscaleStatus> = {}): AutoscaleStatus {
  return {
    min: 1,
    max: 5,
    current: 2,
    target: 3,
    targetInFlight: 10,
    medianInFlight: 14,
    lastDecision: 'up',
    ...overrides,
  };
}

function makeHistory(values: Array<Pick<AutoscaleSample, 'current' | 'target' | 'medianInFlight'>>): AutoscaleSample[] {
  const base = 1_700_000_000_000;
  return values.map((v, i) => ({ t: base + i * 3000, ...v }));
}

describe('AutoscalePanel', () => {
  it('renders the headline with current, target, and range', () => {
    render(<AutoscalePanel status={makeStatus()} history={[]} decisions={[]} />);
    expect(screen.getByText('Current')).toBeInTheDocument();
    expect(screen.getByText('/ Target')).toBeInTheDocument();
    expect(screen.getByText('· Range')).toBeInTheDocument();
    expect(screen.getByText('1–5')).toBeInTheDocument();
  });

  it('renders the scaling-up dwell phrase', () => {
    render(<AutoscalePanel status={makeStatus({ lastDecision: 'up' })} history={[]} decisions={[]} />);
    expect(screen.getByTestId('autoscale-dwell')).toHaveTextContent(
      'Scaling up · median in-flight 14, target 10',
    );
  });

  it('renders the scaling-down dwell phrase', () => {
    render(
      <AutoscalePanel
        status={makeStatus({ lastDecision: 'down', medianInFlight: 2 })}
        history={[]}
        decisions={[]}
      />,
    );
    expect(screen.getByTestId('autoscale-dwell')).toHaveTextContent(
      'Scaling down · median in-flight 2 below target 10',
    );
  });

  it('renders the stable dwell phrase when noop and current==target', () => {
    render(
      <AutoscalePanel
        status={makeStatus({ lastDecision: 'noop', current: 3, target: 3, medianInFlight: 8 })}
        history={[]}
        decisions={[]}
      />,
    );
    expect(screen.getByTestId('autoscale-dwell')).toHaveTextContent(
      'Stable · median in-flight 8, target 10',
    );
  });

  it('renders the idle-to-zero dwell phrase', () => {
    render(
      <AutoscalePanel
        status={makeStatus({ lastDecision: 'noop', current: 0, target: 0, idleToZero: true })}
        history={[]}
        decisions={[]}
      />,
    );
    expect(screen.getByTestId('autoscale-dwell')).toHaveTextContent('Idle · scaled to zero');
  });

  it('renders a placeholder when no history is available', () => {
    render(<AutoscalePanel status={makeStatus()} history={[]} decisions={[]} />);
    expect(screen.getByText('Collecting samples…')).toBeInTheDocument();
  });

  it('renders sparkline paths with correct M/L shape for current and target', () => {
    const history = makeHistory([
      { current: 1, target: 2, medianInFlight: 3 },
      { current: 2, target: 3, medianInFlight: 8 },
      { current: 3, target: 3, medianInFlight: 9 },
    ]);
    render(<AutoscalePanel status={makeStatus()} history={history} decisions={[]} />);
    const svg = screen.getByTestId('autoscale-sparkline');
    expect(svg).toBeInTheDocument();
    const currentPath = screen.getByTestId('autoscale-sparkline-current');
    const d = currentPath.getAttribute('d') ?? '';
    // Three samples → one M and two L commands.
    expect(d).toMatch(/^M /);
    expect(d.split('L').length - 1).toBe(2);
  });

  it('decision feed opens on click and renders entries', () => {
    const decisions: AutoscaleDecision[] = [
      { t: 1_700_000_000_000, kind: 'up', from: 1, to: 2, reason: 'test' },
      { t: 1_700_000_010_000, kind: 'down', from: 2, to: 1, reason: 'test' },
    ];
    render(<AutoscalePanel status={makeStatus()} history={[]} decisions={decisions} />);
    const toggle = screen.getByRole('button', { name: /Recent Decisions/i });
    fireEvent.click(toggle);
    const feed = screen.getByTestId('autoscale-decision-feed');
    expect(feed).toBeInTheDocument();
    expect(feed.textContent).toMatch(/1→2/);
    expect(feed.textContent).toMatch(/2→1/);
  });

  it('decision feed is collapsed by default', () => {
    render(<AutoscalePanel status={makeStatus()} history={[]} decisions={[]} />);
    expect(screen.queryByTestId('autoscale-decision-feed')).not.toBeInTheDocument();
  });
});

describe('dwellPhrase', () => {
  it('returns holding copy when current and target diverge on noop', () => {
    const s = makeStatus({ lastDecision: 'noop', current: 1, target: 3 });
    expect(dwellPhrase(s)).toBe('Holding · current 1, target 3');
  });
});
