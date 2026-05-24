import { useId, useRef, useState } from 'react';
import { X } from 'lucide-react';
import { cn } from '../../lib/cn';

// Split the human/comma form (what validateVariableInput accepts for `list`)
// into distinct, trimmed tags.
function parseTags(value: string): string[] {
  const out: string[] = [];
  for (const part of value.split(',')) {
    const t = part.trim();
    if (t && !out.includes(t)) out.push(t);
  }
  return out;
}

// ListTagInput edits a `list` variable as removable chips. It emits the
// comma-joined "human" form (not a JSON array) so the existing
// validateVariableInput('list', …) pipeline normalizes it to a JSON array on
// save — emitting JSON here would double-encode when re-validated. The pending
// buffer is folded into the emitted value so an uncommitted tag is never lost
// when the surrounding form submits.
export function ListTagInput({
  value,
  onChange,
  onCancel,
  compact,
  enableZoom,
  autoFocus,
}: {
  value: string;
  onChange: (next: string) => void;
  onCancel?: () => void;
  compact?: boolean;
  enableZoom?: boolean;
  autoFocus?: boolean;
}) {
  // Seeded once from the incoming value; thereafter the chips are the source
  // of truth and changes flow up via onChange. Callers remount (by type/edit
  // state) when they need a reset, so we don't sync back from the prop.
  const [tags, setTags] = useState<string[]>(() => parseTags(value));
  const [buffer, setBuffer] = useState('');
  const [announce, setAnnounce] = useState('');
  const liveId = useId();
  const inputRef = useRef<HTMLInputElement>(null);

  const emit = (nextTags: string[], nextBuffer: string) => {
    const pending = nextBuffer.trim();
    const all =
      pending && !nextTags.includes(pending) ? [...nextTags, pending] : nextTags;
    onChange(all.join(', '));
  };

  const commit = () => {
    const t = buffer.trim();
    if (t && !tags.includes(t)) {
      const next = [...tags, t];
      setTags(next);
      setBuffer('');
      setAnnounce(`Added ${t}`);
      emit(next, '');
    } else {
      // Empty or duplicate — just drop the buffer.
      setBuffer('');
      emit(tags, '');
    }
  };

  const removeAt = (idx: number) => {
    const removed = tags[idx];
    const next = tags.filter((_, i) => i !== idx);
    setTags(next);
    setAnnounce(`Removed ${removed}`);
    emit(next, buffer);
  };

  return (
    <div>
      <div
        onClick={() => inputRef.current?.focus()}
        className={cn(
          'flex flex-wrap items-center gap-1.5 w-full bg-surface border border-border rounded-lg px-2 py-1.5',
          'focus-within:border-primary/50 focus-within:ring-1 focus-within:ring-primary/30 transition-colors cursor-text',
          compact ? 'min-h-[2rem]' : 'min-h-[2.25rem]',
        )}
      >
        {tags.map((tag, idx) => (
          <span
            key={`${tag}-${idx}`}
            className="inline-flex items-center gap-1 rounded-md bg-sky-500/10 text-sky-300 pl-2 pr-1 py-0.5 text-[11px] font-mono"
          >
            {tag}
            <button
              type="button"
              tabIndex={-1}
              aria-label={`Remove ${tag}`}
              onClick={(e) => {
                e.stopPropagation();
                removeAt(idx);
              }}
              className="p-0.5 rounded hover:bg-sky-500/20 transition-colors"
            >
              <X size={10} />
            </button>
          </span>
        ))}
        <input
          ref={inputRef}
          type="text"
          value={buffer}
          autoFocus={autoFocus}
          aria-label="Add list item"
          placeholder={tags.length === 0 ? 'item1, item2, item3' : 'add item…'}
          onChange={(e) => {
            // A pasted/typed comma commits the preceding token.
            if (e.target.value.includes(',')) {
              const [head, ...rest] = e.target.value.split(',');
              const t = head.trim();
              if (t && !tags.includes(t)) {
                const next = [...tags, t];
                setTags(next);
                const nextBuffer = rest.join(',');
                setBuffer(nextBuffer);
                setAnnounce(`Added ${t}`);
                emit(next, nextBuffer);
              } else {
                const nextBuffer = rest.join(',');
                setBuffer(nextBuffer);
                emit(tags, nextBuffer);
              }
              return;
            }
            setBuffer(e.target.value);
            emit(tags, e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              commit();
            } else if (e.key === 'Backspace' && buffer === '' && tags.length > 0) {
              e.preventDefault();
              removeAt(tags.length - 1);
            } else if (e.key === 'Escape') {
              onCancel?.();
            }
          }}
          className={cn(
            'flex-1 min-w-[6rem] bg-transparent text-xs font-mono text-text-primary',
            'placeholder:text-text-muted outline-none',
            enableZoom && 'log-text',
          )}
        />
      </div>
      <div id={liveId} role="status" aria-live="polite" className="sr-only">
        {announce}
      </div>
    </div>
  );
}
