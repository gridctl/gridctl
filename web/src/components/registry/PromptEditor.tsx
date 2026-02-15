import { useState, useEffect, useRef } from 'react';
import { Plus, Trash2, X } from 'lucide-react';
import { Modal } from '../ui/Modal';
import { showToast } from '../ui/Toast';
import { createRegistryPrompt, updateRegistryPrompt } from '../../lib/api';
import { cn } from '../../lib/cn';
import type { Prompt, ItemState } from '../../types';

interface PromptEditorProps {
  isOpen: boolean;
  onClose: () => void;
  onSaved: () => void;
  prompt?: Prompt;
}

// Internal representation with stable IDs for React keys
interface ArgEntry {
  id: number;
  name: string;
  description: string;
  required: boolean;
}

export function PromptEditor({ isOpen, onClose, onSaved, prompt }: PromptEditorProps) {
  const isEditing = !!prompt;
  const idCounter = useRef(0);

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [content, setContent] = useState('');
  const [args, setArgs] = useState<ArgEntry[]>([]);
  const [tags, setTags] = useState<string[]>([]);
  const [tagInput, setTagInput] = useState('');
  const [state, setState] = useState<ItemState>('draft');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    idCounter.current = 0;
    if (prompt) {
      setName(prompt.name);
      setDescription(prompt.description);
      setContent(prompt.content);
      setArgs(
        (prompt.arguments ?? []).map((a) => ({
          id: ++idCounter.current,
          name: a.name,
          description: a.description,
          required: a.required,
        })),
      );
      setTags(prompt.tags ?? []);
      setState(prompt.state);
    } else {
      setName('');
      setDescription('');
      setContent('');
      setArgs([]);
      setTags([]);
      setState('draft');
    }
    setError(null);
    setTagInput('');
  }, [prompt, isOpen]);

  const handleSave = async () => {
    setError(null);
    setSaving(true);
    try {
      const payload: Prompt = {
        name,
        description,
        content,
        arguments: args.map(({ name: n, description: d, required: r }) => ({
          name: n,
          description: d,
          required: r,
        })),
        tags,
        state,
      };
      if (isEditing) {
        await updateRegistryPrompt(name, payload);
      } else {
        await createRegistryPrompt(payload);
      }
      showToast('success', isEditing ? 'Prompt updated' : 'Prompt created');
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

  const addArgument = () => {
    setArgs([...args, { id: ++idCounter.current, name: '', description: '', required: false }]);
  };

  const removeArgument = (argId: number) => {
    setArgs(args.filter((a) => a.id !== argId));
  };

  const updateArgument = (argId: number, field: 'name' | 'description' | 'required', value: string | boolean) => {
    setArgs(args.map((a) => (a.id === argId ? { ...a, [field]: value } : a)));
  };

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
    <Modal isOpen={isOpen} onClose={onClose} title={isEditing ? 'Edit Prompt' : 'New Prompt'}>
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
            placeholder="e.g., code-review"
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
            placeholder="Brief description of this prompt"
            className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
          />
        </div>

        {/* Content */}
        <div>
          <label className="text-xs text-text-secondary font-medium block mb-1">Content</label>
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            placeholder="Prompt template content. Use {{argument_name}} for parameters."
            rows={6}
            className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none font-mono resize-y min-h-[120px]"
          />
        </div>

        {/* Arguments */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="text-xs text-text-secondary font-medium">Arguments</label>
            <button
              onClick={addArgument}
              className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors"
            >
              <Plus size={10} /> Add
            </button>
          </div>
          <div className="space-y-2">
            {(args ?? []).map((arg) => (
              <div key={arg.id} className="flex items-center gap-2">
                <input
                  type="text"
                  value={arg.name}
                  onChange={(e) => updateArgument(arg.id, 'name', e.target.value)}
                  placeholder="name"
                  className="flex-1 bg-surface border border-border/30 rounded-lg px-2 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                />
                <input
                  type="text"
                  value={arg.description}
                  onChange={(e) => updateArgument(arg.id, 'description', e.target.value)}
                  placeholder="description"
                  className="flex-[2] bg-surface border border-border/30 rounded-lg px-2 py-1.5 text-xs text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                />
                <label className="flex items-center gap-1 text-[10px] text-text-muted whitespace-nowrap">
                  <input
                    type="checkbox"
                    checked={arg.required}
                    onChange={(e) => updateArgument(arg.id, 'required', e.target.checked)}
                    className="rounded"
                  />
                  Req
                </label>
                <button
                  onClick={() => removeArgument(arg.id)}
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
                  name="prompt-state"
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
            disabled={saving || !name || !content}
            className={cn(
              'px-4 py-2 text-xs rounded-lg font-medium transition-all',
              'bg-primary text-background hover:bg-primary/90',
              (saving || !name || !content) && 'opacity-50 cursor-not-allowed',
            )}
          >
            {saving ? 'Saving...' : isEditing ? 'Save Changes' : 'Create Prompt'}
          </button>
        </div>
      </div>
    </Modal>
  );
}
