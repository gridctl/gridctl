import { describe, it, expect, beforeEach } from 'vitest';
import { useUIStore, EDITOR_PREFS_DEFAULTS } from '../stores/useUIStore';

describe('useUIStore editor prefs slice', () => {
  beforeEach(() => {
    useUIStore.setState({ editorPrefs: { ...EDITOR_PREFS_DEFAULTS } });
  });

  it('defaults to a collapsed frontmatter, visible preview, body-heavy split', () => {
    const prefs = useUIStore.getState().editorPrefs;
    expect(prefs.showFrontmatter).toBe(false);
    expect(prefs.showPreview).toBe(true);
    expect(prefs.splitRatio).toBeGreaterThan(0.5);
  });

  it('setEditorPrefs merges partial updates without dropping other keys', () => {
    useUIStore.getState().setEditorPrefs({ showFrontmatter: true });
    expect(useUIStore.getState().editorPrefs).toEqual({
      ...EDITOR_PREFS_DEFAULTS,
      showFrontmatter: true,
    });

    useUIStore.getState().setEditorPrefs({ splitRatio: 0.4 });
    expect(useUIStore.getState().editorPrefs).toEqual({
      ...EDITOR_PREFS_DEFAULTS,
      showFrontmatter: true,
      splitRatio: 0.4,
    });
  });
});
