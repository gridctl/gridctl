import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ApprovalFields, PinApprovalHistory } from './PinApprovalHistory';
import type { PinApproval } from '../../lib/api';

vi.mock('../ui/Toast', () => ({ showToast: vi.fn(), copyWithToast: vi.fn() }));

const entry: PinApproval = {
  approved_at: '2026-10-02T15:04:05Z',
  approved_by: 'wcollins',
  approved_by_source: 'os_user',
  approved_hash: 'abc123',
  bound: true,
  reason: 'Adds runbook step 4',
  evidence: ['https://example.test/pull/42', 'plan/review/note.md'],
};

describe('PinApprovalHistory', () => {
  it('renders references as escaped text and never as anchors', () => {
    const { container } = render(
      <PinApprovalHistory
        approvals={[
          {
            ...entry,
            evidence: ['https://example.test/pull/42\u202Ehidden', 'plan/review/note.md'],
          },
          { ...entry, approved_at: '2026-09-01T00:00:00Z', reason: 'older' },
        ]}
      />,
    );

    expect(screen.getByText('Last approval')).toBeInTheDocument();
    expect(screen.getByText('History')).toBeInTheDocument();
    expect(container.querySelector('a')).toBeNull();
    expect(screen.queryByRole('link')).toBeNull();
    expect(container.textContent).toContain('https://example.test/pull/42');
    expect(container.textContent).toContain('\\u202e');
  });
});

describe('ApprovalFields', () => {
  it('labels the reason and evidence inputs and switches the reason placeholder', () => {
    const { rerender } = render(
      <ApprovalFields reason="" onReason={() => {}} evidence="" onEvidence={() => {}} reasonRequired={false} />,
    );
    expect(screen.getByLabelText('Approval reason')).toHaveAttribute('placeholder', 'Reason (optional)');
    expect(screen.getByLabelText('Evidence references, one per line')).toBeInTheDocument();
    expect(screen.getByText('Evidence references, one per line')).toBeInTheDocument();

    rerender(<ApprovalFields reason="" onReason={() => {}} evidence="" onEvidence={() => {}} reasonRequired />);
    expect(screen.getByLabelText('Approval reason')).toHaveAttribute(
      'placeholder',
      'Reason (required: advisory findings present)',
    );
  });
});
