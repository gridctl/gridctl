import { useState } from 'react';
import {
  BookOpen,
  CheckCircle2,
  AlertTriangle,
  Shield,
  ShieldAlert,
  ChevronDown,
  ChevronRight,
  Square,
  CheckSquare,
} from 'lucide-react';
import { cn } from '../../../lib/cn';
import type { SkillPreview } from '../../../types';

interface BrowseStepProps {
  previews: SkillPreview[];
  selected: Set<string>;
  onSelectionChange: (selected: Set<string>) => void;
}

export function BrowseStep({ previews, selected, onSelectionChange }: BrowseStepProps) {
  const [activePreview, setActivePreview] = useState<string | null>(
    previews.length > 0 ? previews[0].name : null,
  );

  const toggleSkill = (name: string) => {
    const next = new Set(selected);
    if (next.has(name)) {
      next.delete(name);
    } else {
      next.add(name);
    }
    onSelectionChange(next);
  };

  const toggleAll = () => {
    const validSkills = previews.filter((p) => p.valid && !p.exists);
    const allSelected = validSkills.every((p) => selected.has(p.name));
    if (allSelected) {
      onSelectionChange(new Set());
    } else {
      onSelectionChange(new Set(validSkills.map((p) => p.name)));
    }
  };

  const activeSkill = previews.find((p) => p.name === activePreview);
  const validCount = previews.filter((p) => p.valid).length;

  return (
    <div className="space-y-3">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-text-primary">
            {previews.length} skill{previews.length !== 1 ? 's' : ''} found
          </h3>
          <p className="text-[10px] text-text-muted">
            {validCount} valid, {selected.size} selected
          </p>
        </div>

        <button
          onClick={toggleAll}
          className="flex items-center gap-1.5 text-[10px] text-primary hover:text-primary/80 transition-colors px-2 py-1 rounded-lg hover:bg-primary/5"
        >
          {previews.filter((p) => p.valid && !p.exists).every((p) => selected.has(p.name))
            ? <CheckSquare size={12} />
            : <Square size={12} />
          }
          Select All
        </button>
      </div>

      {/* Two-column layout */}
      <div className="grid grid-cols-5 gap-3 min-h-[280px]">
        {/* Left column — skill list */}
        <div className="col-span-2 space-y-1 overflow-y-auto scrollbar-dark pr-1">
          {previews.map((preview) => {
            const isSelected = selected.has(preview.name);
            const isActive = activePreview === preview.name;
            const hasFindings = (preview.findings?.length ?? 0) > 0;
            const isValid = preview.valid;
            const isExisting = preview.exists;

            return (
              <div
                key={preview.name}
                className={cn(
                  'flex items-center gap-2 rounded-lg px-2.5 py-2 cursor-pointer transition-all',
                  'border',
                  isActive
                    ? 'bg-white/[0.04] border-primary/30'
                    : 'bg-white/[0.01] border-transparent hover:bg-white/[0.03] hover:border-white/[0.06]',
                  !isValid && 'opacity-60',
                )}
                onClick={() => setActivePreview(preview.name)}
              >
                {/* Checkbox */}
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    if (isValid) toggleSkill(preview.name);
                  }}
                  disabled={!isValid}
                  className="flex-shrink-0"
                >
                  {isSelected ? (
                    <CheckSquare size={14} className="text-primary" />
                  ) : (
                    <Square size={14} className={isValid ? 'text-text-muted/40' : 'text-text-muted/20'} />
                  )}
                </button>

                {/* Info */}
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-1.5">
                    <span className="text-xs font-medium text-text-primary truncate">
                      {preview.name}
                    </span>
                  </div>
                  {preview.description && (
                    <p className="text-[10px] text-text-muted truncate mt-0.5">
                      {preview.description}
                    </p>
                  )}
                </div>

                {/* Status badges */}
                <div className="flex items-center gap-1 flex-shrink-0">
                  {!isValid && (
                    <span className="text-[8px] px-1 py-0.5 rounded bg-status-error/10 text-status-error">
                      invalid
                    </span>
                  )}
                  {isExisting && (
                    <span className="text-[8px] px-1 py-0.5 rounded bg-primary/10 text-primary">
                      exists
                    </span>
                  )}
                  {hasFindings && (
                    <ShieldAlert size={10} className="text-status-pending" />
                  )}
                </div>
              </div>
            );
          })}
        </div>

        {/* Right column — preview */}
        <div className="col-span-3 rounded-xl border border-border/20 bg-white/[0.01] overflow-hidden flex flex-col">
          {activeSkill ? (
            <>
              {/* Preview header */}
              <div className="px-4 py-3 border-b border-border/20 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <BookOpen size={14} className="text-primary" />
                  <span className="text-xs font-medium text-text-primary">{activeSkill.name}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  {activeSkill.valid ? (
                    <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-status-running/10 text-status-running flex items-center gap-1">
                      <CheckCircle2 size={8} />
                      valid
                    </span>
                  ) : (
                    <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-status-error/10 text-status-error flex items-center gap-1">
                      <AlertTriangle size={8} />
                      invalid
                    </span>
                  )}
                  {(activeSkill.findings?.length ?? 0) > 0 ? (
                    <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-status-pending/10 text-status-pending flex items-center gap-1">
                      <ShieldAlert size={8} />
                      flagged
                    </span>
                  ) : (
                    <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-status-running/10 text-status-running flex items-center gap-1">
                      <Shield size={8} />
                      safe
                    </span>
                  )}
                </div>
              </div>

              {/* Preview content */}
              <div className="flex-1 overflow-y-auto scrollbar-dark p-4 space-y-3">
                {/* Description */}
                {activeSkill.description && (
                  <p className="text-xs text-text-secondary leading-relaxed">
                    {activeSkill.description}
                  </p>
                )}

                {/* Body preview */}
                {activeSkill.body && (
                  <pre className="text-[10px] text-text-muted font-mono bg-background/60 p-3 rounded-lg overflow-x-auto whitespace-pre-wrap leading-relaxed max-h-48 scrollbar-dark">
                    {activeSkill.body.slice(0, 2000)}
                    {activeSkill.body.length > 2000 && '\n...'}
                  </pre>
                )}

                {/* Validation errors */}
                {(activeSkill.errors?.length ?? 0) > 0 && (
                  <DetailsSection title="Validation Errors" variant="error" defaultOpen>
                    {activeSkill.errors?.map((e, i) => (
                      <div key={i} className="text-[10px] text-status-error py-0.5">
                        {e}
                      </div>
                    ))}
                  </DetailsSection>
                )}

                {/* Warnings */}
                {(activeSkill.warnings?.length ?? 0) > 0 && (
                  <DetailsSection title="Warnings" variant="warning">
                    {activeSkill.warnings?.map((w, i) => (
                      <div key={i} className="text-[10px] text-status-pending py-0.5">
                        {w}
                      </div>
                    ))}
                  </DetailsSection>
                )}

                {/* Security findings */}
                {(activeSkill.findings?.length ?? 0) > 0 && (
                  <DetailsSection title="Security Findings" variant="warning" defaultOpen>
                    {activeSkill.findings?.map((f, i) => (
                      <div
                        key={i}
                        className={cn(
                          'text-[10px] py-1 flex items-start gap-1.5',
                          f.severity === 'danger' ? 'text-status-error' : 'text-status-pending',
                        )}
                      >
                        <ShieldAlert size={10} className="flex-shrink-0 mt-0.5" />
                        <div>
                          <span className="font-medium">{f.description}</span>
                          <span className="text-text-muted ml-1">({f.stepId})</span>
                        </div>
                      </div>
                    ))}
                  </DetailsSection>
                )}
              </div>
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center text-text-muted">
              <p className="text-xs">Select a skill to preview</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// Collapsible details section
function DetailsSection({
  title,
  variant,
  defaultOpen = false,
  children,
}: {
  title: string;
  variant: 'error' | 'warning';
  defaultOpen?: boolean;
  children: React.ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div
      className={cn(
        'rounded-lg border overflow-hidden',
        variant === 'error'
          ? 'border-status-error/20 bg-status-error/5'
          : 'border-status-pending/20 bg-status-pending/5',
      )}
    >
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-medium uppercase tracking-wider"
      >
        {open ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
        <span className={variant === 'error' ? 'text-status-error' : 'text-status-pending'}>
          {title}
        </span>
      </button>
      {open && <div className="px-3 pb-2">{children}</div>}
    </div>
  );
}
