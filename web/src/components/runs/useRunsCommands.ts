import { useEffect } from 'react';
import { PlayCircle, AlertCircle, Filter, ExternalLink } from 'lucide-react';
import { createElement } from 'react';
import { useNavigate } from 'react-router-dom';
import { useCommandRegistry } from '../../hooks/useCommandRegistry';
import { useRunsStore } from '../../stores/useRunsStore';
import { showToast } from '../ui/Toast';
import type { PaletteCommand } from '../../types/palette';

/**
 * useRunsCommands registers the workspace-scoped palette commands for
 * the /runs surface. Mounted at AppShell level so the commands are
 * present in every workspace's palette but filtered to `workspaces:
 * ['runs']` — they only appear when the user is on /runs, matching
 * the per-workspace scoping contract documented in Phase 1.
 */
export function useRunsCommands(): void {
  const { registerCommands, unregisterCommands } = useCommandRegistry();
  const navigate = useNavigate();
  const setFilters = useRunsStore((s) => s.setFilters);

  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'runs:filter-errored',
        label: 'Filter runs: errored only',
        section: 'canvas',
        workspaces: ['runs'],
        icon: createElement(AlertCircle, { size: 14 }),
        keywords: ['failed', 'errors', 'red'],
        onSelect: () => {
          setFilters({ status: 'error' });
          navigate('/runs');
        },
      },
      {
        id: 'runs:filter-awaiting-approval',
        label: 'Filter runs: awaiting approval',
        section: 'canvas',
        workspaces: ['runs'],
        icon: createElement(Filter, { size: 14 }),
        keywords: ['paused', 'pending'],
        onSelect: () => {
          setFilters({ status: 'awaiting_approval' });
          navigate('/runs');
        },
      },
      {
        id: 'runs:open-by-id',
        label: 'Open run by ID…',
        section: 'canvas',
        workspaces: ['runs'],
        icon: createElement(ExternalLink, { size: 14 }),
        keywords: ['jump', 'goto'],
        onSelect: () => {
          const id = window.prompt('Run ID');
          if (!id) return;
          const trimmed = id.trim();
          if (!trimmed) return;
          navigate(`/runs/${encodeURIComponent(trimmed)}`);
        },
      },
      {
        id: 'runs:reset-filters',
        label: 'Reset run filters',
        section: 'canvas',
        workspaces: ['runs'],
        icon: createElement(Filter, { size: 14 }),
        keywords: ['clear', 'reset'],
        onSelect: () => {
          useRunsStore.getState().resetFilters();
          showToast('success', 'Filters reset');
        },
      },
      {
        id: 'runs:compare',
        label: 'Compare runs… (coming soon)',
        section: 'canvas',
        workspaces: ['runs'],
        icon: createElement(PlayCircle, { size: 14 }),
        unavailable: true,
        onSelect: () => {
          showToast('error', 'Compare view ships in a follow-up phase');
        },
      },
    ];
    registerCommands('runs', commands);
    return () => unregisterCommands('runs');
  }, [registerCommands, unregisterCommands, navigate, setFilters]);
}
