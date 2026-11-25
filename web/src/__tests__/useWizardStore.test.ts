import { beforeEach, describe, expect, it } from 'vitest';
import { isWizardDirty, useWizardStore, type WizardDraft } from '../stores/useWizardStore';

function draft(overrides: Partial<WizardDraft> = {}): WizardDraft {
  return {
    id: 'd1',
    name: 'saved',
    resourceType: 'resource',
    formData: { name: 'db', image: 'postgres:16' },
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

function enterYamlMode(type: 'stack' | 'mcp-server' | 'resource' = 'stack') {
  useWizardStore.setState({
    selectedType: type,
    expertMode: true,
    yamlContent: 'name: previous\n',
    yamlError: 'stale',
  });
}

describe('useWizardStore YAML state', () => {
  beforeEach(() => {
    useWizardStore.getState().reset();
  });

  it('loadDraft with yaml opens YAML mode with the saved text', () => {
    enterYamlMode();
    useWizardStore.getState().loadDraft(draft({ yaml: 'name: db\nimage: postgres:16\nrestart: always\n' }));
    const s = useWizardStore.getState();
    expect(s.expertMode).toBe(true);
    expect(s.yamlContent).toBe('name: db\nimage: postgres:16\nrestart: always\n');
    expect(s.yamlError).toBeNull();
    expect(s.selectedType).toBe('resource');
  });

  it('loadDraft without yaml clears YAML mode', () => {
    enterYamlMode();
    useWizardStore.getState().loadDraft(draft());
    const s = useWizardStore.getState();
    expect(s.expertMode).toBe(false);
    expect(s.yamlContent).toBe('');
    expect(s.yamlError).toBeNull();
    expect(s.formData.resource).toEqual({ name: 'db', image: 'postgres:16' });
  });

  it('setSelectedType clears YAML state only on a real type change', () => {
    enterYamlMode('stack');
    useWizardStore.getState().setSelectedType('stack');
    expect(useWizardStore.getState().expertMode).toBe(true);
    expect(useWizardStore.getState().yamlContent).toBe('name: previous\n');

    useWizardStore.getState().setSelectedType('resource');
    const s = useWizardStore.getState();
    expect(s.expertMode).toBe(false);
    expect(s.yamlContent).toBe('');
    expect(s.yamlError).toBeNull();
  });

  it('open with a preset type clears YAML state only on a real type change', () => {
    enterYamlMode('stack');
    useWizardStore.getState().open('stack');
    expect(useWizardStore.getState().expertMode).toBe(true);
    expect(useWizardStore.getState().yamlContent).toBe('name: previous\n');

    useWizardStore.getState().open('mcp-server');
    const s = useWizardStore.getState();
    expect(s.selectedType).toBe('mcp-server');
    expect(s.expertMode).toBe(false);
    expect(s.yamlContent).toBe('');
    expect(s.yamlError).toBeNull();
  });

  it('open without a preset keeps YAML state', () => {
    enterYamlMode('stack');
    useWizardStore.getState().open();
    expect(useWizardStore.getState().expertMode).toBe(true);
  });

  it('persists the YAML reset to session storage', () => {
    enterYamlMode('stack');
    useWizardStore.getState().setSelectedType('resource');
    const saved = JSON.parse(sessionStorage.getItem('gridctl-wizard-state') ?? '{}');
    expect(saved.expertMode).toBe(false);
    expect(saved.yamlContent).toBe('');
  });
});

describe('isWizardDirty', () => {
  beforeEach(() => {
    useWizardStore.getState().reset();
  });

  it('is false after reset', () => {
    expect(isWizardDirty(useWizardStore.getState())).toBe(false);
  });

  it('is true after a form edit', () => {
    useWizardStore.getState().updateFormData('stack', { name: 'demo' });
    expect(isWizardDirty(useWizardStore.getState())).toBe(true);
  });

  it('ignores fields cleared back to undefined', () => {
    useWizardStore.getState().updateFormData('mcp-server', { env: undefined });
    expect(isWizardDirty(useWizardStore.getState())).toBe(false);
  });

  it('is true in YAML mode with content and false with blank content', () => {
    useWizardStore.setState({ expertMode: true, yamlContent: '  \n' });
    expect(isWizardDirty(useWizardStore.getState())).toBe(false);
    useWizardStore.setState({ yamlContent: 'name: x\n' });
    expect(isWizardDirty(useWizardStore.getState())).toBe(true);
  });
});

describe('importInProgress', () => {
  beforeEach(() => {
    useWizardStore.getState().reset();
  });

  it('is cleared by close and reset and never persisted', () => {
    useWizardStore.getState().setImportInProgress(true);
    useWizardStore.getState().setStep('form');
    const saved = JSON.parse(sessionStorage.getItem('gridctl-wizard-state') ?? '{}');
    expect('importInProgress' in saved).toBe(false);

    useWizardStore.getState().close();
    expect(useWizardStore.getState().importInProgress).toBe(false);

    useWizardStore.getState().setImportInProgress(true);
    useWizardStore.getState().reset();
    expect(useWizardStore.getState().importInProgress).toBe(false);
  });
});
