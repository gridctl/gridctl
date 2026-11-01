import { useState, useCallback, useMemo, useEffect, useId } from 'react';
import {
  Container,
  GitBranch,
  Globe,
  Terminal,
  MonitorSmartphone,
  FileCode2,
  ChevronDown,
  ChevronRight,
  Plus,
  X,
  Zap,
  KeyRound,
  ShieldCheck,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { customHeaderErrors, probeHeaders, type AutoscaleFormData, type ExternalAuthFormData, type MCPServerFormData, type ServerType } from '../../../lib/yaml-builder';
import type { ProbeServerAuth, ProbeServerConfig } from '../../../lib/api';
import { VariablesPopover } from '../VariablesPopover';
import { TransportAdvisor } from '../TransportAdvisor';
import { ToolsPicker } from './ToolsPicker';
import { OperationsPicker } from './OperationsPicker';
import { fetchPythonPackageVersions } from '../../../lib/api';
import { cardPrefill, useDetectCard } from '../../../hooks/useDetectCard';
import type { DetectedCardInput } from '../../../lib/api';
import { Field, FieldError } from '../../ui/Field';
import { ExecutionFields } from './ExecutionFields';

// --- Server type definitions ---

interface ServerTypeOption {
  type: ServerType;
  icon: LucideIcon;
  label: string;
  description: string;
  transportDefault: string;
  transportLocked?: boolean;
  hidePort?: boolean;
}

const SERVER_TYPES: ServerTypeOption[] = [
  {
    type: 'container',
    icon: Container,
    label: 'Container',
    description: 'Docker image with HTTP, stdio, or SSE transport',
    transportDefault: 'http',
  },
  {
    type: 'source',
    icon: GitBranch,
    label: 'Source',
    description: 'Build from a Git repository, local path, or Python package',
    transportDefault: 'http',
  },
  {
    type: 'external',
    icon: Globe,
    label: 'External URL',
    description: 'Connect to an already-running remote server',
    transportDefault: 'http',
    hidePort: true,
  },
  {
    type: 'local',
    icon: Terminal,
    label: 'Local Process',
    description: 'Run a local command over stdio',
    transportDefault: 'stdio',
    transportLocked: true,
    hidePort: true,
  },
  {
    type: 'ssh',
    icon: MonitorSmartphone,
    label: 'SSH',
    description: 'Execute commands on a remote host via SSH',
    transportDefault: 'stdio',
    transportLocked: true,
    hidePort: true,
  },
  {
    type: 'openapi',
    icon: FileCode2,
    label: 'OpenAPI',
    description: 'Auto-generate tools from an OpenAPI specification',
    transportDefault: '',
    transportLocked: true,
    hidePort: true,
  },
];

const TRANSPORT_OPTIONS = [
  { value: 'http', label: 'HTTP' },
  { value: 'stdio', label: 'stdio' },
  { value: 'sse', label: 'SSE' },
];

const IMAGE_PRESETS = [
  'mcp/filesystem:latest',
  'mcp/fetch:latest',
  'mcp/postgres:latest',
  'mcp/sqlite:latest',
  'mcp/github:latest',
  'mcp/slack:latest',
  'mcp/memory:latest',
  'mcp/brave-search:latest',
];

const OUTPUT_FORMATS = ['json', 'toon', 'csv', 'text'];

// --- Field visibility logic ---

interface FieldVisibility {
  image: boolean;
  port: boolean;
  transport: boolean;
  command: boolean;
  source: boolean;
  url: boolean;
  ssh: boolean;
  openapi: boolean;
  buildArgs: boolean;
  network: boolean;
  replicas: boolean;
  autoscale: boolean;
}

function getFieldVisibility(serverType: ServerType): FieldVisibility {
  switch (serverType) {
    case 'container':
      return { image: true, port: true, transport: true, command: true, source: false, url: false, ssh: false, openapi: false, buildArgs: false, network: true, replicas: true, autoscale: true };
    case 'source':
      return { image: false, port: true, transport: true, command: true, source: true, url: false, ssh: false, openapi: false, buildArgs: true, network: true, replicas: true, autoscale: true };
    case 'external':
      return { image: false, port: false, transport: true, command: false, source: false, url: true, ssh: false, openapi: false, buildArgs: false, network: false, replicas: false, autoscale: false };
    case 'local':
      return { image: false, port: false, transport: false, command: true, source: false, url: false, ssh: false, openapi: false, buildArgs: false, network: false, replicas: true, autoscale: true };
    case 'ssh':
      return { image: false, port: false, transport: false, command: true, source: false, url: false, ssh: true, openapi: false, buildArgs: false, network: false, replicas: true, autoscale: true };
    case 'openapi':
      return { image: false, port: false, transport: false, command: false, source: false, url: false, ssh: false, openapi: true, buildArgs: false, network: false, replicas: false, autoscale: false };
  }
}

function getAvailableTransports(serverType: ServerType): string[] {
  switch (serverType) {
    case 'external':
      return ['http', 'sse'];
    case 'local':
    case 'ssh':
      return ['stdio'];
    case 'openapi':
      return [];
    default:
      return ['http', 'stdio', 'sse'];
  }
}

function showPortField(serverType: ServerType, transport: string): boolean {
  if (serverType === 'external' || serverType === 'local' || serverType === 'ssh' || serverType === 'openapi') return false;
  return transport !== 'stdio';
}

// Translate the wizard form into the probe endpoint's wire shape. Returns
// null for every transport the probe does not support — which, after the
// descope, is everything except external URL. Container / local-process / SSH
// servers are curated from the Stack sidebar after deploy; OpenAPI servers use
// the spec preview in OperationsPicker, which enumerates operations without
// running anything.
function buildProbeConfig(data: MCPServerFormData): ProbeServerConfig | null {
  if (data.serverType !== 'external') return null;
  if (!data.url) return null;
  return {
    name: data.name,
    execution: data.execution,
    url: data.url,
    transport: data.transport || '',
    env: data.env,
    auth: buildProbeAuth(data.auth),
    headers: probeHeaders(data.headers),
  };
}

// Map the form's auth block to the probe wire shape, dropping empty optional
// fields so Test Connection sends exactly what the YAML would declare.
function buildProbeAuth(auth: ExternalAuthFormData | undefined): ProbeServerAuth | undefined {
  if (!auth) return undefined;
  return {
    type: auth.type,
    token: auth.token || undefined,
    header: auth.header || undefined,
    value: auth.value || undefined,
    scopes: auth.scopes?.length ? auth.scopes : undefined,
    client_id: auth.clientId || undefined,
    client_secret: auth.clientSecret || undefined,
  };
}

const EXTERNAL_AUTH_LABELS: Record<ExternalAuthFormData['type'], string> = {
  bearer: 'Bearer token',
  header: 'Custom header',
  oauth: 'OAuth 2.1',
};

// --- Kebab-case validation ---

function toKebabCase(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-/, ''); // strip leading hyphen only; trailing stripped on blur
}

// --- Accordion section ---

function Section({
  title,
  icon: Icon,
  expanded,
  onToggle,
  children,
  badge,
}: {
  title: string;
  icon: LucideIcon;
  expanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
  badge?: string;
}) {
  return (
    <div className="border border-border/20 rounded-xl overflow-hidden transition-colors hover:border-border/30">
      <button
        type="button"
        onClick={onToggle}
        className="w-full flex items-center gap-2.5 px-4 py-3 text-left hover:bg-surface-overlay-1 transition-colors"
      >
        {expanded ? (
          <ChevronDown size={14} className="text-text-muted flex-shrink-0" />
        ) : (
          <ChevronRight size={14} className="text-text-muted flex-shrink-0" />
        )}
        <Icon size={14} className="text-primary flex-shrink-0" />
        <span className="text-xs font-medium text-text-primary">{title}</span>
        {badge && (
          <span className="ml-auto text-[10px] text-text-muted bg-surface-highlight px-1.5 py-0.5 rounded-full">
            {badge}
          </span>
        )}
      </button>
      {expanded && (
        <div className="px-4 pb-4 pt-1 space-y-3 animate-fade-in-up" style={{ animationDuration: '200ms' }}>
          {children}
        </div>
      )}
    </div>
  );
}

// --- Form field components ---

const inputClass = 'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors';
const labelClass = 'block text-xs text-text-secondary mb-1.5';

function CustomHeadersEditor({
  headers,
  authSet,
  onChange,
}: {
  headers?: Record<string, string>;
  authSet: boolean;
  onChange: (headers: Record<string, string> | undefined) => void;
}) {
  const value = headers ?? {};
  const rowErrors = customHeaderErrors(value, authSet);
  return (
    <div>
      <KeyValueEditor
        label="Custom headers"
        value={value}
        onChange={(next) => onChange(Object.keys(next).length ? next : undefined)}
        placeholder={{ key: 'X-Tenant', value: '${var:TENANT}' }}
        showSecrets
      />
      <p className="text-[10px] text-text-muted mt-1">Use a ${'{'}var:KEY{'}'} reference. Values are treated as sensitive.</p>
      {Object.entries(rowErrors).map(([name, message]) => (
        <FieldError key={name} error={`${name}: ${message}`} />
      ))}
    </div>
  );
}

// --- Key-Value Editor with secrets popover ---

function KeyValueEditor({
  label,
  value,
  onChange,
  placeholder,
  showSecrets = false,
}: {
  label: string;
  value: Record<string, string>;
  onChange: (val: Record<string, string>) => void;
  placeholder?: { key: string; value: string };
  showSecrets?: boolean;
}) {
  const entries = Object.entries(value);

  const addEntry = () => {
    onChange({ ...value, '': '' });
  };

  const updateKey = (_oldKey: string, newKey: string, idx: number) => {
    const newVal: Record<string, string> = {};
    Object.entries(value).forEach(([k, v], i) => {
      newVal[i === idx ? newKey : k] = v;
    });
    onChange(newVal);
  };

  const updateValue = (key: string, newValue: string) => {
    onChange({ ...value, [key]: newValue });
  };

  const removeEntry = (key: string) => {
    const next = { ...value };
    delete next[key];
    onChange(next);
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <label className="text-xs text-text-secondary">{label}</label>
        <button
          type="button"
          onClick={addEntry}
          className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <Plus size={10} />
          Add
        </button>
      </div>
      {entries.length === 0 && (
        <p className="text-[10px] text-text-muted/60 italic py-2">No entries</p>
      )}
      <div className="space-y-1.5">
        {entries.map(([key, val], i) => (
          <div key={i} className="flex items-center gap-1.5">
            <input
              type="text"
              value={key}
              onChange={(e) => updateKey(key, e.target.value, i)}
              placeholder={placeholder?.key ?? 'KEY'}
              aria-label={`${label} key ${i + 1}`}
              className={cn(inputClass, 'w-[40%] font-mono')}
            />
            <span className="text-text-muted text-xs">=</span>
            <div className="flex-1 flex items-center gap-0.5">
              <input
                type="text"
                value={val}
                onChange={(e) => updateValue(key, e.target.value)}
                placeholder={placeholder?.value ?? 'value'}
                aria-label={`${label} value ${i + 1}`}
                className={cn(inputClass, 'flex-1', val.startsWith('${var:') && 'text-tertiary font-medium')}
              />
              {showSecrets && (
                <VariablesPopover
                  onSelect={(ref) => updateValue(key, ref)}
                />
              )}
            </div>
            <button
              type="button"
              onClick={() => removeEntry(key)}
              aria-label={`Remove ${key || 'entry'}`}
              className="p-1 text-text-muted hover:text-status-error transition-colors flex-shrink-0"
            >
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

// --- Command Array Builder ---

function CommandArrayBuilder({
  value,
  onChange,
}: {
  value: string[];
  onChange: (val: string[]) => void;
}) {
  const addItem = () => onChange([...value, '']);
  const updateItem = (idx: number, val: string) => {
    const next = [...value];
    next[idx] = val;
    onChange(next);
  };
  const removeItem = (idx: number) => {
    onChange(value.filter((_, i) => i !== idx));
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <label className={labelClass}>Command</label>
        <button
          type="button"
          onClick={addItem}
          className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <Plus size={10} />
          Add argument
        </button>
      </div>
      {value.length === 0 && (
        <p className="text-[10px] text-text-muted/60 italic py-2">No command specified</p>
      )}
      <div className="space-y-1.5">
        {value.map((item, i) => (
          <div key={i} className="flex items-center gap-1.5">
            <span className="text-[10px] text-text-muted w-4 text-right flex-shrink-0">{i === 0 ? '$' : ''}</span>
            <input
              type="text"
              value={item}
              onChange={(e) => updateItem(i, e.target.value)}
              placeholder={i === 0 ? 'command' : `arg ${i}`}
              aria-label={i === 0 ? 'Command' : `Argument ${i}`}
              className={cn(inputClass, 'flex-1 font-mono')}
            />
            <button
              type="button"
              onClick={() => removeItem(i)}
              aria-label={`Remove ${i === 0 ? 'command' : `argument ${i}`}`}
              className="p-1 text-text-muted hover:text-status-error transition-colors flex-shrink-0"
            >
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

function splitValues(value: string): string[] | undefined {
  const values = value.split(',').map((item) => item.trim()).filter(Boolean);
  return values.length ? values : undefined;
}

function PythonSourceFields({
  data,
  onChange,
  errors,
}: MCPServerFormProps) {
  const source = data.source ?? { type: 'git' };
  const [versionResult, setVersionResult] = useState<{
    project: string;
    versions: string[];
    error: string;
    loading: boolean;
  }>({ project: '', versions: [], error: '', loading: false });
  const generated = source.type === 'pypi' || (source.runtime === 'python' && !source.dockerfile);
  const project = source.type === 'pypi' ? source.package?.trim() ?? '' : '';
  const currentVersionResult = versionResult.project === project ? versionResult : undefined;

  useEffect(() => {
    if (!project) return;
    let active = true;
    const timer = window.setTimeout(async () => {
      setVersionResult({ project, versions: [], error: '', loading: true });
      try {
        const result = await fetchPythonPackageVersions(project);
        if (!active) return;
        setVersionResult({ project, versions: result.versions, error: '', loading: false });
        if (!source.ref && result.latest) {
          onChange({ source: { ...source, ref: result.latest } });
        }
      } catch (error) {
        if (active) setVersionResult({
          project,
          versions: [],
          error: error instanceof Error ? error.message : 'Could not resolve package versions',
          loading: false,
        });
      }
    }, 350);
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [project, source.ref]); // eslint-disable-line react-hooks/exhaustive-deps

  const updateSource = (patch: Partial<NonNullable<MCPServerFormData['source']>>) => {
    onChange({ source: { ...source, ...patch } as MCPServerFormData['source'] });
  };

  const selectCustomDockerfile = (dockerfile: string) => updateSource({
    runtime: undefined,
    dockerfile,
    path: source.type === 'git' ? undefined : source.path,
    projectPath: undefined,
    python: undefined,
    extras: undefined,
    with: undefined,
    packages: undefined,
  });

  const selectGeneratedPython = () => onChange({
    source: { ...source, runtime: 'python', dockerfile: undefined },
    transport: 'stdio',
  });

  return (
    <div className="space-y-3 p-3 rounded-xl bg-surface-overlay-1 border border-border-subtle">
      <div>
        <label className={labelClass}>Source Type</label>
        <div className="flex gap-2" role="radiogroup" aria-label="Source type">
          {[
            ['git', 'Git'],
            ['local', 'Local'],
            ['pypi', 'Package'],
          ].map(([type, label]) => (
            <button
              key={type}
              type="button"
              role="radio"
              aria-checked={source.type === type}
              onClick={() => onChange({
                source: {
                  type,
                  runtime: type === 'pypi' ? 'python' : source.runtime,
                } as MCPServerFormData['source'],
                transport: type === 'pypi' ? 'stdio' : data.transport,
              })}
              className={cn(
                'px-3 py-1.5 rounded-lg text-xs font-medium transition-all border',
                source.type === type ? 'bg-primary/10 border-primary/30 text-primary' : 'border-border-subtle text-text-muted',
              )}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {source.type === 'pypi' ? (
        <div className="grid grid-cols-2 gap-2">
          <div>
            <label htmlFor="python-package" className={labelClass}>Project name <span className="text-status-error">*</span></label>
            <input id="python-package" value={source.package ?? ''} onChange={(event) => updateSource({ package: event.target.value })} placeholder="mcp-server-fetch" aria-invalid={errors?.['source.package'] ? true : undefined} aria-describedby={errors?.['source.package'] ? 'python-package-error' : undefined} className={cn(inputClass, 'font-mono')} />
            <FieldError id="python-package-error" error={errors?.['source.package']} />
          </div>
          <div>
            <label htmlFor="python-version" className={labelClass}>Exact version <span className="text-status-error">*</span></label>
            <input id="python-version" value={source.ref ?? ''} onChange={(event) => updateSource({ ref: event.target.value })} list="python-package-versions" placeholder={!currentVersionResult || currentVersionResult.loading ? 'Resolving...' : '1.0.0'} aria-invalid={errors?.['source.ref'] || currentVersionResult?.error ? true : undefined} aria-describedby={errors?.['source.ref'] || currentVersionResult?.error ? 'python-version-error' : undefined} className={cn(inputClass, 'font-mono')} />
            <datalist id="python-package-versions">{currentVersionResult?.versions.map((version) => <option key={version} value={version} />)}</datalist>
            <FieldError id="python-version-error" error={errors?.['source.ref'] ?? currentVersionResult?.error} />
          </div>
        </div>
      ) : (
        <>
          {source.type === 'git' ? (
            <>
              <div>
                <label htmlFor="source-url" className={labelClass}>Repository URL <span className="text-status-error">*</span></label>
                <input id="source-url" type="url" value={source.url ?? ''} onChange={(event) => updateSource({ url: event.target.value })} placeholder="https://github.com/org/repo.git" aria-invalid={errors?.['source.url'] ? true : undefined} aria-describedby={errors?.['source.url'] ? 'source-url-error' : undefined} className={cn(inputClass, errors?.['source.url'] && 'border-status-error/50')} />
                <FieldError id="source-url-error" error={errors?.['source.url']} />
              </div>
              <div>
                <label htmlFor="source-ref" className={labelClass}>Ref</label>
                <input id="source-ref" value={source.ref ?? ''} onChange={(event) => updateSource({ ref: event.target.value })} placeholder="main" className={cn(inputClass, 'font-mono')} />
                {(!source.ref || !/^[0-9a-f]{40}$/i.test(source.ref)) && <p className="text-[10px] text-status-pending mt-1">This ref resembles a mutable branch. Review will show the resolved commit.</p>}
              </div>
              <SourceAuthField value={source.auth?.credentialRef} onChange={(credentialRef) => updateSource({ auth: credentialRef ? { method: 'token', credentialRef } : undefined })} />
            </>
          ) : (
            <div>
              <label htmlFor="source-path" className={labelClass}>Local root <span className="text-status-error">*</span></label>
              <input id="source-path" value={source.path ?? ''} onChange={(event) => updateSource({ path: event.target.value })} placeholder="./path/to/server" aria-invalid={errors?.['source.path'] ? true : undefined} aria-describedby={errors?.['source.path'] ? 'source-path-error' : undefined} className={cn(inputClass, 'font-mono')} />
              <FieldError id="source-path-error" error={errors?.['source.path']} />
            </div>
          )}

          <div>
            <label className={labelClass}>Build strategy</label>
            <div className="flex gap-2" role="radiogroup" aria-label="Build strategy">
              <button
                type="button"
                role="radio"
                aria-checked={!generated}
                onClick={() => selectCustomDockerfile(source.dockerfile || 'Dockerfile')}
                className={cn('px-3 py-1.5 rounded-lg text-xs border', !generated ? 'border-primary/30 text-primary' : 'border-border-subtle text-text-muted')}
              >Custom Dockerfile</button>
              <button
                type="button"
                role="radio"
                aria-checked={generated}
                onClick={selectGeneratedPython}
                className={cn('px-3 py-1.5 rounded-lg text-xs border', generated ? 'border-primary/30 text-primary' : 'border-border-subtle text-text-muted')}
              >Generated Python</button>
            </div>
            {!generated && (
              <p className="text-[10px] text-text-muted mt-1">
                Custom Dockerfiles can derive from Gridctl&apos;s Python runtime base. That image is a foundation, not a ready MCP server. Keep stdio explicit, pin a tested digest, and do not replace Generated Python 3.10-3.13 automatically. See docs/mcp-runtime-python.md.
              </p>
            )}
          </div>
          <div className={cn('grid gap-2', generated && 'grid-cols-2')}>
            {generated && <div>
              <label htmlFor="project-path" className={labelClass}>Project subdirectory</label>
              <input
                id="project-path"
                value={(source.type === 'git' ? source.path : source.projectPath) ?? ''}
                onChange={(event) => updateSource(source.type === 'git'
                  ? { path: event.target.value, projectPath: undefined }
                  : { projectPath: event.target.value })}
                placeholder="packages/server"
                className={cn(inputClass, 'font-mono')}
              />
            </div>}
            <div>
              <label htmlFor="source-dockerfile" className={labelClass}>Dockerfile</label>
              <input
                id="source-dockerfile"
                value={source.dockerfile ?? ''}
                onChange={(event) => event.target.value
                  ? selectCustomDockerfile(event.target.value)
                  : selectGeneratedPython()}
                placeholder={generated ? 'Empty uses generated Python' : 'Dockerfile'}
                className={cn(inputClass, 'font-mono')}
              />
            </div>
          </div>
        </>
      )}

      {generated && (
        <details className="rounded-lg border border-border/30 px-3 py-2">
          <summary className="cursor-pointer text-xs text-text-secondary">Advanced Python options</summary>
          <div className="grid grid-cols-2 gap-2 pt-3">
            <label className={labelClass}>Python version<input aria-label="Python version" value={source.python ?? ''} onChange={(event) => updateSource({ python: event.target.value })} placeholder="3.12" className={cn(inputClass, 'mt-1 font-mono')} /></label>
            <label className={labelClass}>Extras<input aria-label="Python extras" value={source.extras?.join(', ') ?? ''} onChange={(event) => updateSource({ extras: splitValues(event.target.value) })} placeholder="cli, speedups" className={cn(inputClass, 'mt-1 font-mono')} /></label>
            <label className={labelClass}>Extra dependencies<input aria-label="Python extra dependencies" value={source.with?.join(', ') ?? ''} onChange={(event) => updateSource({ with: splitValues(event.target.value) })} placeholder="httpx>=0.27" className={cn(inputClass, 'mt-1 font-mono')} /></label>
            <label className={labelClass}>OS packages<input aria-label="Python OS packages" value={source.packages?.join(', ') ?? ''} onChange={(event) => updateSource({ packages: splitValues(event.target.value) })} placeholder="libpq5" className={cn(inputClass, 'mt-1 font-mono')} /></label>
          </div>
        </details>
      )}

      {generated && <p className="text-[10px] text-text-muted">The first apply builds an image. Later applies reuse it while all build inputs stay unchanged.</p>}
    </div>
  );
}

// --- Main MCPServerForm ---

interface MCPServerFormProps {
  data: MCPServerFormData;
  onChange: (data: Partial<MCPServerFormData>) => void;
  errors?: Record<string, string>;
}

export function MCPServerForm({ data, onChange, errors }: MCPServerFormProps) {
  const uid = useId();
  const cardDetect = useDetectCard();
  const [cardInputs, setCardInputs] = useState<DetectedCardInput[]>([]);
  const [expandedSections, setExpandedSections] = useState<Set<string>>(
    new Set(['identity', 'type', 'config']),
  );

  const toggleSection = useCallback((section: string) => {
    setExpandedSections((prev) => {
      const next = new Set(prev);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  }, []);

  const visibility = useMemo(() => getFieldVisibility(data.serverType), [data.serverType]);
  const availableTransports = useMemo(() => getAvailableTransports(data.serverType), [data.serverType]);
  const typeOption = SERVER_TYPES.find((t) => t.type === data.serverType)!;
  const portVisible = showPortField(data.serverType, data.transport ?? typeOption.transportDefault);

  const handleTypeChange = (newType: ServerType) => {
    const opt = SERVER_TYPES.find((t) => t.type === newType)!;
    onChange({
      serverType: newType,
      transport: opt.transportLocked ? opt.transportDefault : (data.transport ?? opt.transportDefault),
    });
    // Auto-expand config section on type change
    setExpandedSections((prev) => new Set([...prev, 'config']));
  };

  const probeConfig = useMemo(() => buildProbeConfig(data), [data]);

  const envCount = data.env ? Object.keys(data.env).length : 0;
  const advancedCount =
    (data.tools?.length ?? 0) +
    (data.outputFormat ? 1 : 0) +
    (data.buildArgs ? Object.keys(data.buildArgs).length : 0) +
    (data.network ? 1 : 0) +
    (data.pinSchemas !== undefined ? 1 : 0) +
    (data.replicas !== undefined && data.replicas !== 1 ? 1 : 0) +
    (data.autoscale ? 1 : 0);

  return (
    <div className="space-y-3">
      {/* Section 1: Identity */}
      <Section
        title="Identity"
        icon={Zap}
        expanded={expandedSections.has('identity')}
        onToggle={() => toggleSection('identity')}
      >
        <Field label="Name" required error={errors?.name} help="Kebab-case identifier for this server">
          {({ id, describedBy, invalid }) => (
            <input
              id={id}
              type="text"
              value={data.name}
              onChange={(e) => onChange({ name: toKebabCase(e.target.value) })}
              onBlur={(e) => onChange({ name: e.target.value.replace(/-+$/, '') })}
              placeholder="my-server"
              aria-describedby={describedBy}
              aria-invalid={invalid}
              className={cn(inputClass, 'font-mono', errors?.name && 'border-status-error/50')}
            />
          )}
        </Field>
      </Section>

      {/* Section 2: Server Type */}
      <Section
        title="Server Type"
        icon={Container}
        expanded={expandedSections.has('type')}
        onToggle={() => toggleSection('type')}
        badge={typeOption.label}
      >
        <div className="grid grid-cols-3 gap-2">
          {SERVER_TYPES.map((opt) => {
            const Icon = opt.icon;
            const isSelected = data.serverType === opt.type;
            return (
              <button
                key={opt.type}
                type="button"
                onClick={() => handleTypeChange(opt.type)}
                className={cn(
                  'group flex flex-col items-center text-center p-3 rounded-xl border transition-all duration-200',
                  'bg-surface-overlay-1 hover:bg-surface-overlay-3',
                  isSelected
                    ? 'border-primary/50 bg-primary/[0.06] shadow-[0_0_16px_--alpha(var(--color-primary)/8%)]'
                    : 'border-border-subtle hover:border-border',
                )}
              >
                <Icon
                  size={16}
                  className={cn(
                    'mb-1.5 transition-colors',
                    isSelected ? 'text-primary' : 'text-text-muted group-hover:text-text-secondary',
                  )}
                />
                <span className={cn('text-[11px] font-medium', isSelected ? 'text-primary' : 'text-text-primary')}>
                  {opt.label}
                </span>
                <span className="text-[9px] text-text-muted leading-tight mt-0.5 line-clamp-2">
                  {opt.description}
                </span>
              </button>
            );
          })}
        </div>
      </Section>

      {/* Section 3: Type-Specific Config */}
      <Section
        title="Configuration"
        icon={typeOption.icon}
        expanded={expandedSections.has('config')}
        onToggle={() => toggleSection('config')}
      >
        <div className="space-y-3 transition-all duration-200">
          {/* Container: image */}
          {visibility.image && (
            <Field label="Image" required error={errors?.image}>
              {({ id, describedBy, invalid }) => (
                <div className="relative">
                  <input
                    id={id}
                    type="text"
                    value={data.image ?? ''}
                    onChange={(e) => onChange({ image: e.target.value })}
                    placeholder="image:tag"
                    list="image-presets"
                    aria-describedby={describedBy}
                    aria-invalid={invalid}
                    className={cn(inputClass, 'font-mono', errors?.image && 'border-status-error/50')}
                  />
                  <datalist id="image-presets">
                    {IMAGE_PRESETS.map((p) => (
                      <option key={p} value={p} />
                    ))}
                  </datalist>
                </div>
              )}
            </Field>
          )}

          {/* External: url */}
          {visibility.url && (
            <div>
              <label htmlFor={`${uid}-url`} className={labelClass}>
                URL <span className="text-status-error">*</span>
              </label>
              <div className="flex items-center gap-2">
                <input
                  id={`${uid}-url`}
                  type="url"
                  value={data.url ?? ''}
                  onChange={(e) => onChange({ url: e.target.value })}
                  placeholder="https://my-server.example.com/mcp"
                  aria-invalid={errors?.url ? true : undefined}
                  aria-describedby={errors?.url ? `${uid}-url-error` : undefined}
                  className={cn(inputClass, 'flex-1', errors?.url && 'border-status-error/50')}
                />
                <button
                  type="button"
                  className="shrink-0 rounded-lg border border-border/40 px-2 py-2 text-xs text-text-secondary hover:text-text-primary"
                  disabled={cardDetect.loading || !data.url}
                  onClick={() => {
                    if (!data.url) return;
                    void cardDetect.detect(data.url).then((result) => {
                      if (!result) return;
                      onChange(cardPrefill(result.server));
                      setCardInputs(result.inputs ?? []);
                    });
                  }}
                >
                  {cardDetect.loading ? 'Detecting...' : 'Detect from URL'}
                </button>
              </div>
              <FieldError id={`${uid}-url-error`} error={errors?.url} />
              {cardDetect.error && (
                <p className="text-[10px] text-status-error mt-1">{cardDetect.error.message}</p>
              )}
              {cardInputs.length > 0 && (
                <ul className="mt-1 space-y-0.5 text-[10px] text-text-muted">
                  {cardInputs.map((input) => (
                    <li key={input.name}>
                      {input.name}
                      {input.required ? ' (required)' : ''}
                      {input.secret ? ', secret' : ''}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}

          {/* Source config */}
          {visibility.source && (
            <PythonSourceFields data={data} onChange={onChange} errors={errors} />
          )}

          {/* SSH config */}
          {visibility.ssh && (
            <div className="space-y-3 p-3 rounded-xl bg-surface-overlay-1 border border-border-subtle">
              <div className="grid grid-cols-2 gap-2">
                <Field label="Host" required error={errors?.['ssh.host']}>
                  {({ id, describedBy, invalid }) => (
                    <input
                      id={id}
                      type="text"
                      value={data.ssh?.host ?? ''}
                      onChange={(e) =>
                        onChange({ ssh: { ...data.ssh, host: e.target.value, user: data.ssh?.user ?? '' } })
                      }
                      placeholder="192.168.1.100"
                      aria-describedby={describedBy}
                      aria-invalid={invalid}
                      className={cn(inputClass, 'font-mono', errors?.['ssh.host'] && 'border-status-error/50')}
                    />
                  )}
                </Field>
                <Field label="User" required error={errors?.['ssh.user']}>
                  {({ id, describedBy, invalid }) => (
                    <input
                      id={id}
                      type="text"
                      value={data.ssh?.user ?? ''}
                      onChange={(e) =>
                        onChange({ ssh: { ...data.ssh, host: data.ssh?.host ?? '', user: e.target.value } })
                      }
                      placeholder="root"
                      aria-describedby={describedBy}
                      aria-invalid={invalid}
                      className={cn(inputClass, 'font-mono', errors?.['ssh.user'] && 'border-status-error/50')}
                    />
                  )}
                </Field>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label htmlFor={`${uid}-ssh-port`} className={labelClass}>Port</label>
                  <input
                    id={`${uid}-ssh-port`}
                    type="number"
                    value={data.ssh?.port ?? 22}
                    onChange={(e) =>
                      onChange({
                        ssh: {
                          ...data.ssh,
                          host: data.ssh?.host ?? '',
                          user: data.ssh?.user ?? '',
                          port: e.target.value ? Number(e.target.value) : undefined,
                        },
                      })
                    }
                    placeholder="22"
                    className={inputClass}
                  />
                </div>
                <div>
                  <label htmlFor={`${uid}-identity-file`} className={labelClass}>Identity File</label>
                  <input
                    id={`${uid}-identity-file`}
                    type="text"
                    value={data.ssh?.identityFile ?? ''}
                    onChange={(e) =>
                      onChange({
                        ssh: {
                          ...data.ssh,
                          host: data.ssh?.host ?? '',
                          user: data.ssh?.user ?? '',
                          identityFile: e.target.value,
                        },
                      })
                    }
                    placeholder="~/.ssh/id_rsa"
                    className={cn(inputClass, 'font-mono')}
                  />
                </div>
              </div>
              <div>
                <label htmlFor={`${uid}-known-hosts`} className={labelClass}>Known Hosts File</label>
                <input
                  id={`${uid}-known-hosts`}
                  type="text"
                  value={data.ssh?.knownHostsFile ?? ''}
                  onChange={(e) =>
                    onChange({
                      ssh: {
                        ...data.ssh,
                        host: data.ssh?.host ?? '',
                        user: data.ssh?.user ?? '',
                        knownHostsFile: e.target.value,
                      },
                    })
                  }
                  placeholder="~/.ssh/known_hosts"
                  className={cn(inputClass, 'font-mono')}
                />
                <p className="text-[10px] text-text-muted mt-1">Optional — enables StrictHostKeyChecking=yes</p>
              </div>
              <div>
                <label htmlFor={`${uid}-jump-host`} className={labelClass}>Jump Host</label>
                <input
                  id={`${uid}-jump-host`}
                  type="text"
                  value={data.ssh?.jumpHost ?? ''}
                  onChange={(e) =>
                    onChange({
                      ssh: {
                        ...data.ssh,
                        host: data.ssh?.host ?? '',
                        user: data.ssh?.user ?? '',
                        jumpHost: e.target.value,
                      },
                    })
                  }
                  placeholder="[user@]bastion.example.com[:22]"
                  className={cn(inputClass, 'font-mono')}
                />
                <p className="text-[10px] text-text-muted mt-1">Optional — bastion/jump host for multi-hop SSH</p>
              </div>
            </div>
          )}

          {/* OpenAPI config */}
          {visibility.openapi && (
            <div className="space-y-3 p-3 rounded-xl bg-surface-overlay-1 border border-border-subtle">
              <Field label="Spec" required error={errors?.['openapi.spec']}>
                {({ id, describedBy, invalid }) => (
                  <input
                    id={id}
                    type="text"
                    value={data.openapi?.spec ?? ''}
                    onChange={(e) =>
                      onChange({ openapi: { ...data.openapi, spec: e.target.value } })
                    }
                    placeholder="https://api.example.com/openapi.yaml or ./spec.yaml"
                    aria-describedby={describedBy}
                    aria-invalid={invalid}
                    className={cn(inputClass, errors?.['openapi.spec'] && 'border-status-error/50')}
                  />
                )}
              </Field>
              {/* openapi.auth is not auth:; the server does not treat it as a conflict. */}
              <CustomHeadersEditor
                headers={data.headers}
                authSet={Boolean(data.auth)}
                onChange={(headers) => onChange({ headers })}
              />
              <div>
                <label htmlFor={`${uid}-base-url`} className={labelClass}>Base URL</label>
                <input
                  id={`${uid}-base-url`}
                  type="url"
                  value={data.openapi?.baseUrl ?? ''}
                  onChange={(e) =>
                    onChange({ openapi: { ...data.openapi, spec: data.openapi?.spec ?? '', baseUrl: e.target.value } })
                  }
                  placeholder="https://api.example.com"
                  className={inputClass}
                />
              </div>
              {/* Auth */}
              <div>
                <label htmlFor={`${uid}-openapi-auth`} className={labelClass}>Authentication</label>
                <select
                  id={`${uid}-openapi-auth`}
                  value={data.openapi?.auth?.type ?? ''}
                  onChange={(e) => {
                    const authType = e.target.value;
                    if (!authType) {
                      onChange({ openapi: { ...data.openapi, spec: data.openapi?.spec ?? '', auth: undefined } });
                    } else {
                      onChange({
                        openapi: {
                          ...data.openapi,
                          spec: data.openapi?.spec ?? '',
                          auth: { type: authType, tokenEnv: data.openapi?.auth?.tokenEnv ?? '' },
                        },
                      });
                    }
                  }}
                  className={inputClass}
                >
                  <option value="">None</option>
                  <option value="bearer">Bearer Token</option>
                  <option value="header">Custom Header</option>
                  <option value="query">Query Parameter</option>
                  <option value="oauth2">OAuth2 Client Credentials</option>
                  <option value="basic">Basic Auth</option>
                </select>
              </div>
              {data.openapi?.auth?.type === 'bearer' && (
                <div>
                  <label htmlFor={`${uid}-token-env`} className={labelClass}>Token Environment Variable</label>
                  <input
                    id={`${uid}-token-env`}
                    type="text"
                    value={data.openapi.auth.tokenEnv ?? ''}
                    onChange={(e) =>
                      onChange({
                        openapi: {
                          ...data.openapi,
                          spec: data.openapi?.spec ?? '',
                          auth: { ...data.openapi!.auth!, tokenEnv: e.target.value },
                        },
                      })
                    }
                    placeholder="API_TOKEN"
                    className={cn(inputClass, 'font-mono')}
                  />
                </div>
              )}
              {data.openapi?.auth?.type === 'header' && (
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label htmlFor={`${uid}-header-name`} className={labelClass}>Header Name</label>
                    <input
                      id={`${uid}-header-name`}
                      type="text"
                      value={data.openapi.auth.header ?? ''}
                      onChange={(e) =>
                        onChange({
                          openapi: {
                            ...data.openapi,
                            spec: data.openapi?.spec ?? '',
                            auth: { ...data.openapi!.auth!, header: e.target.value },
                          },
                        })
                      }
                      placeholder="X-API-Key"
                      className={cn(inputClass, 'font-mono')}
                    />
                  </div>
                  <div>
                    <label htmlFor={`${uid}-header-value-env`} className={labelClass}>Value Environment Variable</label>
                    <input
                      id={`${uid}-header-value-env`}
                      type="text"
                      value={data.openapi.auth.valueEnv ?? ''}
                      onChange={(e) =>
                        onChange({
                          openapi: {
                            ...data.openapi,
                            spec: data.openapi?.spec ?? '',
                            auth: { ...data.openapi!.auth!, valueEnv: e.target.value },
                          },
                        })
                      }
                      placeholder="API_KEY"
                      className={cn(inputClass, 'font-mono')}
                    />
                  </div>
                </div>
              )}
              {data.openapi?.auth?.type === 'query' && (
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label htmlFor={`${uid}-param-name`} className={labelClass}>Parameter Name</label>
                    <input
                      id={`${uid}-param-name`}
                      type="text"
                      value={data.openapi.auth.paramName ?? ''}
                      onChange={(e) =>
                        onChange({
                          openapi: {
                            ...data.openapi,
                            spec: data.openapi?.spec ?? '',
                            auth: { ...data.openapi!.auth!, paramName: e.target.value },
                          },
                        })
                      }
                      placeholder="appid"
                      className={cn(inputClass, 'font-mono')}
                    />
                  </div>
                  <div>
                    <label htmlFor={`${uid}-query-value-env`} className={labelClass}>Value Environment Variable</label>
                    <input
                      id={`${uid}-query-value-env`}
                      type="text"
                      value={data.openapi.auth.valueEnv ?? ''}
                      onChange={(e) =>
                        onChange({
                          openapi: {
                            ...data.openapi,
                            spec: data.openapi?.spec ?? '',
                            auth: { ...data.openapi!.auth!, valueEnv: e.target.value },
                          },
                        })
                      }
                      placeholder="WEATHER_API_KEY"
                      className={cn(inputClass, 'font-mono')}
                    />
                  </div>
                </div>
              )}
              {data.openapi?.auth?.type === 'oauth2' && (
                <div className="space-y-2">
                  <div className="grid grid-cols-2 gap-2">
                    <div>
                      <label htmlFor={`${uid}-client-id-env`} className={labelClass}>Client ID Env Var</label>
                      <input
                        id={`${uid}-client-id-env`}
                        type="text"
                        value={data.openapi.auth.clientIdEnv ?? ''}
                        onChange={(e) =>
                          onChange({
                            openapi: {
                              ...data.openapi,
                              spec: data.openapi?.spec ?? '',
                              auth: { ...data.openapi!.auth!, clientIdEnv: e.target.value },
                            },
                          })
                        }
                        placeholder="OAUTH2_CLIENT_ID"
                        className={cn(inputClass, 'font-mono')}
                      />
                    </div>
                    <div>
                      <label htmlFor={`${uid}-client-secret-env`} className={labelClass}>Client Secret Env Var</label>
                      <input
                        id={`${uid}-client-secret-env`}
                        type="text"
                        value={data.openapi.auth.clientSecretEnv ?? ''}
                        onChange={(e) =>
                          onChange({
                            openapi: {
                              ...data.openapi,
                              spec: data.openapi?.spec ?? '',
                              auth: { ...data.openapi!.auth!, clientSecretEnv: e.target.value },
                            },
                          })
                        }
                        placeholder="OAUTH2_CLIENT_SECRET"
                        className={cn(inputClass, 'font-mono')}
                      />
                    </div>
                  </div>
                  <div>
                    <label htmlFor={`${uid}-token-url`} className={labelClass}>Token URL</label>
                    <input
                      id={`${uid}-token-url`}
                      type="url"
                      value={data.openapi.auth.tokenUrl ?? ''}
                      onChange={(e) =>
                        onChange({
                          openapi: {
                            ...data.openapi,
                            spec: data.openapi?.spec ?? '',
                            auth: { ...data.openapi!.auth!, tokenUrl: e.target.value },
                          },
                        })
                      }
                      placeholder="https://auth.example.com/oauth/token"
                      className={inputClass}
                    />
                  </div>
                  <div>
                    <label htmlFor={`${uid}-openapi-scopes`} className={labelClass}>Scopes</label>
                    <input
                      id={`${uid}-openapi-scopes`}
                      type="text"
                      value={(data.openapi.auth.scopes ?? []).join(' ')}
                      onChange={(e) =>
                        onChange({
                          openapi: {
                            ...data.openapi,
                            spec: data.openapi?.spec ?? '',
                            auth: {
                              ...data.openapi!.auth!,
                              scopes: e.target.value ? e.target.value.split(/[\s,]+/).filter(Boolean) : undefined,
                            },
                          },
                        })
                      }
                      placeholder="read:data write:data"
                      className={inputClass}
                    />
                    <p className="text-[10px] text-text-muted mt-1">Space or comma-separated</p>
                  </div>
                </div>
              )}
              {data.openapi?.auth?.type === 'basic' && (
                <div className="grid grid-cols-2 gap-2">
                  <div>
                    <label htmlFor={`${uid}-username-env`} className={labelClass}>Username Env Var</label>
                    <input
                      id={`${uid}-username-env`}
                      type="text"
                      value={data.openapi.auth.usernameEnv ?? ''}
                      onChange={(e) =>
                        onChange({
                          openapi: {
                            ...data.openapi,
                            spec: data.openapi?.spec ?? '',
                            auth: { ...data.openapi!.auth!, usernameEnv: e.target.value },
                          },
                        })
                      }
                      placeholder="API_USERNAME"
                      className={cn(inputClass, 'font-mono')}
                    />
                  </div>
                  <div>
                    <label htmlFor={`${uid}-password-env`} className={labelClass}>Password Env Var</label>
                    <input
                      id={`${uid}-password-env`}
                      type="text"
                      value={data.openapi.auth.passwordEnv ?? ''}
                      onChange={(e) =>
                        onChange({
                          openapi: {
                            ...data.openapi,
                            spec: data.openapi?.spec ?? '',
                            auth: { ...data.openapi!.auth!, passwordEnv: e.target.value },
                          },
                        })
                      }
                      placeholder="API_PASSWORD"
                      className={cn(inputClass, 'font-mono')}
                    />
                  </div>
                </div>
              )}
              {/* Operations filter */}
              <OperationsPicker
                spec={data.openapi?.spec ?? ''}
                tls={data.openapi?.tls}
                operations={data.openapi?.operations}
                onChange={(operations) =>
                  onChange({
                    openapi: { ...data.openapi, spec: data.openapi?.spec ?? '', operations },
                  })
                }
              />
            </div>
          )}

          {/* TLS / mTLS section for OpenAPI */}
          {visibility.openapi && (
            <Section
              title="TLS / mTLS"
              icon={KeyRound}
              expanded={expandedSections.has('tls')}
              onToggle={() => toggleSection('tls')}
            >
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label htmlFor={`${uid}-cert-file`} className={labelClass}>Client Cert File</label>
                  <input
                    id={`${uid}-cert-file`}
                    type="text"
                    value={data.openapi?.tls?.certFile ?? ''}
                    onChange={(e) =>
                      onChange({
                        openapi: {
                          ...data.openapi,
                          spec: data.openapi?.spec ?? '',
                          tls: { ...data.openapi?.tls, certFile: e.target.value },
                        },
                      })
                    }
                    placeholder="./certs/client.crt"
                    className={cn(inputClass, 'font-mono')}
                  />
                </div>
                <div>
                  <label htmlFor={`${uid}-key-file`} className={labelClass}>Client Key File</label>
                  <input
                    id={`${uid}-key-file`}
                    type="text"
                    value={data.openapi?.tls?.keyFile ?? ''}
                    onChange={(e) =>
                      onChange({
                        openapi: {
                          ...data.openapi,
                          spec: data.openapi?.spec ?? '',
                          tls: { ...data.openapi?.tls, keyFile: e.target.value },
                        },
                      })
                    }
                    placeholder="./certs/client.key"
                    className={cn(inputClass, 'font-mono')}
                  />
                </div>
              </div>
              <div>
                <label htmlFor={`${uid}-ca-file`} className={labelClass}>CA File</label>
                <input
                  id={`${uid}-ca-file`}
                  type="text"
                  value={data.openapi?.tls?.caFile ?? ''}
                  onChange={(e) =>
                    onChange({
                      openapi: {
                        ...data.openapi,
                        spec: data.openapi?.spec ?? '',
                        tls: { ...data.openapi?.tls, caFile: e.target.value },
                      },
                    })
                  }
                  placeholder="./certs/ca.crt"
                  className={cn(inputClass, 'font-mono')}
                />
              </div>
              <div className="flex items-start gap-3">
                <input
                  type="checkbox"
                  id="insecureSkipVerify"
                  checked={data.openapi?.tls?.insecureSkipVerify ?? false}
                  onChange={(e) =>
                    onChange({
                      openapi: {
                        ...data.openapi,
                        spec: data.openapi?.spec ?? '',
                        tls: { ...data.openapi?.tls, insecureSkipVerify: e.target.checked || undefined },
                      },
                    })
                  }
                  className="mt-0.5 accent-primary"
                />
                <div>
                  <label htmlFor="insecureSkipVerify" className="text-xs text-text-secondary cursor-pointer">
                    Skip TLS Verification
                    <span className="ml-2 text-[10px] text-status-error font-medium">Dangerous — dev only</span>
                  </label>
                  <p className="text-[10px] text-text-muted mt-0.5">Disables certificate validation. Never use in production.</p>
                </div>
              </div>
            </Section>
          )}

          {/* Command (local + ssh) */}
          {visibility.command && (
            <CommandArrayBuilder
              value={data.command ?? []}
              onChange={(command) => onChange({ command })}
            />
          )}

          {/* Transport + Port row */}
          {(visibility.transport || portVisible) && (
            <div className={cn('grid gap-2', portVisible ? 'grid-cols-2' : 'grid-cols-1')}>
              {visibility.transport && (
                <div>
                  <label htmlFor={`${uid}-transport`} className={labelClass}>Transport</label>
                  {typeOption.transportLocked ? (
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-surface-elevated/60 border border-border/30 text-xs text-text-muted">
                      <Zap size={12} className="text-primary" />
                      {typeOption.transportDefault || 'N/A'}
                      <span className="text-[10px] ml-auto opacity-60">locked</span>
                    </div>
                  ) : (
                    <select
                      id={`${uid}-transport`}
                      value={data.transport ?? typeOption.transportDefault}
                      onChange={(e) => onChange({ transport: e.target.value })}
                      className={inputClass}
                    >
                      {TRANSPORT_OPTIONS.filter((t) => availableTransports.includes(t.value)).map((t) => (
                        <option key={t.value} value={t.value}>
                          {t.label}
                        </option>
                      ))}
                    </select>
                  )}
                </div>
              )}
              {portVisible && (
                <Field label="Port" required error={errors?.port}>
                  {({ id, describedBy, invalid }) => (
                    <input
                      id={id}
                      type="number"
                      value={data.port ?? ''}
                      onChange={(e) =>
                        onChange({ port: e.target.value ? Number(e.target.value) : undefined })
                      }
                      placeholder="8080"
                      min={1}
                      max={65535}
                      aria-describedby={describedBy}
                      aria-invalid={invalid}
                      className={cn(inputClass, errors?.port && 'border-status-error/50')}
                    />
                  )}
                </Field>
              )}
            </div>
          )}

          {/* Transport compatibility advisor */}
          <TransportAdvisor
            serverType={data.serverType}
            transport={data.transport ?? typeOption.transportDefault}
          />
        </div>
      </Section>

      {/* Authentication (external URL servers only) */}
      {data.serverType === 'external' && (
        <Section
          title="Authentication"
          icon={ShieldCheck}
          expanded={expandedSections.has('auth')}
          onToggle={() => toggleSection('auth')}
          badge={data.auth ? EXTERNAL_AUTH_LABELS[data.auth.type] : undefined}
        >
          <ExternalAuthFields
            auth={data.auth}
            onChange={(auth) => onChange({ auth })}
          />
          <CustomHeadersEditor
            headers={data.headers}
            authSet={Boolean(data.auth)}
            onChange={(headers) => onChange({ headers })}
          />
        </Section>
      )}

      {/* Section 4: Environment & Secrets */}
      <Section
        title="Environment & Secrets"
        icon={KeyRound}
        expanded={expandedSections.has('env')}
        onToggle={() => toggleSection('env')}
        badge={envCount > 0 ? `${envCount}` : undefined}
      >
        <KeyValueEditor
          label="Environment Variables"
          value={data.env ?? {}}
          onChange={(env) => onChange({ env })}
          placeholder={{ key: 'ENV_VAR', value: 'value' }}
          showSecrets
        />
      </Section>

      {/* Section 5: Advanced */}
      <ExecutionFields data={data} onChange={onChange} />
      <Section
        title="Advanced"
        icon={FileCode2}
        expanded={expandedSections.has('advanced')}
        onToggle={() => toggleSection('advanced')}
        badge={advancedCount > 0 ? `${advancedCount}` : undefined}
      >
        <ToolsPicker
          value={data.tools ?? []}
          onChange={(tools) => onChange({ tools })}
          serverName={data.name}
          probeConfig={probeConfig}
        />

        <div>
          <label htmlFor={`${uid}-output-format`} className={labelClass}>Output Format</label>
          <select
            id={`${uid}-output-format`}
            value={data.outputFormat ?? ''}
            onChange={(e) => onChange({ outputFormat: e.target.value || undefined })}
            className={inputClass}
          >
            <option value="">Default</option>
            {OUTPUT_FORMATS.map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </select>
        </div>

        {visibility.buildArgs && (
          <KeyValueEditor
            label="Build Arguments"
            value={data.buildArgs ?? {}}
            onChange={(buildArgs) => onChange({ buildArgs })}
            placeholder={{ key: 'ARG', value: 'value' }}
          />
        )}

        {visibility.network && (
          <div>
            <label htmlFor={`${uid}-network`} className={labelClass}>Network</label>
            <input
              id={`${uid}-network`}
              type="text"
              value={data.network ?? ''}
              onChange={(e) => onChange({ network: e.target.value || undefined })}
              placeholder="network-name"
              className={cn(inputClass, 'font-mono')}
            />
          </div>
        )}

        {(data.serverType === 'container' || data.serverType === 'source') && (
          <div>
            <label htmlFor="server-volumes" className={labelClass}>Volumes</label>
            <input
              id="server-volumes"
              value={data.volumes?.join(', ') ?? ''}
              onChange={(event) => onChange({ volumes: splitValues(event.target.value) })}
              placeholder="./data:/data:ro"
              className={cn(inputClass, 'font-mono')}
            />
            <p className="text-[10px] text-text-muted mt-1">Comma-separated host:container[:mode] mounts; ./ and ../ are relative to the stack file</p>
          </div>
        )}

        <div>
          <label htmlFor={`${uid}-schema-pinning`} className={labelClass}>Schema Pinning</label>
          <select
            id={`${uid}-schema-pinning`}
            value={data.pinSchemas === undefined ? '' : String(data.pinSchemas)}
            onChange={(e) => {
              const v = e.target.value;
              onChange({ pinSchemas: v === '' ? undefined : v === 'true' });
            }}
            className={inputClass}
          >
            <option value="">Inherit from gateway</option>
            <option value="true">Enable</option>
            <option value="false">Disable</option>
          </select>
          <p className="text-[10px] text-text-muted mt-1">Override the gateway-level schema pinning setting for this server</p>
        </div>

        {visibility.autoscale && (
          <ScalingControl data={data} onChange={onChange} errors={errors} />
        )}
      </Section>
    </div>
  );
}

// --- Scaling control (Static replicas | Autoscale) ---

const AUTOSCALE_DEFAULTS: AutoscaleFormData = {
  min: 1,
  max: 5,
  targetInFlight: 10,
  scaleUpAfter: '30s',
  scaleDownAfter: '5m',
};

function clampInt(raw: number, min: number, max: number): number {
  if (!Number.isFinite(raw)) return min;
  return Math.max(min, Math.min(max, Math.trunc(raw)));
}

function ScalingControl({
  data,
  onChange,
  errors,
}: {
  data: MCPServerFormData;
  onChange: (data: Partial<MCPServerFormData>) => void;
  errors?: Record<string, string>;
}) {
  const uid = useId();
  const mode: 'static' | 'autoscale' = data.autoscale ? 'autoscale' : 'static';

  const setMode = useCallback(
    (next: 'static' | 'autoscale') => {
      if (next === mode) return;
      if (next === 'autoscale') {
        onChange({
          replicas: undefined,
          replicaPolicy: undefined,
          autoscale: { ...AUTOSCALE_DEFAULTS },
        });
      } else {
        onChange({ autoscale: undefined });
      }
    },
    [mode, onChange],
  );

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight' || e.key === 'ArrowUp' || e.key === 'ArrowDown') {
      e.preventDefault();
      setMode(mode === 'static' ? 'autoscale' : 'static');
    }
  };

  const updateAutoscale = (patch: Partial<AutoscaleFormData>) => {
    onChange({ autoscale: { ...(data.autoscale as AutoscaleFormData), ...patch } });
  };

  return (
    <div>
      <label className={labelClass}>Scaling</label>
      <div
        role="radiogroup"
        aria-label="Scaling mode"
        onKeyDown={handleKeyDown}
        className="inline-flex items-center rounded-lg border border-border/40 bg-background/60 p-0.5 mb-3"
      >
        {(['static', 'autoscale'] as const).map((m) => (
          <button
            key={m}
            type="button"
            role="radio"
            aria-checked={mode === m}
            tabIndex={mode === m ? 0 : -1}
            onClick={() => setMode(m)}
            className={cn(
              'px-3 py-1.5 rounded-md text-[11px] font-medium transition-colors',
              mode === m
                ? 'bg-primary/15 text-primary'
                : 'text-text-muted hover:text-text-secondary',
            )}
          >
            {m === 'static' ? 'Static replicas' : 'Autoscale'}
          </button>
        ))}
      </div>

      {mode === 'static' ? (
        <div>
          <label htmlFor={`${uid}-replicas`} className={labelClass}>Replicas</label>
          <input
            id={`${uid}-replicas`}
            type="number"
            aria-label="Replicas"
            aria-invalid={errors?.replicas ? true : undefined}
            aria-describedby={errors?.replicas ? `${uid}-replicas-error` : undefined}
            value={data.replicas ?? 1}
            onChange={(e) => {
              const n = Number(e.target.value);
              if (!Number.isFinite(n)) return;
              const clamped = Math.max(1, Math.min(32, Math.trunc(n)));
              onChange({ replicas: clamped === 1 ? undefined : clamped });
            }}
            placeholder="1"
            min={1}
            max={32}
            className={cn(inputClass, errors?.replicas && 'border-status-error/50')}
          />
          <FieldError id={`${uid}-replicas-error`} error={errors?.replicas} />
          <p className="text-[10px] text-text-muted mt-1">Number of parallel instances to run (1–32). Supported for container, local-process, and SSH transports.</p>
          {data.replicas !== undefined && data.replicas > 1 && (
            <div className="mt-3">
              <label htmlFor={`${uid}-replica-policy`} className={labelClass}>Replica Policy</label>
              <select
                id={`${uid}-replica-policy`}
                aria-label="Replica Policy"
                value={data.replicaPolicy ?? 'round-robin'}
                onChange={(e) => {
                  const v = e.target.value as 'round-robin' | 'least-connections';
                  onChange({ replicaPolicy: v === 'round-robin' ? undefined : v });
                }}
                className={inputClass}
              >
                <option value="round-robin">Round-robin</option>
                <option value="least-connections">Least connections</option>
              </select>
              <p className="text-[10px] text-text-muted mt-1">How tool calls are distributed across replicas</p>
            </div>
          )}
        </div>
      ) : (
        <AutoscaleFields
          data={data.autoscale ?? AUTOSCALE_DEFAULTS}
          onChange={updateAutoscale}
          errors={errors}
        />
      )}
    </div>
  );
}

function AutoscaleFields({
  data,
  onChange,
  errors,
}: {
  data: AutoscaleFormData;
  onChange: (patch: Partial<AutoscaleFormData>) => void;
  errors?: Record<string, string>;
}) {
  const uid = useId();
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-2">
        <div>
          <label htmlFor={`${uid}-min`} className={labelClass}>Min replicas</label>
          <input
            id={`${uid}-min`}
            type="number"
            aria-label="Min replicas"
            aria-invalid={errors?.['autoscale.min'] ? true : undefined}
            aria-describedby={errors?.['autoscale.min'] ? `${uid}-min-error` : undefined}
            value={data.min}
            onChange={(e) => {
              const n = Number(e.target.value);
              if (!Number.isFinite(n)) return;
              onChange({ min: clampInt(n, 0, 32) });
            }}
            min={0}
            max={32}
            className={cn(inputClass, errors?.['autoscale.min'] && 'border-status-error/50')}
          />
          <FieldError id={`${uid}-min-error`} error={errors?.['autoscale.min']} />
        </div>
        <div>
          <label htmlFor={`${uid}-max`} className={labelClass}>Max replicas</label>
          <input
            id={`${uid}-max`}
            type="number"
            aria-label="Max replicas"
            aria-invalid={errors?.['autoscale.max'] ? true : undefined}
            aria-describedby={errors?.['autoscale.max'] ? `${uid}-max-error` : undefined}
            value={data.max}
            onChange={(e) => {
              const n = Number(e.target.value);
              if (!Number.isFinite(n)) return;
              onChange({ max: clampInt(n, 1, 32) });
            }}
            min={1}
            max={32}
            className={cn(inputClass, errors?.['autoscale.max'] && 'border-status-error/50')}
          />
          <FieldError id={`${uid}-max-error`} error={errors?.['autoscale.max']} />
        </div>
      </div>

      <div>
        <label htmlFor={`${uid}-target`} className={labelClass}>Target concurrent requests per replica</label>
        <input
          id={`${uid}-target`}
          type="number"
          aria-label="Target concurrent requests per replica"
          aria-invalid={errors?.['autoscale.target_in_flight'] ? true : undefined}
          aria-describedby={errors?.['autoscale.target_in_flight'] ? `${uid}-target-error` : undefined}
          value={data.targetInFlight}
          onChange={(e) => {
            const n = Number(e.target.value);
            if (!Number.isFinite(n)) return;
            onChange({ targetInFlight: clampInt(n, 1, 10000) });
          }}
          min={1}
          max={10000}
          className={cn(inputClass, errors?.['autoscale.target_in_flight'] && 'border-status-error/50')}
        />
        <FieldError id={`${uid}-target-error`} error={errors?.['autoscale.target_in_flight']} />
        <p className="text-[10px] text-text-muted mt-1">
          gridctl adds replicas when the average in-flight request count exceeds this.
        </p>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <div>
          <label htmlFor={`${uid}-scale-up`} className={labelClass}>Scale up after</label>
          <input
            id={`${uid}-scale-up`}
            type="text"
            aria-label="Scale up after"
            aria-invalid={errors?.['autoscale.scale_up_after'] ? true : undefined}
            aria-describedby={errors?.['autoscale.scale_up_after'] ? `${uid}-scale-up-error` : undefined}
            value={data.scaleUpAfter ?? ''}
            onChange={(e) => onChange({ scaleUpAfter: e.target.value || undefined })}
            placeholder="30s"
            className={cn(inputClass, 'font-mono', errors?.['autoscale.scale_up_after'] && 'border-status-error/50')}
          />
          <FieldError id={`${uid}-scale-up-error`} error={errors?.['autoscale.scale_up_after']} />
          <p className="text-[10px] text-text-muted mt-1">
            Wait this long while above target before spawning a replica (min 10s).
          </p>
        </div>
        <div>
          <label htmlFor={`${uid}-scale-down`} className={labelClass}>Scale down after</label>
          <input
            id={`${uid}-scale-down`}
            type="text"
            aria-label="Scale down after"
            aria-invalid={errors?.['autoscale.scale_down_after'] ? true : undefined}
            aria-describedby={errors?.['autoscale.scale_down_after'] ? `${uid}-scale-down-error` : undefined}
            value={data.scaleDownAfter ?? ''}
            onChange={(e) => onChange({ scaleDownAfter: e.target.value || undefined })}
            placeholder="5m"
            className={cn(inputClass, 'font-mono', errors?.['autoscale.scale_down_after'] && 'border-status-error/50')}
          />
          <FieldError id={`${uid}-scale-down-error`} error={errors?.['autoscale.scale_down_after']} />
          <p className="text-[10px] text-text-muted mt-1">
            Wait this long while below target before reaping a replica (min 1m).
          </p>
        </div>
      </div>

      <div>
        <label htmlFor={`${uid}-warm-pool`} className={labelClass}>Warm pool</label>
        <input
          id={`${uid}-warm-pool`}
          type="number"
          aria-label="Warm pool"
          aria-invalid={errors?.['autoscale.warm_pool'] ? true : undefined}
          aria-describedby={errors?.['autoscale.warm_pool'] ? `${uid}-warm-pool-error` : undefined}
          value={data.warmPool ?? 0}
          onChange={(e) => {
            const n = Number(e.target.value);
            if (!Number.isFinite(n)) return;
            const clamped = clampInt(n, 0, 32);
            onChange({ warmPool: clamped === 0 ? undefined : clamped });
          }}
          min={0}
          max={32}
          className={cn(inputClass, errors?.['autoscale.warm_pool'] && 'border-status-error/50')}
        />
        <FieldError id={`${uid}-warm-pool-error`} error={errors?.['autoscale.warm_pool']} />
        <p className="text-[10px] text-text-muted mt-1">
          Extra idle replicas kept above the load-derived target.
        </p>
      </div>

      <div className="flex items-start gap-3">
        <input
          type="checkbox"
          id="autoscale-idle-to-zero"
          checked={data.idleToZero ?? false}
          onChange={(e) => onChange({ idleToZero: e.target.checked ? true : undefined })}
          className="mt-0.5 accent-primary"
        />
        <div>
          <label htmlFor="autoscale-idle-to-zero" className="text-xs text-text-secondary cursor-pointer">
            Scale to zero when idle
          </label>
          <p className="text-[10px] text-text-muted mt-0.5">
            Allow reaping every replica after sustained idle. First request after idle may be slower.
          </p>
        </div>
      </div>

      <p className="text-[11px] text-text-secondary border-t border-border/20 pt-2 font-mono">
        Autoscale {data.min}–{data.max} replicas · {data.targetInFlight} concurrent/replica
      </p>
    </div>
  );
}

// ExternalAuthFields — downstream auth for external URL servers. Mirrors
// config.ServerAuth: static bearer/header credentials or OAuth 2.1 brokering.
// Secret fields nudge toward ${var:KEY} references; the YAML preview emits
// exactly what the user types, so literals are their explicit choice.
function ExternalAuthFields({
  auth,
  onChange,
}: {
  auth: ExternalAuthFormData | undefined;
  onChange: (auth: ExternalAuthFormData | undefined) => void;
}) {
  const uid = useId();
  const update = (patch: Partial<ExternalAuthFormData>) => {
    onChange({ ...(auth as ExternalAuthFormData), ...patch });
  };

  return (
    <div className="space-y-3">
      <p className="text-[10px] text-text-muted">
        How gridctl authenticates to the remote server: a bearer token, a
        custom header, or OAuth 2.1 brokered by the gateway.
      </p>
      <div>
        <label htmlFor={`${uid}-type`} className={labelClass}>Type</label>
        <select
          id={`${uid}-type`}
          aria-label="Authentication type"
          value={auth?.type ?? ''}
          onChange={(e) => {
            const v = e.target.value;
            onChange(v ? { type: v as ExternalAuthFormData['type'] } : undefined);
          }}
          className={inputClass}
        >
          <option value="">None</option>
          <option value="bearer">Bearer token</option>
          <option value="header">Custom header</option>
          <option value="oauth">OAuth 2.1</option>
        </select>
      </div>

      {auth?.type === 'bearer' && (
        <div>
          <label htmlFor={`${uid}-token`} className={labelClass}>
            Token <span className="text-status-error">*</span>
          </label>
          <div className="flex items-center gap-0.5">
            <input
              id={`${uid}-token`}
              type="text"
              value={auth.token ?? ''}
              onChange={(e) => update({ token: e.target.value })}
              placeholder="${var:MY_TOKEN}"
              className={cn(inputClass, 'flex-1 font-mono', auth.token?.startsWith('${var:') && 'text-tertiary font-medium')}
            />
            <VariablesPopover onSelect={(ref) => update({ token: ref })} />
          </div>
          <p className="text-[10px] text-text-muted mt-1">
            Sent as "Authorization: Bearer". Prefer a {'${var:KEY}'} reference over a literal secret.
          </p>
        </div>
      )}

      {auth?.type === 'header' && (
        <div className="grid grid-cols-2 gap-2">
          <div>
            <label htmlFor={`${uid}-header`} className={labelClass}>
              Header name <span className="text-status-error">*</span>
            </label>
            <input
              id={`${uid}-header`}
              type="text"
              value={auth.header ?? ''}
              onChange={(e) => update({ header: e.target.value })}
              placeholder="X-API-Key"
              className={cn(inputClass, 'font-mono')}
            />
          </div>
          <div>
            <label htmlFor={`${uid}-value`} className={labelClass}>
              Value <span className="text-status-error">*</span>
            </label>
            <div className="flex items-center gap-0.5">
              <input
                id={`${uid}-value`}
                type="text"
                value={auth.value ?? ''}
                onChange={(e) => update({ value: e.target.value })}
                placeholder="${var:MY_API_KEY}"
                className={cn(inputClass, 'flex-1 font-mono', auth.value?.startsWith('${var:') && 'text-tertiary font-medium')}
              />
              <VariablesPopover onSelect={(ref) => update({ value: ref })} />
            </div>
            <p className="text-[10px] text-text-muted mt-1">
              Prefer a {'${var:KEY}'} reference over a literal secret.
            </p>
          </div>
        </div>
      )}

      {auth?.type === 'oauth' && (
        <div className="space-y-3">
          <p className="text-[10px] text-text-muted">
            gridctl runs the browser login after deploy and refreshes tokens
            automatically. All fields are optional: dynamic client
            registration is used when they are left empty.
          </p>
          <div>
            <label htmlFor={`${uid}-scopes`} className={labelClass}>Scopes</label>
            <input
              id={`${uid}-scopes`}
              type="text"
              aria-label="OAuth scopes"
              value={(auth.scopes ?? []).join(' ')}
              onChange={(e) =>
                update({
                  scopes: e.target.value ? e.target.value.split(/[\s,]+/).filter(Boolean) : undefined,
                })
              }
              placeholder="read write"
              className={inputClass}
            />
            <p className="text-[10px] text-text-muted mt-1">
              Space or comma-separated. Empty uses the scopes the server advertises.
            </p>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <div>
              <label htmlFor={`${uid}-client-id`} className={labelClass}>Client ID</label>
              <input
                id={`${uid}-client-id`}
                type="text"
                aria-label="OAuth client ID"
                value={auth.clientId ?? ''}
                onChange={(e) => update({ clientId: e.target.value })}
                placeholder="pre-registered client ID"
                className={cn(inputClass, 'font-mono')}
              />
            </div>
            <div>
              <label htmlFor={`${uid}-client-secret`} className={labelClass}>Client Secret</label>
              <div className="flex items-center gap-0.5">
                <input
                  id={`${uid}-client-secret`}
                  type="text"
                  aria-label="OAuth client secret"
                  value={auth.clientSecret ?? ''}
                  onChange={(e) => update({ clientSecret: e.target.value })}
                  placeholder="${var:MY_CLIENT_SECRET}"
                  className={cn(inputClass, 'flex-1 font-mono', auth.clientSecret?.startsWith('${var:') && 'text-tertiary font-medium')}
                />
                <VariablesPopover onSelect={(ref) => update({ clientSecret: ref })} />
              </div>
            </div>
          </div>
          <p className="text-[10px] text-text-muted">
            Only needed for providers that refuse dynamic registration. Prefer
            a {'${var:KEY}'} reference for the secret.
          </p>
        </div>
      )}
    </div>
  );
}

// SourceAuthField — inline collapsible "Repository Authentication" block for
// private git sources. Persisted to YAML as a vault reference only (no raw
// tokens); the server resolves it against the live vault at clone time.
function SourceAuthField({
  value,
  onChange,
}: {
  value: string | undefined;
  onChange: (credentialRef: string | undefined) => void;
}) {
  const [open, setOpen] = useState(Boolean(value));
  return (
    <div
      className={cn(
        'rounded-lg border border-border/30 bg-surface-overlay-1 transition-colors',
        open && 'border-border/50 bg-surface-overlay-2',
      )}
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center gap-2 px-3 py-2 text-[11px] text-text-secondary hover:text-text-primary transition-colors"
      >
        <ShieldCheck size={12} className="text-primary/70" />
        <span className="font-medium">Repository Authentication</span>
        <span className="text-text-muted text-[10px]">
          {value ? '' : '(optional, for private repos)'}
        </span>
        <span className="ml-auto text-text-muted">
          {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </span>
      </button>
      {open && (
        <div className="px-3 pb-3 pt-1 space-y-2 border-t border-border/20">
          <p className="text-[10px] text-text-muted">
            Choose a vault key that holds a Personal Access Token. The server
            resolves it at clone time; the raw value never enters the YAML.
          </p>
          <div className="flex items-center gap-2">
            {value ? (
              <div className="flex-1 flex items-center justify-between gap-2 bg-background/60 border border-border/40 rounded-md px-2 py-1.5 text-[10px] font-mono text-text-primary">
                <span className="truncate">{value}</span>
                <button
                  type="button"
                  onClick={() => onChange(undefined)}
                  className="text-text-muted hover:text-status-error transition-colors"
                  aria-label="Clear vault selection"
                >
                  <X size={11} />
                </button>
              </div>
            ) : (
              <div className="flex-1 text-[10px] text-text-muted italic px-1">
                No credential selected
              </div>
            )}
            <VariablesPopover onSelect={onChange} />
          </div>
        </div>
      )}
    </div>
  );
}
