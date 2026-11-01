import { useState, useEffect } from 'react';
import { CheckCircle, XCircle, AlertTriangle, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { copyTextToClipboard } from '../../lib/clipboard';

interface ToastAction {
  label: string;
  onClick: () => void;
}

interface Toast {
  id: string;
  type: 'success' | 'error' | 'warning';
  message: string;
  detail?: string;
  action?: ToastAction;
  duration: number;
  persistent: boolean;
  dismissible: boolean;
}

interface ToastOptions {
  action?: ToastAction;
  duration?: number;
  persistent?: boolean;
  /** Defaults to true. Set false to hide the dismiss control. */
  dismissible?: boolean;
  /** Untrimmed server text, shown as the message title on hover. */
  detail?: string;
}

interface TimerRecord {
  remaining: number;
  startedAt: number | null;
  handle: ReturnType<typeof setTimeout> | null;
}

const MAX_VISIBLE = 5;
const DEFAULT_DURATION = 3000;

let toasts: Toast[] = [];
let listeners: (() => void)[] = [];
let nextId = 0;
const timers = new Map<string, TimerRecord>();

function notify() {
  listeners.forEach((l) => l());
}

function clearTimer(id: string) {
  const record = timers.get(id);
  if (record?.handle != null) clearTimeout(record.handle);
  timers.delete(id);
}

function isPersistent(type: Toast['type'], options?: ToastOptions): boolean {
  return options?.persistent === true || Boolean(options?.action) || type === 'error';
}

function removeToast(id: string) {
  clearTimer(id);
  const next = toasts.filter((t) => t.id !== id);
  if (next.length === toasts.length) return;
  toasts = next;
  notify();
}

function startTimer(id: string, duration: number) {
  clearTimer(id);
  const handle = setTimeout(() => removeToast(id), duration);
  timers.set(id, { remaining: duration, startedAt: Date.now(), handle });
}

// A repeat of a toast the user is still hovering or focused must not arm a
// fresh timeout. Reset the budget and leave the pause in place.
function resetTimer(id: string, duration: number) {
  const record = timers.get(id);
  if (record && record.startedAt == null) {
    timers.set(id, { remaining: duration, startedAt: null, handle: null });
    return;
  }
  startTimer(id, duration);
}

function pauseTimer(id: string) {
  const record = timers.get(id);
  if (!record || record.startedAt == null) return;
  const elapsed = Date.now() - record.startedAt;
  const remaining = Math.max(0, record.remaining - elapsed);
  if (record.handle != null) clearTimeout(record.handle);
  timers.set(id, { remaining, startedAt: null, handle: null });
}

function resumeTimer(id: string) {
  const record = timers.get(id);
  if (!record || record.startedAt != null) return;
  if (record.remaining <= 0) {
    removeToast(id);
    return;
  }
  const handle = setTimeout(() => removeToast(id), record.remaining);
  timers.set(id, { remaining: record.remaining, startedAt: Date.now(), handle });
}

// Make room for an incoming toast. Drop the oldest timed toast first, then
// the oldest persistent one. Never drop the toast being added.
function dropOldest(list: Toast[]) {
  const timedIndex = list.findIndex((t) => !t.persistent);
  const index = timedIndex >= 0 ? timedIndex : 0;
  const [removed] = list.splice(index, 1);
  if (removed) clearTimer(removed.id);
}

/** Copy text to the clipboard and toast the outcome. */
// eslint-disable-next-line react-refresh/only-export-components -- imperative toast API lives with its container by design; HMR falls back to a full reload for this module
export async function copyWithToast(text: string, label: string): Promise<void> {
  try {
    await copyTextToClipboard(text);
    showToast('success', `${label} copied`);
  } catch {
    showToast('error', 'Copy failed');
  }
}

// eslint-disable-next-line react-refresh/only-export-components -- imperative toast API lives with its container by design; HMR falls back to a full reload for this module
export function showToast(
  type: 'success' | 'error' | 'warning',
  message: string,
  options?: ToastOptions,
): string {
  const existing = toasts.find((t) => t.type === type && t.message === message);
  if (existing) {
    if (!existing.persistent) resetTimer(existing.id, existing.duration);
    notify();
    return existing.id;
  }

  const id = String(++nextId);
  const persistent = isPersistent(type, options);
  const duration = options?.duration ?? DEFAULT_DURATION;
  const dismissible = options?.dismissible !== false;
  const next = [...toasts];
  while (next.length >= MAX_VISIBLE) dropOldest(next);
  next.push({ id, type, message, detail: options?.detail, action: options?.action, duration, persistent, dismissible });
  toasts = next;
  if (!persistent) startTimer(id, duration);
  notify();
  return id;
}

// eslint-disable-next-line react-refresh/only-export-components -- imperative toast API lives with its container by design; HMR falls back to a full reload for this module
export function dismissToast(id: string) {
  removeToast(id);
}

export function ToastContainer() {
  const [, setTick] = useState(0);

  useEffect(() => {
    const listener = () => setTick((t) => t + 1);
    listeners.push(listener);
    return () => {
      listeners = listeners.filter((l) => l !== listener);
    };
  }, []);

  return (
    <div
      role="region"
      aria-label="Notifications"
      aria-live="polite"
      aria-atomic="false"
      className="fixed bottom-10 right-4 z-toast space-y-2 max-w-sm"
    >
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role={toast.type === 'error' ? 'alert' : 'status'}
          aria-atomic="true"
          onPointerEnter={() => pauseTimer(toast.id)}
          onPointerLeave={() => resumeTimer(toast.id)}
          onFocus={() => pauseTimer(toast.id)}
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
              resumeTimer(toast.id);
            }
          }}
          className={cn(
            'flex items-center gap-2 px-4 py-2.5 rounded-lg text-xs font-medium shadow-lg',
            'glass-panel-elevated animate-slide-in-right max-w-sm',
            toast.type === 'success' && 'text-status-running',
            toast.type === 'error' && 'text-status-error',
            toast.type === 'warning' && 'text-status-pending',
          )}
        >
          {toast.type === 'success' && <CheckCircle size={14} />}
          {toast.type === 'error' && <XCircle size={14} />}
          {toast.type === 'warning' && <AlertTriangle size={14} />}
          <span className="flex-1" title={toast.detail}>{toast.message}</span>
          {toast.action && (
            <button
              type="button"
              onClick={toast.action.onClick}
              className="ml-1 underline underline-offset-2 opacity-80 hover:opacity-100 transition-opacity"
            >
              {toast.action.label}
            </button>
          )}
          {toast.dismissible && (
            <button
              type="button"
              aria-label="Dismiss"
              onClick={() => dismissToast(toast.id)}
              className="ml-1 p-0.5 rounded opacity-70 hover:opacity-100 transition-opacity"
            >
              <X size={12} />
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
