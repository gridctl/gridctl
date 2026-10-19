import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ProviderReferenceSection } from '../components/vault/ProviderReferences';
import type { SecretReferenceInfo } from '../lib/api';

function ref(status: SecretReferenceInfo['status'], error: string | null = null): SecretReferenceInfo {
  return {
    key: 'secret:op/a/b',
    provider: 'op',
    path: 'a/b',
    consumers: [{ kind: 'mcp-server', name: 'netbox', field: 'env.TOKEN' }],
    status,
    error,
    last_resolved: status === 'resolved' ? '2026-10-19T12:00:00Z' : null,
  };
}

describe('ProviderReferenceSection', () => {
  it.each(['resolved', 'error', 'pending'] as const)('renders a %s chip and no value control', (status) => {
    render(
      <ProviderReferenceSection
        references={[ref(status, status === 'error' ? 'secret op/a/b: provider exited 1' : null)]}
      />,
    );
    expect(screen.getByText(status)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /reveal|copy|edit|delete|mask/i })).toBeNull();
    expect(screen.queryByDisplayValue(/secret/i)).toBeNull();
  });

  it('opens the inspector without a value', () => {
    render(
      <ProviderReferenceSection references={[ref('error', 'secret op/a/b: provider exited 1')]} />,
    );
    fireEvent.click(screen.getByRole('option'));
    expect(screen.getByLabelText('Provider reference inspector')).toHaveTextContent('provider exited 1');
    expect(screen.getByLabelText('Provider reference inspector')).toHaveTextContent('a/b');
    expect(screen.queryByText(/super-secret/)).toBeNull();
  });
});
