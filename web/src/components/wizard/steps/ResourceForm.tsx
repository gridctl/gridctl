import { useState, useCallback } from 'react';
import {
  Database,
  Box,
  ChevronDown,
  ChevronRight,
  Plus,
  X,
  KeyRound,
  Network,
  Settings,
  AlertCircle,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../../lib/cn';
import type { ResourceFormData } from '../../../lib/yaml-builder';
import { KeyValueEditor } from '../KeyValueEditor';
import { Field } from '../../ui/Field';

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
  hasError,
}: {
  title: string;
  icon: LucideIcon;
  expanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
  badge?: string;
  hasError?: boolean;
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
        <Icon size={14} className="text-secondary flex-shrink-0" />
        <span className="text-xs font-medium text-text-primary">{title}</span>
        {hasError && (
          <span className="flex items-center text-status-error">
            <AlertCircle size={12} aria-hidden="true" />
            <span className="sr-only">, has errors</span>
          </span>
        )}
        {badge && (
          <span className="ml-auto text-3xs text-text-muted bg-surface-highlight px-1.5 py-0.5 rounded-full">
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

const inputClass = 'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted transition-colors';

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
          className="flex items-center gap-1 text-3xs text-secondary hover:text-secondary-light transition-colors"
        >
          <Plus size={10} />
          {addLabel}
        </button>
      </div>
      {value.length === 0 && (
        <p className="text-3xs text-text-muted italic py-2">{emptyText}</p>
      )}
      <div className="space-y-1.5">
        {value.map((item, i) => (
          <div key={i} className="flex items-center gap-1.5">
            <input
              type="text"
              value={item}
              onChange={(e) => updateItem(i, e.target.value)}
              placeholder={placeholder}
              aria-label={`${label} ${i + 1}`}
              className={cn(inputClass, 'flex-1 font-mono')}
            />
            <button
              type="button"
              onClick={() => removeItem(i)}
              aria-label={`Remove ${label} ${i + 1}`}
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

// --- Main ResourceForm ---

interface ResourceFormProps {
  data: ResourceFormData;
  onChange: (data: Partial<ResourceFormData>) => void;
  errors?: Record<string, string>;
  // Each new value expands the sections that hold an error.
  revealKey?: number;
}

export function ResourceForm({ data, onChange, errors, revealKey }: ResourceFormProps) {
  const [expandedSections, setExpandedSections] = useState<Set<string>>(
    new Set(['identity', 'config']),
  );
  const identityHasError = Boolean(errors?.name || errors?.image);
  const [revealed, setRevealed] = useState<number | undefined>(undefined);
  if (revealKey && revealKey !== revealed) {
    setRevealed(revealKey);
    if (identityHasError) setExpandedSections((prev) => new Set([...prev, 'identity']));
  }

  const toggleSection = useCallback((section: string) => {
    setExpandedSections((prev) => {
      const next = new Set(prev);
      if (next.has(section)) next.delete(section);
      else next.add(section);
      return next;
    });
  }, []);

  const envCount = data.env ? Object.keys(data.env).length : 0;
  const portCount = data.ports?.length ?? 0;
  const volumeCount = data.volumes?.length ?? 0;

  return (
    <div className="space-y-3">
      {/* Section 1: Identity */}
      <Section
        title="Identity"
        icon={Box}
        expanded={expandedSections.has('identity')}
        onToggle={() => toggleSection('identity')}
        hasError={identityHasError}
      >
        <div className="space-y-3">
          <Field label="Name" required error={errors?.name} help="Kebab-case identifier for this resource">
            {({ id, describedBy, invalid }) => (
              <input
                id={id}
                type="text"
                value={data.name}
                onChange={(e) => onChange({ name: toKebabCase(e.target.value) })}
                onBlur={(e) => onChange({ name: e.target.value.replace(/-+$/, '') })}
                data-wizard-field="name"
                placeholder="my-resource"
                aria-describedby={describedBy}
                aria-invalid={invalid}
                className={cn(inputClass, 'font-mono', errors?.name && 'border-status-error/50')}
              />
            )}
          </Field>
          <Field label="Image" required error={errors?.image}>
            {({ id, describedBy, invalid }) => (
              <input
                id={id}
                type="text"
                value={data.image}
                onChange={(e) => onChange({ image: e.target.value })}
                data-wizard-field="image"
                placeholder="postgres:16"
                aria-describedby={describedBy}
                aria-invalid={invalid}
                className={cn(inputClass, 'font-mono', errors?.image && 'border-status-error/50')}
              />
            )}
          </Field>
        </div>
      </Section>

      {/* Section 2: Environment & Variables */}
      <Section
        title="Environment & Variables"
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

      {/* Section 3: Ports */}
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
        <p className="text-3xs text-text-muted">Format: host:container (e.g., 5432:5432)</p>
      </Section>

      {/* Section 4: Volumes */}
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
        <p className="text-3xs text-text-muted">Format: host:container (e.g., ./data:/var/lib/data)</p>
      </Section>

      {/* Section 5: Network */}
      <Section
        title="Network"
        icon={Settings}
        expanded={expandedSections.has('network')}
        onToggle={() => toggleSection('network')}
        badge={data.network ? data.network : undefined}
      >
        <Field label="Network" help="Optional network name for this resource">
          {({ id, describedBy }) => (
            <input
              id={id}
              type="text"
              value={data.network ?? ''}
              onChange={(e) => onChange({ network: e.target.value || undefined })}
              placeholder="network-name"
              aria-describedby={describedBy}
              className={cn(inputClass, 'font-mono')}
            />
          )}
        </Field>
      </Section>
    </div>
  );
}
