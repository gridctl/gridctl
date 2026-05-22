import { describe, it, expect } from 'vitest';
import { workspaceLayoutStorageKey } from '../hooks/useWorkspaceLayout';

describe('workspaceLayoutStorageKey', () => {
  it('namespaces by workspace, key, and version suffix', () => {
    expect(workspaceLayoutStorageKey('library', 'rails')).toBe(
      'gridctl:layout:library:rails:v1',
    );
    expect(workspaceLayoutStorageKey('topology', 'inspector')).toBe(
      'gridctl:layout:topology:inspector:v1',
    );
  });

  it('keeps workspaces isolated from one another', () => {
    const topology = workspaceLayoutStorageKey('topology', 'rails');
    const library = workspaceLayoutStorageKey('library', 'rails');
    expect(topology).not.toEqual(library);
  });
});
