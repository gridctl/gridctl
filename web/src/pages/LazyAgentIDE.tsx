import { lazy, Suspense } from 'react';

// Agent IDE is route-level code-split: it pulls in @xyflow/react and
// the parser-driven hooks only when the operator actually visits
// /agent. Keeps the main bundle below the 900 kB regression budget.
const AgentIDEPage = lazy(() => import('./AgentIDEPage'));

export function LazyAgentIDEPage() {
  return (
    <Suspense
      fallback={
        <div className="h-screen w-screen flex items-center justify-center bg-background text-text-muted font-mono text-xs uppercase tracking-[0.3em]">
          loading agent ide…
        </div>
      }
    >
      <AgentIDEPage />
    </Suspense>
  );
}
