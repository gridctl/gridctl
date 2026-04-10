import { useState, useCallback, useMemo } from 'react';
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
  AlertCircle,
  KeyRound,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../../lib/cn';
import type { MCPServerFormData, ServerType } from '../../../lib/yaml-builder';
import { SecretsPopover } from '../SecretsPopover';
import { TransportAdvisor } from '../TransportAdvisor';

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
    description: 'Build from a Git repository or local path',
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
}

function getFieldVisibility(serverType: ServerType): FieldVisibility {
  switch (serverType) {
    case 'container':
      return { image: true, port: true, transport: true, command: true, source: false, url: false, ssh: false, openapi: false, buildArgs: false, network: true };
    case 'source':
      return { image: false, port: true, transport: true, command: false, source: true, url: false, ssh: false, openapi: false, buildArgs: true, network: true };
    case 'external':
      return { image: false, port: false, transport: true, command: false, source: false, url: true, ssh: false, openapi: false, buildArgs: false, network: false };
    case 'local':
      return { image: false, port: false, transport: false, command: true, source: false, url: false, ssh: false, openapi: false, buildArgs: false, network: false };
    case 'ssh':
      return { image: false, port: false, transport: false, command: true, source: false, url: false, ssh: true, openapi: false, buildArgs: false, network: false };
    case 'openapi':
      return { image: false, port: false, transport: false, command: false, source: false, url: false, ssh: false, openapi: true, buildArgs: false, network: false };
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
        className="w-full flex items-center gap-2.5 px-4 py-3 text-left hover:bg-white/[0.02] transition-colors"
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
const errorClass = 'text-[10px] text-status-error mt-1';

function FieldError({ error }: { error?: string }) {
  if (!error) return null;
  return (
    <p className={errorClass}>
      <AlertCircle size={10} className="inline mr-1 -mt-0.5" />
      {error}
    </p>
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
              className={cn(inputClass, 'w-[40%] font-mono')}
            />
            <span className="text-text-muted text-xs">=</span>
            <div className="flex-1 flex items-center gap-0.5">
              <input
                type="text"
                value={val}
                onChange={(e) => updateValue(key, e.target.value)}
                placeholder={placeholder?.value ?? 'value'}
                className={cn(inputClass, 'flex-1', val.startsWith('${vault:') && 'text-tertiary font-medium')}
              />
              {showSecrets && (
                <SecretsPopover
                  onSelect={(ref) => updateValue(key, ref)}
                />
              )}
            </div>
            <button
              type="button"
              onClick={() => removeEntry(key)}
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
              className={cn(inputClass, 'flex-1 font-mono')}
            />
            <button
              type="button"
              onClick={() => removeItem(i)}
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

// --- Tools Whitelist ---

function ToolsWhitelist({
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
        <label className={labelClass}>Tools Whitelist</label>
        <button
          type="button"
          onClick={addItem}
          className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <Plus size={10} />
          Add tool
        </button>
      </div>
      {value.length === 0 && (
        <p className="text-[10px] text-text-muted/60 italic py-2">All tools exposed (no whitelist)</p>
      )}
      <div className="space-y-1.5">
        {value.map((item, i) => (
          <div key={i} className="flex items-center gap-1.5">
            <input
              type="text"
              value={item}
              onChange={(e) => updateItem(i, e.target.value)}
              placeholder="tool-name"
              className={cn(inputClass, 'flex-1 font-mono')}
            />
            <button
              type="button"
              onClick={() => removeItem(i)}
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

// --- Main MCPServerForm ---

interface MCPServerFormProps {
  data: MCPServerFormData;
  onChange: (data: Partial<MCPServerFormData>) => void;
  errors?: Record<string, string>;
}

export function MCPServerForm({ data, onChange, errors }: MCPServerFormProps) {
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

  const envCount = data.env ? Object.keys(data.env).length : 0;
  const advancedCount = (data.tools?.length ?? 0) + (data.outputFormat ? 1 : 0) + (data.buildArgs ? Object.keys(data.buildArgs).length : 0) + (data.network ? 1 : 0) + (data.pinSchemas !== undefined ? 1 : 0);

  return (
    <div className="space-y-3">
      {/* Section 1: Identity */}
      <Section
        title="Identity"
        icon={Zap}
        expanded={expandedSections.has('identity')}
        onToggle={() => toggleSection('identity')}
      >
        <div>
          <label className={labelClass}>
            Name <span className="text-status-error">*</span>
          </label>
          <input
            type="text"
            value={data.name}
            onChange={(e) => onChange({ name: toKebabCase(e.target.value) })}
            onBlur={(e) => onChange({ name: e.target.value.replace(/-+$/, '') })}
            placeholder="my-server"
            className={cn(inputClass, 'font-mono', errors?.name && 'border-status-error/50')}
          />
          <FieldError error={errors?.name} />
          <p className="text-[10px] text-text-muted mt-1">Kebab-case identifier for this server</p>
        </div>
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
                  'bg-white/[0.02] hover:bg-white/[0.05]',
                  isSelected
                    ? 'border-primary/50 bg-primary/[0.06] shadow-[0_0_16px_rgba(245,158,11,0.08)]'
                    : 'border-white/[0.06] hover:border-white/[0.1]',
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
            <div>
              <label className={labelClass}>
                Image <span className="text-status-error">*</span>
              </label>
              <div className="relative">
                <input
                  type="text"
                  value={data.image ?? ''}
                  onChange={(e) => onChange({ image: e.target.value })}
                  placeholder="image:tag"
                  list="image-presets"
                  className={cn(inputClass, 'font-mono', errors?.image && 'border-status-error/50')}
                />
                <datalist id="image-presets">
                  {IMAGE_PRESETS.map((p) => (
                    <option key={p} value={p} />
                  ))}
                </datalist>
              </div>
              <FieldError error={errors?.image} />
            </div>
          )}

          {/* External: url */}
          {visibility.url && (
            <div>
              <label className={labelClass}>
                URL <span className="text-status-error">*</span>
              </label>
              <input
                type="url"
                value={data.url ?? ''}
                onChange={(e) => onChange({ url: e.target.value })}
                placeholder="https://my-server.example.com/mcp"
                className={cn(inputClass, errors?.url && 'border-status-error/50')}
              />
              <FieldError error={errors?.url} />
            </div>
          )}

          {/* Source config */}
          {visibility.source && (
            <div className="space-y-3 p-3 rounded-xl bg-white/[0.02] border border-white/[0.04]">
              <div>
                <label className={labelClass}>Source Type</label>
                <div className="flex gap-2">
                  {['git', 'local'].map((t) => (
                    <button
                      key={t}
                      type="button"
                      onClick={() =>
                        onChange({
                          source: {
                            ...data.source,
                            type: t,
                            url: t === 'git' ? (data.source?.url ?? '') : undefined,
                            path: t === 'local' ? (data.source?.path ?? '') : undefined,
                          } as MCPServerFormData['source'],
                        })
                      }
                      className={cn(
                        'px-3 py-1.5 rounded-lg text-xs font-medium transition-all duration-200 border',
                        (data.source?.type ?? 'git') === t
                          ? 'bg-primary/10 border-primary/30 text-primary'
                          : 'bg-white/[0.02] border-white/[0.06] text-text-muted hover:text-text-secondary',
                      )}
                    >
                      {t === 'git' ? 'Git' : 'Local'}
                    </button>
                  ))}
                </div>
              </div>
              {(data.source?.type ?? 'git') === 'git' ? (
                <>
                  <div>
                    <label className={labelClass}>
                      Repository URL <span className="text-status-error">*</span>
                    </label>
                    <input
                      type="url"
                      value={data.source?.url ?? ''}
                      onChange={(e) =>
                        onChange({ source: { ...data.source, type: 'git', url: e.target.value } as MCPServerFormData['source'] })
                      }
                      placeholder="https://github.com/org/repo.git"
                      className={cn(inputClass, errors?.['source.url'] && 'border-status-error/50')}
                    />
                    <FieldError error={errors?.['source.url']} />
                  </div>
                  <div className="grid grid-cols-2 gap-2">
                    <div>
                      <label className={labelClass}>Ref</label>
                      <input
                        type="text"
                        value={data.source?.ref ?? ''}
                        onChange={(e) =>
                          onChange({ source: { ...data.source, type: 'git', ref: e.target.value } as MCPServerFormData['source'] })
                        }
                        placeholder="main"
                        className={cn(inputClass, 'font-mono')}
                      />
                    </div>
                    <div>
                      <label className={labelClass}>Dockerfile</label>
                      <input
                        type="text"
                        value={data.source?.dockerfile ?? ''}
                        onChange={(e) =>
                          onChange({ source: { ...data.source, type: 'git', dockerfile: e.target.value } as MCPServerFormData['source'] })
                        }
                        placeholder="Dockerfile"
                        className={cn(inputClass, 'font-mono')}
                      />
                    </div>
                  </div>
                </>
              ) : (
                <div>
                  <label className={labelClass}>
                    Local Path <span className="text-status-error">*</span>
                  </label>
                  <input
                    type="text"
                    value={data.source?.path ?? ''}
                    onChange={(e) =>
                      onChange({ source: { ...data.source, type: 'local', path: e.target.value } as MCPServerFormData['source'] })
                    }
                    placeholder="./path/to/server"
                    className={cn(inputClass, 'font-mono', errors?.['source.path'] && 'border-status-error/50')}
                  />
                  <FieldError error={errors?.['source.path']} />
                  <div className="mt-2">
                    <label className={labelClass}>Dockerfile</label>
                    <input
                      type="text"
                      value={data.source?.dockerfile ?? ''}
                      onChange={(e) =>
                        onChange({ source: { ...data.source, type: 'local', dockerfile: e.target.value } as MCPServerFormData['source'] })
                      }
                      placeholder="Dockerfile"
                      className={cn(inputClass, 'font-mono')}
                    />
                  </div>
                </div>
              )}
            </div>
          )}

          {/* SSH config */}
          {visibility.ssh && (
            <div className="space-y-3 p-3 rounded-xl bg-white/[0.02] border border-white/[0.04]">
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className={labelClass}>
                    Host <span className="text-status-error">*</span>
                  </label>
                  <input
                    type="text"
                    value={data.ssh?.host ?? ''}
                    onChange={(e) =>
                      onChange({ ssh: { ...data.ssh, host: e.target.value, user: data.ssh?.user ?? '' } })
                    }
                    placeholder="192.168.1.100"
                    className={cn(inputClass, 'font-mono', errors?.['ssh.host'] && 'border-status-error/50')}
                  />
                  <FieldError error={errors?.['ssh.host']} />
                </div>
                <div>
                  <label className={labelClass}>
                    User <span className="text-status-error">*</span>
                  </label>
                  <input
                    type="text"
                    value={data.ssh?.user ?? ''}
                    onChange={(e) =>
                      onChange({ ssh: { ...data.ssh, host: data.ssh?.host ?? '', user: e.target.value } })
                    }
                    placeholder="root"
                    className={cn(inputClass, 'font-mono', errors?.['ssh.user'] && 'border-status-error/50')}
                  />
                  <FieldError error={errors?.['ssh.user']} />
                </div>
              </div>
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className={labelClass}>Port</label>
                  <input
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
                  <label className={labelClass}>Identity File</label>
                  <input
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
                <label className={labelClass}>Known Hosts File</label>
                <input
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
                <label className={labelClass}>Jump Host</label>
                <input
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
            <div className="space-y-3 p-3 rounded-xl bg-white/[0.02] border border-white/[0.04]">
              <div>
                <label className={labelClass}>
                  Spec <span className="text-status-error">*</span>
                </label>
                <input
                  type="text"
                  value={data.openapi?.spec ?? ''}
                  onChange={(e) =>
                    onChange({ openapi: { ...data.openapi, spec: e.target.value } })
                  }
                  placeholder="https://api.example.com/openapi.yaml or ./spec.yaml"
                  className={cn(inputClass, errors?.['openapi.spec'] && 'border-status-error/50')}
                />
                <FieldError error={errors?.['openapi.spec']} />
              </div>
              <div>
                <label className={labelClass}>Base URL</label>
                <input
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
                <label className={labelClass}>Authentication</label>
                <select
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
                  <label className={labelClass}>Token Environment Variable</label>
                  <input
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
                    <label className={labelClass}>Header Name</label>
                    <input
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
                    <label className={labelClass}>Value Environment Variable</label>
                    <input
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
                    <label className={labelClass}>Parameter Name</label>
                    <input
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
                    <label className={labelClass}>Value Environment Variable</label>
                    <input
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
                      <label className={labelClass}>Client ID Env Var</label>
                      <input
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
                      <label className={labelClass}>Client Secret Env Var</label>
                      <input
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
                    <label className={labelClass}>Token URL</label>
                    <input
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
                    <label className={labelClass}>Scopes</label>
                    <input
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
                    <label className={labelClass}>Username Env Var</label>
                    <input
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
                    <label className={labelClass}>Password Env Var</label>
                    <input
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
              <div>
                <label className={labelClass}>Operations Filter</label>
                <div className="flex gap-2 mb-2">
                  {['include', 'exclude'].map((mode) => (
                    <button
                      key={mode}
                      type="button"
                      onClick={() => {
                        const ops = mode === 'include'
                          ? { include: data.openapi?.operations?.include ?? [], exclude: undefined }
                          : { exclude: data.openapi?.operations?.exclude ?? [], include: undefined };
                        onChange({
                          openapi: { ...data.openapi, spec: data.openapi?.spec ?? '', operations: ops as MCPServerFormData['openapi'] extends undefined ? never : NonNullable<MCPServerFormData['openapi']>['operations'] },
                        });
                      }}
                      className={cn(
                        'px-3 py-1 rounded-lg text-[10px] font-medium transition-all border',
                        (mode === 'include' && data.openapi?.operations?.include) ||
                        (mode === 'exclude' && data.openapi?.operations?.exclude)
                          ? 'bg-primary/10 border-primary/30 text-primary'
                          : 'bg-white/[0.02] border-white/[0.06] text-text-muted',
                      )}
                    >
                      {mode}
                    </button>
                  ))}
                  {(data.openapi?.operations?.include || data.openapi?.operations?.exclude) && (
                    <button
                      type="button"
                      onClick={() =>
                        onChange({ openapi: { ...data.openapi, spec: data.openapi?.spec ?? '', operations: undefined } })
                      }
                      className="px-2 py-1 text-[10px] text-text-muted hover:text-text-secondary"
                    >
                      Clear
                    </button>
                  )}
                </div>
                {data.openapi?.operations?.include && (
                  <textarea
                    value={data.openapi.operations.include.join('\n')}
                    onChange={(e) =>
                      onChange({
                        openapi: {
                          ...data.openapi,
                          spec: data.openapi?.spec ?? '',
                          operations: { include: e.target.value.split('\n').filter(Boolean) },
                        },
                      })
                    }
                    placeholder="One operation per line"
                    rows={3}
                    className={cn(inputClass, 'font-mono resize-none')}
                  />
                )}
                {data.openapi?.operations?.exclude && (
                  <textarea
                    value={data.openapi.operations.exclude.join('\n')}
                    onChange={(e) =>
                      onChange({
                        openapi: {
                          ...data.openapi,
                          spec: data.openapi?.spec ?? '',
                          operations: { exclude: e.target.value.split('\n').filter(Boolean) },
                        },
                      })
                    }
                    placeholder="One operation per line"
                    rows={3}
                    className={cn(inputClass, 'font-mono resize-none')}
                  />
                )}
              </div>
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
                  <label className={labelClass}>Client Cert File</label>
                  <input
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
                  <label className={labelClass}>Client Key File</label>
                  <input
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
                <label className={labelClass}>CA File</label>
                <input
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
                  <label className={labelClass}>Transport</label>
                  {typeOption.transportLocked ? (
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-surface-elevated/60 border border-border/30 text-xs text-text-muted">
                      <Zap size={12} className="text-primary" />
                      {typeOption.transportDefault || 'N/A'}
                      <span className="text-[10px] ml-auto opacity-60">locked</span>
                    </div>
                  ) : (
                    <select
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
                <div>
                  <label className={labelClass}>
                    Port <span className="text-status-error">*</span>
                  </label>
                  <input
                    type="number"
                    value={data.port ?? ''}
                    onChange={(e) =>
                      onChange({ port: e.target.value ? Number(e.target.value) : undefined })
                    }
                    placeholder="8080"
                    min={1}
                    max={65535}
                    className={cn(inputClass, errors?.port && 'border-status-error/50')}
                  />
                  <FieldError error={errors?.port} />
                </div>
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
      <Section
        title="Advanced"
        icon={FileCode2}
        expanded={expandedSections.has('advanced')}
        onToggle={() => toggleSection('advanced')}
        badge={advancedCount > 0 ? `${advancedCount}` : undefined}
      >
        <ToolsWhitelist
          value={data.tools ?? []}
          onChange={(tools) => onChange({ tools })}
        />

        <div>
          <label className={labelClass}>Output Format</label>
          <select
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
            <label className={labelClass}>Network</label>
            <input
              type="text"
              value={data.network ?? ''}
              onChange={(e) => onChange({ network: e.target.value || undefined })}
              placeholder="network-name"
              className={cn(inputClass, 'font-mono')}
            />
          </div>
        )}

        <div>
          <label className={labelClass}>Schema Pinning</label>
          <select
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
      </Section>
    </div>
  );
}

