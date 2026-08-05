// The Library's catalog kinds. Skills are the default segment; agents are
// imported definitions projected to clients. Lives outside the component
// file so non-component consumers (URL guards, command hooks) can import
// it without tripping fast-refresh's only-export-components rule.
export type RegistryKind = 'skill' | 'agent' | 'pack';

export function isRegistryKind(value: string | null): value is RegistryKind {
  return value === 'skill' || value === 'agent' || value === 'pack';
}
