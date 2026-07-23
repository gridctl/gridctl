// Suspense fallback rendered inside <AppShell> while a workspace chunk is
// being fetched. The chrome (Header/StatusBar) is already
// painted, so this only fills the main content area.
export function WorkspaceLoadingShell({ label = 'loading workspace…' }: { label?: string }) {
  return (
    <div className="h-full w-full flex items-center justify-center bg-background">
      <div className="text-center space-y-3">
        <div className="relative mx-auto w-10 h-10">
          <div className="absolute inset-0 rounded-full border-2 border-primary/20" />
          <div className="absolute inset-0 rounded-full border-2 border-primary border-t-transparent animate-spin" />
        </div>
        <p className="text-xs uppercase tracking-[0.3em] text-text-muted">{label}</p>
      </div>
    </div>
  );
}
