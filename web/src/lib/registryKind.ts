// The Library's catalog kinds. Skills are the default segment; agents and
// hooks are imported definitions projected to clients. Lives outside the
// component file so non-component consumers (URL guards, command hooks) can
// import it without tripping fast-refresh's only-export-components rule.
export type RegistryKind = 'skill' | 'agent' | 'hook' | 'pack';

export function isRegistryKind(value: string | null): value is RegistryKind {
  return value === 'skill' || value === 'agent' || value === 'hook' || value === 'pack';
}
