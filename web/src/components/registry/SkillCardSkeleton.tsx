import { memo } from 'react';

export const SkillCardSkeleton = memo(() => {
  return (
    <div className="relative rounded-xl overflow-hidden flex flex-col backdrop-blur-xl border border-white/[0.08] bg-gradient-to-b from-surface/95 via-surface/90 to-primary/[0.02]">
      {/* Top accent line — neutral to match the new muted card resting state */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-white/10 to-transparent" />

      {/* Card body */}
      <div className="p-3 flex flex-col gap-2 flex-1">
        {/* Header row */}
        <div className="flex items-start gap-2">
          <div className="w-7 h-7 rounded-md bg-surface-elevated animate-pulse flex-shrink-0" />
          <div className="flex-1 flex flex-col gap-1.5 mt-0.5">
            <div className="h-4 w-24 rounded bg-surface-elevated animate-pulse" />
          </div>
          <div className="h-4 w-10 rounded bg-surface-elevated animate-pulse flex-shrink-0" />
        </div>

        {/* Description lines */}
        <div className="flex flex-col gap-1.5">
          <div className="h-3 rounded bg-surface-elevated animate-pulse" />
          <div className="h-3 w-3/4 rounded bg-surface-elevated animate-pulse" />
        </div>
      </div>

      {/* Footer */}
      <div className="px-3 pb-3 pt-2 border-t border-border-subtle/50 flex items-center justify-between gap-2">
        <div className="h-4 w-16 rounded bg-surface-elevated animate-pulse" />
        <div className="flex items-center gap-0.5">
          <div className="w-6 h-6 rounded-lg bg-surface-elevated animate-pulse" />
          <div className="w-6 h-6 rounded-lg bg-surface-elevated animate-pulse" />
          <div className="w-6 h-6 rounded-lg bg-surface-elevated animate-pulse" />
        </div>
      </div>
    </div>
  );
});

SkillCardSkeleton.displayName = 'SkillCardSkeleton';
