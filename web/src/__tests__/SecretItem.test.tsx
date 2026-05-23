import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { SecretItem, type SecretItemProps } from '../components/vault/SecretItem';
import type { Consumer, Variable } from '../lib/api';

const baseVariable: Variable = {
  key: 'GITHUB_TOKEN',
  type: 'string',
  is_secret: true,
};

function renderItem(overrides: Partial<SecretItemProps> = {}) {
  const props: SecretItemProps = {
    secret: baseVariable,
    isEditing: false,
    editValue: '',
    showEditValue: false,
    onReveal: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    onEditValueChange: vi.fn(),
    onEditToggleShow: vi.fn(),
    onEditSave: vi.fn(),
    onEditCancel: vi.fn(),
    sets: [],
    onAssignSet: vi.fn(),
    ...overrides,
  };
  return render(<SecretItem {...props} />);
}

describe('SecretItem — usage badge', () => {
  it('renders no badge when there are no consumers', () => {
    renderItem({ consumers: [] });
    expect(screen.queryByRole('button', { name: /used by/i })).toBeNull();
  });

  it('renders a "Used by N" badge with the consumer count', () => {
    const consumers: Consumer[] = [
      { kind: 'mcp-server', name: 'github', field: 'env.GITHUB_TOKEN' },
      { kind: 'resource', name: 'postgres', field: 'env.GITHUB_TOKEN' },
    ];
    renderItem({ consumers });
    expect(
      screen.getByRole('button', { name: /used by 2 consumers/i }),
    ).toBeInTheDocument();
  });

  it('reveals the consumer list when the badge is clicked, without expanding the row', () => {
    const consumers: Consumer[] = [
      { kind: 'mcp-server', name: 'github', field: 'env.GITHUB_TOKEN' },
    ];
    renderItem({ consumers });

    // Consumer detail and the row's expanded "Value" section are both hidden initially.
    expect(screen.queryByText(/github · env\.GITHUB_TOKEN/)).toBeNull();
    expect(screen.queryByText('Value')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: /used by 1 consumer/i }));

    // The drill-down appears...
    expect(
      screen.getByText(/github · env\.GITHUB_TOKEN/),
    ).toBeInTheDocument();
    // ...but the row's expand-only "Value" section did NOT open.
    expect(screen.queryByText('Value')).toBeNull();
  });

  it('navigates when a server/resource consumer is clicked', () => {
    const onConsumerClick = vi.fn();
    const consumers: Consumer[] = [
      { kind: 'mcp-server', name: 'github', field: 'env.GITHUB_TOKEN' },
    ];
    renderItem({ consumers, onConsumerClick });

    fireEvent.click(screen.getByRole('button', { name: /used by 1 consumer/i }));
    fireEvent.click(screen.getByRole('button', { name: /go to github/i }));

    expect(onConsumerClick).toHaveBeenCalledWith(consumers[0]);
  });

  it('renders non-navigable consumers (gateway/network) as plain text, not links', () => {
    const onConsumerClick = vi.fn();
    const consumers: Consumer[] = [
      { kind: 'gateway', field: 'auth.token' },
    ];
    renderItem({ consumers, onConsumerClick });

    fireEvent.click(screen.getByRole('button', { name: /used by 1 consumer/i }));

    expect(screen.getByText(/gateway · auth\.token/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /go to/i })).toBeNull();
  });

  it('collapses the consumer list on a second badge click', () => {
    const consumers: Consumer[] = [
      { kind: 'mcp-server', name: 'github', field: 'env.GITHUB_TOKEN' },
    ];
    renderItem({ consumers });
    const badge = screen.getByRole('button', { name: /used by 1 consumer/i });

    fireEvent.click(badge);
    expect(screen.getByText(/github · env\.GITHUB_TOKEN/)).toBeInTheDocument();
    fireEvent.click(badge);
    expect(screen.queryByText(/github · env\.GITHUB_TOKEN/)).toBeNull();
  });
});

describe('SecretItem — secret generator (edit mode)', () => {
  it('offers the generator while editing a string variable', () => {
    renderItem({ isEditing: true });
    expect(
      screen.getByRole('button', { name: 'Generate value' }),
    ).toBeInTheDocument();
  });

  it('hides the generator for non-string variables', () => {
    renderItem({
      isEditing: true,
      secret: { key: 'PORT', type: 'number', is_secret: false },
    });
    expect(screen.queryByRole('button', { name: 'Generate value' })).toBeNull();
  });

  it('writes a generated value and reveals it via the toggle', () => {
    const onEditValueChange = vi.fn();
    const onEditToggleShow = vi.fn();
    renderItem({
      isEditing: true,
      showEditValue: false,
      onEditValueChange,
      onEditToggleShow,
    });

    fireEvent.click(screen.getByRole('button', { name: 'Generate value' }));
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));

    expect(onEditValueChange).toHaveBeenCalledTimes(1);
    expect(onEditValueChange.mock.calls[0][0]).toHaveLength(24);
    // Value was masked, so the reveal toggle is fired exactly once.
    expect(onEditToggleShow).toHaveBeenCalledTimes(1);
  });
});
