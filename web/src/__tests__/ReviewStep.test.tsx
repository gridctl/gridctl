import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ReviewStep } from '../components/wizard/steps/ReviewStep';
import { appendToStack, fetchStatus, initializeStack, resolvePythonSource, saveStack, StackAlreadyActiveError, validateStackResource, validateStackSpec } from '../lib/api';
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
    fetchStatus: vi.fn(),
    resolvePythonSource: vi.fn(),
    validateStackResource: vi.fn(),
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

describe('ReviewStep operations summary', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (validateStackSpec as ReturnType<typeof vi.fn>).mockResolvedValue({ issues: [] });
    (validateStackResource as ReturnType<typeof vi.fn>).mockResolvedValue({ issues: [] });
  });

  function renderWith(operationsSummary?: string | null) {
    render(
      <ReviewStep
        yaml={YAML}
        resourceType="mcp-server"
        resourceName="petstore"
        operationsSummary={operationsSummary}
      />,
    );
  }

  it.each([
    ['All 517', 'all mode'],
    ['12 of 517 (include)', 'include mode'],
    ['12 of 517 excluded (exclude)', 'exclude mode'],
  ])('shows the summary row "%s" for %s', (summary) => {
    renderWith(summary);
    expect(screen.getByText('Operations')).toBeInTheDocument();
    expect(screen.getByText(summary)).toBeInTheDocument();
  });

  it('omits the row entirely for resources that have no operations filter', () => {
    renderWith(null);
    expect(screen.queryByText('Operations')).not.toBeInTheDocument();
  });
});

describe('ReviewStep Python container review', () => {
  const server = {
    name: 'fetch',
    serverType: 'source' as const,
    source: { type: 'pypi', package: 'mcp-server-fetch', ref: '0.6.0', runtime: 'python' as const },
    transport: 'stdio',
  };
  const resolution = {
    declaredIdentity: { type: 'pypi', package: 'mcp-server-fetch', version: '0.6.0' },
    resolvedIdentity: { type: 'pypi', package: 'mcp-server-fetch', version: '0.6.0', artifact: 'fetch.whl' },
    python: '3.12',
    command: ['mcp-server-fetch'],
    buildInputDigest: 'abc',
    imageTag: 'gridctl-preview-fetch:0.6.0-abc',
    cached: false,
    mutableRef: false,
    generatedFile: { name: '.gridctl.Dockerfile', mediaType: 'text/x-dockerfile', content: 'FROM python@sha256:abc\n' },
  };

  beforeEach(() => {
    vi.clearAllMocks();
    (validateStackResource as ReturnType<typeof vi.fn>).mockResolvedValue({ issues: [] });
    (resolvePythonSource as ReturnType<typeof vi.fn>).mockResolvedValue(resolution);
  });

  it('shows exact build intent and the backend-generated Dockerfile', async () => {
    render(
      <ReviewStep
        yaml={'name: fetch\nsource:\n  type: pypi\n  package: mcp-server-fetch\n  ref: 0.6.0\n'}
        resourceType="mcp-server"
        resourceName="fetch"
        server={server}
      />,
    );

    expect(await screen.findByText('mcp-server-fetch==0.6.0')).toBeInTheDocument();
    expect(screen.getByText('gridctl-preview-fetch:0.6.0-abc')).toBeInTheDocument();
    const phases = screen.getByRole('list', { name: 'Build phases' });
    for (const phase of ['Resolving package/ref', 'Cloning/preparing context', 'Generating Dockerfile', 'Building image', 'Starting container', 'Connecting server']) {
      expect(within(phases).getByText(phase)).toBeInTheDocument();
    }
    fireEvent.click(screen.getByText('Generated Dockerfile'));
    expect(screen.getByText(/FROM python@sha256:abc/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Deploy' })).not.toBeDisabled();
  });

  it('waits for registration before reporting deployment success', async () => {
    const onDeploy = vi.fn();
    (appendToStack as ReturnType<typeof vi.fn>).mockResolvedValue({
      success: true,
      resourceType: 'mcp-server',
      resourceName: 'fetch',
    });
    (fetchStatus as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce({ 'mcp-servers': [] })
      .mockResolvedValueOnce({ 'mcp-servers': [{ name: 'fetch', initialized: true }] });
    render(
      <ReviewStep
        yaml={'name: fetch\nsource:\n  type: pypi\n  package: mcp-server-fetch\n  ref: 0.6.0\n'}
        resourceType="mcp-server"
        resourceName="fetch"
        server={server}
        onDeploy={onDeploy}
      />,
    );

    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);

    await waitFor(() => expect(fetchStatus).toHaveBeenCalledTimes(1));
    expect(showToast).not.toHaveBeenCalledWith('success', expect.anything());
    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1), { timeout: 2500 });
    expect(showToast).toHaveBeenCalledWith('success', 'fetch deployed with a built image');
  });

  it('polls the appended YAML name instead of the form name', async () => {
    (appendToStack as ReturnType<typeof vi.fn>).mockResolvedValue({
      success: true,
      resourceType: 'mcp-server',
      resourceName: 'fetch-server',
    });
    (fetchStatus as ReturnType<typeof vi.fn>).mockResolvedValue({
      'mcp-servers': [{ name: 'fetch-server', initialized: true }],
    });
    render(
      <ReviewStep
        yaml={'name: fetch-server\nsource:\n  type: pypi\n  package: mcp-server-fetch\n  ref: 0.6.0\n'}
        resourceType="mcp-server"
        resourceName="fetch"
        server={server}
      />,
    );

    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);

    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'fetch-server deployed with a built image'));
    expect(fetchStatus).toHaveBeenCalledTimes(1);
  });

  it('retries registration polling without appending the server twice', async () => {
    (appendToStack as ReturnType<typeof vi.fn>).mockResolvedValue({
      success: true,
      resourceType: 'mcp-server',
      resourceName: 'fetch',
    });
    (fetchStatus as ReturnType<typeof vi.fn>)
      .mockResolvedValueOnce({
        'mcp-servers': [{ name: 'fetch', initialized: false, registrationFailed: true, healthError: 'registration failed' }],
      })
      .mockResolvedValueOnce({
        'mcp-servers': [{ name: 'fetch', initialized: true }],
      });
    render(
      <ReviewStep
        yaml={'name: fetch\nsource:\n  type: pypi\n  package: mcp-server-fetch\n  ref: 0.6.0\n'}
        resourceType="mcp-server"
        resourceName="fetch"
        server={server}
      />,
    );

    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);
    const retry = await screen.findByRole('button', { name: 'Check deployment' });
    fireEvent.click(retry);

    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'fetch deployed with a built image'));
    expect(appendToStack).toHaveBeenCalledTimes(1);
    expect(fetchStatus).toHaveBeenCalledTimes(2);
  });

  it('shows the backend mutable-ref warning', async () => {
    (resolvePythonSource as ReturnType<typeof vi.fn>).mockResolvedValue({ ...resolution, mutableRef: true });
    render(
      <ReviewStep
        yaml={'name: fetch\nsource:\n  type: git\n  url: https://github.com/example/fetch.git\n  runtime: python\n'}
        resourceType="mcp-server"
        resourceName="fetch"
        server={{ ...server, source: { type: 'git', url: 'https://github.com/example/fetch.git', runtime: 'python' } }}
      />,
    );

    expect(await screen.findByText(/source uses a mutable Git ref/i)).toBeInTheDocument();
  });
});
