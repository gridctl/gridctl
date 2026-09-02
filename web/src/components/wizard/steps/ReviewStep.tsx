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
import { validateStackSpec, validateStackResource, appendToStack, saveStack, initializeStack, StackAlreadyActiveError, resolvePythonSource, fetchStatus, type PythonResolution } from '../../../lib/api';
import type { ValidationIssue } from '../../../types';
import type { MCPServerFormData } from '../../../lib/yaml-builder';

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
  const [failurePhase, setFailurePhase] = useState('');
  const resolutionErrorRef = useRef<HTMLParagraphElement>(null);
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
    if (!yaml.trim()) return;
    try {
      const result = resourceType === 'mcp-server' || resourceType === 'resource'
        ? await validateStackResource(yaml, resourceType)
        : await validateStackSpec(yaml);
      setIssues(result.issues || []);
    } catch {
      setIssues([
        { field: 'yaml', message: 'Validation unavailable', severity: 'warning' },
      ]);
    } finally {
      setValidating(false);
    }
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
    if (resolutionError || deployError) resolutionErrorRef.current?.focus();
  }, [resolutionError, deployError]);

  const errorCount = issues.filter((i) => i.severity === 'error').length;
  const warningCount = issues.filter((i) => i.severity === 'warning').length;
  const hasErrors = errorCount > 0;

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
    setDeploying(true);
    setBuildState('waiting');
    setDeployError('');
    setFailurePhase('');
    let activePhase = 'Updating stack';
    try {
      const result = await appendToStack(yaml, resourceType);
      if (resourceType !== 'mcp-server') {
        showToast('success', `${result.resourceType} '${result.resourceName}' added to stack`);
        onDeploy?.();
        return;
      }
      activePhase = 'Building image, starting container, or connecting server';
      for (let attempt = 0; attempt < 300; attempt += 1) {
        const status = await fetchStatus();
        const deployed = status['mcp-servers']?.find((item) => item.name === resourceName);
        if (deployed?.registrationFailed || deployed?.healthy === false) {
          activePhase = 'Connecting server';
          throw new Error(deployed.healthError || 'Server registration failed');
        }
        if (deployed?.initialized) {
          showToast('success', `${result.resourceName} deployed${currentResolution?.cached ? ' with a reused image' : currentResolution ? ' with a built image' : ''}`);
          setBuildState('complete');
          onDeploy?.();
          return;
        }
        await new Promise((resolve) => window.setTimeout(resolve, 1000));
      }
      setBuildState('pending');
      showToast('warning', `${result.resourceName} was added to the stack; deployment is still in progress`);
    } catch (err) {
      setBuildState('failed');
      const message = err instanceof Error ? err.message : 'Deploy failed';
      setFailurePhase(activePhase);
      setDeployError(message);
      showToast('error', message);
    } finally {
      setDeploying(false);
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
      showToast('error', err instanceof Error ? err.message : 'Save failed');
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

  return (
    <div className="space-y-6">
      {/* Validation Status */}
      <div
        className={cn(
          'flex items-center gap-3 p-4 rounded-xl border',
          validating
            ? 'bg-white/[0.02] border-border/30'
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
                Fix errors before generating
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
                Ready to generate
              </div>
            </div>
          </>
        )}
      </div>

      {/* Issues List */}
      {issues.length > 0 && !validating && (
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
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Resource Summary */}
      <div className="bg-white/[0.03] border border-white/[0.06] rounded-xl p-4">
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

      {pythonSource && (
        <div className="rounded-xl border border-primary/20 bg-primary/[0.03] p-4 space-y-3" aria-live="polite">
          <div className="flex items-center justify-between gap-3">
            <span className="text-xs font-medium text-text-primary">Python container build</span>
            {resolutionPending && <span className="text-[10px] text-primary">Resolving package/ref...</span>}
            {buildState === 'waiting' && <span className="text-[10px] text-primary">Building image, starting container, and connecting server...</span>}
            {buildState === 'pending' && <span className="text-[10px] text-status-pending">Deployment is still in progress</span>}
            {buildState === 'complete' && <span className="text-[10px] text-status-running">Connected</span>}
          </div>
          {(resolutionError || deployError) && (
            <p ref={resolutionErrorRef} tabIndex={-1} className="text-xs text-status-error focus:outline-none" role="alert">
              {deployError && failurePhase ? `${failurePhase} failed: ${deployError}` : resolutionError}
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
              <div className="col-span-2 text-text-muted">{currentResolution.cached ? 'The matching image is already cached and will be reused.' : 'The first apply builds this image. Unchanged later applies reuse it.'}</div>
            </div>
          )}
          <ol className="grid grid-cols-2 gap-1 text-[10px] text-text-muted" aria-label="Build phases">
            {['Resolving package/ref', 'Cloning/preparing context', 'Generating Dockerfile', 'Building image', 'Starting container', 'Connecting server'].map((phase) => <li key={phase}>{phase}</li>)}
          </ol>
          {(buildState === 'waiting' || buildState === 'pending' || buildState === 'failed') && (
            <a href={`/logs?source=${encodeURIComponent(resourceName)}`} className="inline-flex items-center gap-1 text-xs text-primary hover:underline">View filtered logs <ExternalLink size={11} /></a>
          )}
        </div>
      )}

      {resourceType === 'mcp-server' && (
        <details className="rounded-xl border border-border/30 bg-background/30">
          <summary className="cursor-pointer px-4 py-3 text-xs font-medium text-text-secondary">Exact server YAML</summary>
          <pre className="overflow-x-auto border-t border-border/20 p-4 text-[11px] text-text-secondary"><code>{yaml}</code></pre>
        </details>
      )}

      {currentResolution?.generatedFile && (
        <details className="rounded-xl border border-border/30 bg-background/30">
          <summary className="cursor-pointer px-4 py-3 text-xs font-medium text-text-secondary">Generated Dockerfile</summary>
          <pre className="overflow-x-auto border-t border-border/20 p-4 text-[11px] text-text-secondary"><code>{currentResolution.generatedFile.content}</code></pre>
        </details>
      )}

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
            disabled={hasErrors || validating || deploying || buildState === 'pending' || Boolean(pythonSource && (!currentResolution || resolutionError))}
            className="ml-auto"
          >
            {deploying ? <Loader2 size={14} className="animate-spin" /> : <Rocket size={14} />}
            {deploying ? 'Deploying...' : 'Deploy'}
          </Button>
        )}
      </div>
    </div>
  );
}
