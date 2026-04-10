import { useState, useCallback } from 'react';
import {
  Database,
  Box,
  ChevronDown,
  ChevronRight,
  Plus,
  X,
  AlertCircle,
  KeyRound,
  Network,
  Settings,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../../lib/cn';
import type { ResourceFormData } from '../../../lib/yaml-builder';
import { SecretsPopover } from '../SecretsPopover';

// --- Resource presets ---

interface ResourcePreset {
  id: string;
  name: string;
  icon: LucideIcon;
  description: string;
  data: Partial<ResourceFormData>;
}

const RESOURCE_PRESETS: ResourcePreset[] = [
  {
    id: 'postgres',
    name: 'PostgreSQL',
    icon: Database,
    description: 'Relational database',
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
    description: 'In-memory cache',
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
    description: 'Relational database',
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
    description: 'Document database',
    data: {
      name: 'mongodb',
      image: 'mongo:7',
      ports: ['27017:27017'],
    },
  },
];

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
        <Icon size={14} className="text-secondary flex-shrink-0" />
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

function FieldError({ error }: { error?: string }) {
  if (!error) return null;
  return (
    <p className="text-[10px] text-status-error mt-1">
      <AlertCircle size={10} className="inline mr-1 -mt-0.5" />
      {error}
    </p>
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

// --- Preset Picker ---

function PresetPicker({
  selectedPreset,
  onSelect,
}: {
  selectedPreset: string | null;
  onSelect: (preset: ResourcePreset | null) => void;
}) {
  return (
    <div className="space-y-2">
      <p className="text-[10px] text-text-muted">Start from a preset or create a custom resource</p>
      <div className="grid grid-cols-5 gap-1.5">
        {RESOURCE_PRESETS.map((preset) => (
          <button
            key={preset.id}
            type="button"
            onClick={() => onSelect(preset)}
            className={cn(
              'flex flex-col items-center gap-1 p-2.5 rounded-lg border transition-all duration-200 text-center',
              selectedPreset === preset.id
                ? 'bg-secondary/[0.08] border-secondary/30 shadow-[0_0_12px_rgba(13,148,136,0.06)]'
                : 'border-white/[0.06] bg-white/[0.02] hover:bg-white/[0.05] hover:border-white/[0.1]',
            )}
          >
            <Database size={14} className={cn(selectedPreset === preset.id ? 'text-secondary' : 'text-text-muted')} />
            <span className={cn('text-[10px] font-medium', selectedPreset === preset.id ? 'text-secondary' : 'text-text-primary')}>
              {preset.name}
            </span>
          </button>
        ))}
        <button
          type="button"
          onClick={() => onSelect(null)}
          className={cn(
            'flex flex-col items-center gap-1 p-2.5 rounded-lg border transition-all duration-200 text-center',
            selectedPreset === 'custom'
              ? 'bg-secondary/[0.08] border-secondary/30'
              : 'border-white/[0.06] bg-white/[0.02] hover:bg-white/[0.05] hover:border-white/[0.1]',
          )}
        >
          <Plus size={14} className="text-text-muted" />
          <span className="text-[10px] text-text-muted font-medium">Custom</span>
        </button>
      </div>
    </div>
  );
}

// --- Main ResourceForm ---

interface ResourceFormProps {
  data: ResourceFormData;
  onChange: (data: Partial<ResourceFormData>) => void;
  errors?: Record<string, string>;
}

export function ResourceForm({ data, onChange, errors }: ResourceFormProps) {
  const [expandedSections, setExpandedSections] = useState<Set<string>>(
    new Set(['preset', 'identity', 'config']),
  );
  const [selectedPreset, setSelectedPreset] = useState<string | null>(null);

  const toggleSection = useCallback((section: string) => {
    setExpandedSections((prev) => {
      const next = new Set(prev);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  }, []);

  const handlePresetSelect = useCallback((preset: ResourcePreset | null) => {
    if (preset) {
      setSelectedPreset(preset.id);
      onChange({
        name: preset.data.name ?? '',
        image: preset.data.image ?? '',
        env: preset.data.env,
        ports: preset.data.ports,
        volumes: preset.data.volumes,
      });
    } else {
      setSelectedPreset('custom');
      onChange({ name: '', image: '' });
    }
    setExpandedSections((prev) => new Set([...prev, 'identity', 'config']));
  }, [onChange]);

  const envCount = data.env ? Object.keys(data.env).length : 0;
  const portCount = data.ports?.length ?? 0;
  const volumeCount = data.volumes?.length ?? 0;

  return (
    <div className="space-y-3">
      {/* Section 1: Preset */}
      <Section
        title="Preset"
        icon={Database}
        expanded={expandedSections.has('preset')}
        onToggle={() => toggleSection('preset')}
        badge={selectedPreset && selectedPreset !== 'custom' ? RESOURCE_PRESETS.find((p) => p.id === selectedPreset)?.name : undefined}
      >
        <PresetPicker selectedPreset={selectedPreset} onSelect={handlePresetSelect} />
      </Section>

      {/* Section 2: Identity */}
      <Section
        title="Identity"
        icon={Box}
        expanded={expandedSections.has('identity')}
        onToggle={() => toggleSection('identity')}
      >
        <div className="space-y-3">
          <div>
            <label className={labelClass}>
              Name <span className="text-status-error">*</span>
            </label>
            <input
              type="text"
              value={data.name}
              onChange={(e) => onChange({ name: toKebabCase(e.target.value) })}
              onBlur={(e) => onChange({ name: e.target.value.replace(/-+$/, '') })}
              placeholder="my-resource"
              className={cn(inputClass, 'font-mono', errors?.name && 'border-status-error/50')}
            />
            <FieldError error={errors?.name} />
            <p className="text-[10px] text-text-muted mt-1">Kebab-case identifier for this resource</p>
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
              className={cn(inputClass, 'font-mono', errors?.image && 'border-status-error/50')}
            />
            <FieldError error={errors?.image} />
          </div>
        </div>
      </Section>

      {/* Section 3: Environment & Secrets */}
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

      {/* Section 4: Ports */}
      <Section
        title="Ports"
        icon={Network}
        expanded={expandedSections.has('ports')}
        onToggle={() => toggleSection('ports')}
        badge={portCount > 0 ? `${portCount}` : undefined}
      >
        <StringArrayEditor
          label="Port Mappings"
          value={data.ports ?? []}
          onChange={(ports) => onChange({ ports })}
          placeholder="8080:8080"
          addLabel="Add port"
          emptyText="No port mappings"
        />
        <p className="text-[10px] text-text-muted">Format: host:container (e.g., 5432:5432)</p>
      </Section>

      {/* Section 5: Volumes */}
      <Section
        title="Volumes"
        icon={Database}
        expanded={expandedSections.has('volumes')}
        onToggle={() => toggleSection('volumes')}
        badge={volumeCount > 0 ? `${volumeCount}` : undefined}
      >
        <StringArrayEditor
          label="Volume Mounts"
          value={data.volumes ?? []}
          onChange={(volumes) => onChange({ volumes })}
          placeholder="/host/path:/container/path"
          addLabel="Add volume"
          emptyText="No volumes"
        />
        <p className="text-[10px] text-text-muted">Format: host:container (e.g., ./data:/var/lib/data)</p>
      </Section>

      {/* Section 6: Network */}
      <Section
        title="Network"
        icon={Settings}
        expanded={expandedSections.has('network')}
        onToggle={() => toggleSection('network')}
        badge={data.network ? data.network : undefined}
      >
        <div>
          <label className={labelClass}>Network</label>
          <input
            type="text"
            value={data.network ?? ''}
            onChange={(e) => onChange({ network: e.target.value || undefined })}
            placeholder="network-name"
            className={cn(inputClass, 'font-mono')}
          />
          <p className="text-[10px] text-text-muted mt-1">Optional network name for this resource</p>
        </div>
      </Section>
    </div>
  );
}
