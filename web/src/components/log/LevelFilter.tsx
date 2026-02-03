import { useEffect, useRef, useState } from 'react';
import { Filter, Check } from 'lucide-react';
import { cn } from '../../lib/cn';
import { LOG_LEVELS, LEVEL_STYLES, type LogLevel } from './logTypes';

export function LevelFilter({
  enabledLevels,
  onToggle,
}: {
  enabledLevels: Set<LogLevel>;
  onToggle: (level: LogLevel) => void;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const activeCount = enabledLevels.size;

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        className={cn(
          'flex items-center gap-1.5 px-2 py-1 rounded-md text-xs',
          'border transition-all duration-200',
          activeCount < 4
            ? 'bg-primary/10 border-primary/30 text-primary'
            : 'bg-surface-elevated/60 border-border/50 text-text-muted hover:text-text-primary hover:border-text-muted/30'
        )}
      >
        <Filter size={12} />
        <span>Level</span>
        {activeCount < 4 && (
          <span className="px-1.5 py-0.5 bg-primary/20 rounded text-[10px] font-medium">
            {activeCount}
          </span>
        )}
      </button>

      {isOpen && (
        <div className="absolute top-full left-0 mt-1 z-50 min-w-[140px] py-1 rounded-lg bg-surface-elevated border border-border/50 shadow-xl backdrop-blur-xl">
          {LOG_LEVELS.map((level) => {
            const enabled = enabledLevels.has(level);
            const styles = LEVEL_STYLES[level];
            return (
              <button
                key={level}
                onClick={() => onToggle(level)}
                className={cn(
                  'w-full flex items-center gap-2 px-3 py-1.5 text-xs transition-colors',
                  'hover:bg-surface-highlight',
                  enabled ? styles.text : 'text-text-muted'
                )}
              >
                <span
                  className={cn(
                    'w-4 h-4 rounded flex items-center justify-center border',
                    enabled ? `${styles.bg} ${styles.border}` : 'border-border/50'
                  )}
                >
                  {enabled && <Check size={10} />}
                </span>
                <span className={cn('w-2 h-2 rounded-full', styles.dot)} />
                <span className="font-medium">{level}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
