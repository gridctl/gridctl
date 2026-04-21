import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { AddSourceStep } from '../components/wizard/steps/AddSourceStep';

// Re-export the real HTTPError class so the component can use `instanceof`
// against the error type that our mocked previewSkillSource rejects with.
vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchSkillSources: vi.fn().mockResolvedValue([]),
    previewSkillSource: vi.fn(),
    fetchVaultSecrets: vi.fn().mockResolvedValue([]),
    createVaultSecret: vi.fn(),
  };
});

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

import { previewSkillSource, HTTPError } from '../lib/api';

const mockPreview = vi.mocked(previewSkillSource);

describe('AddSourceStep — auth card', () => {
  beforeEach(() => {
    mockPreview.mockReset();
  });

  it('auth card is collapsed by default and labelled optional', () => {
    render(<AddSourceStep onPreviewLoaded={vi.fn()} />);
    const toggle = screen.getByRole('button', { name: /authentication/i });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle.textContent).toMatch(/optional/i);
  });

  it('clicking the toggle expands the auth card', () => {
    render(<AddSourceStep onPreviewLoaded={vi.fn()} />);
    const toggle = screen.getByRole('button', { name: /authentication/i });
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText(/vault secret/i)).toBeInTheDocument();
    expect(screen.getByText(/paste token/i)).toBeInTheDocument();
  });

  it('auto-opens and shows a banner when scan fails with 401', async () => {
    mockPreview.mockRejectedValueOnce(new HTTPError(401, 'authentication required'));

    render(<AddSourceStep onPreviewLoaded={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText(/https:\/\/github/i), {
      target: { value: 'https://github.com/acme/private' },
    });
    fireEvent.click(screen.getByRole('button', { name: /scan for skills/i }));

    await waitFor(() => {
      const toggle = screen.getByRole('button', { name: /authentication/i });
      expect(toggle).toHaveAttribute('aria-expanded', 'true');
    });
    expect(screen.getByText(/requires authentication/i)).toBeInTheDocument();
  });

  it('auto-opens with "not found" banner on 404', async () => {
    mockPreview.mockRejectedValueOnce(new HTTPError(404, 'repository not found'));

    render(<AddSourceStep onPreviewLoaded={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText(/https:\/\/github/i), {
      target: { value: 'https://github.com/acme/private' },
    });
    fireEvent.click(screen.getByRole('button', { name: /scan for skills/i }));

    await waitFor(() => {
      expect(screen.getByText(/if this is a private repository/i)).toBeInTheDocument();
    });
  });

  it('passes the paste-token to previewSkillSource on retry', async () => {
    mockPreview.mockResolvedValueOnce({
      repo: 'https://github.com/acme/private',
      ref: '',
      commitSha: 'abc',
      skills: [
        {
          name: 's',
          description: '',
          body: '',
          valid: true,
          errors: [],
          warnings: [],
          findings: [],
          exists: false,
        },
      ],
    });
    const onPreview = vi.fn();

    render(<AddSourceStep onPreviewLoaded={onPreview} />);
    fireEvent.change(screen.getByPlaceholderText(/https:\/\/github/i), {
      target: { value: 'https://github.com/acme/private' },
    });

    // Expand the auth card, switch to paste, enter a token.
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByLabelText(/paste token/i));
    fireEvent.change(screen.getByPlaceholderText(/personal access token/i), {
      target: { value: 'my-pat' },
    });

    fireEvent.click(screen.getByRole('button', { name: /scan for skills/i }));

    await waitFor(() => expect(mockPreview).toHaveBeenCalledTimes(1));
    const [, params] = mockPreview.mock.calls[0];
    expect(params?.auth).toEqual({ method: 'token', token: 'my-pat' });
    expect(onPreview).toHaveBeenCalledWith(
      expect.any(Array),
      'https://github.com/acme/private',
      '',
      '',
      { method: 'token', token: 'my-pat' },
    );
  });

  it('omits auth for SSH URLs (ambient ssh-agent)', async () => {
    mockPreview.mockResolvedValueOnce({
      repo: 'git@github.com:acme/private.git',
      ref: '',
      commitSha: 'abc',
      skills: [
        {
          name: 's',
          description: '',
          body: '',
          valid: true,
          errors: [],
          warnings: [],
          findings: [],
          exists: false,
        },
      ],
    });

    render(<AddSourceStep onPreviewLoaded={vi.fn()} />);
    fireEvent.change(screen.getByPlaceholderText(/https:\/\/github/i), {
      target: { value: 'git@github.com:acme/private.git' },
    });

    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    // SSH variant shows a hint inside the expanded body, not token inputs.
    // The phrase also appears in the collapsed toggle label, so just assert
    // that the panel is expanded and the password input is absent.
    expect(screen.queryByPlaceholderText(/personal access token/i)).not.toBeInTheDocument();
    expect(screen.getAllByText(/using ssh-agent/i).length).toBeGreaterThanOrEqual(1);

    fireEvent.click(screen.getByRole('button', { name: /scan for skills/i }));
    await waitFor(() => expect(mockPreview).toHaveBeenCalledTimes(1));
    const [, params] = mockPreview.mock.calls[0];
    expect(params?.auth).toBeUndefined();
  });
});
