import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import {
  Library,
  Settings,
  ChevronDown,
  ChevronRight,
  Plus,
  X,
  Check,
  AlertCircle,
  AlertTriangle,
  GripVertical,
  Eye,
  EyeOff,
  Code2,
  GitBranch,
  Play,
} from 'lucide-react';
import { marked } from 'marked';
import { Modal } from '../ui/Modal';
import { showToast } from '../ui/Toast';
import { SkillFileTree } from './SkillFileTree';
import { WorkflowPanel } from '../workflow/WorkflowPanel';
import { VisualDesigner } from '../workflow/VisualDesigner';
import { createRegistrySkill, updateRegistrySkill, validateSkillContent } from '../../lib/api';
import { cn } from '../../lib/cn';
import type { AgentSkill, ItemState, SkillValidationResult, WorkflowStep, SkillInput, WorkflowOutput } from '../../types';

// --- Types ---

interface MetadataEntry {
  id: number;
  key: string;
  value: string;
}

// --- Frontmatter builder ---

function buildSkillMDContent(fields: {
  name: string;
  description: string;
  license?: string;
  compatibility?: string;
  allowedTools?: string;
  metadata?: Record<string, string>;
  state: string;
  body: string;
}): string {
  const lines: string[] = [];
  lines.push(`name: ${fields.name}`);
  lines.push(`description: ${fields.description}`);
  if (fields.license) lines.push(`license: ${fields.license}`);
  if (fields.compatibility) lines.push(`compatibility: ${fields.compatibility}`);
  if (fields.allowedTools) lines.push(`allowed-tools: ${fields.allowedTools}`);
  if (fields.metadata && Object.keys(fields.metadata).length > 0) {
    lines.push('metadata:');
    for (const [k, v] of Object.entries(fields.metadata)) {
      if (k) lines.push(`  ${k}: "${v}"`);
    }
  }
  if (fields.state) lines.push(`state: ${fields.state}`);
  return `---\n${lines.join('\n')}\n---\n\n${fields.body}`;
}

// --- Markdown rendering ---

function renderMarkdown(content: string): string {
  return marked.parse(content, { breaks: true, gfm: true }) as string;
}

// --- Debounced validation ---

interface ValidationFields {
  name: string;
  description: string;
  license: string;
  compatibility: string;
  allowedTools: string;
  metadata: MetadataEntry[];
  state: string;
  body: string;
}

function createDebouncedValidator(
  ms: number,
  onResult: (result: SkillValidationResult) => void,
): { trigger: (fields: ValidationFields) => void; cancel: () => void } {
  let timer: ReturnType<typeof setTimeout>;
  return {
    trigger: (fields: ValidationFields) => {
      clearTimeout(timer);
      timer = setTimeout(async () => {
        const metaRecord: Record<string, string> = {};
        for (const entry of fields.metadata ?? []) {
          if (entry.key) metaRecord[entry.key] = entry.value;
        }
        const content = buildSkillMDContent({
          name: fields.name,
          description: fields.description,
          license: fields.license,
          compatibility: fields.compatibility,
          allowedTools: fields.allowedTools,
          metadata: metaRecord,
          state: fields.state,
          body: fields.body,
        });
        try {
          const result = await validateSkillContent(content);
          onResult(result);
        } catch {
          // Validation API error - don't block editing
        }
      }, ms);
    },
    cancel: () => clearTimeout(timer),
  };
}

// --- Resizable split pane hook ---

function useSplitPane(defaultRatio = 0.5, minRatio = 0.25, maxRatio = 0.75) {
  const [ratio, setRatio] = useState(defaultRatio);
  const [isDragging, setIsDragging] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      setIsDragging(true);
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';

      const handleMouseMove = (moveEvent: MouseEvent) => {
        if (!containerRef.current) return;
        const rect = containerRef.current.getBoundingClientRect();
        const x = moveEvent.clientX - rect.left;
        const newRatio = Math.min(maxRatio, Math.max(minRatio, x / rect.width));
        setRatio(newRatio);
      };

      const handleMouseUp = () => {
        setIsDragging(false);
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
      };

      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
    },
    [minRatio, maxRatio],
  );

  return { ratio, containerRef, handleMouseDown, isDragging };
}

// --- MetadataEditor ---

function MetadataEditor({
  entries,
  onAdd,
  onRemove,
  onUpdate,
}: {
  entries: MetadataEntry[];
  onAdd: () => void;
  onRemove: (id: number) => void;
  onUpdate: (id: number, field: 'key' | 'value', val: string) => void;
}) {
  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <label className="text-xs text-text-muted uppercase tracking-wider">Metadata</label>
        <button
          onClick={onAdd}
          className="text-xs text-primary hover:text-primary/80 flex items-center gap-0.5 transition-colors"
        >
          <Plus size={12} /> Add
        </button>
      </div>
      {(entries ?? []).length === 0 && (
        <p className="text-xs text-text-muted/50 italic">No metadata entries</p>
      )}
      <div className="space-y-2">
        {(entries ?? []).map((entry) => (
          <div key={entry.id} className="flex items-center gap-2">
            <input
              value={entry.key}
              onChange={(e) => onUpdate(entry.id, 'key', e.target.value)}
              placeholder="key"
              className="w-36 bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
            />
            <span className="text-text-muted text-xs">=</span>
            <input
              value={entry.value}
              onChange={(e) => onUpdate(entry.id, 'value', e.target.value)}
              placeholder="value"
              className="flex-1 bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
            />
            <button
              onClick={() => onRemove(entry.id)}
              className="p-1 text-text-muted hover:text-status-error transition-colors"
            >
              <X size={12} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

// --- MarkdownPreview ---

function MarkdownPreview({ content }: { content: string }) {
  const html = useMemo(() => (content ? renderMarkdown(content) : ''), [content]);

  if (!content) {
    return (
      <div className="flex items-center justify-center h-full min-h-[200px]">
        <p className="text-text-muted/40 text-sm italic">
          Preview will appear here as you type...
        </p>
      </div>
    );
  }

  return (
    <div
      className={cn(
        'prose prose-invert prose-sm max-w-none',
        // Headings
        'prose-headings:text-text-primary prose-headings:font-semibold prose-headings:tracking-tight',
        'prose-h1:text-lg prose-h1:border-b prose-h1:border-border/30 prose-h1:pb-2 prose-h1:mb-4',
        'prose-h2:text-base prose-h2:mt-6 prose-h2:mb-2',
        'prose-h3:text-sm prose-h3:mt-4 prose-h3:mb-1',
        // Body text
        'prose-p:text-text-secondary prose-p:text-sm prose-p:leading-relaxed',
        // Code
        'prose-code:text-primary prose-code:bg-surface-highlight prose-code:px-1.5 prose-code:py-0.5 prose-code:rounded prose-code:text-xs prose-code:font-mono prose-code:before:content-none prose-code:after:content-none',
        'prose-pre:bg-background/60 prose-pre:border prose-pre:border-border/30 prose-pre:rounded-lg prose-pre:text-xs',
        // Links
        'prose-a:text-primary prose-a:no-underline hover:prose-a:underline',
        // Lists
        'prose-li:text-text-secondary prose-li:text-sm prose-li:marker:text-text-muted',
        'prose-ul:my-2 prose-ol:my-2',
        // Strong / emphasis
        'prose-strong:text-text-primary',
        'prose-em:text-text-secondary',
        // Blockquotes
        'prose-blockquote:border-primary/40 prose-blockquote:text-text-muted prose-blockquote:not-italic',
        // Tables
        'prose-th:text-text-primary prose-th:text-xs prose-th:uppercase prose-th:tracking-wider prose-th:font-medium',
        'prose-td:text-text-secondary prose-td:text-sm',
        // Horizontal rules
        'prose-hr:border-border/30',
      )}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

// --- SplitPaneHandle ---

function SplitPaneHandle({
  onMouseDown,
  isDragging,
}: {
  onMouseDown: (e: React.MouseEvent) => void;
  isDragging: boolean;
}) {
  return (
    <div
      onMouseDown={onMouseDown}
      className={cn(
        'relative flex items-center justify-center cursor-col-resize select-none',
        'w-2 hover:bg-primary/5 transition-colors duration-150',
        isDragging && 'bg-primary/10',
      )}
    >
      {/* Hit area */}
      <div className="absolute inset-y-0 -inset-x-2" />
      {/* Visible grip */}
      <div
        className={cn(
          'flex flex-col gap-0.5 transition-opacity duration-150',
          'opacity-0 group-hover/split:opacity-100',
          isDragging && 'opacity-100',
        )}
      >
        <GripVertical
          size={12}
          className={cn(
            'text-text-muted/40 transition-colors',
            isDragging ? 'text-primary' : 'hover:text-primary/60',
          )}
        />
      </div>
      {/* Visible line */}
      <div
        className={cn(
          'absolute top-1/2 -translate-y-1/2 w-px h-12 rounded-full transition-all duration-150',
          'bg-border/30',
          isDragging && 'bg-primary h-20 shadow-[0_0_8px_rgba(245,158,11,0.4)]',
        )}
      />
    </div>
  );
}

// --- SkillEditor (main component) ---

interface SkillEditorProps {
  isOpen: boolean;
  onClose: () => void;
  onSaved: () => void;
  skill?: AgentSkill;
  onPopout?: () => void;
  popoutDisabled?: boolean;
  size?: 'default' | 'wide' | 'full';
  flush?: boolean;
}

export function SkillEditor({
  isOpen,
  onClose,
  onSaved,
  skill,
  onPopout,
  popoutDisabled,
  size,
  flush,
}: SkillEditorProps) {
  const isNew = !skill;
  const idCounter = useRef(0);

  // Editor state
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [license, setLicense] = useState('');
  const [compatibility, setCompatibility] = useState('');
  const [metadata, setMetadata] = useState<MetadataEntry[]>([]);
  const [allowedTools, setAllowedTools] = useState('');
  const [state, setState] = useState<ItemState>('draft');
  const [body, setBody] = useState('');

  // UI state
  const [showFrontmatter, setShowFrontmatter] = useState(true);
  const [showPreview, setShowPreview] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validation, setValidation] = useState<SkillValidationResult | null>(null);
  const [editorMode, setEditorMode] = useState<'code' | 'visual' | 'test'>('code');

  // Visual designer workflow state
  const [designerSteps, setDesignerSteps] = useState<WorkflowStep[]>([]);
  const [designerInputs, setDesignerInputs] = useState<Record<string, SkillInput>>({});
  const [designerOutput, setDesignerOutput] = useState<WorkflowOutput | undefined>();

  // Detect if skill has a workflow block
  const hasWorkflow = useMemo(() => {
    if (!skill) return false;
    const fullContent = buildSkillMDContent({
      name: skill.name,
      description: skill.description,
      license: skill.license,
      compatibility: skill.compatibility,
      allowedTools: skill.allowedTools,
      metadata: skill.metadata,
      state: skill.state,
      body: skill.body ?? '',
    });
    return /^workflow:/m.test(fullContent) || /\nworkflow:/m.test(skill.body ?? '');
  }, [skill]);

  // Resizable split pane
  const { ratio, containerRef, handleMouseDown: handleSplitMouseDown, isDragging: splitDragging } = useSplitPane(0.5, 0.25, 0.75);

  // Computed
  const lineCount = useMemo(() => body.split('\n').length, [body]);
  const charCount = body.length;

  // --- Initialize from skill prop ---

  useEffect(() => {
    idCounter.current = 0;
    if (skill) {
      setName(skill.name);
      setDescription(skill.description);
      setLicense(skill.license ?? '');
      setCompatibility(skill.compatibility ?? '');
      setMetadata(
        Object.entries(skill.metadata ?? {}).map(([key, value]) => ({
          id: ++idCounter.current,
          key,
          value,
        })),
      );
      setAllowedTools(skill.allowedTools ?? '');
      setState(skill.state);
      setBody(skill.body ?? '');
    } else {
      setName('');
      setDescription('');
      setLicense('');
      setCompatibility('');
      setMetadata([]);
      setAllowedTools('');
      setState('draft');
      setBody('');
    }
    setError(null);
    setValidation(null);
  }, [skill, isOpen]);

  // --- Metadata management ---

  const addMetadata = useCallback(() => {
    setMetadata((prev) => [...prev, { id: ++idCounter.current, key: '', value: '' }]);
  }, []);

  const removeMetadata = useCallback((entryId: number) => {
    setMetadata((prev) => prev.filter((m) => m.id !== entryId));
  }, []);

  const updateMetadata = useCallback((entryId: number, field: 'key' | 'value', val: string) => {
    setMetadata((prev) => prev.map((m) => (m.id === entryId ? { ...m, [field]: val } : m)));
  }, []);

  // --- Debounced validation ---

  const validator = useMemo(
    () => createDebouncedValidator(500, (result) => setValidation(result)),
    [],
  );

  useEffect(() => {
    if (name || description || body) {
      validator.trigger({ name, description, license, compatibility, allowedTools, metadata, state, body });
    }
    return () => validator.cancel();
  }, [name, description, body, license, compatibility, allowedTools, metadata, state, validator]);

  // --- Save handler ---

  const handleSave = useCallback(async () => {
    if (saving || !name || !description) return;
    setSaving(true);
    setError(null);
    try {
      const metadataRecord: Record<string, string> = {};
      for (const entry of metadata) {
        if (entry.key) metadataRecord[entry.key] = entry.value;
      }

      const skillData: AgentSkill = {
        name,
        description,
        body,
        state,
        fileCount: skill?.fileCount ?? 0,
        ...(license && { license }),
        ...(compatibility && { compatibility }),
        ...(Object.keys(metadataRecord).length > 0 && { metadata: metadataRecord }),
        ...(allowedTools && { allowedTools }),
      };

      if (isNew) {
        await createRegistrySkill(skillData);
        showToast('success', `Skill "${name}" created`);
      } else {
        await updateRegistrySkill(skill!.name, skillData);
        showToast('success', `Skill "${name}" updated`);
      }
      onSaved();
      onClose();
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Save failed';
      setError(msg);
      showToast('error', msg);
    } finally {
      setSaving(false);
    }
  }, [saving, name, description, metadata, body, state, skill, license, compatibility, allowedTools, isNew, onSaved, onClose]);

  // --- Keyboard shortcut: Cmd/Ctrl+S to save ---

  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        handleSave();
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [isOpen, handleSave]);

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={isNew ? 'New Skill' : `Edit: ${skill!.name}`}
      expandable
      size={size ?? 'full'}
      flush={flush}
      onPopout={onPopout}
      popoutDisabled={popoutDisabled}
    >
      <div className="flex flex-col h-[calc(85vh-3.5rem)] -mx-6 -my-4">
        {/* Header bar with state toggle, preview toggle, and save */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-border/50 bg-surface-elevated/30 flex-shrink-0">
          <div className="flex items-center gap-3 min-w-0">
            <Library size={16} className="text-primary flex-shrink-0" />
            <span className="font-semibold text-text-primary truncate text-sm tracking-tight">
              {isNew ? 'New Skill' : skill!.name}
            </span>
          </div>
          <div className="flex items-center gap-3">
            {/* Preview toggle */}
            <button
              onClick={() => setShowPreview(!showPreview)}
              title={showPreview ? 'Hide preview' : 'Show preview'}
              className={cn(
                'p-1.5 rounded-lg transition-all duration-200 group',
                showPreview
                  ? 'text-text-muted hover:text-primary hover:bg-primary/10'
                  : 'text-primary bg-primary/10',
              )}
            >
              {showPreview ? <Eye size={14} /> : <EyeOff size={14} />}
            </button>

            {/* State selector */}
            <select
              value={state}
              onChange={(e) => setState(e.target.value as ItemState)}
              className="bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs text-text-primary focus:outline-none focus:border-primary/50 transition-colors cursor-pointer"
            >
              <option value="draft">Draft</option>
              <option value="active">Active</option>
              <option value="disabled">Disabled</option>
            </select>

            {/* Save button */}
            <button
              onClick={handleSave}
              disabled={saving || !name || !description}
              className={cn(
                'px-4 py-2 text-xs font-medium rounded-lg transition-all',
                'bg-primary text-background hover:bg-primary/90',
                (saving || !name || !description) && 'opacity-50 cursor-not-allowed',
              )}
            >
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>

        {/* Mode toggle (shown when skill has a workflow) */}
        {hasWorkflow && !isNew && (
          <div className="flex items-center justify-center px-5 py-2 border-b border-border/30 bg-surface/30 flex-shrink-0">
            <div className="flex items-center rounded-lg border border-border/50 bg-surface-elevated/60 overflow-hidden">
              <button
                onClick={() => setEditorMode('code')}
                className={cn(
                  'px-3 py-1.5 text-xs font-medium transition-all duration-200 flex items-center gap-1.5',
                  editorMode === 'code'
                    ? 'bg-primary/15 text-primary'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight',
                )}
              >
                <Code2 size={12} />
                Code
              </button>
              <button
                onClick={() => setEditorMode('visual')}
                className={cn(
                  'px-3 py-1.5 text-xs font-medium transition-all duration-200 border-x border-border/30 flex items-center gap-1.5',
                  editorMode === 'visual'
                    ? 'bg-primary/15 text-primary'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight',
                )}
              >
                <GitBranch size={12} />
                Visual
              </button>
              <button
                onClick={() => setEditorMode('test')}
                className={cn(
                  'px-3 py-1.5 text-xs font-medium transition-all duration-200 flex items-center gap-1.5',
                  editorMode === 'test'
                    ? 'bg-primary/15 text-primary'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight',
                )}
              >
                <Play size={12} />
                Test
              </button>
            </div>
          </div>
        )}

        {/* Error display */}
        {error && (
          <div className="px-5 py-2.5 bg-status-error/10 border-b border-status-error/30 flex-shrink-0">
            <p className="text-sm text-status-error">{error}</p>
          </div>
        )}

        {/* Visual mode */}
        {editorMode === 'visual' && hasWorkflow && !isNew && (
          <VisualDesigner
            steps={designerSteps}
            inputs={designerInputs}
            output={designerOutput}
            onStepsChange={setDesignerSteps}
            onInputsChange={setDesignerInputs}
            onOutputChange={setDesignerOutput}
          />
        )}

        {/* Test mode */}
        {editorMode === 'test' && hasWorkflow && !isNew && skill && (
          <WorkflowPanel skillName={skill.name} />
        )}

        {/* Code mode */}
        {editorMode === 'code' && (
          <>
        {/* Frontmatter helpers (collapsible, scrollable) */}
        <div className="border-b border-border/30 flex-shrink-0">
          <button
            onClick={() => setShowFrontmatter(!showFrontmatter)}
            className="w-full flex items-center justify-between px-5 py-2.5 text-xs text-text-muted hover:text-text-secondary transition-colors"
          >
            <span className="flex items-center gap-2">
              <Settings size={14} />
              <span className="uppercase tracking-wider">Frontmatter</span>
            </span>
            {showFrontmatter ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          </button>

          {showFrontmatter && (
            <div className="max-h-[35vh] overflow-y-auto scrollbar-dark">
              <div className="px-5 pb-4 space-y-4">
                {/* Name */}
                <div>
                  <label className="text-xs text-text-muted uppercase tracking-wider block mb-1.5">Name</label>
                  <input
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    disabled={!isNew}
                    placeholder="my-skill-name"
                    className={cn(
                      'w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-sm font-mono text-text-primary',
                      'placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors',
                      !isNew && 'opacity-50 cursor-not-allowed',
                    )}
                  />
                  {!isNew && (
                    <p className="text-[10px] text-text-muted mt-1">Name cannot be changed after creation</p>
                  )}
                </div>

                {/* Description */}
                <div>
                  <label className="text-xs text-text-muted uppercase tracking-wider block mb-1.5">
                    Description
                    <span className="ml-2 text-text-muted/60 normal-case">{description.length}/1024</span>
                  </label>
                  <textarea
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    placeholder="Brief description of what this skill does"
                    rows={4}
                    maxLength={1024}
                    className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2.5 text-sm text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 resize-y transition-colors leading-relaxed"
                  />
                </div>

                {/* License + Compatibility (side by side) */}
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="text-xs text-text-muted uppercase tracking-wider block mb-1.5">License</label>
                    <input
                      value={license}
                      onChange={(e) => setLicense(e.target.value)}
                      placeholder="Apache-2.0"
                      className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-sm font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
                    />
                  </div>
                  <div>
                    <label className="text-xs text-text-muted uppercase tracking-wider block mb-1.5">Compatibility</label>
                    <input
                      value={compatibility}
                      onChange={(e) => setCompatibility(e.target.value)}
                      placeholder="Requires git"
                      className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
                    />
                  </div>
                </div>

                {/* Allowed Tools */}
                <div>
                  <label className="text-xs text-text-muted uppercase tracking-wider block mb-1.5">Allowed Tools</label>
                  <input
                    value={allowedTools}
                    onChange={(e) => setAllowedTools(e.target.value)}
                    placeholder="Bash(git:*) Read Write"
                    className="w-full bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-sm font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
                  />
                </div>

                {/* Metadata */}
                <MetadataEditor
                  entries={metadata}
                  onAdd={addMetadata}
                  onRemove={removeMetadata}
                  onUpdate={updateMetadata}
                />
              </div>
            </div>
          )}
        </div>

        {/* File tree (only for existing skills) */}
        {!isNew && skill && (
          <SkillFileTree
            skillName={skill.name}
            onSelectFile={(path) => {
              showToast('success', `Selected: ${path}`);
            }}
          />
        )}

        {/* Editor area: split pane when preview on, full-width when off */}
        <div
          ref={containerRef}
          className="flex-1 flex min-h-0 group/split"
        >
          {/* Editor pane */}
          <div
            className={cn(
              'flex flex-col min-w-0 min-h-0',
              showPreview && 'border-r border-border/30',
            )}
            style={showPreview ? { width: `${ratio * 100}%` } : { width: '100%' }}
          >
            <div className="px-4 py-2 border-b border-border/20 flex-shrink-0">
              <span className="text-xs text-text-muted uppercase tracking-wider">Markdown</span>
            </div>
            <textarea
              value={body}
              onChange={(e) => setBody(e.target.value)}
              placeholder={'# Skill Instructions\n\nWrite markdown instructions that the agent will follow...\n\n## Steps\n\n1. First step\n2. Second step'}
              className="flex-1 w-full bg-background/40 px-5 py-4 text-sm font-mono text-text-primary placeholder:text-text-muted/30 resize-none focus:outline-none leading-relaxed"
              spellCheck={false}
            />
          </div>

          {showPreview && (
            <>
              {/* Resize handle */}
              <SplitPaneHandle
                onMouseDown={handleSplitMouseDown}
                isDragging={splitDragging}
              />

              {/* Preview pane */}
              <div
                className="flex flex-col min-w-0 min-h-0"
                style={{ width: `${(1 - ratio) * 100}%` }}
              >
                <div className="px-4 py-2 border-b border-border/20 flex-shrink-0">
                  <span className="text-xs text-text-muted uppercase tracking-wider">Preview</span>
                </div>
                <div className="flex-1 overflow-y-auto scrollbar-dark">
                  <div className="px-5 py-4">
                    <MarkdownPreview content={body} />
                  </div>
                </div>
              </div>
            </>
          )}
        </div>

        {/* Bottom status bar */}
        <div className="flex items-center justify-between px-5 py-2 border-t border-border/30 bg-surface/50 flex-shrink-0">
          <div className="flex items-center gap-3">
            {/* Validation indicator */}
            {validation && (
              <span
                className={cn(
                  'text-xs flex items-center gap-1',
                  validation.valid ? 'text-status-running' : 'text-status-error',
                )}
              >
                {validation.valid ? <Check size={12} /> : <AlertCircle size={12} />}
                {validation.valid ? 'Valid' : `${(validation.errors ?? []).length} error(s)`}
              </span>
            )}
            {/* Warnings */}
            {(validation?.warnings ?? []).length > 0 && (
              <span className="text-xs text-status-pending flex items-center gap-1">
                <AlertTriangle size={12} />
                {(validation?.warnings ?? []).length} warning(s)
              </span>
            )}
          </div>
          <div className="flex items-center gap-4 text-xs text-text-muted font-mono">
            <span>{lineCount} lines</span>
            <span>{charCount} chars</span>
          </div>
        </div>
          </>
        )}
      </div>
    </Modal>
  );
}
