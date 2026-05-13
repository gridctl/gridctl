import { forwardRef } from 'react';
import { Play } from 'lucide-react';
import { cn } from '../../../lib/cn';

interface SkillRunButtonProps {
  skillName: string;
  onClick: () => void;
  className?: string;
}

/**
 * SkillRunButton is the small ▶ affordance rendered on each row of
 * the SkillSidebar. The button is keyboard-focusable and always-rendered
 * (display, not visibility, is governed by the parent row's :hover /
 * :focus-within state via the `group` Tailwind pattern). Clicks
 * stopPropagation so the row's own onClick (select-skill) does not
 * fire in tandem.
 *
 * We forward the ref so the modal can return focus to this button
 * when it closes — the focus-return requirement from the a11y spec.
 */
export const SkillRunButton = forwardRef<HTMLButtonElement, SkillRunButtonProps>(
  function SkillRunButton({ skillName, onClick, className }, ref) {
    return (
      <button
        ref={ref}
        type="button"
        aria-label={`Run ${skillName}`}
        title={`Run ${skillName}`}
        onClick={(e) => {
          e.stopPropagation();
          onClick();
        }}
        onKeyDown={(e) => {
          // Prevent the parent row's onClick from also firing when the
          // user presses Space/Enter on the button.
          if (e.key === ' ' || e.key === 'Enter') {
            e.stopPropagation();
          }
        }}
        className={cn(
          'inline-flex items-center justify-center w-6 h-6 rounded-md',
          'text-text-muted/60 hover:text-text-primary',
          'border border-transparent hover:border-border-subtle hover:bg-surface-elevated',
          'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus:opacity-100',
          'transition-opacity transition-colors',
          'focus:outline-none focus:ring-1 focus:ring-primary/60',
          className,
        )}
      >
        <Play size={12} className="translate-x-px" aria-hidden />
      </button>
    );
  },
);
