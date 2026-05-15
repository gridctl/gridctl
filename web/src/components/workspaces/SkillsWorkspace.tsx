// Phase 1 placeholder for /skills. The real Skills workspace lifts Agent IDE
// content into the unified shell in Phase 3.
export function SkillsWorkspace() {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-background">
      <div className="max-w-md text-center space-y-4 p-8 rounded-2xl bg-surface/60 backdrop-blur-xl border border-border/40">
        <div className="text-[10px] uppercase tracking-[0.4em] text-text-muted/60">skills</div>
        <h2 className="text-xl font-semibold text-text-primary">Skills workspace</h2>
        <p className="text-sm text-text-muted leading-relaxed">
          The unified Skills workspace is on its way. Today the Agent IDE still lives at{' '}
          <code className="px-1 py-0.5 rounded bg-surface-elevated/80 text-primary text-xs">/agent</code>{' '}
          and redirects here — full migration into this shell ships in a follow-up.
        </p>
      </div>
    </div>
  );
}

export default SkillsWorkspace;
