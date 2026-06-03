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
  Bold,
  List,
  Code2,
  Heading,
  Files,
  GitBranch,
  GitCompareArrows,
  RotateCcw,
  Unlink,
  GitFork,
} from 'lucide-react';
import { Modal } from '../ui/Modal';
import { showToast } from '../ui/Toast';
import { SkillFileTree } from './SkillFileTree';
import { MarkdownPreview } from './MarkdownPreview';
import { SkillCompareDialog } from './SkillCompareDialog';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import {
  createRegistrySkill,
  updateRegistrySkill,
  validateSkillContent,
  resetSkill,
  detachSkill,
} from '../../lib/api';
import { parseAcceptanceCriterion } from '../../lib/skillCriteria';
import { extractRepoInfo } from '../../lib/repo';
import { applyMarkdownAction, type MarkdownAction } from '../../lib/markdownEdit';
import { cn } from '../../lib/cn';
import { useUIStore } from '../../stores/useUIStore';
import type { AgentSkill, ItemState, SkillSourceStatus, SkillValidationResult } from '../../types';

// One-time hint shown the first time a user saves an edit to a remote skill,
// explaining that the edit becomes a local customization sync will preserve.
const LOCAL_EDIT_NOTE_KEY = 'gridctl-skill-local-edit-note-seen';

// --- Types ---

interface MetadataEntry {
  id: number;
  key: string;
  value: string;
}

interface CriterionEntry {
  id: number;
  given: string;
  when: string;
  then: string;
}

// --- Frontmatter builder ---

function buildSkillMDContent(fields: {
  name: string;
  description: string;
  license?: string;
  compatibility?: string;
  allowedTools?: string;
  metadata?: Record<string, string>;
  acceptanceCriteria?: string[];
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
  if (fields.acceptanceCriteria && fields.acceptanceCriteria.length > 0) {
    lines.push('acceptance_criteria:');
    for (const c of fields.acceptanceCriteria) {
      lines.push(`  - ${c}`);
    }
  }
  if (fields.state) lines.push(`state: ${fields.state}`);
  return `---\n${lines.join('\n')}\n---\n\n${fields.body}`;
}

// --- Debounced validation ---

interface ValidationFields {
  name: string;
  description: string;
  license: string;
  compatibility: string;
  allowedTools: string;
  metadata: MetadataEntry[];
  criteria: CriterionEntry[];
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
        const criteriaStrings = (fields.criteria ?? [])
          .filter((c) => c.given && c.when && c.then)
          .map((c) => `GIVEN ${c.given} WHEN ${c.when} THEN ${c.then}`);
        const content = buildSkillMDContent({
          name: fields.name,
          description: fields.description,
          license: fields.license,
          compatibility: fields.compatibility,
          allowedTools: fields.allowedTools,
          metadata: metaRecord,
          acceptanceCriteria: criteriaStrings.length > 0 ? criteriaStrings : undefined,
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

function useSplitPane(defaultRatio = 0.5, minRatio = 0.25, maxRatio = 0.75, onCommit?: (ratio: number) => void) {
  const [ratio, setRatio] = useState(defaultRatio);
  const [isDragging, setIsDragging] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  // Latest ratio during a drag, read on mouse-up so the committed value (which
  // we persist) is the final position rather than a stale render snapshot.
  const ratioRef = useRef(defaultRatio);

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
        ratioRef.current = newRatio;
        setRatio(newRatio);
      };

      const handleMouseUp = () => {
        setIsDragging(false);
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
        onCommit?.(ratioRef.current);
      };

      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
    },
    [minRatio, maxRatio, onCommit],
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
        <p className="text-xs text-text-muted italic">No metadata entries</p>
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

// --- AcceptanceCriteriaEditor ---

function AcceptanceCriteriaEditor({
  entries,
  onAdd,
  onRemove,
  onUpdate,
}: {
  entries: CriterionEntry[];
  onAdd: () => void;
  onRemove: (id: number) => void;
  onUpdate: (id: number, field: 'given' | 'when' | 'then', val: string) => void;
}) {
  return (
    <div>
      <div className="flex items-center justify-between mb-1.5">
        <label className="text-xs text-text-muted uppercase tracking-wider">Acceptance Criteria</label>
        <button
          onClick={onAdd}
          className="text-xs text-primary hover:text-primary/80 flex items-center gap-0.5 transition-colors"
        >
          <Plus size={12} /> Add
        </button>
      </div>
      {(entries ?? []).length === 0 && (
        <p className="text-xs text-text-muted italic">No acceptance criteria defined</p>
      )}
      <div className="space-y-3">
        {(entries ?? []).map((entry) => (
          <div key={entry.id} className="rounded-lg border border-border/30 bg-background/40 p-3 space-y-2">
            <div className="flex items-start gap-2">
              <span className="text-[10px] text-text-muted uppercase tracking-wider w-12 pt-2.5 flex-shrink-0 font-mono">
                GIVEN
              </span>
              <input
                value={entry.given}
                onChange={(e) => onUpdate(entry.id, 'given', e.target.value)}
                placeholder="a valid input is provided"
                className="flex-1 bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 transition-colors"
              />
              <button
                onClick={() => onRemove(entry.id)}
                className="p-1 text-text-muted hover:text-status-error transition-colors mt-1 flex-shrink-0"
              >
                <X size={12} />
              </button>
            </div>
            <div className="flex items-start gap-2">
              <span className="text-[10px] text-text-muted uppercase tracking-wider w-12 pt-2.5 flex-shrink-0 font-mono">
                WHEN
              </span>
              <input
                value={entry.when}
                onChange={(e) => onUpdate(entry.id, 'when', e.target.value)}
                placeholder="the skill is called"
                className="flex-1 bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 transition-colors"
              />
              <div className="w-6 flex-shrink-0" />
            </div>
            <div className="flex items-start gap-2">
              <span className="text-[10px] text-text-muted uppercase tracking-wider w-12 pt-2.5 flex-shrink-0 font-mono">
                THEN
              </span>
              <input
                value={entry.then}
                onChange={(e) => onUpdate(entry.id, 'then', e.target.value)}
                placeholder="contains expected_output"
                className="flex-1 bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 transition-colors"
              />
              <div className="w-6 flex-shrink-0" />
            </div>
          </div>
        ))}
      </div>
    </div>
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
      {/* Visible grip — subtle at rest, fully visible on hover/drag so the
          resize affordance is discoverable without a blind mouse-over */}
      <div
        className={cn(
          'flex flex-col gap-0.5 transition-opacity duration-150',
          'opacity-30 group-hover/split:opacity-100',
          isDragging && 'opacity-100',
        )}
      >
        <GripVertical
          size={12}
          className={cn(
            'text-text-muted transition-colors',
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
  /** Owning git source, when this skill was imported. Drives the provenance
   *  strip and reconciliation actions (compare/reset/detach/fork). */
  source?: SkillSourceStatus;
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
  source,
  onPopout,
  popoutDisabled,
  size,
  flush,
}: SkillEditorProps) {
  const isNew = !skill;
  const idCounter = useRef(0);
  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const originalBodyRef = useRef('');

  // Persisted editor view preferences (frontmatter/preview/split).
  const editorPrefs = useUIStore((s) => s.editorPrefs);
  const setEditorPrefs = useUIStore((s) => s.setEditorPrefs);

  // Provenance / reconciliation
  const repoInfo = source ? extractRepoInfo(source.repo) : null;
  const isRemote = !!source && !isNew;
  const hasLocalEdits = (source?.driftedSkills?.includes(skill?.name ?? '') ?? false) && !isNew;
  const [showCompare, setShowCompare] = useState(false);
  const [pendingAction, setPendingAction] = useState<'reset' | 'detach' | null>(null);
  const [forking, setForking] = useState(false);
  const [forkName, setForkName] = useState('');
  const [reconciling, setReconciling] = useState(false);
  const [showFiles, setShowFiles] = useState(false);

  // Editor state
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [license, setLicense] = useState('');
  const [compatibility, setCompatibility] = useState('');
  const [metadata, setMetadata] = useState<MetadataEntry[]>([]);
  const [allowedTools, setAllowedTools] = useState('');
  const [criteria, setCriteria] = useState<CriterionEntry[]>([]);
  const [state, setState] = useState<ItemState>('draft');
  const [body, setBody] = useState('');

  // UI state. Existing skills open with frontmatter collapsed (body-first); a
  // new skill always opens it so its required fields are reachable. Preview and
  // split ratio come from persisted prefs.
  const [showFrontmatter, setShowFrontmatter] = useState(isNew ? true : editorPrefs.showFrontmatter);
  const [showPreview, setShowPreview] = useState(editorPrefs.showPreview);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validation, setValidation] = useState<SkillValidationResult | null>(null);

  const toggleFrontmatter = useCallback(() => {
    setShowFrontmatter((prev) => {
      const next = !prev;
      setEditorPrefs({ showFrontmatter: next });
      return next;
    });
  }, [setEditorPrefs]);

  const togglePreview = useCallback(() => {
    setShowPreview((prev) => {
      const next = !prev;
      setEditorPrefs({ showPreview: next });
      return next;
    });
  }, [setEditorPrefs]);

  // Resizable split pane: body-heavy by default; committed ratio persisted.
  const persistRatio = useCallback((r: number) => setEditorPrefs({ splitRatio: r }), [setEditorPrefs]);
  const { ratio, containerRef, handleMouseDown: handleSplitMouseDown, isDragging: splitDragging } = useSplitPane(editorPrefs.splitRatio, 0.25, 0.75, persistRatio);

  // Computed
  const lineCount = useMemo(() => body.split('\n').length, [body]);
  const charCount = body.length;

  // One-line frontmatter summary shown while the section is collapsed.
  const frontmatterSummary = useMemo(() => {
    const metaCount = metadata.filter((m) => m.key).length;
    const criteriaCount = criteria.filter((c) => c.given && c.when && c.then).length;
    const parts = [
      license || 'no license',
      `${metaCount} metadata`,
      `${criteriaCount} criteria`,
      state,
    ];
    return parts.join(' · ');
  }, [license, metadata, criteria, state]);

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
      setCriteria(
        (skill.acceptanceCriteria ?? []).map((c) => {
          const parsed = parseAcceptanceCriterion(c);
          return {
            id: ++idCounter.current,
            given: parsed.given,
            when: parsed.when,
            then: parsed.then,
          };
        }),
      );
      setState(skill.state);
      setBody(skill.body ?? '');
      originalBodyRef.current = skill.body ?? '';
    } else {
      setName('');
      setDescription('');
      setLicense('');
      setCompatibility('');
      setMetadata([]);
      setAllowedTools('');
      setCriteria([{ id: ++idCounter.current, given: '', when: 'the skill is called', then: '' }]);
      setState('draft');
      setBody('');
      originalBodyRef.current = '';
    }
    setError(null);
    setValidation(null);
    // A new skill always opens with frontmatter expanded (its required fields
    // must be reachable); an existing skill honors the persisted preference.
    setShowFrontmatter(skill ? useUIStore.getState().editorPrefs.showFrontmatter : true);
    // Reset transient reconciliation UI when the open skill changes.
    setShowCompare(false);
    setPendingAction(null);
    setForking(false);
    setForkName('');
    setShowFiles(false);
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

  // --- Criteria management ---

  const addCriterion = useCallback(() => {
    setCriteria((prev) => [...prev, { id: ++idCounter.current, given: '', when: 'the skill is called', then: '' }]);
  }, []);

  const removeCriterion = useCallback((entryId: number) => {
    setCriteria((prev) => prev.filter((c) => c.id !== entryId));
  }, []);

  const updateCriterion = useCallback((entryId: number, field: 'given' | 'when' | 'then', val: string) => {
    setCriteria((prev) => prev.map((c) => (c.id === entryId ? { ...c, [field]: val } : c)));
  }, []);

  // --- Debounced validation ---

  const validator = useMemo(
    () => createDebouncedValidator(500, (result) => setValidation(result)),
    [],
  );

  useEffect(() => {
    if (name || description || body) {
      validator.trigger({ name, description, license, compatibility, allowedTools, metadata, criteria, state, body });
    }
    return () => validator.cancel();
  }, [name, description, body, license, compatibility, allowedTools, metadata, criteria, state, validator]);

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

      const criteriaStrings = criteria
        .filter((c) => c.given && c.when && c.then)
        .map((c) => `GIVEN ${c.given} WHEN ${c.when} THEN ${c.then}`);

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
        ...(criteriaStrings.length > 0 && { acceptanceCriteria: criteriaStrings }),
      };

      if (isNew) {
        await createRegistrySkill(skillData);
        showToast('success', `Skill "${name}" created`);
      } else {
        await updateRegistrySkill(skill!.name, skillData);
        showToast('success', `Skill "${name}" updated`);
        // First time a tracked skill is edited, explain that the change becomes
        // a local customization that future syncs will preserve.
        if (isRemote && body !== originalBodyRef.current) {
          try {
            if (!localStorage.getItem(LOCAL_EDIT_NOTE_KEY)) {
              localStorage.setItem(LOCAL_EDIT_NOTE_KEY, '1');
              showToast(
                'warning',
                'Saved as a local customization. Future syncs will skip this skill unless you overwrite.',
                { duration: 6000 },
              );
            }
          } catch {
            // localStorage unavailable (private mode): skip the one-time note.
          }
        }
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
  }, [saving, name, description, metadata, criteria, body, state, skill, license, compatibility, allowedTools, isNew, isRemote, onSaved, onClose]);

  // --- Markdown toolbar ---

  const applyMarkdown = useCallback((action: MarkdownAction) => {
    const ta = bodyRef.current;
    if (!ta) return;
    const next = applyMarkdownAction(body, ta.selectionStart, ta.selectionEnd, action);
    setBody(next.value);
    requestAnimationFrame(() => {
      ta.focus();
      ta.setSelectionRange(next.selStart, next.selEnd);
    });
  }, [body]);

  // --- Scroll sync: the preview follows the editor proportionally ---

  // Proportional (fraction-of-scrollable-height) mapping. The rendered preview
  // is taller or shorter than the source, so an exact line mapping is not
  // possible without a source map; fraction tracking keeps both panes aligned
  // closely enough to feel tethered.
  const handleEditorScroll = useCallback((e: React.UIEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget;
    const preview = previewRef.current;
    if (!preview) return;
    const srcMax = ta.scrollHeight - ta.clientHeight;
    if (srcMax <= 0) return;
    const dstMax = preview.scrollHeight - preview.clientHeight;
    preview.scrollTop = (ta.scrollTop / srcMax) * dstMax;
  }, []);

  // --- Reconciliation actions (remote skills only) ---

  const handleReset = useCallback(async () => {
    if (!source || !skill) return;
    setReconciling(true);
    try {
      await resetSkill(source.name, skill.name);
      showToast('success', `"${skill.name}" reset to upstream`);
      onSaved();
      onClose();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Reset failed');
    } finally {
      setReconciling(false);
      setPendingAction(null);
    }
  }, [source, skill, onSaved, onClose]);

  const handleDetach = useCallback(async () => {
    if (!source || !skill) return;
    setReconciling(true);
    try {
      await detachSkill(source.name, skill.name);
      showToast('success', `"${skill.name}" detached; now a local skill`);
      onSaved();
      onClose();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Detach failed');
    } finally {
      setReconciling(false);
      setPendingAction(null);
    }
  }, [source, skill, onSaved, onClose]);

  const handleFork = useCallback(async () => {
    const newName = forkName.trim();
    if (!newName) return;
    setReconciling(true);
    try {
      const metadataRecord: Record<string, string> = {};
      for (const entry of metadata) {
        if (entry.key) metadataRecord[entry.key] = entry.value;
      }
      const criteriaStrings = criteria
        .filter((c) => c.given && c.when && c.then)
        .map((c) => `GIVEN ${c.given} WHEN ${c.when} THEN ${c.then}`);
      // A fork is a brand-new local skill (no origin) seeded from the current
      // editor fields, so it captures any unsaved local edits.
      const copy: AgentSkill = {
        name: newName,
        description,
        body,
        state,
        fileCount: 0,
        ...(license && { license }),
        ...(compatibility && { compatibility }),
        ...(Object.keys(metadataRecord).length > 0 && { metadata: metadataRecord }),
        ...(allowedTools && { allowedTools }),
        ...(criteriaStrings.length > 0 && { acceptanceCriteria: criteriaStrings }),
      };
      await createRegistrySkill(copy);
      showToast('success', `Forked as "${newName}"`);
      onSaved();
      onClose();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Fork failed (name may already exist)');
    } finally {
      setReconciling(false);
      setForking(false);
    }
  }, [forkName, description, body, state, license, compatibility, metadata, allowedTools, criteria, onSaved, onClose]);

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
      {/* Fill the modal's padded content box (100% + the cancelled py-4) so the
          editor grows with the panel: 85vh base, 94vh expanded, full viewport
          when detached. */}
      <div className="flex flex-col h-[calc(100%+2rem)] -mx-6 -my-4">
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
              onClick={togglePreview}
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

        {/* Error display */}
        {error && (
          <div className="px-5 py-2.5 bg-status-error/10 border-b border-status-error/30 flex-shrink-0">
            <p className="text-sm text-status-error">{error}</p>
          </div>
        )}

        {/* Provenance strip: only for skills tracked from a git source */}
        {isRemote && source && (
          <div className="flex items-center justify-between gap-3 flex-wrap px-5 py-2 border-b border-border/30 bg-surface/40 flex-shrink-0">
            <div className="flex items-center gap-2 min-w-0 text-[11px] text-text-muted">
              <GitBranch size={12} className="text-text-muted/70 flex-shrink-0" />
              <span className="truncate">
                Tracked from{' '}
                <span className="font-mono text-text-secondary">
                  {repoInfo ? `${repoInfo.owner}/${repoInfo.repo}` : source.name}
                </span>
                {source.commitSha && (
                  <span className="font-mono text-text-muted/70">@{source.commitSha.slice(0, 7)}</span>
                )}
                {source.lastFetched && (
                  <span className="text-text-muted/70">
                    {' '}· synced {new Date(source.lastFetched).toLocaleDateString()}
                  </span>
                )}
              </span>
              {hasLocalEdits && (
                <span
                  title="Edited locally; a sync will skip this unless you overwrite"
                  className="flex-shrink-0 text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-amber-400/30 bg-amber-400/10 text-amber-300"
                >
                  Local edits
                </span>
              )}
            </div>
            <div className="flex items-center gap-1 flex-shrink-0">
              <ProvenanceAction icon={GitCompareArrows} label="Compare" onClick={() => setShowCompare(true)} disabled={reconciling} />
              <ProvenanceAction icon={RotateCcw} label="Reset" onClick={() => setPendingAction('reset')} disabled={reconciling} />
              <ProvenanceAction icon={Unlink} label="Detach" onClick={() => setPendingAction('detach')} disabled={reconciling} />
              <ProvenanceAction icon={GitFork} label="Fork as…" onClick={() => { setForkName(`${skill!.name}-local`); setForking(true); }} disabled={reconciling} />
            </div>
          </div>
        )}

        {/* Fork-as inline input */}
        {forking && (
          <div className="flex items-center gap-2 px-5 py-2 border-b border-border/30 bg-background/40 flex-shrink-0">
            <span className="text-[11px] text-text-muted flex-shrink-0">Fork as a new local skill:</span>
            <input
              autoFocus
              value={forkName}
              onChange={(e) => setForkName(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') handleFork(); if (e.key === 'Escape') setForking(false); }}
              placeholder="new-skill-name"
              className="flex-1 max-w-xs bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50 transition-colors"
            />
            <button
              onClick={handleFork}
              disabled={!forkName.trim() || reconciling}
              className={cn(
                'px-3 py-1.5 text-xs font-medium rounded-lg bg-primary text-background hover:bg-primary-light transition-colors',
                (!forkName.trim() || reconciling) && 'opacity-50 cursor-not-allowed',
              )}
            >
              Fork
            </button>
            <button
              onClick={() => setForking(false)}
              className="px-3 py-1.5 text-xs rounded-lg text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight transition-colors"
            >
              Cancel
            </button>
          </div>
        )}

        {/* Frontmatter helpers (collapsible, scrollable). Collapsed by default
            for existing skills so the body gets the room; a one-line summary
            keeps the key fields glanceable, with "Edit metadata" to expand. */}
        <div className="border-b border-border/30 flex-shrink-0">
          <button
            onClick={toggleFrontmatter}
            className="w-full flex items-center justify-between gap-3 px-5 py-2.5 text-xs text-text-muted hover:text-text-secondary transition-colors"
          >
            <span className="flex items-center gap-2 min-w-0">
              <Settings size={14} className="flex-shrink-0" />
              <span className="uppercase tracking-wider flex-shrink-0">Frontmatter</span>
              {!showFrontmatter && (
                <span className="truncate text-text-muted/70 normal-case tracking-normal">
                  {frontmatterSummary}
                </span>
              )}
            </span>
            <span className="flex items-center gap-1.5 flex-shrink-0">
              {!showFrontmatter && <span className="text-[10px] text-primary/80">Edit metadata</span>}
              {showFrontmatter ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            </span>
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
                    <span className="ml-2 text-text-muted normal-case">{description.length}/1024</span>
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

                {/* Acceptance Criteria */}
                <AcceptanceCriteriaEditor
                  entries={criteria}
                  onAdd={addCriterion}
                  onRemove={removeCriterion}
                  onUpdate={updateCriterion}
                />
              </div>
            </div>
          )}
        </div>

        {/* File tree (existing skills), demoted behind a pill so it no longer
            crowds the editor; expands on demand. */}
        {!isNew && skill && (
          <div className="border-b border-border/30 flex-shrink-0">
            <button
              onClick={() => setShowFiles((v) => !v)}
              className="flex items-center gap-1.5 px-5 py-1.5 text-[11px] text-text-muted hover:text-text-secondary transition-colors"
            >
              <Files size={12} />
              <span>Files ({skill.fileCount})</span>
              {showFiles ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
            </button>
            {showFiles && (
              <div className="max-h-[22vh] overflow-y-auto scrollbar-dark border-t border-border/20">
                <SkillFileTree
                  skillName={skill.name}
                  onSelectFile={(path) => {
                    showToast('success', `Selected: ${path}`);
                  }}
                />
              </div>
            )}
          </div>
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
            <div className="flex items-center justify-between gap-2 px-4 py-1.5 border-b border-border/20 flex-shrink-0">
              <span className="text-xs text-text-muted uppercase tracking-wider">Markdown</span>
              {/* Minimal formatting toolbar: inserts at the textarea cursor */}
              <div className="flex items-center gap-0.5">
                <ToolbarButton icon={Bold} label="Bold" onClick={() => applyMarkdown('bold')} />
                <ToolbarButton icon={Heading} label="Heading" onClick={() => applyMarkdown('heading')} />
                <ToolbarButton icon={List} label="List item" onClick={() => applyMarkdown('list')} />
                <ToolbarButton icon={Code2} label="Code block" onClick={() => applyMarkdown('code')} />
              </div>
            </div>
            <textarea
              ref={bodyRef}
              value={body}
              onChange={(e) => setBody(e.target.value)}
              onScroll={handleEditorScroll}
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
                <div ref={previewRef} className="flex-1 overflow-y-auto scrollbar-dark">
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
      </div>

      {/* Reconciliation: compare-with-upstream and reset/detach confirms */}
      {isRemote && source && skill && (
        <SkillCompareDialog
          isOpen={showCompare}
          sourceName={source.name}
          skillName={skill.name}
          onClose={() => setShowCompare(false)}
          onTookUpstream={() => { setShowCompare(false); onSaved(); onClose(); }}
        />
      )}

      <ConfirmDialog
        isOpen={pendingAction === 'reset'}
        onClose={() => setPendingAction(null)}
        onConfirm={handleReset}
        title="Reset to upstream"
        message={
          <>
            <p>
              Replace <span className="font-mono text-primary">{skill?.name}</span> with the latest
              upstream content?
            </p>
            <p>Your local edits are backed up next to the skill before they are overwritten.</p>
          </>
        }
        confirmLabel="Reset to upstream"
        autoFocus="cancel"
      />

      <ConfirmDialog
        isOpen={pendingAction === 'detach'}
        onClose={() => setPendingAction(null)}
        onConfirm={handleDetach}
        title="Detach from source"
        message={
          <>
            <p>
              Stop tracking <span className="font-mono text-primary">{skill?.name}</span> from its
              source?
            </p>
            <p>The skill and your edits stay; it becomes a local skill that sync no longer touches.</p>
          </>
        }
        confirmLabel="Detach"
      />
    </Modal>
  );
}

// --- Small toolbar / provenance action buttons ---

function ToolbarButton({
  icon: Icon,
  label,
  onClick,
}: {
  icon: typeof Bold;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
      className="p-1.5 rounded-md text-text-muted hover:text-primary hover:bg-primary/10 transition-colors"
    >
      <Icon size={13} />
    </button>
  );
}

function ProvenanceAction({
  icon: Icon,
  label,
  onClick,
  disabled,
}: {
  icon: typeof Bold;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'inline-flex items-center gap-1 px-2 py-1 text-[11px] font-medium rounded-md transition-colors',
        'text-text-secondary hover:text-text-primary bg-surface-elevated hover:bg-surface-highlight border border-border/40',
        disabled && 'opacity-50 cursor-not-allowed',
      )}
    >
      <Icon size={11} /> {label}
    </button>
  );
}
