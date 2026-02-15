import { useState, useEffect, useRef } from 'react';
import { Plus, Trash2, X } from 'lucide-react';
import { Modal } from '../ui/Modal';
import { showToast } from '../ui/Toast';
import { createRegistrySkill, updateRegistrySkill } from '../../lib/api';
import { cn } from '../../lib/cn';
import type { Skill, ItemState } from '../../types';

// Internal representation with stable IDs for React keys
interface ArgEntry {
  id: number;
  key: string;
  value: string;
}

interface EditableStep {
  id: number;
  tool: string;
  args: ArgEntry[];
}

interface InputEntry {
  id: number;
  name: string;
  description: string;
  required: boolean;
}

interface SkillEditorProps {
  isOpen: boolean;
  onClose: () => void;
  onSaved: () => void;
  skill?: Skill;
}

// Convert Record<string, string> to ArgEntry[] with stable IDs
function recordToArgs(record: Record<string, string>, counter: { current: number }): ArgEntry[] {
  return Object.entries(record ?? {}).map(([key, value]) => ({
    id: ++counter.current,
    key,
    value,
  }));
}

// Convert ArgEntry[] back to Record<string, string> for API
function argsToRecord(args: ArgEntry[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (const arg of args) {
    if (arg.key) result[arg.key] = arg.value;
  }
  return result;
}

export function SkillEditor({ isOpen, onClose, onSaved, skill }: SkillEditorProps) {
  const isEditing = !!skill;
  const idCounter = useRef(0);

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [steps, setSteps] = useState<EditableStep[]>([]);
  const [input, setInput] = useState<InputEntry[]>([]);
  const [tags, setTags] = useState<string[]>([]);
  const [tagInput, setTagInput] = useState('');
  const [state, setState] = useState<ItemState>('draft');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    idCounter.current = 0;
    if (skill) {
      setName(skill.name);
      setDescription(skill.description);
      setSteps(
        (skill.steps ?? []).map((s) => ({
          id: ++idCounter.current,
          tool: s.tool,
          args: recordToArgs(s.arguments, idCounter),
        })),
      );
      setInput(
        (skill.input ?? []).map((p) => ({
          id: ++idCounter.current,
          name: p.name,
          description: p.description,
          required: p.required,
        })),
      );
      setTags(skill.tags ?? []);
      setState(skill.state);
    } else {
      setName('');
      setDescription('');
      setSteps([]);
      setInput([]);
      setTags([]);
      setState('draft');
    }
    setError(null);
    setTagInput('');
  }, [skill, isOpen]);

  const handleSave = async () => {
    setError(null);
    setSaving(true);
    try {
      const payload: Skill = {
        name,
        description,
        steps: steps.map((s) => ({
          tool: s.tool,
          arguments: argsToRecord(s.args),
        })),
        input: input.map(({ name: n, description: d, required: r }) => ({
          name: n,
          description: d,
          required: r,
        })),
        tags,
        state,
      };
      if (isEditing) {
        await updateRegistrySkill(name, payload);
      } else {
        await createRegistrySkill(payload);
      }
      showToast('success', isEditing ? 'Skill updated' : 'Skill created');
      onClose();
      onSaved();
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Save failed';
      setError(msg);
      showToast('error', msg);
    } finally {
      setSaving(false);
    }
  };

  // --- Step management ---

  const addStep = () => {
    setSteps([...steps, { id: ++idCounter.current, tool: '', args: [] }]);
  };

  const removeStep = (stepId: number) => {
    setSteps(steps.filter((s) => s.id !== stepId));
  };

  const updateStepTool = (stepId: number, tool: string) => {
    setSteps(steps.map((s) => (s.id === stepId ? { ...s, tool } : s)));
  };

  const addStepArg = (stepId: number) => {
    const newArg: ArgEntry = { id: ++idCounter.current, key: '', value: '' };
    setSteps(steps.map((s) =>
      s.id === stepId ? { ...s, args: [...s.args, newArg] } : s,
    ));
  };

  const removeStepArg = (stepId: number, argId: number) => {
    setSteps(steps.map((s) =>
      s.id === stepId ? { ...s, args: s.args.filter((a) => a.id !== argId) } : s,
    ));
  };

  const updateStepArg = (stepId: number, argId: number, field: 'key' | 'value', val: string) => {
    setSteps(steps.map((s) =>
      s.id === stepId
        ? { ...s, args: s.args.map((a) => (a.id === argId ? { ...a, [field]: val } : a)) }
        : s,
    ));
  };

  // --- Input parameter management ---

  const addInput = () => {
    setInput([...input, { id: ++idCounter.current, name: '', description: '', required: false }]);
  };

  const removeInput = (inputId: number) => {
    setInput(input.filter((p) => p.id !== inputId));
  };

  const updateInput = (inputId: number, field: 'name' | 'description' | 'required', value: string | boolean) => {
    setInput(input.map((p) => (p.id === inputId ? { ...p, [field]: value } : p)));
  };

  // --- Tags ---

  const addTag = () => {
    const trimmed = tagInput.trim();
    if (trimmed && !(tags ?? []).includes(trimmed)) {
      setTags([...(tags ?? []), trimmed]);
      setTagInput('');
    }
  };

  const removeTag = (tag: string) => {
    setTags((tags ?? []).filter((t) => t !== tag));
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={isEditing ? 'Edit Skill' : 'New Skill'}>
      <div className="space-y-4">
        {/* Error */}
        {error && (
          <div className="text-xs text-status-error bg-status-error/10 rounded-lg px-3 py-2">
            {error}
          </div>
        )}

        {/* Name */}
        <div>
          <label className="text-xs text-text-secondary font-medium block mb-1">Name</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            disabled={isEditing}
            placeholder="e.g., deploy-pipeline"
            className={cn(
              'w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary',
              'placeholder:text-text-muted focus:border-primary/50 focus:outline-none font-mono',
              isEditing && 'opacity-60 cursor-not-allowed',
            )}
          />
        </div>

        {/* Description */}
        <div>
          <label className="text-xs text-text-secondary font-medium block mb-1">Description</label>
          <input
            type="text"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Brief description of this skill"
            className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
          />
        </div>

        {/* Tool Chain Steps */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="text-xs text-text-secondary font-medium">Tool Chain</label>
            <button
              onClick={addStep}
              className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors"
            >
              <Plus size={10} /> Add Step
            </button>
          </div>
          <div className="space-y-2">
            {(steps ?? []).map((step, i) => (
              <div key={step.id}>
                <div className="glass-panel p-3 rounded-lg">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-[10px] text-text-muted font-mono">Step {i + 1}</span>
                    <button
                      onClick={() => removeStep(step.id)}
                      className="p-1 text-text-muted hover:text-status-error transition-colors"
                    >
                      <Trash2 size={12} />
                    </button>
                  </div>
                  <input
                    type="text"
                    value={step.tool}
                    onChange={(e) => updateStepTool(step.id, e.target.value)}
                    placeholder="server__tool_name"
                    className="w-full bg-surface border border-border/30 rounded-lg px-2 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none mb-2"
                  />
                  {/* Step arguments */}
                  <div className="space-y-1">
                    {(step.args ?? []).map((arg) => (
                      <div key={arg.id} className="flex items-center gap-2">
                        <input
                          type="text"
                          value={arg.key}
                          onChange={(e) => updateStepArg(step.id, arg.id, 'key', e.target.value)}
                          placeholder="key"
                          className="flex-1 bg-surface border border-border/30 rounded px-2 py-1 text-[10px] font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                        />
                        <input
                          type="text"
                          value={arg.value}
                          onChange={(e) => updateStepArg(step.id, arg.id, 'value', e.target.value)}
                          placeholder="{{input.name}} or {{step1.result}}"
                          className="flex-[2] bg-surface border border-border/30 rounded px-2 py-1 text-[10px] font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                        />
                        <button
                          onClick={() => removeStepArg(step.id, arg.id)}
                          className="p-0.5 text-text-muted hover:text-status-error transition-colors"
                        >
                          <Trash2 size={10} />
                        </button>
                      </div>
                    ))}
                    <button
                      onClick={() => addStepArg(step.id)}
                      className="text-[10px] text-primary hover:text-primary/80 transition-colors"
                    >
                      + Add argument
                    </button>
                  </div>
                </div>
                {/* Connector to next step */}
                {i < (steps ?? []).length - 1 && (
                  <div className="flex justify-center py-1">
                    <div className="w-px h-4 bg-border/50" />
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>

        {/* Input Parameters */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="text-xs text-text-secondary font-medium">Input Parameters</label>
            <button
              onClick={addInput}
              className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors"
            >
              <Plus size={10} /> Add
            </button>
          </div>
          <div className="space-y-2">
            {(input ?? []).map((param) => (
              <div key={param.id} className="flex items-center gap-2">
                <input
                  type="text"
                  value={param.name}
                  onChange={(e) => updateInput(param.id, 'name', e.target.value)}
                  placeholder="name"
                  className="flex-1 bg-surface border border-border/30 rounded-lg px-2 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                />
                <input
                  type="text"
                  value={param.description}
                  onChange={(e) => updateInput(param.id, 'description', e.target.value)}
                  placeholder="description"
                  className="flex-[2] bg-surface border border-border/30 rounded-lg px-2 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                />
                <label className="flex items-center gap-1 text-[10px] text-text-muted whitespace-nowrap">
                  <input
                    type="checkbox"
                    checked={param.required}
                    onChange={(e) => updateInput(param.id, 'required', e.target.checked)}
                    className="rounded"
                  />
                  Req
                </label>
                <button
                  onClick={() => removeInput(param.id)}
                  className="p-1 text-text-muted hover:text-status-error transition-colors"
                >
                  <Trash2 size={12} />
                </button>
              </div>
            ))}
          </div>
        </div>

        {/* Tags */}
        <div>
          <label className="text-xs text-text-secondary font-medium block mb-1">Tags</label>
          <div className="flex items-center gap-2 flex-wrap">
            {(tags ?? []).map((tag) => (
              <span
                key={tag}
                className="flex items-center gap-1 text-[10px] px-2 py-0.5 rounded bg-surface-highlight text-text-secondary"
              >
                {tag}
                <button
                  onClick={() => removeTag(tag)}
                  className="text-text-muted hover:text-status-error transition-colors"
                >
                  <X size={8} />
                </button>
              </span>
            ))}
            <input
              type="text"
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  addTag();
                }
              }}
              placeholder="+ Add tag"
              className="bg-transparent text-[10px] text-text-muted placeholder:text-text-muted focus:outline-none w-16"
            />
          </div>
        </div>

        {/* State */}
        <div>
          <label className="text-xs text-text-secondary font-medium block mb-2">State</label>
          <div className="flex gap-3">
            {(['draft', 'active', 'disabled'] as ItemState[]).map((s) => (
              <label
                key={s}
                className="flex items-center gap-1.5 text-xs text-text-secondary cursor-pointer"
              >
                <input
                  type="radio"
                  name="skill-state"
                  value={s}
                  checked={state === s}
                  onChange={() => setState(s)}
                  className="text-primary"
                />
                {s.charAt(0).toUpperCase() + s.slice(1)}
              </label>
            ))}
          </div>
        </div>

        {/* Actions */}
        <div className="flex justify-end gap-2 pt-4 border-t border-border/30">
          <button
            onClick={onClose}
            className="px-4 py-2 text-xs text-text-secondary hover:text-text-primary bg-surface-elevated rounded-lg transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving || !name}
            className={cn(
              'px-4 py-2 text-xs rounded-lg font-medium transition-all',
              'bg-primary text-background hover:bg-primary/90',
              (saving || !name) && 'opacity-50 cursor-not-allowed',
            )}
          >
            {saving ? 'Saving...' : isEditing ? 'Save Changes' : 'Create Skill'}
          </button>
        </div>
      </div>
    </Modal>
  );
}
