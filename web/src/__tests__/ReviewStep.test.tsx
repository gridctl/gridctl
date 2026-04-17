import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ReviewStep } from '../components/wizard/steps/ReviewStep';
import { saveStack, initializeStack, StackAlreadyActiveError, validateStackSpec } from '../lib/api';
import { showToast } from '../components/ui/Toast';

vi.mock('../lib/api', async () => {
  // Preserve the real StackAlreadyActiveError class so `instanceof` still works
  // in the component code under test.
  class StackAlreadyActiveError extends Error {
    constructor() {
      super('Stack already active');
      this.name = 'StackAlreadyActiveError';
    }
  }
  return {
    saveStack: vi.fn(),
    initializeStack: vi.fn(),
    appendToStack: vi.fn(),
    validateStackSpec: vi.fn(),
    StackAlreadyActiveError,
  };
});

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

const YAML = 'version: "1"\nname: daily\n';

describe('ReviewStep handleSaveAndLoad', () => {
  let onDeploy: () => void;

  beforeEach(() => {
    vi.clearAllMocks();
    onDeploy = vi.fn<() => void>();
    (validateStackSpec as ReturnType<typeof vi.fn>).mockResolvedValue({ issues: [] });
  });

  async function clickSaveAndLoad() {
    render(
      <ReviewStep
        yaml={YAML}
        resourceType="stack"
        resourceName="daily"
        onDeploy={onDeploy}
      />,
    );

    // Wait for initial validation to finish so the button isn't disabled.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /save & load/i })).not.toBeDisabled();
    });

    fireEvent.click(screen.getByRole('button', { name: /save & load/i }));
  }

  it('calls onDeploy after a fully successful save + load', async () => {
    (saveStack as ReturnType<typeof vi.fn>).mockResolvedValue({
      success: true,
      name: 'daily',
      path: '/tmp/daily.yaml',
    });
    (initializeStack as ReturnType<typeof vi.fn>).mockResolvedValue({
      success: true,
      name: 'daily',
    });

    await clickSaveAndLoad();

    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1));
    expect(showToast).toHaveBeenCalledWith(
      'success',
      'Stack loaded — daily is now active',
    );
  });

  it('calls onDeploy when initialize fails with StackAlreadyActiveError (409)', async () => {
    (saveStack as ReturnType<typeof vi.fn>).mockResolvedValue({
      success: true,
      name: 'daily',
      path: '/tmp/daily.yaml',
    });
    (initializeStack as ReturnType<typeof vi.fn>).mockRejectedValue(
      new StackAlreadyActiveError(),
    );

    await clickSaveAndLoad();

    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1));
    expect(showToast).toHaveBeenCalledWith('success', 'Stack saved to library');
  });

  it('calls onDeploy when initialize fails with a generic error (non-409 fallback)', async () => {
    (saveStack as ReturnType<typeof vi.fn>).mockResolvedValue({
      success: true,
      name: 'daily',
      path: '/tmp/daily.yaml',
    });
    (initializeStack as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error('Initialize failed: 500'),
    );

    await clickSaveAndLoad();

    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1));
    expect(showToast).toHaveBeenCalledWith(
      'error',
      expect.stringContaining('gridctl apply'),
      expect.objectContaining({ duration: expect.any(Number) }),
    );
  });

  it('does NOT call onDeploy when saveStack itself fails', async () => {
    (saveStack as ReturnType<typeof vi.fn>).mockRejectedValue(
      new Error('Save failed: 400'),
    );

    await clickSaveAndLoad();

    // saveStack rejected — onDeploy must not be called (user's work is not persisted).
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('error', 'Save failed: 400'),
    );
    expect(onDeploy).not.toHaveBeenCalled();
    expect(initializeStack).not.toHaveBeenCalled();
  });
});
