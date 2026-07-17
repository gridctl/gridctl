import { describe, it, expect } from 'vitest';
import { workspaceLayoutStorageKey } from '../hooks/useWorkspaceLayout';

describe('workspaceLayoutStorageKey', () => {
  it('namespaces by workspace, key, and version suffix', () => {
    expect(workspaceLayoutStorageKey('library', 'rails')).toBe(
      'gridctl:layout:library:rails:v1',
    );
    expect(workspaceLayoutStorageKey('stack', 'inspector')).toBe(
      'gridctl:layout:stack:inspector:v1',
    );
  });

  it('keeps workspaces isolated from one another', () => {
    const stack = workspaceLayoutStorageKey('stack', 'rails');
    const library = workspaceLayoutStorageKey('library', 'rails');
    expect(stack).not.toEqual(library);
  });
});
