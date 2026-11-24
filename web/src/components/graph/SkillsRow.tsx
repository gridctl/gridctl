import { useGuardedNavigate } from '../../hooks/useGuardedNavigate';
import { Library, ArrowRight } from 'lucide-react';

// SkillsRow is the gateway node's Skills count, an in-shell link to the
// Library workspace. Lives in its own module so the navigate hook (router context)
// is only required when skills are registered, keeping router-free
// GatewayNode renders (and tests) working. `nodrag` keeps the click a clean
// activation instead of a canvas drag start.
export function SkillsRow({ activeSkills, totalSkills }: { activeSkills: number; totalSkills: number }) {
  const navigate = useGuardedNavigate();

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigate('/library');
  };

  return (
    <button
      type="button"
      className="nodrag flex items-center justify-between group cursor-pointer hover:bg-primary/5 rounded-md transition-colors px-1 -mx-1 w-full text-left"
      onClick={handleClick}
      title="Open skills in the Library"
      aria-label="Open skills in the Library"
    >
      <div className="flex items-center gap-2.5">
        <div className="p-1.5 rounded-lg bg-surface-overlay-2 border border-border-subtle group-hover:bg-primary/15 group-hover:border-primary/20 transition-colors">
          <Library size={12} className="text-text-secondary group-hover:text-primary transition-colors" />
        </div>
        <span className="text-xs text-text-secondary font-medium">Skills</span>
      </div>
      <div className="flex items-center gap-1">
        <span className="text-sm font-bold text-text-primary tabular-nums">
          {activeSkills}<span className="text-text-muted font-normal">/{totalSkills}</span>
        </span>
        <ArrowRight size={9} className="text-text-muted opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 hover-none:opacity-100 transition-opacity" />
      </div>
    </button>
  );
}
