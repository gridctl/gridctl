export function ShortcutKeys({ keys }: { keys: string[] }) {
  return (
    <span className="flex items-center gap-0.5 shrink-0">
      {keys.map((key, i) => (
        <kbd
          key={`${key}-${i}`}
          className="inline-flex items-center justify-center px-1.5 py-0.5 rounded text-3xs font-mono text-text-muted bg-surface border border-border/50 min-w-[18px]"
        >
          {key}
        </kbd>
      ))}
    </span>
  );
}
