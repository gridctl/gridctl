import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ReviewStep } from '../components/wizard/steps/ReviewStep';
import { appendToStack, fetchStatus, initializeStack, resolvePythonSource, saveStack, StackAlreadyActiveError, StackAppendError, validateStackResource, validateStackSpec } from '../lib/api';
import { showToast } from '../components/ui/Toast';
import { useStackStore } from '../stores/useStackStore';

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  // Preserve a local class so `instanceof` still matches the value this file throws.
  class StackAlreadyActiveError extends Error {
    constructor() {
      super('Stack already active');
      this.name = 'StackAlreadyActiveError';
    }
  }
  return {
    ...actual,
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
      expect(showToast).toHaveBeenCalledWith('error', 'Save failed: Save failed: 400.', { detail: undefined }),
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

describe('ReviewStep execution intent', () => {
  it('uses backend-normalized proposed YAML and hides stale previews', async () => {
    vi.clearAllMocks();
    vi.mocked(validateStackResource).mockResolvedValueOnce({ valid: true, errorCount: 0, warningCount: 0, issues: [], execution: {
      fixture: { mode: 'hardened', revision: 'backend-revision', instance: '', eligible: false, outcome: 'pending', observed_at: '', runtime: 'unknown', daemon_rootless: 'unknown', user_namespace: 'unknown', controls: [{ field: 'memory_bytes', requested: '134217728', outcome: 'configured', source: 'normalized desired contract' }] },
    } }).mockImplementationOnce(() => new Promise(() => {}));
    const { rerender } = render(<ReviewStep yaml={'name: fixture\nimage: alpine\n'} resourceType="mcp-server" resourceName="fixture" />);
    expect(await screen.findByText('134217728')).toBeInTheDocument();
    expect(screen.getByText(/These settings are not runtime evidence/)).toBeInTheDocument();
    rerender(<ReviewStep yaml={'name: changed\nimage: alpine\n'} resourceType="mcp-server" resourceName="changed" />);
    expect(screen.queryByText('134217728')).not.toBeInTheDocument();
    expect(validateStackResource).toHaveBeenLastCalledWith('name: changed\nimage: alpine\n', 'mcp-server');
  });
});

describe('ReviewStep validation and deploy failures', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useStackStore.setState({ mcpServers: [], resources: [] });
  });

  it('lists an unknown field with its line and disables Deploy', async () => {
    vi.mocked(validateStackResource).mockResolvedValue({
      valid: false,
      errorCount: 1,
      warningCount: 0,
      issues: [{ field: 'imagee', message: 'unknown field "imagee"', severity: 'error', line: 4 }],
    });
    render(<ReviewStep yaml={'name: fixture\nimagee: alpine\n'} resourceType="mcp-server" resourceName="fixture" />);

    expect(await screen.findByText('imagee')).toBeInTheDocument();
    expect(screen.getByText(/unknown field "imagee" \(line 4\)/)).toBeInTheDocument();
    expect(screen.getByText('1 validation error')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Deploy' })).toBeDisabled();
  });

  it('shows one warning with the reason when validation cannot run and keeps Deploy enabled', async () => {
    vi.mocked(validateStackResource).mockRejectedValue(new Error('gateway unreachable'));
    render(<ReviewStep yaml={'name: fixture\nimage: alpine\n'} resourceType="mcp-server" resourceName="fixture" />);

    expect(await screen.findByText(/Validation unavailable\. Validation failed: gateway unreachable\./)).toBeInTheDocument();
    expect(screen.getByText('1 warning')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Deploy' })).not.toBeDisabled());
  });

  it('renders a rejected append for a non-Python server as an alert with every issue', async () => {
    vi.mocked(validateStackResource).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    vi.mocked(appendToStack).mockRejectedValue(new StackAppendError(422, 'stack validation failed after append', [
      { field: 'mcp-servers[1].name', message: "duplicate MCP server name 'github'", severity: 'error' },
    ]));
    render(<ReviewStep yaml={'name: github\nimage: alpine\n'} resourceType="mcp-server" resourceName="github" />);

    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('stack validation failed after append');
    expect(within(alert).getByText('mcp-servers[1].name')).toBeInTheDocument();
    expect(within(alert).getByText(/duplicate MCP server name 'github'/)).toBeInTheDocument();
    expect(alert).toHaveFocus();
    expect(showToast).toHaveBeenCalledWith('error', 'stack validation failed after append');
    expect(screen.queryByText(/Deploy failed: 400/)).not.toBeInTheDocument();
  });

  it('renders a rejected resource append in the same alert', async () => {
    vi.mocked(validateStackResource).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    vi.mocked(appendToStack).mockRejectedValue(new StackAppendError(400, 'The YAML was rejected', [
      { field: 'imagee', message: 'unknown field "imagee" (line 2)', severity: 'error', line: 2 },
    ]));
    render(<ReviewStep yaml={'name: db\nimagee: postgres\n'} resourceType="resource" resourceName="db" />);

    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('The YAML was rejected');
    expect(within(alert).getByText('unknown field "imagee" (line 2)')).toBeInTheDocument();
  });

  it('shows the Python deploy error once, in the shared alert', async () => {
    vi.mocked(validateStackResource).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    vi.mocked(resolvePythonSource).mockResolvedValue({
      declaredIdentity: { type: 'pypi', package: 'mcp-server-fetch', version: '0.6.0' },
      resolvedIdentity: { type: 'pypi', package: 'mcp-server-fetch', version: '0.6.0' },
      buildInputDigest: 'abc',
      imageTag: 'gridctl-preview-fetch:0.6.0-abc',
      cached: false,
      mutableRef: false,
    } as Awaited<ReturnType<typeof resolvePythonSource>>);
    vi.mocked(appendToStack).mockRejectedValue(new StackAppendError(422, 'stack validation failed after append', []));
    render(
      <ReviewStep
        yaml={'name: fetch\nsource:\n  type: pypi\n  package: mcp-server-fetch\n  ref: 0.6.0\n'}
        resourceType="mcp-server"
        resourceName="fetch"
        server={{ name: 'fetch', serverType: 'source', source: { type: 'pypi', package: 'mcp-server-fetch', ref: '0.6.0', runtime: 'python' }, transport: 'stdio' } as never}
      />,
    );

    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);

    const alerts = await screen.findAllByRole('alert');
    expect(alerts).toHaveLength(1);
    expect(alerts[0]).toHaveTextContent('Updating stack failed: stack validation failed after append');
  });

  it('flags a name already in the stack and does not deploy', async () => {
    vi.mocked(validateStackResource).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    useStackStore.setState({ mcpServers: [{ name: 'github' } as never], resources: [] });
    render(<ReviewStep yaml={'name: "github"\nimage: alpine\n'} resourceType="mcp-server" resourceName="github" />);

    expect(await screen.findByRole('alert')).toHaveTextContent("A server or resource named 'github' already exists in this stack");
    const deploy = screen.getByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(validateStackResource).toHaveBeenCalled());
    expect(deploy).toBeDisabled();
    fireEvent.click(deploy);
    expect(appendToStack).not.toHaveBeenCalled();
  });

  it('checks resource names against servers too', async () => {
    vi.mocked(validateStackResource).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    useStackStore.setState({ mcpServers: [{ name: 'db' } as never], resources: [] });
    render(<ReviewStep yaml={'name: db # primary\nimage: postgres\n'} resourceType="resource" resourceName="db" />);

    expect(await screen.findByRole('alert')).toHaveTextContent("named 'db' already exists");
    expect(screen.getByRole('button', { name: 'Deploy' })).toBeDisabled();
  });

  it('keeps Check deployment enabled after the appended name reaches the store', async () => {
    vi.mocked(validateStackResource).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    vi.mocked(appendToStack).mockImplementation(async () => {
      useStackStore.setState({ mcpServers: [{ name: 'github' } as never] });
      return { success: true, resourceType: 'mcp-server', resourceName: 'github' };
    });
    vi.mocked(fetchStatus).mockResolvedValue({
      'mcp-servers': [{ name: 'github', initialized: false, registrationFailed: true, healthError: 'registration failed' }],
    } as never);
    render(<ReviewStep yaml={'name: github\nimage: alpine\n'} resourceType="mcp-server" resourceName="github" />);

    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);

    const check = await screen.findByRole('button', { name: 'Check deployment' });
    await waitFor(() => expect(check).not.toBeDisabled());
    expect(screen.queryByText(/already exists in this stack/)).not.toBeInTheDocument();
  });

  it('ignores a late validation response for older YAML', async () => {
    let resolveFirst: (value: Awaited<ReturnType<typeof validateStackResource>>) => void = () => {};
    vi.mocked(validateStackResource)
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
      .mockResolvedValueOnce({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    const { rerender } = render(<ReviewStep yaml={'name: old\nimagee: x\n'} resourceType="mcp-server" resourceName="old" />);
    rerender(<ReviewStep yaml={'name: new\nimage: x\n'} resourceType="mcp-server" resourceName="new" />);

    expect(await screen.findByText('Spec is valid')).toBeInTheDocument();
    await act(async () => {
      resolveFirst({ valid: false, errorCount: 1, warningCount: 0, issues: [{ field: 'imagee', message: 'unknown field', severity: 'error' }] });
    });
    expect(screen.getByText('Spec is valid')).toBeInTheDocument();
    expect(screen.queryByText('imagee')).not.toBeInTheDocument();
  });
});
