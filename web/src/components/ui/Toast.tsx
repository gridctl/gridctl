import { useState, useEffect } from 'react';
import { CheckCircle, XCircle } from 'lucide-react';
import { cn } from '../../lib/cn';

interface Toast {
  id: string;
  type: 'success' | 'error';
  message: string;
}

let toasts: Toast[] = [];
let listeners: (() => void)[] = [];
let nextId = 0;

export function showToast(type: 'success' | 'error', message: string) {
  const id = String(++nextId);
  toasts = [...toasts, { id, type, message }];
  listeners.forEach((l) => l());

  setTimeout(() => {
    toasts = toasts.filter((t) => t.id !== id);
    listeners.forEach((l) => l());
  }, 3000);
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
          )}
        >
          {toast.type === 'success' ? (
            <CheckCircle size={14} />
          ) : (
            <XCircle size={14} />
          )}
          {toast.message}
        </div>
      ))}
    </div>
  );
}
