import { describe, it, expect } from 'vitest';
import { workspaceLayoutStorageKey } from '../hooks/useWorkspaceLayout';

describe('workspaceLayoutStorageKey', () => {
  it('namespaces by workspace, key, and version suffix', () => {
    expect(workspaceLayoutStorageKey('skills', 'rails')).toBe(
      'gridctl:layout:skills:rails:v1',
    );
    expect(workspaceLayoutStorageKey('runs', 'rails')).toBe(
      'gridctl:layout:runs:rails:v1',
    );
    expect(workspaceLayoutStorageKey('topology', 'inspector')).toBe(
      'gridctl:layout:topology:inspector:v1',
    );
  });

  it('keeps workspaces isolated from one another', () => {
    const skills = workspaceLayoutStorageKey('skills', 'rails');
    const runs = workspaceLayoutStorageKey('runs', 'rails');
    expect(skills).not.toEqual(runs);
  });
});
