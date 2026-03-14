import { useState, useCallback, useMemo } from 'react';
import {
  Layers,
  Shield,
  Network,
  KeyRound,
  Server,
  Bot,
  Database,
  ChevronDown,
  ChevronRight,
  Plus,
  X,
  Trash2,
  Container,
  Cpu,
  Zap,
  AlertCircle,
  Check,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../../lib/cn';
import type {
  StackFormData,
  MCPServerFormData,
  AgentFormData,
  ResourceFormData,
} from '../../../lib/yaml-builder';
import { SecretsPopover } from '../SecretsPopover';
import { MCPServerForm } from './MCPServerForm';

// --- Shared form primitives ---

const inputClass =
  'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors';
const labelClass = 'block text-xs text-text-secondary mb-1.5';

function toKebabCase(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');
}

function FieldError({ error }: { error?: string }) {
  if (!error) return null;
  return (
    <p className="text-[10px] text-status-error mt-1">
      <AlertCircle size={10} className="inline mr-1 -mt-0.5" />
      {error}
    </p>
  );
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

// --- Key-Value Editor ---

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

  const addEntry = () => onChange({ ...value, '': '' });

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
                <SecretsPopover onSelect={(ref) => updateValue(key, ref)} />
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

// --- String Array Editor ---

function StringArrayEditor({
  label,
  value,
  onChange,
  placeholder,
  addLabel = 'Add',
  emptyText = 'None',
}: {
  label: string;
  value: string[];
  onChange: (val: string[]) => void;
  placeholder?: string;
  addLabel?: string;
  emptyText?: string;
}) {
  const addItem = () => onChange([...value, '']);
  const updateItem = (idx: number, val: string) => {
    const next = [...value];
    next[idx] = val;
    onChange(next);
  };
  const removeItem = (idx: number) => onChange(value.filter((_, i) => i !== idx));

  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <label className="text-xs text-text-secondary">{label}</label>
        <button
          type="button"
          onClick={addItem}
          className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          <Plus size={10} />
          {addLabel}
        </button>
      </div>
      {value.length === 0 && (
        <p className="text-[10px] text-text-muted/60 italic py-2">{emptyText}</p>
      )}
      <div className="space-y-1.5">
        {value.map((item, i) => (
          <div key={i} className="flex items-center gap-1.5">
            <input
              type="text"
              value={item}
              onChange={(e) => updateItem(i, e.target.value)}
              placeholder={placeholder}
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

// --- Resource Presets ---

interface ResourcePreset {
  id: string;
  name: string;
  icon: LucideIcon;
  data: Partial<ResourceFormData>;
}

const RESOURCE_PRESETS: ResourcePreset[] = [
  {
    id: 'postgres',
    name: 'PostgreSQL',
    icon: Database,
    data: {
      name: 'postgres',
      image: 'postgres:16',
      env: { POSTGRES_DB: 'mydb', POSTGRES_USER: 'user', POSTGRES_PASSWORD: '${vault:POSTGRES_PASSWORD}' },
      ports: ['5432:5432'],
    },
  },
  {
    id: 'redis',
    name: 'Redis',
    icon: Database,
    data: {
      name: 'redis',
      image: 'redis:7-alpine',
      ports: ['6379:6379'],
    },
  },
  {
    id: 'mysql',
    name: 'MySQL',
    icon: Database,
    data: {
      name: 'mysql',
      image: 'mysql:8',
      env: { MYSQL_DATABASE: 'mydb', MYSQL_ROOT_PASSWORD: '${vault:MYSQL_ROOT_PASSWORD}' },
      ports: ['3306:3306'],
    },
  },
  {
    id: 'mongodb',
    name: 'MongoDB',
    icon: Database,
    data: {
      name: 'mongodb',
      image: 'mongo:7',
      ports: ['27017:27017'],
    },
  },
];

// --- Collapsible item header ---

function ItemHeader({
  icon: Icon,
  name,
  label,
  expanded,
  onToggle,
  onRemove,
  iconColor = 'text-primary',
}: {
  icon: LucideIcon;
  name: string;
  label: string;
  expanded: boolean;
  onToggle: () => void;
  onRemove: () => void;
  iconColor?: string;
}) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 bg-white/[0.02] border border-border/20 rounded-lg group hover:border-border/30 transition-colors">
      <button type="button" onClick={onToggle} className="flex items-center gap-2 flex-1 text-left">
        {expanded ? (
          <ChevronDown size={12} className="text-text-muted flex-shrink-0" />
        ) : (
          <ChevronRight size={12} className="text-text-muted flex-shrink-0" />
        )}
        <Icon size={12} className={cn(iconColor, 'flex-shrink-0')} />
        <span className="text-xs font-medium text-text-primary truncate">
          {name || <span className="text-text-muted italic">{label}</span>}
        </span>
      </button>
      <button
        type="button"
        onClick={onRemove}
        className="p-1 text-text-muted hover:text-status-error transition-colors opacity-0 group-hover:opacity-100 flex-shrink-0"
        title="Remove"
      >
        <Trash2 size={11} />
      </button>
    </div>
  );
}

// --- Inline Agent Form ---

function InlineAgentForm({
  data,
  onChange,
  availableServers,
}: {
  data: AgentFormData;
  onChange: (data: Partial<AgentFormData>) => void;
  availableServers: string[];
}) {
  const isHeadless = data.agentType === 'headless';

  return (
    <div className="space-y-3 px-3 pb-3">
      {/* Name */}
      <div>
        <label className={labelClass}>
          Name <span className="text-status-error">*</span>
        </label>
        <input
          type="text"
          value={data.name}
          onChange={(e) => onChange({ name: toKebabCase(e.target.value) })}
          placeholder="my-agent"
          className={cn(inputClass, 'font-mono')}
        />
      </div>

      {/* Agent Type Toggle */}
      <div>
        <label className={labelClass}>Type</label>
        <div className="grid grid-cols-2 gap-2">
          {[
            { type: 'container' as const, icon: Container, label: 'Container' },
            { type: 'headless' as const, icon: Cpu, label: 'Headless' },
          ].map((opt) => {
            const Icon = opt.icon;
            const selected = data.agentType === opt.type;
            return (
              <button
                key={opt.type}
                type="button"
                onClick={() => onChange({ agentType: opt.type })}
                className={cn(
                  'flex items-center gap-2 px-3 py-2 rounded-lg border text-xs font-medium transition-all duration-200',
                  selected
                    ? 'bg-tertiary/[0.08] border-tertiary/30 text-tertiary'
                    : 'bg-white/[0.02] border-white/[0.06] text-text-muted hover:text-text-secondary hover:border-white/[0.1]',
                )}
              >
                <Icon size={13} />
                {opt.label}
              </button>
            );
          })}
        </div>
      </div>

      {/* Container fields */}
      {!isHeadless && (
        <div>
          <label className={labelClass}>Image</label>
          <input
            type="text"
            value={data.image ?? ''}
            onChange={(e) => onChange({ image: e.target.value })}
            placeholder="my-agent:latest"
            className={cn(inputClass, 'font-mono')}
          />
        </div>
      )}

      {/* Headless fields */}
      {isHeadless && (
        <>
          <div>
            <label className={labelClass}>Runtime</label>
            <select
              value={data.runtime ?? ''}
              onChange={(e) => onChange({ runtime: e.target.value || undefined })}
              className={inputClass}
            >
              <option value="">Select runtime</option>
              <option value="node">Node.js</option>
              <option value="python">Python</option>
              <option value="deno">Deno</option>
              <option value="bun">Bun</option>
            </select>
          </div>
          <div>
            <label className={labelClass}>
              Prompt <span className="text-status-error">*</span>
            </label>
            <textarea
              value={data.prompt ?? ''}
              onChange={(e) => onChange({ prompt: e.target.value })}
              placeholder="Agent system prompt..."
              rows={3}
              className={cn(inputClass, 'resize-none')}
            />
          </div>
        </>
      )}

      {/* Uses — server selection */}
      {availableServers.length > 0 && (
        <div>
          <label className={labelClass}>Tool Access (MCP Servers)</label>
          <div className="space-y-1">
            {availableServers.map((serverName) => {
              const isUsed = data.uses?.some((u) => u.server === serverName) ?? false;
              return (
                <button
                  key={serverName}
                  type="button"
                  onClick={() => {
                    const currentUses = data.uses ?? [];
                    if (isUsed) {
                      onChange({ uses: currentUses.filter((u) => u.server !== serverName) });
                    } else {
                      onChange({ uses: [...currentUses, { server: serverName }] });
                    }
                  }}
                  className={cn(
                    'w-full flex items-center gap-2 px-3 py-1.5 rounded-lg text-xs transition-all duration-200 border',
                    isUsed
                      ? 'bg-primary/[0.06] border-primary/20 text-primary'
                      : 'bg-white/[0.02] border-white/[0.04] text-text-muted hover:text-text-secondary hover:border-white/[0.08]',
                  )}
                >
                  <div className={cn(
                    'w-3.5 h-3.5 rounded border flex items-center justify-center transition-colors',
                    isUsed ? 'bg-primary/20 border-primary/40' : 'border-border/40',
                  )}>
                    {isUsed && <Check size={9} className="text-primary" />}
                  </div>
                  <Server size={11} className="flex-shrink-0" />
                  <span className="font-mono truncate">{serverName}</span>
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Env */}
      <KeyValueEditor
        label="Environment Variables"
        value={data.env ?? {}}
        onChange={(env) => onChange({ env })}
        placeholder={{ key: 'ENV_VAR', value: 'value' }}
        showSecrets
      />

      {/* A2A Toggle */}
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() =>
            onChange({ a2a: { enabled: !(data.a2a?.enabled ?? false), skills: data.a2a?.skills } })
          }
          className={cn(
            'relative w-8 h-4 rounded-full transition-colors duration-200',
            data.a2a?.enabled ? 'bg-tertiary/40' : 'bg-border/40',
          )}
        >
          <div
            className={cn(
              'absolute top-0.5 w-3 h-3 rounded-full transition-all duration-200',
              data.a2a?.enabled ? 'left-4.5 bg-tertiary' : 'left-0.5 bg-text-muted',
            )}
          />
        </button>
        <span className="text-xs text-text-secondary">A2A Protocol</span>
      </div>

      {data.a2a?.enabled && (
        <div className="space-y-2 p-3 rounded-xl bg-tertiary/[0.03] border border-tertiary/10">
          <div className="flex items-center justify-between mb-1">
            <label className="text-[10px] text-tertiary/80 uppercase tracking-wider font-medium">A2A Skills</label>
            <button
              type="button"
              onClick={() => {
                const skills = [...(data.a2a?.skills ?? []), { id: '', name: '', description: '' }];
                onChange({ a2a: { enabled: true, skills } });
              }}
              className="flex items-center gap-1 text-[10px] text-tertiary hover:text-tertiary/80 transition-colors"
            >
              <Plus size={10} />
              Add skill
            </button>
          </div>
          {(!data.a2a?.skills || data.a2a.skills.length === 0) && (
            <p className="text-[10px] text-text-muted/60 italic py-1">No A2A skills configured</p>
          )}
          {data.a2a?.skills?.map((skill, i) => (
            <div key={i} className="flex items-start gap-1.5">
              <div className="flex-1 space-y-1">
                <input
                  type="text"
                  value={skill.id}
                  onChange={(e) => {
                    const skills = [...(data.a2a?.skills ?? [])];
                    skills[i] = { ...skills[i], id: e.target.value };
                    onChange({ a2a: { enabled: true, skills } });
                  }}
                  placeholder="skill-id"
                  className={cn(inputClass, 'font-mono')}
                />
                <input
                  type="text"
                  value={skill.name}
                  onChange={(e) => {
                    const skills = [...(data.a2a?.skills ?? [])];
                    skills[i] = { ...skills[i], name: e.target.value };
                    onChange({ a2a: { enabled: true, skills } });
                  }}
                  placeholder="Skill Name"
                  className={inputClass}
                />
              </div>
              <button
                type="button"
                onClick={() => {
                  const skills = (data.a2a?.skills ?? []).filter((_, j) => j !== i);
                  onChange({ a2a: { enabled: true, skills } });
                }}
                className="p-1 mt-1 text-text-muted hover:text-status-error transition-colors flex-shrink-0"
              >
                <X size={12} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// --- Inline Resource Form ---

function InlineResourceForm({
  data,
  onChange,
}: {
  data: ResourceFormData;
  onChange: (data: Partial<ResourceFormData>) => void;
}) {
  return (
    <div className="space-y-3 px-3 pb-3">
      <div>
        <label className={labelClass}>
          Name <span className="text-status-error">*</span>
        </label>
        <input
          type="text"
          value={data.name}
          onChange={(e) => onChange({ name: toKebabCase(e.target.value) })}
          placeholder="my-resource"
          className={cn(inputClass, 'font-mono')}
        />
      </div>
      <div>
        <label className={labelClass}>
          Image <span className="text-status-error">*</span>
        </label>
        <input
          type="text"
          value={data.image}
          onChange={(e) => onChange({ image: e.target.value })}
          placeholder="postgres:16"
          className={cn(inputClass, 'font-mono')}
        />
      </div>
      <KeyValueEditor
        label="Environment Variables"
        value={data.env ?? {}}
        onChange={(env) => onChange({ env })}
        placeholder={{ key: 'ENV_VAR', value: 'value' }}
        showSecrets
      />
      <StringArrayEditor
        label="Ports"
        value={data.ports ?? []}
        onChange={(ports) => onChange({ ports })}
        placeholder="8080:8080"
        addLabel="Add port"
        emptyText="No port mappings"
      />
      <StringArrayEditor
        label="Volumes"
        value={data.volumes ?? []}
        onChange={(volumes) => onChange({ volumes })}
        placeholder="/host/path:/container/path"
        addLabel="Add volume"
        emptyText="No volumes"
      />
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
    </div>
  );
}

// --- Resource Preset Picker ---

function ResourcePresetPicker({ onSelect }: { onSelect: (preset: ResourcePreset | null) => void }) {
  return (
    <div className="grid grid-cols-5 gap-1.5 mb-3">
      {RESOURCE_PRESETS.map((preset) => (
        <button
          key={preset.id}
          type="button"
          onClick={() => onSelect(preset)}
          className="flex flex-col items-center gap-1 p-2 rounded-lg border border-white/[0.06] bg-white/[0.02] hover:bg-white/[0.05] hover:border-white/[0.1] transition-all duration-200 text-center"
        >
          <Database size={12} className="text-secondary" />
          <span className="text-[10px] text-text-primary font-medium">{preset.name}</span>
        </button>
      ))}
      <button
        type="button"
        onClick={() => onSelect(null)}
        className="flex flex-col items-center gap-1 p-2 rounded-lg border border-white/[0.06] bg-white/[0.02] hover:bg-white/[0.05] hover:border-white/[0.1] transition-all duration-200 text-center"
      >
        <Plus size={12} className="text-text-muted" />
        <span className="text-[10px] text-text-muted font-medium">Custom</span>
      </button>
    </div>
  );
}

// --- Output Format Options ---

const OUTPUT_FORMATS = ['json', 'toon', 'csv', 'text'];
const NETWORK_DRIVERS = ['bridge', 'host', 'overlay', 'macvlan'];

// --- Main StackForm ---

interface StackFormProps {
  data: StackFormData;
  onChange: (data: Partial<StackFormData>) => void;
  errors?: Record<string, string>;
}

export function StackForm({ data, onChange, errors }: StackFormProps) {
  const [expandedSections, setExpandedSections] = useState<Set<string>>(
    new Set(['identity']),
  );
  const [expandedServers, setExpandedServers] = useState<Set<number>>(new Set());
  const [expandedAgents, setExpandedAgents] = useState<Set<number>>(new Set());
  const [expandedResources, setExpandedResources] = useState<Set<number>>(new Set());
  const [showResourcePresets, setShowResourcePresets] = useState(false);

  const toggleSection = useCallback((section: string) => {
    setExpandedSections((prev) => {
      const next = new Set(prev);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  }, []);

  const toggleServer = useCallback((idx: number) => {
    setExpandedServers((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) next.delete(idx);
      else next.add(idx);
      return next;
    });
  }, []);

  const toggleAgent = useCallback((idx: number) => {
    setExpandedAgents((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) next.delete(idx);
      else next.add(idx);
      return next;
    });
  }, []);

  const toggleResource = useCallback((idx: number) => {
    setExpandedResources((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) next.delete(idx);
      else next.add(idx);
      return next;
    });
  }, []);

  // Compute available server names for agent "uses" selection
  const availableServers = useMemo(
    () => (data.mcpServers ?? []).map((s) => s.name).filter(Boolean),
    [data.mcpServers],
  );

  // Counts for badges
  const serverCount = data.mcpServers?.length ?? 0;
  const agentCount = data.agents?.length ?? 0;
  const resourceCount = data.resources?.length ?? 0;
  const gatewayFieldCount =
    (data.gateway?.allowedOrigins?.length ? 1 : 0) +
    (data.gateway?.auth ? 1 : 0) +
    (data.gateway?.codeMode ? 1 : 0) +
    (data.gateway?.outputFormat ? 1 : 0);
  const secretSetCount = data.secrets?.sets?.length ?? 0;

  // --- Handlers for nested arrays ---

  const addServer = () => {
    const servers = [...(data.mcpServers ?? []), { name: '', serverType: 'container' as const }];
    onChange({ mcpServers: servers });
    setExpandedServers((prev) => new Set([...prev, servers.length - 1]));
  };

  const updateServer = (idx: number, partial: Partial<MCPServerFormData>) => {
    const servers = [...(data.mcpServers ?? [])];
    servers[idx] = { ...servers[idx], ...partial };
    onChange({ mcpServers: servers });
  };

  const removeServer = (idx: number) => {
    const servers = (data.mcpServers ?? []).filter((_, i) => i !== idx);
    onChange({ mcpServers: servers });
    setExpandedServers((prev) => {
      const next = new Set<number>();
      prev.forEach((v) => {
        if (v < idx) next.add(v);
        else if (v > idx) next.add(v - 1);
      });
      return next;
    });
  };

  const addAgent = () => {
    const agents = [...(data.agents ?? []), { name: '', agentType: 'container' as const }];
    onChange({ agents });
    setExpandedAgents((prev) => new Set([...prev, agents.length - 1]));
  };

  const updateAgent = (idx: number, partial: Partial<AgentFormData>) => {
    const agents = [...(data.agents ?? [])];
    agents[idx] = { ...agents[idx], ...partial };
    onChange({ agents });
  };

  const removeAgent = (idx: number) => {
    const agents = (data.agents ?? []).filter((_, i) => i !== idx);
    onChange({ agents });
    setExpandedAgents((prev) => {
      const next = new Set<number>();
      prev.forEach((v) => {
        if (v < idx) next.add(v);
        else if (v > idx) next.add(v - 1);
      });
      return next;
    });
  };

  const addResource = (preset?: ResourcePreset | null) => {
    const newResource: ResourceFormData = preset
      ? { name: preset.data.name ?? '', image: preset.data.image ?? '', ...preset.data }
      : { name: '', image: '' };
    const resources = [...(data.resources ?? []), newResource];
    onChange({ resources });
    setExpandedResources((prev) => new Set([...prev, resources.length - 1]));
    setShowResourcePresets(false);
  };

  const updateResource = (idx: number, partial: Partial<ResourceFormData>) => {
    const resources = [...(data.resources ?? [])];
    resources[idx] = { ...resources[idx], ...partial };
    onChange({ resources });
  };

  const removeResource = (idx: number) => {
    const resources = (data.resources ?? []).filter((_, i) => i !== idx);
    onChange({ resources });
    setExpandedResources((prev) => {
      const next = new Set<number>();
      prev.forEach((v) => {
        if (v < idx) next.add(v);
        else if (v > idx) next.add(v - 1);
      });
      return next;
    });
  };

  return (
    <div className="space-y-3">
      {/* Section 1: Identity */}
      <Section
        title="Identity"
        icon={Layers}
        expanded={expandedSections.has('identity')}
        onToggle={() => toggleSection('identity')}
      >
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className={labelClass}>
              Name <span className="text-status-error">*</span>
            </label>
            <input
              type="text"
              value={data.name}
              onChange={(e) => onChange({ name: toKebabCase(e.target.value) })}
              placeholder="my-stack"
              className={cn(inputClass, 'font-mono', errors?.name && 'border-status-error/50')}
            />
            <FieldError error={errors?.name} />
            <p className="text-[10px] text-text-muted mt-1">Kebab-case stack identifier</p>
          </div>
          <div>
            <label className={labelClass}>Version</label>
            <div className="flex items-center gap-2 px-3 py-2 rounded-lg bg-surface-elevated/60 border border-border/30 text-xs text-text-muted">
              <Zap size={12} className="text-primary" />
              {data.version || '1'}
              <span className="text-[10px] ml-auto opacity-60">locked</span>
            </div>
          </div>
        </div>
      </Section>

      {/* Section 2: Gateway */}
      <Section
        title="Gateway"
        icon={Shield}
        expanded={expandedSections.has('gateway')}
        onToggle={() => toggleSection('gateway')}
        badge={gatewayFieldCount > 0 ? `${gatewayFieldCount}` : undefined}
      >
        <p className="text-[10px] text-text-muted mb-2">Optional gateway configuration for the stack</p>

        {/* Allowed Origins */}
        <StringArrayEditor
          label="Allowed Origins"
          value={data.gateway?.allowedOrigins ?? []}
          onChange={(allowedOrigins) =>
            onChange({ gateway: { ...data.gateway, allowedOrigins: allowedOrigins.length > 0 ? allowedOrigins : undefined } })
          }
          placeholder="https://example.com"
          addLabel="Add origin"
          emptyText="All origins allowed"
        />

        {/* Auth */}
        <div>
          <label className={labelClass}>Authentication</label>
          <select
            value={data.gateway?.auth?.type ?? ''}
            onChange={(e) => {
              const type = e.target.value;
              if (!type) {
                onChange({ gateway: { ...data.gateway, auth: undefined } });
              } else {
                onChange({
                  gateway: {
                    ...data.gateway,
                    auth: { type, token: data.gateway?.auth?.token ?? '', header: data.gateway?.auth?.header },
                  },
                });
              }
            }}
            className={inputClass}
          >
            <option value="">None</option>
            <option value="bearer">Bearer Token</option>
            <option value="header">Custom Header</option>
          </select>
        </div>

        {data.gateway?.auth?.type && (
          <div className="space-y-3 p-3 rounded-xl bg-white/[0.02] border border-white/[0.04]">
            <div>
              <label className={labelClass}>
                Token <span className="text-status-error">*</span>
              </label>
              <div className="flex items-center gap-0.5">
                <input
                  type="text"
                  value={data.gateway.auth.token}
                  onChange={(e) =>
                    onChange({ gateway: { ...data.gateway, auth: { ...data.gateway!.auth!, token: e.target.value } } })
                  }
                  placeholder="auth-token-value"
                  className={cn(
                    inputClass,
                    'flex-1',
                    data.gateway.auth.token.startsWith('${vault:') && 'text-tertiary font-medium',
                  )}
                />
                <SecretsPopover
                  onSelect={(ref) =>
                    onChange({ gateway: { ...data.gateway, auth: { ...data.gateway!.auth!, token: ref } } })
                  }
                />
              </div>
            </div>
            {data.gateway.auth.type === 'header' && (
              <div>
                <label className={labelClass}>Header Name</label>
                <input
                  type="text"
                  value={data.gateway.auth.header ?? ''}
                  onChange={(e) =>
                    onChange({ gateway: { ...data.gateway, auth: { ...data.gateway!.auth!, header: e.target.value } } })
                  }
                  placeholder="X-API-Key"
                  className={cn(inputClass, 'font-mono')}
                />
              </div>
            )}
          </div>
        )}

        {/* Code Mode + Output Format */}
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className={labelClass}>Code Mode</label>
            <select
              value={data.gateway?.codeMode ?? ''}
              onChange={(e) =>
                onChange({ gateway: { ...data.gateway, codeMode: e.target.value || undefined } })
              }
              className={inputClass}
            >
              <option value="">Off</option>
              <option value="on">On</option>
            </select>
          </div>
          <div>
            <label className={labelClass}>Output Format</label>
            <select
              value={data.gateway?.outputFormat ?? ''}
              onChange={(e) =>
                onChange({ gateway: { ...data.gateway, outputFormat: e.target.value || undefined } })
              }
              className={inputClass}
            >
              <option value="">Default</option>
              {OUTPUT_FORMATS.map((f) => (
                <option key={f} value={f}>{f}</option>
              ))}
            </select>
          </div>
        </div>
      </Section>

      {/* Section 3: Network */}
      <Section
        title="Network"
        icon={Network}
        expanded={expandedSections.has('network')}
        onToggle={() => toggleSection('network')}
        badge={data.network ? data.network.name || '1' : undefined}
      >
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className={labelClass}>Network Name</label>
            <input
              type="text"
              value={data.network?.name ?? ''}
              onChange={(e) =>
                onChange({ network: e.target.value ? { name: e.target.value, driver: data.network?.driver ?? 'bridge' } : undefined })
              }
              placeholder="gridctl-net"
              className={cn(inputClass, 'font-mono')}
            />
          </div>
          <div>
            <label className={labelClass}>Driver</label>
            <select
              value={data.network?.driver ?? 'bridge'}
              onChange={(e) =>
                onChange({ network: { name: data.network?.name ?? '', driver: e.target.value } })
              }
              className={inputClass}
            >
              {NETWORK_DRIVERS.map((d) => (
                <option key={d} value={d}>{d}</option>
              ))}
            </select>
          </div>
        </div>
      </Section>

      {/* Section 4: Secrets */}
      <Section
        title="Secrets"
        icon={KeyRound}
        expanded={expandedSections.has('secrets')}
        onToggle={() => toggleSection('secrets')}
        badge={secretSetCount > 0 ? `${secretSetCount}` : undefined}
      >
        <StringArrayEditor
          label="Variable Sets"
          value={data.secrets?.sets ?? []}
          onChange={(sets) =>
            onChange({ secrets: sets.length > 0 ? { sets } : undefined })
          }
          placeholder="secret-set-name"
          addLabel="Add set"
          emptyText="No secret sets referenced"
        />
      </Section>

      {/* Section 5: MCP Servers */}
      <Section
        title="MCP Servers"
        icon={Server}
        expanded={expandedSections.has('servers')}
        onToggle={() => toggleSection('servers')}
        badge={serverCount > 0 ? `${serverCount}` : undefined}
      >
        {serverCount === 0 && (
          <p className="text-[10px] text-text-muted/60 italic py-2">No MCP servers configured</p>
        )}
        <div className="space-y-2">
          {(data.mcpServers ?? []).map((server, i) => (
            <div key={i} className="space-y-0">
              <ItemHeader
                icon={Server}
                name={server.name}
                label={`Server ${i + 1}`}
                expanded={expandedServers.has(i)}
                onToggle={() => toggleServer(i)}
                onRemove={() => removeServer(i)}
              />
              {expandedServers.has(i) && (
                <div className="ml-2 mt-1 pl-3 border-l border-border/20">
                  <MCPServerForm
                    data={server}
                    onChange={(partial) => updateServer(i, partial)}
                  />
                </div>
              )}
            </div>
          ))}
        </div>
        <button
          type="button"
          onClick={addServer}
          className="w-full flex items-center justify-center gap-1.5 py-2 rounded-lg border border-dashed border-border/30 text-xs text-secondary hover:text-secondary-light hover:border-secondary/30 transition-all duration-200"
        >
          <Plus size={12} />
          Add MCP Server
        </button>
      </Section>

      {/* Section 6: Agents */}
      <Section
        title="Agents"
        icon={Bot}
        expanded={expandedSections.has('agents')}
        onToggle={() => toggleSection('agents')}
        badge={agentCount > 0 ? `${agentCount}` : undefined}
      >
        {agentCount === 0 && (
          <p className="text-[10px] text-text-muted/60 italic py-2">No agents configured</p>
        )}
        <div className="space-y-2">
          {(data.agents ?? []).map((agent, i) => (
            <div key={i} className="space-y-0">
              <ItemHeader
                icon={Bot}
                name={agent.name}
                label={`Agent ${i + 1}`}
                expanded={expandedAgents.has(i)}
                onToggle={() => toggleAgent(i)}
                onRemove={() => removeAgent(i)}
                iconColor="text-tertiary"
              />
              {expandedAgents.has(i) && (
                <div className="mt-1 border border-border/15 rounded-lg overflow-hidden">
                  <InlineAgentForm
                    data={agent}
                    onChange={(partial) => updateAgent(i, partial)}
                    availableServers={availableServers}
                  />
                </div>
              )}
            </div>
          ))}
        </div>
        <button
          type="button"
          onClick={addAgent}
          className="w-full flex items-center justify-center gap-1.5 py-2 rounded-lg border border-dashed border-border/30 text-xs text-tertiary hover:text-tertiary/80 hover:border-tertiary/30 transition-all duration-200"
        >
          <Plus size={12} />
          Add Agent
        </button>
      </Section>

      {/* Section 7: Resources */}
      <Section
        title="Resources"
        icon={Database}
        expanded={expandedSections.has('resources')}
        onToggle={() => toggleSection('resources')}
        badge={resourceCount > 0 ? `${resourceCount}` : undefined}
      >
        {resourceCount === 0 && !showResourcePresets && (
          <p className="text-[10px] text-text-muted/60 italic py-2">No resources configured</p>
        )}
        {showResourcePresets && (
          <ResourcePresetPicker onSelect={(preset) => addResource(preset)} />
        )}
        <div className="space-y-2">
          {(data.resources ?? []).map((resource, i) => (
            <div key={i} className="space-y-0">
              <ItemHeader
                icon={Database}
                name={resource.name}
                label={`Resource ${i + 1}`}
                expanded={expandedResources.has(i)}
                onToggle={() => toggleResource(i)}
                onRemove={() => removeResource(i)}
                iconColor="text-secondary"
              />
              {expandedResources.has(i) && (
                <div className="mt-1 border border-border/15 rounded-lg overflow-hidden">
                  <InlineResourceForm
                    data={resource}
                    onChange={(partial) => updateResource(i, partial)}
                  />
                </div>
              )}
            </div>
          ))}
        </div>
        <button
          type="button"
          onClick={() => {
            if (showResourcePresets) {
              addResource(null);
            } else {
              setShowResourcePresets(true);
            }
          }}
          className="w-full flex items-center justify-center gap-1.5 py-2 rounded-lg border border-dashed border-border/30 text-xs text-secondary hover:text-secondary-light hover:border-secondary/30 transition-all duration-200"
        >
          <Plus size={12} />
          Add Resource
        </button>
      </Section>
    </div>
  );
}
