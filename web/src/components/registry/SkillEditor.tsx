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
} from 'lucide-react';
import { marked } from 'marked';
import { Modal } from '../ui/Modal';
import { showToast } from '../ui/Toast';
import { SkillFileTree } from './SkillFileTree';
import { createRegistrySkill, updateRegistrySkill, validateSkillContent } from '../../lib/api';
import { cn } from '../../lib/cn';
import type { AgentSkill, ItemState, SkillValidationResult } from '../../types';

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
      <div className="flex items-center justify-between mb-1">
        <label className="text-[10px] text-text-muted uppercase tracking-wider">Metadata</label>
        <button
          onClick={onAdd}
          className="text-[10px] text-primary hover:text-primary/80 flex items-center gap-0.5 transition-colors"
        >
          <Plus size={10} /> Add
        </button>
      </div>
      {(entries ?? []).length === 0 && (
        <p className="text-[10px] text-text-muted/50 italic">No metadata entries</p>
      )}
      <div className="space-y-1.5">
        {(entries ?? []).map((entry) => (
          <div key={entry.id} className="flex items-center gap-1.5">
            <input
              value={entry.key}
              onChange={(e) => onUpdate(entry.id, 'key', e.target.value)}
              placeholder="key"
              className="flex-1 bg-background/60 border border-border/40 rounded px-2 py-1 text-[10px] font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
            />
            <span className="text-text-muted text-[10px]">=</span>
            <input
              value={entry.value}
              onChange={(e) => onUpdate(entry.id, 'value', e.target.value)}
              placeholder="value"
              className="flex-1 bg-background/60 border border-border/40 rounded px-2 py-1 text-[10px] font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
            />
            <button
              onClick={() => onRemove(entry.id)}
              className="p-0.5 text-text-muted hover:text-status-error transition-colors"
            >
              <X size={10} />
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
        <p className="text-text-muted/40 text-xs italic">
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
        'prose-h1:text-base prose-h1:border-b prose-h1:border-border/30 prose-h1:pb-2 prose-h1:mb-4',
        'prose-h2:text-sm prose-h2:mt-6 prose-h2:mb-2',
        'prose-h3:text-xs prose-h3:mt-4 prose-h3:mb-1',
        // Body text
        'prose-p:text-text-secondary prose-p:text-xs prose-p:leading-relaxed',
        // Code
        'prose-code:text-primary prose-code:bg-surface-highlight prose-code:px-1.5 prose-code:py-0.5 prose-code:rounded prose-code:text-[11px] prose-code:font-mono prose-code:before:content-none prose-code:after:content-none',
        'prose-pre:bg-background/60 prose-pre:border prose-pre:border-border/30 prose-pre:rounded-lg prose-pre:text-[11px]',
        // Links
        'prose-a:text-primary prose-a:no-underline hover:prose-a:underline',
        // Lists
        'prose-li:text-text-secondary prose-li:text-xs prose-li:marker:text-text-muted',
        'prose-ul:my-2 prose-ol:my-2',
        // Strong / emphasis
        'prose-strong:text-text-primary',
        'prose-em:text-text-secondary',
        // Blockquotes
        'prose-blockquote:border-primary/40 prose-blockquote:text-text-muted prose-blockquote:not-italic',
        // Tables
        'prose-th:text-text-primary prose-th:text-[10px] prose-th:uppercase prose-th:tracking-wider prose-th:font-medium',
        'prose-td:text-text-secondary prose-td:text-xs',
        // Horizontal rules
        'prose-hr:border-border/30',
      )}
      dangerouslySetInnerHTML={{ __html: html }}
    />
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
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validation, setValidation] = useState<SkillValidationResult | null>(null);

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

  const handleSave = async () => {
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
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={isNew ? 'New Skill' : `Edit: ${skill!.name}`}
      expandable
      size={size}
      flush={flush}
      onPopout={onPopout}
      popoutDisabled={popoutDisabled}
    >
      <div className="flex flex-col h-[70vh] -mx-6 -my-4">
        {/* Header bar with state toggle and save */}
        <div className="flex items-center justify-between px-4 py-2.5 border-b border-border/50 bg-surface-elevated/30 flex-shrink-0">
          <div className="flex items-center gap-2.5 min-w-0">
            <Library size={14} className="text-primary flex-shrink-0" />
            <span className="font-semibold text-text-primary truncate text-sm tracking-tight">
              {isNew ? 'New Skill' : skill!.name}
            </span>
          </div>
          <div className="flex items-center gap-2">
            {/* State selector */}
            <select
              value={state}
              onChange={(e) => setState(e.target.value as ItemState)}
              className="bg-background/60 border border-border/40 rounded px-2 py-1 text-[10px] text-text-primary focus:outline-none focus:border-primary/50 transition-colors cursor-pointer"
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
                'px-3 py-1.5 text-xs font-medium rounded-lg transition-all',
                'bg-primary text-background hover:bg-primary/90',
                (saving || !name || !description) && 'opacity-50 cursor-not-allowed',
              )}
            >
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>

        {/* Error display */}
        {error && (
          <div className="px-4 py-2 bg-status-error/10 border-b border-status-error/30 flex-shrink-0">
            <p className="text-xs text-status-error">{error}</p>
          </div>
        )}

        {/* Frontmatter helpers (collapsible) */}
        <div className="border-b border-border/30 flex-shrink-0">
          <button
            onClick={() => setShowFrontmatter(!showFrontmatter)}
            className="w-full flex items-center justify-between px-4 py-2 text-xs text-text-muted hover:text-text-secondary transition-colors"
          >
            <span className="flex items-center gap-1.5">
              <Settings size={12} />
              Frontmatter
            </span>
            {showFrontmatter ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          </button>

          {showFrontmatter && (
            <div className="px-4 pb-3 space-y-3">
              {/* Name */}
              <div>
                <label className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">Name</label>
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  disabled={!isNew}
                  placeholder="my-skill-name"
                  className={cn(
                    'w-full bg-background/60 border border-border/40 rounded px-2.5 py-1.5 text-xs font-mono text-text-primary',
                    'placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors',
                    !isNew && 'opacity-50 cursor-not-allowed',
                  )}
                />
                {!isNew && (
                  <p className="text-[9px] text-text-muted mt-0.5">Name cannot be changed after creation</p>
                )}
              </div>

              {/* Description */}
              <div>
                <label className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">
                  Description
                  <span className="ml-1 text-text-muted/60">{description.length}/1024</span>
                </label>
                <textarea
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="Brief description of what this skill does"
                  rows={2}
                  maxLength={1024}
                  className="w-full bg-background/60 border border-border/40 rounded px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 resize-none transition-colors"
                />
              </div>

              {/* License + Compatibility (side by side) */}
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">License</label>
                  <input
                    value={license}
                    onChange={(e) => setLicense(e.target.value)}
                    placeholder="Apache-2.0"
                    className="w-full bg-background/60 border border-border/40 rounded px-2.5 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
                  />
                </div>
                <div>
                  <label className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">Compatibility</label>
                  <input
                    value={compatibility}
                    onChange={(e) => setCompatibility(e.target.value)}
                    placeholder="Requires git"
                    className="w-full bg-background/60 border border-border/40 rounded px-2.5 py-1.5 text-xs text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
                  />
                </div>
              </div>

              {/* Allowed Tools */}
              <div>
                <label className="text-[10px] text-text-muted uppercase tracking-wider block mb-1">Allowed Tools</label>
                <input
                  value={allowedTools}
                  onChange={(e) => setAllowedTools(e.target.value)}
                  placeholder="Bash(git:*) Read Write"
                  className="w-full bg-background/60 border border-border/40 rounded px-2.5 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
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

        {/* Split pane: editor + preview */}
        <div className="flex-1 flex min-h-0">
          {/* Editor pane */}
          <div className="flex-1 flex flex-col min-w-0 border-r border-border/30">
            <div className="px-4 py-1.5 border-b border-border/20 flex-shrink-0">
              <span className="text-[10px] text-text-muted uppercase tracking-wider">Markdown</span>
            </div>
            <textarea
              value={body}
              onChange={(e) => setBody(e.target.value)}
              placeholder={'# Skill Instructions\n\nWrite markdown instructions that the agent will follow...\n\n## Steps\n\n1. First step\n2. Second step'}
              className="flex-1 w-full bg-background/40 px-4 py-3 text-xs font-mono text-text-primary placeholder:text-text-muted/30 resize-none focus:outline-none leading-relaxed"
              spellCheck={false}
            />
          </div>

          {/* Preview pane */}
          <div className="flex-1 flex flex-col min-w-0">
            <div className="px-4 py-1.5 border-b border-border/20 flex-shrink-0">
              <span className="text-[10px] text-text-muted uppercase tracking-wider">Preview</span>
            </div>
            <div className="flex-1 overflow-y-auto scrollbar-dark">
              <div className="px-4 py-3">
                <MarkdownPreview content={body} />
              </div>
            </div>
          </div>
        </div>

        {/* Bottom status bar */}
        <div className="flex items-center justify-between px-4 py-1.5 border-t border-border/30 bg-surface/50 flex-shrink-0">
          <div className="flex items-center gap-3">
            {/* Validation indicator */}
            {validation && (
              <span
                className={cn(
                  'text-[10px] flex items-center gap-1',
                  validation.valid ? 'text-status-running' : 'text-status-error',
                )}
              >
                {validation.valid ? <Check size={10} /> : <AlertCircle size={10} />}
                {validation.valid ? 'Valid' : `${(validation.errors ?? []).length} error(s)`}
              </span>
            )}
            {/* Warnings */}
            {(validation?.warnings ?? []).length > 0 && (
              <span className="text-[10px] text-status-pending flex items-center gap-1">
                <AlertTriangle size={10} />
                {(validation?.warnings ?? []).length} warning(s)
              </span>
            )}
          </div>
          <div className="flex items-center gap-3 text-[10px] text-text-muted font-mono">
            <span>{lineCount} lines</span>
            <span>{charCount} chars</span>
          </div>
        </div>
      </div>
    </Modal>
  );
}
