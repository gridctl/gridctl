import { useState, useEffect, useCallback, useRef } from 'react';
import {
  CheckCircle2,
  AlertCircle,
  AlertTriangle,
  Download,
  Copy,
  Rocket,
  Loader2,
  FileCode2,
  Library,
  ExternalLink,
} from 'lucide-react';
import { cn } from '../../../lib/cn';
import { Button } from '../../ui/Button';
import { showToast } from '../../ui/Toast';
import { validateStackSpec, validateStackResource, appendToStack, saveStack, initializeStack, StackAlreadyActiveError, StackAppendError, resolvePythonSource, fetchStatus, isStackFragmentType, type PythonResolution } from '../../../lib/api';
import type { ValidationIssue, ExecutionReport } from '../../../types';
import type { MCPServerFormData } from '../../../lib/yaml-builder';
import { describeError, isCancellation } from '../../../lib/describeError';
import { useStackStore } from '../../../stores/useStackStore';
import { useWizardStore } from '../../../stores/useWizardStore';
import { DeployChangesSection, SaveLoadChangesSection } from './ReviewChangesSection';

type ServerKind = 'source' | 'container' | 'connect';

// Status line while registration is polled, and the matching failure phase.
const pollingStatus: Record<ServerKind, string> = {
  source: 'Building image, starting container, and connecting server...',
  container: 'Starting container and connecting server...',
  connect: 'Connecting server...',
};
const pollingPhase: Record<ServerKind, string> = {
  source: 'Building image, starting container, or connecting server',
  container: 'Starting container or connecting server',
  connect: 'Connecting server',
};

function serverKind(server?: MCPServerFormData): ServerKind {
  if (!server) return 'container';
  if (server.source || server.serverType === 'source') return 'source';
  return server.serverType === 'container' ? 'container' : 'connect';
}

/** Resolves after `ms`, or as soon as `signal` aborts. */
function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const done = () => {
      window.clearTimeout(timer);
      signal.removeEventListener('abort', done);
      resolve();
    };
    const timer = window.setTimeout(done, ms);
    signal.addEventListener('abort', done);
  });
}

/** Top-level `name:` of a server or resource fragment, with quotes and a trailing comment removed. */
function topLevelName(yaml: string): string {
  const match = yaml.match(/^name:[ \t]*(.*)$/m);
  if (!match) return '';
  const value = match[1].replace(/\s+#.*$/, '').trim();
  const quoted = value.match(/^(['"])(.*)\1$/);
  return (quoted ? quoted[2] : value).trim();
}

function IssueList({ issues }: { issues: ValidationIssue[] }) {
  return (
    <div className="space-y-1.5">
      {issues.map((issue, i) => (
        <div
          key={i}
          className={cn(
            'flex items-start gap-2 px-3 py-2 rounded-lg text-xs',
            issue.severity === 'error'
              ? 'bg-status-error/5 text-status-error'
              : issue.severity === 'warning'
                ? 'bg-status-pending/5 text-status-pending'
                : 'bg-secondary/5 text-secondary',
          )}
        >
          {issue.severity === 'error' ? (
            <AlertCircle size={11} className="flex-shrink-0 mt-0.5" />
          ) : (
            <AlertTriangle size={11} className="flex-shrink-0 mt-0.5" />
          )}
          <div>
            <span className="font-medium">{issue.field}</span>
            <span className="mx-1 text-text-muted">—</span>
            {issue.message}
            {issue.line && !issue.message.includes(`line ${issue.line}`) ? ` (line ${issue.line})` : ''}
          </div>
        </div>
      ))}
    </div>
  );
}

interface ReviewStepProps {
  yaml: string;
  resourceType: string;
  resourceName: string;
  onDeploy?: () => void;
  // Outcome of the OpenAPI operations filter, pre-formatted by the caller.
  // Passed in rather than parsed back out of the YAML, which would have to
  // re-derive a count the generated document deliberately does not carry.
  operationsSummary?: string | null;
  server?: MCPServerFormData;
}

export function ReviewStep({
  yaml,
  resourceType,
  resourceName,
  onDeploy,
  operationsSummary,
  server,
}: ReviewStepProps) {
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [executionPreview, setExecutionPreview] = useState<{ yaml: string; reports: Record<string, ExecutionReport> }>({ yaml: '', reports: {} });
  const [validating, setValidating] = useState(true);
  const [copied, setCopied] = useState(false);
  const [deploying, setDeploying] = useState(false);
  const [resolutionState, setResolutionState] = useState<{
    key: string;
    resolution: PythonResolution | null;
    error: string;
  }>({ key: '', resolution: null, error: '' });
  const [buildState, setBuildState] = useState<'idle' | 'waiting' | 'pending' | 'failed' | 'complete'>('idle');
  const [deployError, setDeployError] = useState('');
  const [deployIssues, setDeployIssues] = useState<ValidationIssue[]>([]);
  const [failurePhase, setFailurePhase] = useState('');
  const resolutionErrorRef = useRef<HTMLParagraphElement>(null);
  const deployErrorRef = useRef<HTMLDivElement>(null);
  const validateGeneration = useRef(0);
  const deployRun = useRef<AbortController | null>(null);
  const appendedResource = useWizardStore((s) => s.appendedResource);
  const appendInFlight = useWizardStore((s) => s.appendInFlight);
  const setAppendedResource = useWizardStore((s) => s.setAppendedResource);
  const setAppendInFlight = useWizardStore((s) => s.setAppendInFlight);
  const appendedMatches = appendedResource?.resourceType === resourceType &&
    appendedResource?.yaml === yaml;
  const appendedName = appendedMatches && appendedResource ? appendedResource.name : '';
  // The wizard renders Review with empty YAML until its debounce fills it in,
  // so the notice waits for YAML to avoid flashing on reopen.
  const previouslyAppendedName = appendedResource && !appendedMatches &&
    appendedResource.resourceType === resourceType && yaml.trim()
    ? appendedResource.name
    : '';
  const kind = serverKind(server);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const stackResources = useStackStore((s) => s.resources);
  const activeStack = useStackStore((s) => s.stackName);
  const pythonSource = server?.source &&
    (server.source.type === 'pypi' || server.source.runtime === 'python') &&
    !server.source.dockerfile;
  const sourceKey = pythonSource && server ? JSON.stringify(server) : '';
  const currentResolution = resolutionState.key === sourceKey ? resolutionState.resolution : null;
  const resolutionError = resolutionState.key === sourceKey ? resolutionState.error : '';
  const resolutionPending = Boolean(sourceKey && resolutionState.key !== sourceKey);

  // Flag validation pending when the YAML changes (state adjustment during
  // render, so the spinner commits together with the change). Runs on first
  // render too, replacing the old validate-on-mount effect prefix.
  const [prevYaml, setPrevYaml] = useState<string | null>(null);
  if (prevYaml !== yaml) {
    setPrevYaml(yaml);
    setValidating(yaml.trim().length > 0);
    if (!yaml.trim()) setIssues([]);
  }

  const validate = useCallback(async () => {
    const generation = ++validateGeneration.current;
    if (!yaml.trim()) return;
    try {
      const result = isStackFragmentType(resourceType)
        ? await validateStackResource(yaml, resourceType)
        : await validateStackSpec(yaml);
      if (generation !== validateGeneration.current) return;
      setIssues(result.issues || []);
      setExecutionPreview({ yaml, reports: result.execution ?? {} });
    } catch (err) {
      if (generation !== validateGeneration.current) return;
      setIssues([
        { field: 'yaml', message: `Validation unavailable. ${describeError(err, 'Validation').text}`, severity: 'warning' },
      ]);
    }
    setValidating(false);
  }, [resourceType, yaml]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async callback; state is set only after await, not synchronously
    validate();
  }, [validate]);

  useEffect(() => {
    if (!sourceKey) return;
    let active = true;
    resolvePythonSource(JSON.parse(sourceKey) as Record<string, unknown>)
      .then((result) => {
        if (!active) return;
        setResolutionState({ key: sourceKey, resolution: result, error: '' });
      })
      .catch((error) => {
        if (!active) return;
        setResolutionState({
          key: sourceKey,
          resolution: null,
          error: error instanceof Error ? error.message : 'Source resolution failed',
        });
      });
    return () => { active = false; };
  }, [sourceKey]);

  useEffect(() => {
    if (deployError) deployErrorRef.current?.focus();
    else if (resolutionError) resolutionErrorRef.current?.focus();
  }, [resolutionError, deployError]);

  // Aborts the controller in the ref rather than setting an "unmounted" flag:
  // StrictMode runs this cleanup once at mount, when the ref is still empty.
  useEffect(() => () => deployRun.current?.abort(), []);

  const errorCount = issues.filter((i) => i.severity === 'error').length;
  const warningCount = issues.filter((i) => i.severity === 'warning').length;
  const hasErrors = errorCount > 0;

  // Advisory only: the store lists what the gateway reports, and the append
  // response stays the authority. Skipped after an append, when the name is
  // expected to be present and "Check deployment" must stay usable.
  const deployName = isStackFragmentType(resourceType) && !appendedName ? topLevelName(yaml) : '';
  const nameConflict = Boolean(deployName) && [
    ...(mcpServers ?? []),
    ...(stackResources ?? []),
  ].some((item) => item.name === deployName);

  const handleDownload = () => {
    const fileName =
      resourceType === 'stack'
        ? 'stack.yaml'
        : `${resourceName || resourceType}.yaml`;
    const blob = new Blob([yaml], { type: 'application/x-yaml' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = fileName;
    a.click();
    URL.revokeObjectURL(url);
    showToast('success', `Downloaded ${fileName}`);
  };

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(yaml);
      setCopied(true);
      showToast('success', 'YAML copied to clipboard');
      setTimeout(() => setCopied(false), 2000);
    } catch {
      showToast('error', 'Failed to copy to clipboard');
    }
  };

  const handleDeploy = async () => {
    // Only the store flag protects a Review step that mounted after another
    // instance started an append; local state cannot.
    if (!appendedName && useWizardStore.getState().appendInFlight) return;
    deployRun.current?.abort();
    const controller = new AbortController();
    deployRun.current = controller;
    const { signal } = controller;
    const sentYaml = yaml;
    const sentType = resourceType;
    const phase = pollingPhase[kind];
    setDeploying(true);
    setBuildState('waiting');
    setDeployError('');
    setDeployIssues([]);
    setFailurePhase('');
    let activePhase = appendedName ? phase : 'Updating stack';
    let appending = false;
    try {
      let deployedName = appendedName;
      if (!deployedName) {
        appending = true;
        setAppendInFlight(true);
        let result: Awaited<ReturnType<typeof appendToStack>>;
        try {
          result = await appendToStack(sentYaml, sentType);
        } finally {
          setAppendInFlight(false);
        }
        appending = false;
        // Recorded even after an abort: the server now has this YAML, and a
        // later Review step must offer "Check deployment" instead of a
        // duplicate append.
        if (sentType === 'mcp-server') {
          setAppendedResource({ resourceType: sentType, name: result.resourceName, yaml: sentYaml });
        }
        // Strictly false: an absent flag (older gateway) keeps the poll.
        if (result.watching === false) {
          if (sentType === 'mcp-server') {
            setAppendedResource(null);
            setBuildState('idle');
            showToast('warning', `${result.resourceName} was added to the stack file. Use Reload Config to start it.`);
          } else {
            showToast('success', `${result.resourceType} '${result.resourceName}' added to stack`);
            showToast('warning', 'Use Reload Config to start it.');
          }
          if (!signal.aborted) onDeploy?.();
          return;
        }
        if (signal.aborted || sentType !== 'mcp-server') {
          showToast('success', `${result.resourceType} '${result.resourceName}' added to stack`);
          if (!signal.aborted) onDeploy?.();
          return;
        }
        deployedName = result.resourceName;
      }
      activePhase = phase;
      for (let attempt = 0; attempt < 300; attempt += 1) {
        const status = await fetchStatus(signal);
        if (signal.aborted) return;
        const deployed = status['mcp-servers']?.find((item) => item.name === deployedName);
        if (deployed?.registrationFailed || deployed?.healthy === false) {
          activePhase = 'Connecting server';
          throw new Error(deployed.healthError || 'Server registration failed');
        }
        if (deployed?.initialized) {
          setAppendedResource(null);
          showToast('success', `${deployedName} deployed${currentResolution?.cached ? ' with a reused image' : currentResolution ? ' with a built image' : ''}`);
          setBuildState('complete');
          onDeploy?.();
          return;
        }
        await sleep(1000, signal);
        if (signal.aborted) return;
      }
      setBuildState('pending');
      showToast('warning', `${deployedName} was added to the stack; deployment is still in progress`);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Deploy failed';
      if (signal.aborted) {
        // The append itself was not cancelled, so its failure is still news.
        if (appending) showToast('error', message);
        return;
      }
      setBuildState('failed');
      setFailurePhase(activePhase);
      setDeployError(message);
      setDeployIssues(err instanceof StackAppendError ? err.issues : []);
      showToast('error', message);
    } finally {
      if (!signal.aborted) setDeploying(false);
    }
  };

  /** Extract name from YAML spec (looks for a top-level `name:` field). */
  const extractStackName = (): string => {
    const match = yaml.match(/^name:\s*(.+)$/m);
    if (match) return match[1].trim();
    // Fallback: timestamp-based name
    return `stack-${Date.now()}`;
  };

  const handleSaveAndLoad = async () => {
    setDeploying(true);
    const name = extractStackName();
    try {
      await saveStack(yaml, name);
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Save');
        showToast('error', described.text, { detail: described.fullDetail });
      }
      setDeploying(false);
      return;
    }

    try {
      await initializeStack(name);
      showToast('success', `Stack loaded — ${name} is now active`);
      onDeploy?.();
    } catch (err) {
      if (err instanceof StackAlreadyActiveError) {
        showToast('success', 'Stack saved to library');
        onDeploy?.();
      } else {
        // Save succeeded, load did not — close the modal so the user isn't trapped,
        // and give the recovery toast enough time to be read and acted on.
        showToast(
          'error',
          `Saved but could not load — restart with \`gridctl apply ~/.gridctl/stacks/${name}.yaml\``,
          { duration: 10000 },
        );
        onDeploy?.();
      }
    } finally {
      setDeploying(false);
    }
  };

  const statusLine = resourceType !== 'mcp-server'
    ? ''
    : appendInFlight
      ? 'Updating stack...'
      : buildState === 'waiting'
        ? pollingStatus[kind]
        : buildState === 'pending'
          ? 'Deployment is still in progress'
          : buildState === 'complete'
            ? 'Connected'
            : '';
  const showLogsLink = resourceType === 'mcp-server' && !appendInFlight &&
    (buildState === 'waiting' || buildState === 'pending' || buildState === 'failed');

  return (
    <div className="space-y-6">
      {executionPreview.yaml === yaml && Object.entries(executionPreview.reports).map(([name, report]) => <section key={name} className="rounded-lg border border-border p-3 space-y-2" aria-label={`Requested execution for ${name}`}>
        <h3 className="text-sm font-medium">Requested execution: {name}</h3>
        <p className="text-sm break-all">Mode: {report.mode}; revision: {report.revision}</p>
        <p className="text-sm">Resolved from the proposed YAML. These settings are not runtime evidence. Changing or removing them requires recreation. Review explicit privilege, filesystem, capability, and connected-network exceptions before applying.</p>
        <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-2">{(report.controls ?? []).map((control) => <div key={control.field}><dt className="font-mono">{control.field}</dt><dd className="break-all">{control.requested || '(empty)'}</dd></div>)}</dl>
      </section>)}
      {/* Validation Status */}
      <div
        className={cn(
          'flex items-center gap-3 p-4 rounded-xl border',
          validating
            ? 'bg-surface-overlay-1 border-border/30'
            : hasErrors
              ? 'bg-status-error/5 border-status-error/20'
              : warningCount > 0
                ? 'bg-status-pending/5 border-status-pending/20'
                : 'bg-status-running/5 border-status-running/20',
        )}
      >
        {validating ? (
          <>
            <Loader2 size={18} className="text-text-muted animate-spin" />
            <span className="text-sm text-text-secondary">Validating spec...</span>
          </>
        ) : hasErrors ? (
          <>
            <AlertCircle size={18} className="text-status-error" />
            <div>
              <div className="text-sm font-medium text-status-error">
                {errorCount} validation error{errorCount > 1 ? 's' : ''}
              </div>
              <div className="text-xs text-text-muted mt-0.5">
                {resourceType === 'stack' ? 'Fix errors before saving' : 'Fix errors before deploying'}
              </div>
            </div>
          </>
        ) : warningCount > 0 ? (
          <>
            <AlertTriangle size={18} className="text-status-pending" />
            <div>
              <div className="text-sm font-medium text-status-pending">
                {warningCount} warning{warningCount > 1 ? 's' : ''}
              </div>
              <div className="text-xs text-text-muted mt-0.5">
                Spec is valid but has warnings
              </div>
            </div>
          </>
        ) : (
          <>
            <CheckCircle2 size={18} className="text-status-running" />
            <div>
              <div className="text-sm font-medium text-status-running">
                Spec is valid
              </div>
              <div className="text-xs text-text-muted mt-0.5">
                {resourceType === 'stack' ? 'Ready to save' : 'Ready to deploy'}
              </div>
            </div>
          </>
        )}
      </div>

      {nameConflict && (
        <div className="flex items-start gap-2 px-4 py-3 rounded-xl border border-status-error/20 bg-status-error/5 text-xs text-status-error" role="alert">
          <AlertCircle size={14} className="flex-shrink-0 mt-0.5" />
          <span>A server or resource named '{deployName}' already exists in this stack. Choose a different name.</span>
        </div>
      )}

      {/* Issues List */}
      {issues.length > 0 && !validating && <IssueList issues={issues} />}

      {/* Resource Summary */}
      <div className="bg-surface-overlay-2 border border-border-subtle rounded-xl p-4">
        <div className="flex items-center gap-2 mb-3">
          <FileCode2 size={14} className="text-primary" />
          <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
            Summary
          </span>
        </div>
        <div className="grid grid-cols-2 gap-3 text-xs">
          <div>
            <span className="text-text-muted">Type</span>
            <div className="text-text-primary font-medium capitalize mt-0.5">{resourceType}</div>
          </div>
          <div>
            <span className="text-text-muted">Name</span>
            <div className="text-text-primary font-medium mt-0.5">{resourceName || '—'}</div>
          </div>
          <div>
            <span className="text-text-muted">Lines</span>
            <div className="text-text-primary font-medium mt-0.5">
              {yaml.split('\n').filter(Boolean).length}
            </div>
          </div>
          <div>
            <span className="text-text-muted">Status</span>
            <div
              className={cn(
                'font-medium mt-0.5',
                validating
                  ? 'text-text-muted'
                  : hasErrors
                    ? 'text-status-error'
                    : 'text-status-running',
              )}
            >
              {validating ? 'Checking...' : hasErrors ? 'Invalid' : 'Valid'}
            </div>
          </div>
          {operationsSummary && (
            <div className="col-span-2">
              <span className="text-text-muted">Operations</span>
              <div className="text-text-primary font-medium mt-0.5">{operationsSummary}</div>
            </div>
          )}
        </div>
      </div>

      {(resourceType === 'mcp-server' || resourceType === 'resource') && (
        <div
          role="group"
          aria-label="Deployment status"
          className={cn(statusLine || deployError || showLogsLink ? 'space-y-2 rounded-xl border border-border/30 p-4' : '')}
        >
          <div aria-live="polite">
            {statusLine && (
              <span
                className={cn(
                  'text-xs',
                  buildState === 'pending' ? 'text-status-pending' : buildState === 'complete' ? 'text-status-running' : 'text-primary',
                )}
              >
                {statusLine}
              </span>
            )}
          </div>
          {deployError && (
            <div ref={deployErrorRef} tabIndex={-1} className="space-y-2 p-4 rounded-xl border border-status-error/20 bg-status-error/5" role="alert">
              <p className="flex items-start gap-2 text-xs text-status-error">
                <AlertCircle size={14} className="flex-shrink-0 mt-0.5" />
                <span>{failurePhase ? `${failurePhase} failed: ${deployError}` : deployError}</span>
              </p>
              {deployIssues.length > 0 && <IssueList issues={deployIssues} />}
            </div>
          )}
          {showLogsLink && (
            <a href={`/logs?source=${encodeURIComponent(appendedName || resourceName)}`} className="inline-flex items-center gap-1 text-xs text-primary hover:underline">View filtered logs <ExternalLink size={11} /></a>
          )}
        </div>
      )}

      {pythonSource && (
        <div className="rounded-xl border border-primary/20 bg-primary/[0.03] p-4 space-y-3" aria-live="polite">
          <div className="flex items-center justify-between gap-3">
            <span className="text-xs font-medium text-text-primary">Python container build</span>
            {resolutionPending && <span className="text-3xs text-primary">Resolving package/ref...</span>}
          </div>
          {resolutionError && (
            <p ref={resolutionErrorRef} tabIndex={-1} className="text-xs text-status-error" role="alert">
              {resolutionError}
            </p>
          )}
          {currentResolution && (
            <div className="grid grid-cols-2 gap-3 text-xs">
              <div><span className="text-text-muted">Declared source</span><div className="font-mono text-text-primary mt-0.5">{currentResolution.declaredIdentity.package ? `${currentResolution.declaredIdentity.package}==${currentResolution.declaredIdentity.version}` : `${currentResolution.declaredIdentity.type}:${currentResolution.declaredIdentity.ref ?? ''}`}</div></div>
              <div><span className="text-text-muted">Resolved source</span><div className="font-mono text-text-primary mt-0.5 break-all">{currentResolution.resolvedIdentity.commit ?? currentResolution.resolvedIdentity.artifact ?? currentResolution.resolvedIdentity.version}</div></div>
              <div><span className="text-text-muted">Command</span><div className="font-mono text-text-primary mt-0.5">{(server?.command?.length ? server.command : currentResolution.command)?.join(' ') || 'Detected during resolution'}</div></div>
              <div><span className="text-text-muted">Python</span><div className="font-mono text-text-primary mt-0.5">{currentResolution.python || 'Selected during resolution'}</div></div>
              <div><span className="text-text-muted">Transport</span><div className="font-mono text-text-primary mt-0.5">{server?.transport || 'stdio'}</div></div>
              <div><span className="text-text-muted">Expected image</span><div className="font-mono text-text-primary mt-0.5 break-all">{currentResolution.imageTag}</div></div>
              {currentResolution.mutableRef && <div className="col-span-2 text-status-pending">This source uses a mutable Git ref. The resolved commit is pinned for this build.</div>}
              <div className="col-span-2 text-text-muted">{currentResolution.cached ? 'The matching image is already cached and will be reused.' : 'The first apply builds this image. Unchanged later applies reuse it.'}</div>
            </div>
          )}
          <ol className="grid grid-cols-2 gap-1 text-3xs text-text-muted" aria-label="Build phases">
            {['Resolving package/ref', 'Cloning/preparing context', 'Generating Dockerfile', 'Building image', 'Starting container', 'Connecting server'].map((phase) => <li key={phase}>{phase}</li>)}
          </ol>
        </div>
      )}

      {resourceType === 'mcp-server' && (
        <details className="rounded-xl border border-border/30 bg-background/30">
          <summary className="cursor-pointer px-4 py-3 text-xs font-medium text-text-secondary">Exact server YAML</summary>
          <pre className="overflow-x-auto border-t border-border/20 p-4 text-2xs text-text-secondary"><code>{yaml}</code></pre>
        </details>
      )}

      {currentResolution?.generatedFile && (
        <details className="rounded-xl border border-border/30 bg-background/30">
          <summary className="cursor-pointer px-4 py-3 text-xs font-medium text-text-secondary">Generated Dockerfile</summary>
          <pre className="overflow-x-auto border-t border-border/20 p-4 text-2xs text-text-secondary"><code>{currentResolution.generatedFile.content}</code></pre>
        </details>
      )}

      {previouslyAppendedName && (
        <p className="text-xs text-text-secondary">
          {previouslyAppendedName} was already added to the stack from this wizard. Deploying again adds another server, so its name must differ.
        </p>
      )}

      {isStackFragmentType(resourceType) && (
        <DeployChangesSection yaml={yaml} resourceType={resourceType} ready={!validating && !hasErrors} appendedName={appendedName} />
      )}
      {resourceType === 'stack' && <SaveLoadChangesSection yaml={yaml} activeStack={activeStack} />}

      {/* Output Actions */}
      <div className="flex items-center gap-3">
        <Button onClick={handleDownload} variant="secondary" size="sm">
          <Download size={14} />
          Download
        </Button>
        <Button onClick={handleCopy} variant="secondary" size="sm">
          {copied ? <CheckCircle2 size={14} /> : <Copy size={14} />}
          {copied ? 'Copied' : 'Copy'}
        </Button>
        {resourceType === 'stack' ? (
          <Button
            variant="primary"
            size="sm"
            onClick={handleSaveAndLoad}
            disabled={hasErrors || validating || deploying || Boolean(pythonSource && (!currentResolution || resolutionError))}
            className="ml-auto"
          >
            {deploying ? <Loader2 size={14} className="animate-spin" /> : <Library size={14} />}
            {deploying ? 'Saving...' : 'Save & Load'}
          </Button>
        ) : (
          <Button
            variant="primary"
            size="sm"
            onClick={handleDeploy}
            disabled={hasErrors || validating || deploying || appendInFlight || nameConflict || Boolean(pythonSource && (!currentResolution || resolutionError))}
            className="ml-auto"
          >
            {deploying || appendInFlight ? <Loader2 size={14} className="animate-spin" /> : <Rocket size={14} />}
            {appendInFlight ? 'Adding to stack...' : deploying ? 'Checking...' : appendedName ? 'Check deployment' : 'Deploy'}
          </Button>
        )}
      </div>
    </div>
  );
}
