import { FileCode2, Check } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { ResourceType } from '../../lib/yaml-builder';
import { templatesByType } from '../../lib/wizard-template-cards';

interface TemplateGridProps {
  resourceType: ResourceType;
  selected: string | null;
  onSelect: (templateId: string) => void;
}

export function TemplateGrid({ resourceType, selected, onSelect }: TemplateGridProps) {
  const templates = templatesByType[resourceType] || [];

  if (templates.length === 0) {
    return (
      <div className="flex items-center justify-center py-16 text-text-muted text-sm">
        No templates available for this type.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 mb-2">
        <FileCode2 size={14} className="text-primary" />
        <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
          Choose a template
        </span>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        {templates.map((template) => {
          const isSelected = selected === template.id;
          return (
            <button
              key={template.id}
              type="button"
              onClick={() => onSelect(template.id)}
              aria-pressed={isSelected}
              className={cn(
                'group relative text-left p-4 rounded-xl border transition-all duration-200',
                'bg-surface-overlay-2 hover:bg-surface-overlay-3',
                isSelected
                  ? 'border-primary/50 shadow-[0_0_20px_--alpha(var(--color-primary)/10%)]'
                  : 'border-border-subtle hover:border-border',
              )}
            >
              {isSelected && (
                <div className="absolute top-3 right-3">
                  <Check size={14} className="text-primary" />
                </div>
              )}
              <div className="text-sm font-medium text-text-primary mb-1">
                {template.name}
              </div>
              <div className="text-xs text-text-muted mb-3">
                {template.description}
              </div>
              <pre className="text-3xs font-mono text-text-muted leading-relaxed bg-background/40 rounded-lg px-2.5 py-2 border border-border-subtle overflow-hidden">
                {template.preview}
              </pre>
            </button>
          );
        })}
      </div>
    </div>
  );
}
