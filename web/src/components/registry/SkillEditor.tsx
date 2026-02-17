import { useState, useEffect, useRef } from 'react';
import { Plus, Trash2 } from 'lucide-react';
import { Modal } from '../ui/Modal';
import { showToast } from '../ui/Toast';
import { createRegistrySkill, updateRegistrySkill } from '../../lib/api';
import { cn } from '../../lib/cn';
import type { AgentSkill, ItemState } from '../../types';

// Internal representation for metadata key-value pairs with stable IDs
interface MetadataEntry {
  id: number;
  key: string;
  value: string;
}

interface SkillEditorProps {
  isOpen: boolean;
  onClose: () => void;
  onSaved: () => void;
  skill?: AgentSkill;
  /** Callback to pop editor into a new window */
  onPopout?: () => void;
  /** Disable popout button */
  popoutDisabled?: boolean;
  /** Force modal size (for detached mode) */
  size?: 'default' | 'wide' | 'full';
  /** Flush mode: panel fills viewport (for detached windows) */
  flush?: boolean;
}

export function SkillEditor({ isOpen, onClose, onSaved, skill, onPopout, popoutDisabled, size, flush }: SkillEditorProps) {
  const isEditing = !!skill;
  const idCounter = useRef(0);

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [body, setBody] = useState('');
  const [allowedTools, setAllowedTools] = useState('');
  const [license, setLicense] = useState('');
  const [compatibility, setCompatibility] = useState('');
  const [metadata, setMetadata] = useState<MetadataEntry[]>([]);
  const [state, setState] = useState<ItemState>('draft');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    idCounter.current = 0;
    if (skill) {
      setName(skill.name);
      setDescription(skill.description);
      setBody(skill.body ?? '');
      setAllowedTools(skill.allowedTools ?? '');
      setLicense(skill.license ?? '');
      setCompatibility(skill.compatibility ?? '');
      setMetadata(
        Object.entries(skill.metadata ?? {}).map(([key, value]) => ({
          id: ++idCounter.current,
          key,
          value,
        })),
      );
      setState(skill.state);
    } else {
      setName('');
      setDescription('');
      setBody('');
      setAllowedTools('');
      setLicense('');
      setCompatibility('');
      setMetadata([]);
      setState('draft');
    }
    setError(null);
  }, [skill, isOpen]);

  const handleSave = async () => {
    setError(null);
    setSaving(true);
    try {
      const metadataRecord: Record<string, string> = {};
      for (const entry of metadata) {
        if (entry.key) metadataRecord[entry.key] = entry.value;
      }

      const payload: AgentSkill = {
        name,
        description,
        body,
        state,
        fileCount: skill?.fileCount ?? 0,
        ...(allowedTools && { allowedTools }),
        ...(license && { license }),
        ...(compatibility && { compatibility }),
        ...(Object.keys(metadataRecord).length > 0 && { metadata: metadataRecord }),
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

  // --- Metadata management ---

  const addMetadata = () => {
    setMetadata([...metadata, { id: ++idCounter.current, key: '', value: '' }]);
  };

  const removeMetadata = (entryId: number) => {
    setMetadata(metadata.filter((m) => m.id !== entryId));
  };

  const updateMetadata = (entryId: number, field: 'key' | 'value', val: string) => {
    setMetadata(metadata.map((m) => (m.id === entryId ? { ...m, [field]: val } : m)));
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={isEditing ? 'Edit Skill' : 'New Skill'} expandable onPopout={onPopout} popoutDisabled={popoutDisabled} size={size} flush={flush}>
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
            placeholder="Brief description of this skill"
            className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
          />
        </div>

        {/* Body (markdown content) */}
        <div>
          <label className="text-xs text-text-secondary font-medium block mb-1">Body</label>
          <textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="Skill instructions in markdown format..."
            rows={8}
            className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none font-mono resize-y min-h-[160px]"
          />
        </div>

        {/* Allowed Tools */}
        <div>
          <label className="text-xs text-text-secondary font-medium block mb-1">
            Allowed Tools <span className="text-text-muted">(optional)</span>
          </label>
          <input
            type="text"
            value={allowedTools}
            onChange={(e) => setAllowedTools(e.target.value)}
            placeholder="e.g., Read, Write, Bash"
            className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none font-mono"
          />
        </div>

        {/* License & Compatibility */}
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="text-xs text-text-secondary font-medium block mb-1">
              License <span className="text-text-muted">(optional)</span>
            </label>
            <input
              type="text"
              value={license}
              onChange={(e) => setLicense(e.target.value)}
              placeholder="e.g., MIT"
              className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none font-mono"
            />
          </div>
          <div>
            <label className="text-xs text-text-secondary font-medium block mb-1">
              Compatibility <span className="text-text-muted">(optional)</span>
            </label>
            <input
              type="text"
              value={compatibility}
              onChange={(e) => setCompatibility(e.target.value)}
              placeholder="e.g., claude, gpt-4"
              className="w-full bg-surface border border-border/30 rounded-lg px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none font-mono"
            />
          </div>
        </div>

        {/* Metadata */}
        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="text-xs text-text-secondary font-medium">
              Metadata <span className="text-text-muted">(optional)</span>
            </label>
            <button
              onClick={addMetadata}
              className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors"
            >
              <Plus size={10} /> Add
            </button>
          </div>
          <div className="space-y-2">
            {(metadata ?? []).map((entry) => (
              <div key={entry.id} className="flex items-center gap-2">
                <input
                  type="text"
                  value={entry.key}
                  onChange={(e) => updateMetadata(entry.id, 'key', e.target.value)}
                  placeholder="key"
                  className="flex-1 bg-surface border border-border/30 rounded-lg px-2 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                />
                <input
                  type="text"
                  value={entry.value}
                  onChange={(e) => updateMetadata(entry.id, 'value', e.target.value)}
                  placeholder="value"
                  className="flex-[2] bg-surface border border-border/30 rounded-lg px-2 py-1.5 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:outline-none"
                />
                <button
                  onClick={() => removeMetadata(entry.id)}
                  className="p-1 text-text-muted hover:text-status-error transition-colors"
                >
                  <Trash2 size={12} />
                </button>
              </div>
            ))}
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
