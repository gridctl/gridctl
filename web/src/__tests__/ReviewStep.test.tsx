import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, cleanup, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ReviewStep } from '../components/wizard/steps/ReviewStep';
import { appendToStack, previewStackAppend, HTTPError, fetchStatus, initializeStack, resolvePythonSource, saveStack, StackAlreadyActiveError, StackAppendError, validateStackResource, validateStackSpec } from '../lib/api';
import { showToast } from '../components/ui/Toast';
import { useStackStore } from '../stores/useStackStore';
import { useWizardStore } from '../stores/useWizardStore';
import type { MCPServerFormData } from '../lib/yaml-builder';

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
    previewStackAppend: vi.fn(),
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

// ReviewStep reads the appended record and in-flight flag from the wizard
// store, so a run in one test must not leak into the next.
beforeEach(() => {
  useWizardStore.getState().reset();
  useWizardStore.getState().setAppendInFlight(false);
  vi.mocked(previewStackAppend).mockResolvedValue({
    resourceType: 'mcp-server',
    resourceName: 'fixture',
    path: '/tmp/stack.yaml',
    before: 'mcp-servers: []\n',
    after: 'mcp-servers: []\n',
    watching: true,
  });
});

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
    await waitFor(() => expect(alert).toHaveFocus());
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

describe('ReviewStep deploy lifecycle', () => {
  const GITHUB = 'name: github\nimage: alpine\n';
  const appended = { success: true, resourceType: 'mcp-server', resourceName: 'github' };

  beforeEach(() => {
    vi.clearAllMocks();
    vi.useRealTimers();
    useStackStore.setState({ mcpServers: [], resources: [] });
    vi.mocked(validateStackResource).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
  });

  function region() {
    return within(screen.getByRole('group', { name: 'Deployment status' }));
  }

  async function clickDeploy(name = 'Deploy') {
    const deploy = await screen.findByRole('button', { name });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);
  }

  it('stops polling after unmount and offers Check deployment on remount', async () => {
    const onDeploy = vi.fn();
    vi.mocked(appendToStack).mockResolvedValue(appended);
    vi.mocked(fetchStatus).mockResolvedValue({ 'mcp-servers': [] } as never);
    const first = render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" onDeploy={onDeploy} />);
    await clickDeploy();
    await waitFor(() => expect(fetchStatus).toHaveBeenCalledTimes(1));
    first.unmount();
    vi.mocked(fetchStatus).mockResolvedValue({ 'mcp-servers': [{ name: 'github', initialized: true }] } as never);

    await new Promise((resolve) => setTimeout(resolve, 1500));
    expect(fetchStatus).toHaveBeenCalledTimes(1);
    expect(onDeploy).not.toHaveBeenCalled();
    expect(showToast).not.toHaveBeenCalledWith('success', expect.anything());

    render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" onDeploy={onDeploy} />);
    await clickDeploy('Check deployment');
    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1));
    expect(appendToStack).toHaveBeenCalledTimes(1);
  });

  it('passes an abort signal to the status poll', async () => {
    vi.mocked(appendToStack).mockResolvedValue(appended);
    vi.mocked(fetchStatus).mockResolvedValue({ 'mcp-servers': [] } as never);
    const view = render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" />);
    await clickDeploy();
    await waitFor(() => expect(fetchStatus).toHaveBeenCalledTimes(1));
    const signal = vi.mocked(fetchStatus).mock.calls[0][0] as AbortSignal;
    expect(signal.aborted).toBe(false);
    view.unmount();
    expect(signal.aborted).toBe(true);
  });

  it('shows Deploy and the already-added notice for changed YAML, and no notice while YAML is empty', async () => {
    useWizardStore.getState().setAppendedResource({ resourceType: 'mcp-server', name: 'github', yaml: GITHUB });
    const view = render(<ReviewStep yaml={'name: github-2\nimage: alpine\n'} resourceType="mcp-server" resourceName="github-2" />);
    expect(await screen.findByRole('button', { name: 'Deploy' })).toBeInTheDocument();
    expect(screen.getByText('github was already added to the stack from this wizard. Deploying again adds another server, so its name must differ.')).toBeInTheDocument();

    view.rerender(<ReviewStep yaml="" resourceType="mcp-server" resourceName="github-2" />);
    expect(screen.queryByText(/was already added to the stack/)).not.toBeInTheDocument();

    view.rerender(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" />);
    expect(await screen.findByRole('button', { name: 'Check deployment' })).toBeInTheDocument();
    expect(screen.queryByText(/was already added to the stack/)).not.toBeInTheDocument();
  });

  const sourceServer: MCPServerFormData = { name: 'github', serverType: 'source', source: { type: 'git', url: 'https://github.com/example/srv.git', dockerfile: 'Dockerfile' } };
  it.each<[string, MCPServerFormData | undefined, string, string]>([
    ['container', { name: 'github', serverType: 'container', image: 'alpine' }, 'Starting container and connecting server...', 'Starting container or connecting server'],
    ['no server data', undefined, 'Starting container and connecting server...', 'Starting container or connecting server'],
    ['source', sourceServer, 'Building image, starting container, and connecting server...', 'Building image, starting container, or connecting server'],
    ['external', { name: 'github', serverType: 'external', url: 'https://example.com/mcp' } as MCPServerFormData, 'Connecting server...', 'Connecting server'],
    ['local', { name: 'github', serverType: 'local', command: ['srv'] }, 'Connecting server...', 'Connecting server'],
    ['ssh', { name: 'github', serverType: 'ssh', command: ['srv'] }, 'Connecting server...', 'Connecting server'],
    ['openapi', { name: 'github', serverType: 'openapi' }, 'Connecting server...', 'Connecting server'],
  ])('shows the %s status line, then a persistent alert and logs link on failure', async (_label, server, line, phase) => {
    vi.mocked(appendToStack).mockResolvedValue(appended);
    vi.mocked(fetchStatus)
      .mockResolvedValueOnce({ 'mcp-servers': [] } as never)
      .mockResolvedValue({ 'mcp-servers': [{ name: 'github', initialized: false, registrationFailed: true, healthError: 'boom' }] } as never);
    render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" server={server} />);
    await clickDeploy();

    await waitFor(() => expect(region().getByText(line)).toBeInTheDocument());
    expect(region().getByRole('link', { name: /View filtered logs/ })).toHaveAttribute('href', '/logs?source=github');

    const alert = await region().findByRole('alert', {}, { timeout: 2500 });
    expect(alert).toHaveTextContent('Connecting server failed: boom');
    expect(region().getByRole('link', { name: /View filtered logs/ })).toHaveAttribute('href', '/logs?source=github');
    expect(region().queryByText(line)).not.toBeInTheDocument();

    // A status request that fails names the polling phase for this kind.
    cleanup();
    vi.mocked(fetchStatus).mockReset().mockRejectedValue(new Error('gateway down'));
    render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" server={server} />);
    await clickDeploy('Check deployment');
    expect(await region().findByRole('alert')).toHaveTextContent(`${phase} failed: gateway down`);
  });

  it('reports a deployment still in progress after the attempt limit', async () => {
    vi.mocked(appendToStack).mockResolvedValue(appended);
    vi.mocked(fetchStatus).mockResolvedValue({ 'mcp-servers': [] } as never);
    render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" server={{ name: 'github', serverType: 'local', command: ['srv'] }} />);
    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      fireEvent.click(deploy);
      await act(async () => { await vi.advanceTimersByTimeAsync(301_000); });
    } finally {
      vi.useRealTimers();
    }
    expect(fetchStatus).toHaveBeenCalledTimes(300);
    expect(region().getByText('Deployment is still in progress')).toBeInTheDocument();
    expect(region().getByRole('link', { name: /View filtered logs/ })).toBeInTheDocument();
    expect(useWizardStore.getState().appendedResource?.name).toBe('github');
    expect(screen.getByRole('button', { name: 'Check deployment' })).toBeInTheDocument();
  });

  it('clears the record after a successful deploy', async () => {
    vi.mocked(appendToStack).mockResolvedValue(appended);
    vi.mocked(fetchStatus).mockResolvedValue({ 'mcp-servers': [{ name: 'github', initialized: true }] } as never);
    const onDeploy = vi.fn(() => {
      expect(useWizardStore.getState().appendedResource).toBeNull();
    });
    render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" onDeploy={onDeploy} />);
    await clickDeploy();
    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1));
    expect(useWizardStore.getState().appendedResource).toBeNull();
    expect(region().getByText('Connected')).toBeInTheDocument();
  });

  it('keeps the append in flight across a remount and never appends twice', async () => {
    let resolveAppend: (value: typeof appended) => void = () => {};
    vi.mocked(appendToStack).mockImplementation(() => new Promise((resolve) => { resolveAppend = resolve; }));
    vi.mocked(fetchStatus).mockResolvedValue({ 'mcp-servers': [] } as never);
    const first = render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" />);
    await clickDeploy();
    await waitFor(() => expect(region().getByText('Updating stack...')).toBeInTheDocument());
    first.unmount();

    render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" />);
    const busy = await screen.findByRole('button', { name: 'Adding to stack...' });
    await waitFor(() => expect(screen.queryByText('Validating spec...')).not.toBeInTheDocument());
    expect(busy).toBeDisabled();
    fireEvent.click(busy);
    expect(appendToStack).toHaveBeenCalledTimes(1);

    await act(async () => { resolveAppend(appended); });
    expect(useWizardStore.getState().appendInFlight).toBe(false);
    expect(useWizardStore.getState().appendedResource).toEqual({ resourceType: 'mcp-server', name: 'github', yaml: GITHUB });
    expect(await screen.findByRole('button', { name: 'Check deployment' })).not.toBeDisabled();
    // The aborted run reports the append once and does not poll.
    expect(showToast).toHaveBeenCalledWith('success', "mcp-server 'github' added to stack");
    expect(fetchStatus).not.toHaveBeenCalled();
    expect(appendToStack).toHaveBeenCalledTimes(1);
  });

  it('reports a failed append once when the run was aborted', async () => {
    let rejectAppend: (err: Error) => void = () => {};
    vi.mocked(appendToStack).mockImplementation(() => new Promise((_resolve, reject) => { rejectAppend = reject; }));
    const first = render(<ReviewStep yaml={GITHUB} resourceType="mcp-server" resourceName="github" />);
    await clickDeploy();
    first.unmount();
    await act(async () => { rejectAppend(new Error('append refused')); });
    expect(showToast).toHaveBeenCalledTimes(1);
    expect(showToast).toHaveBeenCalledWith('error', 'append refused');
    expect(useWizardStore.getState().appendInFlight).toBe(false);
    expect(useWizardStore.getState().appendedResource).toBeNull();
  });

  it('shows a rejected resource append in the status region', async () => {
    vi.mocked(appendToStack).mockRejectedValue(new Error('disk full'));
    render(<ReviewStep yaml={'name: db\nimage: postgres\n'} resourceType="resource" resourceName="db" />);
    await clickDeploy();
    const alert = await region().findByRole('alert');
    expect(alert).toHaveTextContent('Updating stack failed: disk full');
    expect(region().queryByRole('link', { name: /View filtered logs/ })).not.toBeInTheDocument();
    expect(useWizardStore.getState().appendedResource).toBeNull();
  });
});

describe('ReviewStep states what the primary action changes', () => {
  const SERVER = 'name: fetch-renamed\nimage: alpine\n';
  const preview = (over: Partial<Awaited<ReturnType<typeof previewStackAppend>>> = {}) => ({
    resourceType: 'mcp-server',
    resourceName: 'fetch-renamed',
    path: '/home/me/stacks/stack.yaml',
    before: 'version: "1"\nname: s\nmcp-servers: []\n',
    after: 'version: "1"\nname: s\nmcp-servers:\n  - name: fetch-renamed\n    image: alpine\n',
    watching: true,
    ...over,
  });

  beforeEach(() => {
    vi.clearAllMocks();
    useStackStore.setState({ mcpServers: [], resources: [], stackName: '' });
    vi.mocked(validateStackResource).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
    vi.mocked(validateStackSpec).mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] });
  });

  function deployGroup() {
    return within(screen.getByRole('group', { name: 'What Deploy changes' }));
  }

  it('shows deploy wording for a valid server and the matching error line', async () => {
    vi.mocked(previewStackAppend).mockResolvedValue(preview());
    const view = render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" />);
    expect(await screen.findByText('Ready to deploy')).toBeInTheDocument();
    view.unmount();

    vi.mocked(validateStackResource).mockResolvedValue({
      valid: false, errorCount: 1, warningCount: 0,
      issues: [{ field: 'image', message: 'required', severity: 'error' }],
    });
    render(<ReviewStep yaml={SERVER} resourceType="resource" resourceName="db" />);
    expect(await screen.findByText('Fix errors before deploying')).toBeInTheDocument();
    expect(previewStackAppend).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('group', { name: 'What Deploy changes' })).toHaveTextContent('Deploy adds this resource to the active stack file.');
  });

  it('shows save wording for a valid stack and the matching error line', async () => {
    const view = render(<ReviewStep yaml={YAML} resourceType="stack" resourceName="daily" />);
    expect(await screen.findByText('Ready to save')).toBeInTheDocument();
    view.unmount();

    vi.mocked(validateStackSpec).mockResolvedValue({
      valid: false, errorCount: 1, warningCount: 0,
      issues: [{ field: 'name', message: 'required', severity: 'error' }],
    });
    render(<ReviewStep yaml={YAML} resourceType="stack" resourceName="daily" />);
    expect(await screen.findByText('Fix errors before saving')).toBeInTheDocument();
  });

  it('names the path, the server from the preview, the watching attempt, and the diff', async () => {
    vi.mocked(previewStackAppend).mockResolvedValue(preview());
    render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" />);

    const group = screen.getByRole('group', { name: 'What Deploy changes' });
    await within(group).findByText('/home/me/stacks/stack.yaml');
    expect(group).toHaveTextContent('Deploy adds the server fetch-renamed to mcp-servers in /home/me/stacks/stack.yaml.');
    expect(group).toHaveTextContent('tries to start the server');
    expect(group).toHaveTextContent('3 lines added, 1 existing line rewritten');
    const diff = deployGroup().getByLabelText('Stack file diff');
    expect(diff.textContent).toContain('+   - name: fetch-renamed');
    expect(diff.textContent).toContain('+     image: alpine');
    expect(diff.textContent).toContain('- mcp-servers: []');
  });

  it('warns that nothing starts when the gateway is not watching', async () => {
    vi.mocked(previewStackAppend).mockResolvedValue(preview({ watching: false }));
    render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" />);
    const group = screen.getByRole('group', { name: 'What Deploy changes' });
    await within(group).findByText(/The gateway is not watching this file\./);
    expect(group).toHaveTextContent('Reload Config');
    expect(within(group).getByText('gridctl reload').tagName).toBe('CODE');
    expect(group).not.toHaveTextContent('tries to start');
  });

  it('explains re-serialization only when existing lines change', async () => {
    vi.mocked(previewStackAppend).mockResolvedValue(preview({
      before: 'name: s\n\nmcp-servers: []\n',
      after: 'name: s\nmcp-servers: []\n',
    }));
    const view = render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" />);
    let group = screen.getByRole('group', { name: 'What Deploy changes' });
    await within(group).findByText(/existing line rewritten/);
    expect(group).toHaveTextContent('Deploy re-serializes the whole file');
    view.unmount();

    vi.mocked(previewStackAppend).mockResolvedValue(preview({
      before: 'name: s\nmcp-servers:\n  - name: a\n',
      after: 'name: s\nmcp-servers:\n  - name: a\n  - name: fetch-renamed\n',
    }));
    render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" />);
    group = screen.getByRole('group', { name: 'What Deploy changes' });
    await within(group).findByText(/1 line added/);
    expect(group).not.toHaveTextContent('rewritten');
    expect(group).not.toHaveTextContent('re-serializes');
  });

  it('lists 422 issues without a toast and keeps Deploy enabled', async () => {
    vi.mocked(previewStackAppend).mockRejectedValue(new HTTPError(422, 'stack validation failed after append', {
      errors: ['mcp-servers[1].name: duplicate name'],
    }));
    render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" />);
    const group = screen.getByRole('group', { name: 'What Deploy changes' });
    expect(await within(group).findByText('mcp-servers[1].name: duplicate name')).toBeInTheDocument();
    expect(group).toHaveTextContent('The stack file would be invalid after this change:');
    expect(group).toHaveTextContent('Deploy adds this server to the active stack file.');
    expect(screen.getByRole('button', { name: 'Deploy' })).not.toBeDisabled();
    expect(showToast).not.toHaveBeenCalled();
  });

  it('reports a generic preview failure', async () => {
    vi.mocked(previewStackAppend).mockRejectedValue(new HTTPError(500, 'Failed to read stack file'));
    render(<ReviewStep yaml={SERVER} resourceType="resource" resourceName="db" />);
    const group = screen.getByRole('group', { name: 'What Deploy changes' });
    expect(await within(group).findByText('Could not preview the change: Failed to read stack file.')).toBeInTheDocument();
    expect(showToast).not.toHaveBeenCalled();
  });

  it('shows the pending line while the preview is in flight', async () => {
    vi.mocked(previewStackAppend).mockReturnValue(new Promise(() => {}));
    render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" />);
    expect(await screen.findByText('Checking the stack file...')).toBeInTheDocument();
  });

  it('refetches for new YAML and drops a late response for the old YAML', async () => {
    let resolveOld: (v: ReturnType<typeof preview>) => void = () => {};
    vi.mocked(previewStackAppend)
      .mockReturnValueOnce(new Promise((resolve) => { resolveOld = resolve; }))
      .mockResolvedValueOnce(preview({ resourceName: 'newer', path: '/new/stack.yaml' }));
    const view = render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" />);
    await waitFor(() => expect(previewStackAppend).toHaveBeenCalledTimes(1));

    view.rerender(<ReviewStep yaml={'name: newer\nimage: alpine\n'} resourceType="mcp-server" resourceName="fetch" />);
    const group = screen.getByRole('group', { name: 'What Deploy changes' });
    await within(group).findByText('/new/stack.yaml');
    expect(previewStackAppend).toHaveBeenCalledTimes(2);
    expect(previewStackAppend).toHaveBeenLastCalledWith('name: newer\nimage: alpine\n', 'mcp-server');

    await act(async () => { resolveOld(preview({ path: '/old/stack.yaml' })); });
    expect(group).not.toHaveTextContent('/old/stack.yaml');
    expect(group).toHaveTextContent('/new/stack.yaml');
  });

  it('skips the poll and asks for a reload when the append reports not watching', async () => {
    const onDeploy = vi.fn();
    vi.mocked(previewStackAppend).mockResolvedValue(preview({ watching: false }));
    vi.mocked(appendToStack).mockResolvedValue({ success: true, resourceType: 'mcp-server', resourceName: 'fetch-renamed', watching: false, path: '/p' });
    render(<ReviewStep yaml={SERVER} resourceType="mcp-server" resourceName="fetch" onDeploy={onDeploy} />);
    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);

    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1));
    expect(showToast).toHaveBeenCalledWith('warning', 'fetch-renamed was added to the stack file. Use Reload Config to start it.');
    expect(fetchStatus).not.toHaveBeenCalled();
    expect(useWizardStore.getState().appendedResource).toBeNull();
  });

  it('adds a reload warning after a resource append that is not watched', async () => {
    const onDeploy = vi.fn();
    vi.mocked(appendToStack).mockResolvedValue({ success: true, resourceType: 'resource', resourceName: 'db', watching: false, path: '/p' });
    render(<ReviewStep yaml={'name: db\nimage: postgres\n'} resourceType="resource" resourceName="db" onDeploy={onDeploy} />);
    const deploy = await screen.findByRole('button', { name: 'Deploy' });
    await waitFor(() => expect(deploy).not.toBeDisabled());
    fireEvent.click(deploy);

    await waitFor(() => expect(onDeploy).toHaveBeenCalledTimes(1));
    expect(showToast).toHaveBeenCalledWith('success', "resource 'db' added to stack");
    expect(showToast).toHaveBeenCalledWith('warning', 'Use Reload Config to start it.');
  });

  it('describes Save & Load for a named stack against the active stack', async () => {
    useStackStore.setState({ stackName: 'prod' });
    const view = render(<ReviewStep yaml={YAML} resourceType="stack" resourceName="daily" />);
    let group = screen.getByRole('group', { name: 'What Save & Load changes' });
    expect(group).toHaveTextContent('Save & Load writes daily.yaml to the stack library and replaces a saved stack with that name.');
    expect(group).toHaveTextContent('prod is already loaded, so this stack is saved but not loaded.');
    view.unmount();

    useStackStore.setState({ stackName: '' });
    render(<ReviewStep yaml={YAML} resourceType="stack" resourceName="daily" />);
    group = screen.getByRole('group', { name: 'What Save & Load changes' });
    expect(group).toHaveTextContent('No stack is loaded, so the gateway loads this one.');
    await screen.findByText('Ready to save');
    expect(previewStackAppend).not.toHaveBeenCalled();
  });

  it('describes Save & Load for a stack without a name and shows no timestamp', async () => {
    render(<ReviewStep yaml={'version: "1"\n'} resourceType="stack" resourceName="" />);
    const group = screen.getByRole('group', { name: 'What Save & Load changes' });
    expect(group).toHaveTextContent('Save & Load writes the stack to the stack library under a generated name.');
    expect(group).not.toHaveTextContent(/stack-\d+/);
    await screen.findByText('Ready to save');
    expect(previewStackAppend).not.toHaveBeenCalled();
  });
});
