import { useState, useEffect } from 'react';
import { CheckCircle, XCircle, AlertTriangle } from 'lucide-react';
import { cn } from '../../lib/cn';

interface ToastAction {
  label: string;
  onClick: () => void;
}

interface Toast {
  id: string;
  type: 'success' | 'error' | 'warning';
  message: string;
  action?: ToastAction;
  duration: number;
}

interface ToastOptions {
  action?: ToastAction;
  duration?: number;
}

let toasts: Toast[] = [];
let listeners: (() => void)[] = [];
let nextId = 0;

export function showToast(type: 'success' | 'error' | 'warning', message: string, options?: ToastOptions) {
  const id = String(++nextId);
  const duration = options?.duration ?? 3000;
  toasts = [...toasts, { id, type, message, action: options?.action, duration }];
  listeners.forEach((l) => l());

  setTimeout(() => {
    toasts = toasts.filter((t) => t.id !== id);
    listeners.forEach((l) => l());
  }, duration);
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
    <div className="fixed bottom-4 right-4 z-50 space-y-2">
      {toasts.map((toast) => (
        <div
          key={toast.id}
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
          <span className="flex-1">{toast.message}</span>
          {toast.action && (
            <button
              onClick={toast.action.onClick}
              className="ml-1 underline underline-offset-2 opacity-80 hover:opacity-100 transition-opacity"
            >
              {toast.action.label}
            </button>
          )}
        </div>
      ))}
    </div>
  );
}
