// The Library's catalog kinds. Skills are the default segment; agents and
// hooks are imported definitions projected to clients; proposals are staged
// changes waiting for review. Lives outside the component file so
// non-component consumers (URL guards, command hooks) can import it without
// tripping fast-refresh's only-export-components rule.
export type RegistryKind = 'skill' | 'agent' | 'hook' | 'pack' | 'proposal';

export function isRegistryKind(value: string | null): value is RegistryKind {
  return value === 'skill' || value === 'agent' || value === 'hook' || value === 'pack' || value === 'proposal';
}
