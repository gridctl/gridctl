import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import {
  governanceNeedsAttention,
  governanceSummary,
  originLabel,
} from '../lib/skillGovernance';
import { SkillGovernanceBadge } from '../components/registry/SkillGovernanceBadge';
import type { SkillGovernance } from '../types';

describe('governanceNeedsAttention', () => {
  it('is quiet without governance or on a clean pin', () => {
    expect(governanceNeedsAttention(undefined)).toBe(false);
    expect(governanceNeedsAttention({ pinStatus: 'pinned', source: 'local' })).toBe(false);
  });

  it('excludes info-tier findings from attention', () => {
    expect(
      governanceNeedsAttention({ pinStatus: 'pinned', findingsCount: 3, maxFindingSeverity: 'info' }),
    ).toBe(false);
  });

  it('flags pin drift, alert findings, and policy denial', () => {
    expect(governanceNeedsAttention({ pinStatus: 'drift' })).toBe(true);
    expect(
      governanceNeedsAttention({ pinStatus: 'pinned', findingsCount: 1, maxFindingSeverity: 'warn' }),
    ).toBe(true);
    expect(governanceNeedsAttention({ policyDenied: true })).toBe(true);
  });
});

describe('governanceSummary', () => {
  it('uses "pin drift" wording and names the policy rule', () => {
    const g: SkillGovernance = {
      pinStatus: 'drift',
      findingsCount: 2,
      maxFindingSeverity: 'warn',
      policyDenied: true,
      policyRule: '*refund*',
    };
    expect(governanceSummary(g)).toBe(
      'Pin drift · 2 advisory findings (warn) · Blocked by policy (rule: *refund*)',
    );
  });
});

describe('originLabel', () => {
  it('is factual, never a trust judgment', () => {
    expect(originLabel(undefined)).toBe('Local');
    expect(originLabel({ source: 'local' })).toBe('Local');
    expect(
      originLabel({ source: 'git', origin: { repo: 'https://github.com/acme/s.git', ref: 'main' } }),
    ).toBe('Imported: https://github.com/acme/s.git@main');
  });
});

describe('SkillGovernanceBadge', () => {
  it('renders nothing for a quiet skill', () => {
    const { container } = render(
      <SkillGovernanceBadge governance={{ pinStatus: 'pinned', source: 'local' }} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders one labeled icon when attention is needed', () => {
    render(<SkillGovernanceBadge governance={{ pinStatus: 'drift' }} />);
    expect(screen.getByRole('img', { name: 'Pin drift' })).toBeInTheDocument();
  });

  it('is non-interactive (no nested button inside clickable rows)', () => {
    render(<SkillGovernanceBadge governance={{ policyDenied: true, policyRule: 'x' }} />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });
});
