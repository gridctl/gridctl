// Phase 1 placeholder for /runs. The real Runs workspace — filterable grid,
// ancestry tree, live waterfall — ships in Phase 2.
export function RunsWorkspace() {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-background">
      <div className="max-w-md text-center space-y-4 p-8 rounded-2xl bg-surface/60 backdrop-blur-xl border border-border/40">
        <div className="text-[10px] uppercase tracking-[0.4em] text-text-muted/60">runs</div>
        <h2 className="text-xl font-semibold text-text-primary">Runs workspace</h2>
        <p className="text-sm text-text-muted leading-relaxed">
          Live execution observability is coming next — a filterable grid of every skill run, an
          ancestry tree for parent/child chains, and a global trace waterfall in the bottom panel.
        </p>
      </div>
    </div>
  );
}

export default RunsWorkspace;
