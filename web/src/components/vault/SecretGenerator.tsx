import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent,
} from 'react';
import { Wand2, Copy } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { showToast } from '../ui/Toast';
import {
  buildAlphabet,
  entropyBits,
  generateSecret,
  type SecretOptions,
} from '../../lib/generateSecret';

const MIN_LENGTH = 8;
const MAX_LENGTH = 64;
const DEFAULT_LENGTH = 24;

interface SecretGeneratorProps {
  // onGenerate receives the freshly generated value; wire it to the host's
  // value setter (e.g. setNewValue / onEditValueChange).
  onGenerate: (value: string) => void;
  // onReveal asks the host to reveal the (now non-empty) value input.
  onReveal?: () => void;
  iconSize?: number;
  // Applied to the trigger button (e.g. `mr-auto` to push neighbors right).
  className?: string;
}

type ClassKey = 'upper' | 'lower' | 'digits' | 'symbols';

const CLASS_CHIPS: { key: ClassKey; label: string }[] = [
  { key: 'upper', label: 'A-Z' },
  { key: 'lower', label: 'a-z' },
  { key: 'digits', label: '0-9' },
  { key: 'symbols', label: '!@#' },
];

// SecretGenerator is a disclosure-style control: a wand trigger that expands an
// inline panel (length, character classes, live entropy) and fills the host's
// value input with a CSPRNG-generated string. Rendered inline (no portal) so it
// is safe inside the wizard's portal popover — clicks inside the panel still
// count as "inside" that popover.
export function SecretGenerator({
  onGenerate,
  onReveal,
  iconSize = 12,
  className,
}: SecretGeneratorProps) {
  const [open, setOpen] = useState(false);
  const [length, setLength] = useState(DEFAULT_LENGTH);
  const [classes, setClasses] = useState({
    upper: true,
    lower: true,
    digits: true,
    symbols: true,
  });
  // Held only while the panel is open, to enable copy + the Regenerate label.
  // Cleared on close so the value isn't retained in long-lived state.
  const [generated, setGenerated] = useState('');
  const [announcement, setAnnouncement] = useState('');

  const panelId = useId();
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const lengthRef = useRef<HTMLInputElement>(null);

  const opts: SecretOptions = { length, ...classes };
  const alphabetSize = buildAlphabet(opts).length;
  const bits = Math.round(entropyBits(length, alphabetSize));
  const activeCount = CLASS_CHIPS.filter((c) => classes[c.key]).length;

  const close = useCallback(() => {
    setOpen(false);
    setGenerated('');
    setAnnouncement('');
  }, []);

  // Collapse on clicks outside the trigger and panel. Clicks inside the panel
  // keep any host popover open too, since the panel lives in its DOM subtree.
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!triggerRef.current?.contains(t) && !panelRef.current?.contains(t)) {
        close();
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open, close]);

  // Move focus into the panel when it opens.
  useEffect(() => {
    if (open) lengthRef.current?.focus();
  }, [open]);

  const toggleClass = (key: ClassKey) => {
    setClasses((prev) => {
      // Never disable the last enabled class — the alphabet must stay non-empty.
      if (prev[key] && CLASS_CHIPS.filter((c) => prev[c.key]).length === 1) {
        return prev;
      }
      return { ...prev, [key]: !prev[key] };
    });
  };

  const handleGenerate = () => {
    const value = generateSecret({ length, ...classes });
    setGenerated(value);
    onGenerate(value);
    onReveal?.();
    // Announce metadata only — never the characters.
    setAnnouncement(
      `Generated a ${length}-character value, ~${bits} bits of entropy`,
    );
  };

  const handleCopy = async () => {
    if (!generated) return;
    try {
      await navigator.clipboard.writeText(generated);
      showToast('success', 'Copied');
    } catch {
      // Fail quietly — never surface the secret value in an error.
    }
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      // Stop the host (wizard popover / inline edit) from also reacting.
      e.stopPropagation();
      close();
      triggerRef.current?.focus();
    }
  };

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => (open ? close() : setOpen(true))}
        aria-label="Generate value"
        aria-expanded={open}
        aria-controls={panelId}
        title="Generate a secure value"
        className={cn(
          'inline-flex items-center justify-center p-1 rounded text-text-muted transition-colors',
          'hover:text-primary',
          open && 'text-primary',
          className,
        )}
      >
        <Wand2 size={iconSize} />
      </button>

      {open && (
        <div
          ref={panelRef}
          id={panelId}
          onKeyDown={handleKeyDown}
          className="w-full basis-full mt-1 rounded-lg border border-border/60 bg-surface-elevated/60 p-2.5 space-y-2"
        >
          {/* Length */}
          <div className="flex items-center gap-2">
            <span className="w-12 shrink-0 text-[10px] text-text-muted">Length</span>
            <input
              ref={lengthRef}
              type="range"
              min={MIN_LENGTH}
              max={MAX_LENGTH}
              value={length}
              onChange={(e) => setLength(Number(e.target.value))}
              aria-label="Length"
              aria-valuetext={`${length} characters`}
              className="flex-1 accent-primary"
            />
            <span className="w-6 text-right text-[10px] font-mono tabular-nums text-text-primary">
              {length}
            </span>
          </div>

          {/* Character classes */}
          <div
            role="group"
            aria-label="Character classes"
            className="flex flex-wrap gap-1"
          >
            {CLASS_CHIPS.map(({ key, label }) => {
              const enabled = classes[key];
              const locked = enabled && activeCount === 1;
              return (
                <button
                  key={key}
                  type="button"
                  aria-pressed={enabled}
                  disabled={locked}
                  onClick={() => toggleClass(key)}
                  title={locked ? 'At least one class is required' : label}
                  className={cn(
                    'rounded px-2 py-1 text-[10px] font-mono font-medium transition-colors',
                    enabled
                      ? 'bg-primary/20 text-primary'
                      : 'text-text-muted hover:bg-white/[0.04] hover:text-text-primary',
                    locked && 'cursor-not-allowed',
                  )}
                >
                  {label}
                </button>
              );
            })}
          </div>

          {/* Entropy + actions */}
          <div className="flex items-center justify-between gap-2">
            <span className="text-[10px] font-mono text-text-muted">~{bits} bits</span>
            <div className="flex items-center gap-1.5">
              {generated && (
                <button
                  type="button"
                  onClick={handleCopy}
                  title="Copy to clipboard"
                  aria-label="Copy generated value to clipboard"
                  className="p-1 rounded text-text-muted hover:text-text-primary transition-colors"
                >
                  <Copy size={12} />
                </button>
              )}
              <Button type="button" variant="primary" size="sm" onClick={handleGenerate}>
                {generated ? 'Regenerate' : 'Generate'}
              </Button>
            </div>
          </div>

          <div aria-live="polite" className="sr-only">
            {announcement}
          </div>
        </div>
      )}
    </>
  );
}
