import { useEffect, useRef, type ReactNode } from 'react';
import { useAuthStore } from '../../stores/useAuthStore';
import { credentialKey, legacyTokenKey } from '../../lib/credentials';
import { AuthPrompt } from './AuthPrompt';

export function AuthBoundary({ children }: { children: ReactNode }) {
  const required = useAuthStore(s => s.authRequired);
  const notice = useAuthStore(s => s.notice);
  const content = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const changed = (event: StorageEvent) => {
      if (event.key === null || event.key === credentialKey || event.key === legacyTokenKey) {
        useAuthStore.getState().storageChanged();
      }
    };
    window.addEventListener('storage', changed);
    return () => window.removeEventListener('storage', changed);
  }, []);
  useEffect(() => {
    if (!required && useAuthStore.getState().isAuthenticated) {
      content.current?.querySelector<HTMLElement>('button:not(:disabled), input, select, a[href], [tabindex="0"]')?.focus();
    }
  }, [required]);
  return <>
    <div ref={content} tabIndex={-1} inert={required} className="contents">{children}</div>
    {required && <AuthPrompt />}
    {!required && notice && <p role="status" className="fixed bottom-8 left-3 right-3 z-[60] p-3 bg-surface border border-border text-sm text-text-primary">{notice}</p>}
  </>;
}
