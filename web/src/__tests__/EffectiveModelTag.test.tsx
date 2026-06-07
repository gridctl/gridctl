import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { EffectiveModelTag } from '../components/pricing/EffectiveModelTag';
import { topShares, mixedTooltip, sharePct } from '../components/pricing/effectiveModel';
import type { ModelShare } from '../types';

describe('effectiveModel helpers', () => {
  it('sharePct rounds to whole percent', () => {
    expect(sharePct(0.823)).toBe('82%');
    expect(sharePct(1)).toBe('100%');
  });

  it('topShares groups the tail into "other"', () => {
    const models: ModelShare[] = [
      { model: 'a', cost_usd: 5, share: 0.5 },
      { model: 'b', cost_usd: 3, share: 0.3 },
      { model: 'c', cost_usd: 1, share: 0.1 },
      { model: 'd', cost_usd: 0.6, share: 0.06 },
      { model: 'e', cost_usd: 0.4, share: 0.04 },
    ];
    const grouped = topShares(models, 3);
    expect(grouped).toHaveLength(4);
    expect(grouped[3].model).toBe('other');
    expect(grouped[3].share).toBeCloseTo(0.1, 5);
  });

  it('mixedTooltip names models and carries the honesty note', () => {
    const tip = mixedTooltip({
      provenance: 'mixed',
      model: 'claude-opus-4-7',
      share: 0.8,
      models: [
        { model: 'claude-opus-4-7', cost_usd: 0.8, share: 0.8 },
        { model: 'claude-haiku-4-5', cost_usd: 0.2, share: 0.2 },
      ],
    });
    expect(tip).toContain('claude-opus-4-7 80%');
    expect(tip).toContain('not observed client behavior');
  });
});

describe('EffectiveModelTag', () => {
  it('renders mixed with dominant model and share and fires onClick', () => {
    const onClick = vi.fn();
    render(
      <EffectiveModelTag
        effective={{ provenance: 'mixed', model: 'claude-opus-4-7', share: 0.82, models: [] }}
        onClick={onClick}
      />,
    );
    expect(screen.getByText('claude-opus-4-7')).toBeInTheDocument();
    expect(screen.getByText('· 82% · mixed')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button'));
    expect(onClick).toHaveBeenCalled();
  });

  it('renders none as an unpriced tag', () => {
    render(<EffectiveModelTag effective={{ provenance: 'none' }} />);
    expect(screen.getByText('unpriced')).toBeInTheDocument();
  });

  it('exposes an accessible label that does not rely on color', () => {
    render(
      <EffectiveModelTag
        effective={{ provenance: 'mixed', model: 'claude-opus-4-7', share: 0.82, models: [] }}
        onClick={vi.fn()}
      />,
    );
    const btn = screen.getByRole('button');
    expect(btn.getAttribute('aria-label') ?? '').toMatch(/Mixed pricing/);
  });
});
