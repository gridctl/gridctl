import { createElement } from 'react';
import {
  Activity,
  BarChart3,
  ClipboardList,
  ListChecks,
  Lock,
  Plug,
  Server,
  ShieldAlert,
  TriangleAlert,
  Users,
  Wrench,
  type LucideIcon,
} from 'lucide-react';
import { MOD_KEY } from './platform';
import { inWorkspace, navigateToWorkspace } from './workspaceNav';
import { useUIStore } from '../stores/useUIStore';
import { WORKSPACE_CONFIG, type Workspace } from '../types/workspace';
import type { PaletteCommand, PaletteSection } from '../types/palette';

// Synonyms for each workspace's "Go to" command. A Record so a new workspace
// does not compile until it has an entry.
export const WORKSPACE_NAV_KEYWORDS: Record<Workspace, string[]> = {
  stack: ['topology', 'workspace', 'operator', 'graph', 'canvas', 'go'],
  library: ['workspace', 'registry', 'catalog', 'skills', 'agents', 'hooks', 'packs', 'go'],
  vault: ['vault', 'secrets', 'keys', 'env', 'workspace', 'go'],
  tools: ['whitelist', 'allowlist', 'audit', 'risk', 'destructive', 'workspace', 'go'],
  metrics: ['stats', 'tokens', 'usage', 'charts', 'dashboard', 'workspace', 'go'],
  pins: ['workspace', 'drift', 'schema', 'tofu', 'approve', 'go'],
  logs: ['output', 'console', 'stream', 'workspace', 'go'],
  traces: ['activity', 'spans', 'calls', 'runs', 'workspace', 'go'],
  connections: ['clients', 'link', 'hub', 'workspace', 'go'],
};

// The palette section that holds each workspace's own commands, shown as
// Current Context.
export const WORKSPACE_PALETTE_SECTION: Record<Workspace, PaletteSection> = {
  stack: 'canvas',
  library: 'registry',
  vault: 'vault',
  tools: 'tools',
  metrics: 'metrics',
  pins: 'pins',
  logs: 'logs',
  traces: 'traces',
  connections: 'connections',
};

// One "Go to <Workspace>" command per WORKSPACE_CONFIG entry, in switcher
// order, with the switcher's icon and shortcut.
export function buildWorkspaceNavCommands(go: (target: string) => void): PaletteCommand[] {
  return WORKSPACE_CONFIG.map((ws) => {
    const aliases = [...new Set([ws.label.toLowerCase(), ws.id])];
    return {
      id: `navigate:workspace-${ws.id}`,
      label: `Go to ${ws.label}`,
      section: 'global',
      icon: createElement(ws.icon, { size: 14 }),
      shortcut: [MOD_KEY, ws.shortcutKey],
      aliases,
      keywords: [...new Set([...aliases, ...WORKSPACE_NAV_KEYWORDS[ws.id]])],
      onSelect: () => go(`/${ws.id}`),
    };
  });
}

export interface ViewCommandContext {
  getLocation: () => { pathname: string; search: string };
  navigate: (to: string, options?: { replace?: boolean }) => void;
}

interface ViewCommandSpec {
  id: string;
  label: string;
  section: PaletteSection;
  workspace: Workspace;
  icon: LucideIcon;
  keywords: string[];
  // Returns the params to land on. `params` is a copy of the current params
  // inside the workspace and empty from elsewhere.
  apply: (params: URLSearchParams, inside: boolean) => URLSearchParams;
}

function withQuery(path: string, params: URLSearchParams): string {
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}

function viewCommand(spec: ViewCommandSpec, ctx: ViewCommandContext): PaletteCommand {
  return {
    id: spec.id,
    label: spec.label,
    section: spec.section,
    icon: createElement(spec.icon, { size: 14 }),
    keywords: spec.keywords,
    onSelect: () => {
      const { pathname, search } = ctx.getLocation();
      const inside = inWorkspace(pathname, spec.workspace);
      const params = spec.apply(new URLSearchParams(inside ? search : ''), inside);
      if (inside) {
        ctx.navigate(withQuery(pathname, params), { replace: true });
        return;
      }
      navigateToWorkspace(
        (to) => ctx.navigate(to),
        pathname,
        withQuery(`/${spec.workspace}`, params),
      );
    },
  };
}

function set(key: string, value: string) {
  return (params: URLSearchParams) => {
    params.set(key, value);
    return params;
  };
}

function remove(key: string) {
  return (params: URLSearchParams) => {
    params.delete(key);
    return params;
  };
}

// Metrics drops the selection and tools filters when the scope changes
// (setScope), so these rebuild the params and keep only the time range.
function metricsScope(scope: 'overview' | 'clients' | 'servers' | 'tools') {
  return (current: URLSearchParams) => {
    const params = new URLSearchParams();
    const range = current.get('range');
    if (range) params.set('range', range);
    if (scope !== 'overview') params.set('scope', scope);
    return params;
  };
}

// Commands that open a workspace view through its URL params. Each matches
// what the workspace's own control writes. Inside the workspace only the
// named params change; from elsewhere the target is built from empty params
// and goes through the leave guard.
export function buildWorkspaceViewCommands(ctx: ViewCommandContext): PaletteCommand[] {
  const specs: ViewCommandSpec[] = [
    {
      id: 'tools:open-audit',
      label: 'Tools: Open Usage Audit',
      section: 'tools',
      workspace: 'tools',
      icon: ClipboardList,
      keywords: ['audit', 'usage', 'unused', 'tools', 'whitelist'],
      apply: set('audit', '1'),
    },
    {
      id: 'tools:show-destructive',
      label: 'Tools: Show Destructive Tools Only',
      section: 'tools',
      workspace: 'tools',
      icon: ShieldAlert,
      keywords: ['destructive', 'risk', 'dangerous', 'filter', 'tools'],
      // Inside Tools this is the risk toggle (applyRisk), which also saves the
      // preference. From elsewhere only the param is written, so a canceled
      // discard prompt changes nothing.
      apply: (params, inside) => {
        if (inside) useUIStore.getState().setToolsPrefs({ destructiveOnly: true });
        params.set('risk', 'destructive');
        return params;
      },
    },
    {
      id: 'tools:show-all-risk',
      label: 'Tools: Show All Tools',
      section: 'tools',
      workspace: 'tools',
      icon: Wrench,
      keywords: ['all', 'risk', 'destructive', 'filter', 'clear', 'tools'],
      // risk=all from elsewhere: a saved destructiveOnly preference would
      // otherwise win on a bare /tools.
      apply: (params, inside) => {
        if (inside) {
          useUIStore.getState().setToolsPrefs({ destructiveOnly: false });
          params.delete('risk');
        } else {
          params.set('risk', 'all');
        }
        return params;
      },
    },
    {
      id: 'metrics:show-overview',
      label: 'Metrics: Show Overview',
      section: 'metrics',
      workspace: 'metrics',
      icon: BarChart3,
      keywords: ['overview', 'scope', 'metrics', 'summary'],
      apply: metricsScope('overview'),
    },
    {
      id: 'metrics:show-clients',
      label: 'Metrics: Show Clients',
      section: 'metrics',
      workspace: 'metrics',
      icon: Users,
      keywords: ['clients', 'scope', 'metrics', 'usage'],
      apply: metricsScope('clients'),
    },
    {
      id: 'metrics:show-servers',
      label: 'Metrics: Show Servers',
      section: 'metrics',
      workspace: 'metrics',
      icon: Server,
      keywords: ['servers', 'scope', 'metrics', 'usage'],
      apply: metricsScope('servers'),
    },
    {
      id: 'metrics:show-tools',
      label: 'Metrics: Show Tools',
      section: 'metrics',
      workspace: 'metrics',
      icon: Wrench,
      keywords: ['tools', 'scope', 'metrics', 'usage'],
      apply: metricsScope('tools'),
    },
    {
      id: 'traces:show-traces',
      label: 'Traces: Show Traces',
      section: 'traces',
      workspace: 'traces',
      icon: Activity,
      keywords: ['traces', 'spans', 'view'],
      apply: remove('view'),
    },
    {
      id: 'traces:show-runs',
      label: 'Traces: Show Runs',
      section: 'traces',
      workspace: 'traces',
      icon: ListChecks,
      keywords: ['runs', 'records', 'dispatch', 'history', 'view'],
      apply: set('view', 'runs'),
    },
    {
      id: 'connections:show-attention',
      label: 'Connections: Show Clients Needing Attention',
      section: 'connections',
      workspace: 'connections',
      icon: TriangleAlert,
      keywords: ['attention', 'clients', 'problems', 'filter', 'connections'],
      apply: set('attention', '1'),
    },
    {
      id: 'connections:show-all',
      label: 'Connections: Show All Clients',
      section: 'connections',
      workspace: 'connections',
      icon: Plug,
      keywords: ['all', 'clients', 'filter', 'clear', 'connections'],
      apply: remove('attention'),
    },
    {
      id: 'vault:show-unreferenced',
      label: 'Variables: Show Not Referenced',
      section: 'vault',
      workspace: 'vault',
      icon: Lock,
      keywords: ['unreferenced', 'unused', 'reference', 'filter', 'variables', 'vault'],
      apply: set('ref', 'none'),
    },
    {
      id: 'vault:show-all',
      label: 'Variables: Show All',
      section: 'vault',
      workspace: 'vault',
      icon: Lock,
      keywords: ['all', 'reference', 'filter', 'clear', 'variables', 'vault'],
      apply: remove('ref'),
    },
  ];
  return specs.map((spec) => viewCommand(spec, ctx));
}
