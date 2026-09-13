import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import '@testing-library/jest-dom';
import type { SecurityReport } from '../types';

const report: SecurityReport = {
  schema_version: 'gridctl.security-report.v1',
  generated_at: '2026-09-13T12:00:00Z',
  source: { kind: 'gateway', display: 'localhost:8180' },
  coverage: {
    status: 'partial',
    predicates_total: 19,
    predicates_evaluated: 19,
    predicates_unknown: 4,
    included_scopes: [],
    excluded_scopes: [],
    unknown_gaps: 4,
  },
  checks: [
    {
      id: 'pin.schema.continuity.fetch',
      predicate: 'pin.schema.continuity',
      subject: { kind: 'server', name: 'fetch' },
      outcome: 'fail',
      reason_code: 'pin_status_drift',
      explanation: 'Stored pin status is drift for fetch.',
      evidence: { basis: 'declared', availability: 'available', freshness: 'unknown' },
      limitations: ['Known negative observation from the pin store.'],
      actions: [{ id: 'view_pins', label: 'View pins', path: '/pins?server=fetch' }],
    },
    {
      id: 'source.declared.fetch',
      predicate: 'source.declared',
      subject: { kind: 'server', name: 'fetch' },
      outcome: 'unknown',
      reason_code: 'source_undeclared',
      explanation: 'Authored source identity for fetch is unavailable.',
      evidence: { basis: 'declared', availability: 'unavailable', freshness: 'unknown' },
    },
    {
      id: 'gateway.auth.declared.gateway',
      predicate: 'gateway.auth.declared',
      subject: { kind: 'gateway', name: 'gateway' },
      outcome: 'pass',
      reason_code: 'auth_declared',
      explanation: 'Gateway auth is declared. Declaration is not route enforcement.',
      evidence: { basis: 'declared', availability: 'available', freshness: 'unknown' },
    },
  ],
  limitations: ['Exit zero means no established failures among documented fail predicates, not that the stack is secure.'],
  fail_count: 1,
  warn_count: 0,
  unknown_count: 1,
  not_applicable_count: 0,
  pass_count: 1,
};

const fetchSecurityReport = vi.fn();

vi.mock('../lib/api', () => ({
  fetchSecurityReport: (...args: unknown[]) => fetchSecurityReport(...args),
}));

import { SecurityEvidence } from '../components/sidebar/SecurityEvidence';

describe('SecurityEvidence', () => {
  beforeEach(() => {
    fetchSecurityReport.mockReset();
    fetchSecurityReport.mockResolvedValue(report);
  });

  it('shows claim, outcome, basis, and limitation together', async () => {
    render(
      <MemoryRouter>
        <SecurityEvidence scope={{ server: 'fetch' }} />
      </MemoryRouter>,
    );
    expect(await screen.findByText(/fail: pin.schema.continuity/)).toBeInTheDocument();
    expect(screen.getByText(/Stored pin status is drift for fetch/)).toBeInTheDocument();
    expect(screen.getAllByText(/Basis declared/).length).toBeGreaterThan(0);
    expect(screen.getByText(/Known negative observation from the pin store/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'View pins' })).toHaveAttribute('href', '/pins?server=fetch');
    expect(screen.queryByRole('button', { name: /approve/i })).not.toBeInTheDocument();
    expect(screen.getByText(/2026-09-13T12:00:00Z/)).toBeInTheDocument();
  });

  it('keeps unknown details collapsed and keyboard-accessible', async () => {
    render(
      <MemoryRouter>
        <SecurityEvidence scope={{ server: 'fetch' }} />
      </MemoryRouter>,
    );
    const unknown = await screen.findByText(/unknown: source.declared/);
    expect(unknown.closest('details')).not.toHaveAttribute('open');
    fireEvent.click(unknown);
    expect(unknown.closest('details')).toHaveAttribute('open');
  });

  it('ignores arbitrary action destinations and canary text from unused fields', async () => {
    fetchSecurityReport.mockResolvedValue({
      ...report,
      checks: [{
        ...report.checks[0],
        actions: [
          { id: 'view_pins', label: 'View pins', path: '/pins?server=fetch' },
          { id: 'open', label: 'Open', path: 'https://evil.example/canary-link' },
        ],
      }],
    });
    render(
      <MemoryRouter>
        <SecurityEvidence scope={{ server: 'fetch' }} />
      </MemoryRouter>,
    );
    await screen.findByRole('link', { name: 'View pins' });
    expect(screen.queryByText('canary-link')).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Open' })).not.toBeInTheDocument();
  });

  it('refresh keeps the control and does not imply re-observation', async () => {
    render(
      <MemoryRouter>
        <SecurityEvidence scope="gateway" />
      </MemoryRouter>,
    );
    const button = await screen.findByRole('button', { name: 'Refresh report' });
    expect(screen.getByText(/does not re-observe/)).toBeInTheDocument();
    fireEvent.click(button);
    await waitFor(() => expect(fetchSecurityReport).toHaveBeenCalledTimes(2));
    expect(button).toBeInTheDocument();
  });

  it('surfaces source errors without an approval action', async () => {
    fetchSecurityReport.mockRejectedValue(new Error('nope'));
    render(
      <MemoryRouter>
        <SecurityEvidence scope="gateway" />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('Security evidence report is unavailable.');
    expect(screen.queryByRole('button', { name: /approve/i })).not.toBeInTheDocument();
  });
});
