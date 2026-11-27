import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { isWizardDirty, useWizardStore, type WizardDraft } from '../stores/useWizardStore';
// Registers the yaml module the store masks expert-mode YAML with, as the
// wizard does when it loads.
import '../lib/wizard-secrets';

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

describe('appendedResource', () => {
  const record = { resourceType: 'mcp-server', name: 'github', yaml: 'name: github\nimage: alpine\n' };

  beforeEach(() => {
    useWizardStore.getState().reset();
    useWizardStore.getState().setAppendInFlight(false);
  });

  it('is cleared by reset', () => {
    useWizardStore.getState().setAppendedResource(record);
    useWizardStore.getState().reset();
    expect(useWizardStore.getState().appendedResource).toBeNull();
  });

  it('is cleared by loadDraft', () => {
    useWizardStore.getState().setAppendedResource(record);
    useWizardStore.getState().loadDraft(draft());
    expect(useWizardStore.getState().appendedResource).toBeNull();
  });

  it('is kept by close', () => {
    useWizardStore.getState().setAppendedResource(record);
    useWizardStore.getState().close();
    expect(useWizardStore.getState().appendedResource).toEqual(record);
  });

  it('persists the record but never the in-flight flag', () => {
    useWizardStore.getState().setAppendInFlight(true);
    useWizardStore.getState().setAppendedResource(record);
    useWizardStore.getState().close();
    const saved = JSON.parse(sessionStorage.getItem('gridctl-wizard-state') ?? '{}');
    expect(saved.appendedResource).toEqual(record);
    expect('appendInFlight' in saved).toBe(false);
  });
});

describe('appendedResource session restore', () => {
  afterEach(() => {
    sessionStorage.clear();
    vi.resetModules();
  });

  async function loadWith(appendedResource: unknown) {
    sessionStorage.setItem('gridctl-wizard-state', JSON.stringify({ isOpen: true, currentStep: 'review', appendedResource }));
    vi.resetModules();
    const mod = await import('../stores/useWizardStore');
    return mod.useWizardStore.getState();
  }

  it('restores a well-formed record', async () => {
    const record = { resourceType: 'mcp-server', name: 'github', yaml: 'name: github\n' };
    const state = await loadWith(record);
    expect(state.appendedResource).toEqual(record);
    expect(state.appendInFlight).toBe(false);
  });

  it.each([
    ['null', null],
    ['a string', 'github'],
    ['a missing field', { resourceType: 'mcp-server', name: 'github' }],
    ['a non-string field', { resourceType: 'mcp-server', name: 1, yaml: 'name: x\n' }],
  ])('treats %s as no record', async (_label, value) => {
    expect((await loadWith(value)).appendedResource).toBeNull();
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

describe('session redaction', () => {
  const KEY = 'gridctl-wizard-state';
  const stored = () => sessionStorage.getItem(KEY) ?? '';
  const saved = () => JSON.parse(stored() || '{}');

  beforeEach(() => {
    useWizardStore.getState().reset();
    sessionStorage.clear();
  });

  function enterLiteral(token: string) {
    useWizardStore.getState().updateFormData('mcp-server', {
      serverType: 'external',
      auth: { type: 'bearer', token },
    });
    useWizardStore.getState().setYamlContent(`auth:\n  type: bearer\n  token: ${token}\n`);
  }

  it('keeps literals in memory but not in session storage', () => {
    enterLiteral('sk-literal-123');
    expect(stored()).not.toContain('sk-literal-123');
    const auth = useWizardStore.getState().formData['mcp-server'].auth;
    expect(auth?.token).toBe('sk-literal-123');
    expect('isOpen' in saved()).toBe(false);
    expect(saved().secretsRedacted).toBe(true);
    expect(saved().yamlContent).toBe('');
  });

  it('stores references and does not flag redaction', () => {
    enterLiteral('${var:TOKEN}');
    expect(saved().formData['mcp-server'].auth.token).toBe('${var:TOKEN}');
    expect(saved().secretsRedacted).toBe(false);
  });

  it('redacts gateway, header, client secret, and token env literals', () => {
    useWizardStore.getState().updateFormData('stack', { gateway: { auth: { type: 'bearer', token: 'gw-lit' } } });
    useWizardStore.getState().updateFormData('mcp-server', {
      serverType: 'external',
      auth: { type: 'oauth', clientSecret: 'cs-lit' },
      headers: { 'X-Key': 'hdr-lit' },
      env: { GITHUB_TOKEN: 'env-lit' },
    });
    for (const literal of ['gw-lit', 'cs-lit', 'hdr-lit', 'env-lit']) expect(stored()).not.toContain(literal);
  });

  it('masks literals in expert-mode YAML', () => {
    useWizardStore.getState().setExpertMode(true);
    useWizardStore.getState().setYamlContent('name: x\nauth:\n  type: bearer\n  token: sk-literal-123\n');
    expect(stored()).not.toContain('sk-literal-123');
    expect(saved().yamlContent).toBe('name: x\nauth:\n  type: bearer\n  token: ""\n');
    expect(saved().secretsRedacted).toBe(true);
  });

  it('keeps the last safe YAML when expert-mode YAML does not parse', () => {
    useWizardStore.getState().setExpertMode(true);
    useWizardStore.getState().setYamlContent('name: safe\n');
    useWizardStore.getState().setYamlContent('name: x\nauth:\n  token: sk-literal-123\n  bad: [\n');
    expect(stored()).not.toContain('sk-literal-123');
    expect(saved().yamlContent).toBe('name: safe\n');
    expect(saved().secretsRedacted).toBe(true);
  });

  it('masks literals in the stored appended-resource YAML', () => {
    useWizardStore.getState().setAppendedResource({
      resourceType: 'mcp-server',
      name: 'remote',
      yaml: 'name: remote\nauth:\n  type: bearer\n  token: sk-literal-123\n',
    });
    expect(stored()).not.toContain('sk-literal-123');
    expect(useWizardStore.getState().appendedResource?.yaml).toContain('sk-literal-123');
  });
});

describe('session restore', () => {
  afterEach(() => {
    sessionStorage.clear();
    vi.resetModules();
  });

  async function restore(seed: (store: typeof useWizardStore) => void) {
    sessionStorage.clear();
    vi.resetModules();
    const first = (await import('../stores/useWizardStore')).useWizardStore;
    first.getState().open();
    seed(first);
    vi.resetModules();
    return (await import('../stores/useWizardStore')).useWizardStore;
  }

  it('starts closed and resumes the form without literals, with a notice flag', async () => {
    const restored = await restore((store) => {
      store.getState().setSelectedType('mcp-server');
      store.getState().setSelectedTemplate(null);
      store.getState().updateFormData('mcp-server', { serverType: 'external', auth: { type: 'bearer', token: 'sk-literal-123' } });
    });
    const s = restored.getState();
    expect(s.isOpen).toBe(false);
    expect(s.currentStep).toBe('form');
    expect(s.selectedType).toBe('mcp-server');
    expect(s.formData['mcp-server'].auth?.token).toBe('');
    expect(s.secretsRedacted).toBe(true);

    s.open();
    expect(restored.getState().currentStep).toBe('form');
    restored.getState().dismissSecretsRedacted();
    expect(restored.getState().secretsRedacted).toBe(false);
  });

  it('restores references and shows no notice', async () => {
    const restored = await restore((store) => {
      store.getState().updateFormData('mcp-server', { serverType: 'external', auth: { type: 'bearer', token: '${var:TOKEN}' } });
    });
    expect(restored.getState().formData['mcp-server'].auth?.token).toBe('${var:TOKEN}');
    expect(restored.getState().secretsRedacted).toBe(false);
  });

  it('carries stored YAML state over when reopened before the yaml module loads', async () => {
    const record = { resourceType: 'mcp-server', name: 'remote', yaml: 'name: remote\nauth:\n  token: "${var:T}"\n' };
    sessionStorage.setItem('gridctl-wizard-state', JSON.stringify({
      currentStep: 'form',
      selectedType: 'mcp-server',
      expertMode: true,
      yamlContent: 'name: x\nauth:\n  token: "${var:T}"\n',
      appendedResource: record,
      secretsRedacted: false,
    }));
    vi.resetModules();
    const { useWizardStore: store } = await import('../stores/useWizardStore');
    store.getState().open();
    const saved = JSON.parse(sessionStorage.getItem('gridctl-wizard-state') ?? '{}');
    expect(saved.secretsRedacted).toBe(false);
    expect(saved.appendedResource).toEqual(record);
    expect(saved.yamlContent).toBe('name: x\nauth:\n  token: "${var:T}"\n');
  });

  it('ignores a stored isOpen from an earlier release', async () => {
    sessionStorage.setItem('gridctl-wizard-state', JSON.stringify({ isOpen: true, currentStep: 'form' }));
    vi.resetModules();
    const { useWizardStore: store } = await import('../stores/useWizardStore');
    expect(store.getState().isOpen).toBe(false);
    expect(store.getState().currentStep).toBe('form');
  });

  it('is cleared by reset and loadDraft', async () => {
    sessionStorage.setItem('gridctl-wizard-state', JSON.stringify({ secretsRedacted: true }));
    vi.resetModules();
    const { useWizardStore: store } = await import('../stores/useWizardStore');
    expect(store.getState().secretsRedacted).toBe(true);
    store.getState().reset();
    expect(store.getState().secretsRedacted).toBe(false);
    store.setState({ secretsRedacted: true });
    store.getState().loadDraft(draft());
    expect(store.getState().secretsRedacted).toBe(false);
  });
});
