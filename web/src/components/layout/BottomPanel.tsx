import {
  ChevronDown,
  ChevronUp,
  ScrollText,
  BarChart3,
  FileCode2,
  Activity,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { useUIStore } from '../../stores/useUIStore';
import { LogsTab } from '../log/LogsTab';
import { MetricsTab } from '../metrics/MetricsTab';
import { SpecTab } from '../spec/SpecTab';
import { TracesTab } from '../traces/TracesTab';

const TABS = [
  { id: 'logs' as const, label: 'Logs', icon: ScrollText },
  { id: 'metrics' as const, label: 'Metrics', icon: BarChart3 },
  { id: 'spec' as const, label: 'Spec', icon: FileCode2 },
  { id: 'traces' as const, label: 'Traces', icon: Activity },
];

export function BottomPanel() {
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const toggleBottomPanel = useUIStore((s) => s.toggleBottomPanel);
  const bottomPanelTab = useUIStore((s) => s.bottomPanelTab);
  const setBottomPanelTab = useUIStore((s) => s.setBottomPanelTab);
  const logsDetached = useUIStore((s) => s.logsDetached);
  const metricsDetached = useUIStore((s) => s.metricsDetached);
  const tracesDetached = useUIStore((s) => s.tracesDetached);

  return (
    <div
      className={cn(
        'h-full w-full',
        'bg-surface/90 backdrop-blur-xl border-t border-border/50',
        'flex flex-col relative',
        // Skip transition when closing due to detachment
        (logsDetached || metricsDetached || tracesDetached) ? 'duration-0' : 'transition-all duration-300 ease-out'
      )}
    >
      {/* Top accent line */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/20 to-transparent" />

      {/* Header - Always visible */}
      <div className="h-10 flex-shrink-0 flex items-center justify-between px-4">
        {/* Left: collapse toggle + tabs */}
        <div className="flex items-center gap-1">
          <button
            onClick={toggleBottomPanel}
            aria-label={bottomPanelOpen ? 'Collapse panel' : 'Expand panel'}
            className="p-1 rounded-md hover:bg-surface-highlight transition-colors mr-2"
          >
            {bottomPanelOpen ? (
              <ChevronDown size={14} className="text-text-muted" />
            ) : (
              <ChevronUp size={14} className="text-text-muted" />
            )}
          </button>

          {/* Tab buttons */}
          <div role="tablist" aria-label="Bottom panel tabs" className="flex items-center gap-1">
          {TABS.map((tab) => {
            const isActive = bottomPanelTab === tab.id;
            return (
              <button
                key={tab.id}
                role="tab"
                aria-selected={isActive}
                aria-controls={`panel-${tab.id}`}
                onClick={(e) => {
                  e.stopPropagation();
                  setBottomPanelTab(tab.id);
                }}
                className={cn(
                  'flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md transition-colors relative',
                  isActive
                    ? 'text-text-primary bg-surface-highlight/60'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/30'
                )}
              >
                <tab.icon size={12} className={isActive ? 'text-primary' : ''} />
                {tab.label}
                {/* Active indicator */}
                {isActive && (
                  <span className="absolute bottom-0 left-2 right-2 h-0.5 bg-primary rounded-full" />
                )}
              </button>
            );
          })}
          </div>
        </div>
      </div>

      {/* Content - Only visible when panel is open, both tabs rendered to preserve state */}
      {bottomPanelOpen && (
        <div className="flex-1 min-h-0 relative">
          <div id="panel-logs" role="tabpanel" aria-labelledby="tab-logs" className={cn('absolute inset-0', bottomPanelTab !== 'logs' && 'invisible')}>
            <LogsTab />
          </div>
          <div id="panel-metrics" role="tabpanel" aria-labelledby="tab-metrics" className={cn('absolute inset-0', bottomPanelTab !== 'metrics' && 'invisible')}>
            <MetricsTab />
          </div>
          <div id="panel-spec" role="tabpanel" aria-labelledby="tab-spec" className={cn('absolute inset-0', bottomPanelTab !== 'spec' && 'invisible')}>
            <SpecTab />
          </div>
          <div id="panel-traces" role="tabpanel" aria-labelledby="tab-traces" className={cn('absolute inset-0', bottomPanelTab !== 'traces' && 'invisible')}>
            <TracesTab />
          </div>
        </div>
      )}
    </div>
  );
}
