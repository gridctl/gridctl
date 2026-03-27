import { useState, useEffect, useCallback } from 'react';
import {
  CheckCircle2,
  AlertCircle,
  AlertTriangle,
  Download,
  Copy,
  Rocket,
  Loader2,
  FileCode2,
} from 'lucide-react';
import { cn } from '../../../lib/cn';
import { Button } from '../../ui/Button';
import { showToast } from '../../ui/Toast';
import { validateStackSpec, appendToStack } from '../../../lib/api';
import type { ValidationIssue } from '../../../types';

interface ReviewStepProps {
  yaml: string;
  resourceType: string;
  resourceName: string;
  onDeploy?: () => void;
}

export function ReviewStep({ yaml, resourceType, resourceName, onDeploy }: ReviewStepProps) {
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [validating, setValidating] = useState(true);
  const [copied, setCopied] = useState(false);
  const [deploying, setDeploying] = useState(false);

  const validate = useCallback(async () => {
    if (!yaml.trim()) {
      setIssues([]);
      setValidating(false);
      return;
    }
    setValidating(true);
    try {
      const result = await validateStackSpec(yaml);
      setIssues(result.issues || []);
    } catch {
      setIssues([
        { field: 'yaml', message: 'Validation unavailable', severity: 'warning' },
      ]);
    } finally {
      setValidating(false);
    }
  }, [yaml]);

  useEffect(() => {
    validate();
  }, [validate]);

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
    try {
      const result = await appendToStack(yaml, resourceType);
      showToast('success', `${result.resourceType} '${result.resourceName}' deployed to stack`);
      onDeploy?.();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Deploy failed');
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
        </div>
      </div>

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
        <Button
          variant="primary"
          size="sm"
          onClick={handleDeploy}
          disabled={hasErrors || validating || deploying}
          className="ml-auto"
        >
          {deploying ? <Loader2 size={14} className="animate-spin" /> : <Rocket size={14} />}
          {deploying ? 'Deploying...' : 'Deploy'}
        </Button>
      </div>
    </div>
  );
}
