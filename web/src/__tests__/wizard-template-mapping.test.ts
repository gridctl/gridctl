import { describe, it, expect, beforeEach } from 'vitest';
import { useWizardStore } from '../stores/useWizardStore';
import { defaultFormData, RESOURCE_PRESETS, templateFormData } from '../lib/wizard-templates';
import { templatesByType } from '../lib/wizard-template-cards';
import type { MCPServerFormData, ResourceFormData, StackFormData } from '../lib/yaml-builder';

describe('templateFormData', () => {
  const mcpCases: Array<{ templateId: string; serverType: string; transport?: string; source?: unknown }> = [
    { templateId: 'container-http', serverType: 'container', transport: 'http' },
    { templateId: 'container-stdio', serverType: 'container', transport: 'stdio' },
    { templateId: 'external-url', serverType: 'external', transport: 'sse' },
    { templateId: 'local-process', serverType: 'local', transport: 'stdio' },
    { templateId: 'from-source', serverType: 'source', transport: 'http', source: { type: 'git', dockerfile: 'Dockerfile' } },
    { templateId: 'python-package', serverType: 'source', transport: 'stdio', source: { type: 'pypi', runtime: 'python' } },
  ];

  it.each(mcpCases)('$templateId yields serverType $serverType', ({ templateId, serverType, transport, source }) => {
    const data = templateFormData('mcp-server', templateId) as MCPServerFormData;
    expect(data.name).toBe('');
    expect(data.serverType).toBe(serverType);
    expect(data.transport).toBe(transport);
    expect(data.source).toEqual(source);
  });

  it.each(['stack', 'mcp-server', 'resource'] as const)('%s blank equals the default', (type) => {
    expect(templateFormData(type, 'blank')).toEqual(defaultFormData[type]);
  });

  it('postgres yields the preset values', () => {
    const data = templateFormData('resource', 'postgres') as ResourceFormData;
    expect(data.name).toBe('postgres');
    expect(data.image).toBe('postgres:16');
    expect(data.ports).toEqual(['5432:5432']);
    expect(data.env?.POSTGRES_PASSWORD).toBe('${var:POSTGRES_PASSWORD}');
  });

  it.each([
    { id: 'redis', image: 'redis:7-alpine', port: '6379:6379' },
    { id: 'mysql', image: 'mysql:8', port: '3306:3306' },
    { id: 'mongodb', image: 'mongo:7', port: '27017:27017' },
  ])('$id yields its image and ports', ({ id, image, port }) => {
    const data = templateFormData('resource', id) as ResourceFormData;
    expect(data.name).toBe(id);
    expect(data.image).toBe(image);
    expect(data.ports).toEqual([port]);
  });

  it('stack basic yields one container server and no network', () => {
    const data = templateFormData('stack', 'basic') as StackFormData;
    expect(data.mcpServers).toEqual([{ name: '', serverType: 'container' }]);
    expect(data).not.toHaveProperty('network');
    expect(data.version).toBe('1');
  });

  it.each(['stack', 'mcp-server', 'resource'] as const)('covers every %s card in the template grid', (type) => {
    const ids = templatesByType[type].map((t) => t.id);
    expect(ids.length).toBeGreaterThan(0);
    for (const id of ids) {
      expect(templateFormData(type, id), id).not.toBeNull();
    }
  });

  it('returns null for an unknown id or a type without templates', () => {
    expect(templateFormData('resource', 'nope')).toBeNull();
    expect(templateFormData('skill', 'blank')).toBeNull();
    expect(templateFormData('resource', 'toString')).toBeNull();
  });

  it('returns a fresh object on every call', () => {
    const first = templateFormData('resource', 'postgres') as ResourceFormData;
    const second = templateFormData('resource', 'postgres') as ResourceFormData;
    expect(first).not.toBe(second);
    first.env!.POSTGRES_DB = 'changed';
    first.ports!.push('1:1');
    const third = templateFormData('resource', 'postgres') as ResourceFormData;
    expect(third.env?.POSTGRES_DB).toBe('mydb');
    expect(third.ports).toEqual(['5432:5432']);
    expect(RESOURCE_PRESETS[0].data.env?.POSTGRES_DB).toBe('mydb');
    expect(templateFormData('mcp-server', 'blank')).not.toBe(defaultFormData['mcp-server']);
  });

  it('carries no React or icon data in presets', () => {
    for (const preset of RESOURCE_PRESETS) {
      expect(Object.keys(preset).sort()).toEqual(['data', 'id', 'name']);
    }
  });
});

describe('applyTemplate', () => {
  beforeEach(() => {
    useWizardStore.getState().reset();
  });

  it('replaces the form data for the type', () => {
    const { updateFormData, applyTemplate } = useWizardStore.getState();
    updateFormData('mcp-server', {
      name: 'postgres',
      serverType: 'local',
      command: ['npx', 'x'],
      env: { A: 'b' },
      transport: 'stdio',
    });

    applyTemplate('mcp-server', 'blank', templateFormData('mcp-server', 'blank')!);

    const state = useWizardStore.getState();
    expect(state.formData['mcp-server']).toEqual(defaultFormData['mcp-server']);
    expect(state.selectedTemplate).toBe('blank');
    expect(state.currentStep).toBe('form');
  });

  it('clears the name when a template is selected', () => {
    const { updateFormData, applyTemplate } = useWizardStore.getState();
    updateFormData('mcp-server', { name: 'my-api-server' });

    applyTemplate('mcp-server', 'external-url', templateFormData('mcp-server', 'external-url')!);

    const state = useWizardStore.getState();
    expect(state.formData['mcp-server'].name).toBe('');
    expect(state.formData['mcp-server'].serverType).toBe('external');
  });

  it('leaves other types untouched', () => {
    const { updateFormData, applyTemplate } = useWizardStore.getState();
    updateFormData('stack', { name: 'my-stack' });
    const before = useWizardStore.getState().formData.stack;

    applyTemplate('resource', 'redis', templateFormData('resource', 'redis')!);

    const state = useWizardStore.getState();
    expect(state.formData.stack).toBe(before);
    expect(state.formData.stack.name).toBe('my-stack');
    expect(state.formData.resource.image).toBe('redis:7-alpine');
  });

  it('turns expert mode off and clears transient YAML state', () => {
    useWizardStore.setState({
      expertMode: true,
      yamlError: 'bad yaml',
      openapiOperationStats: { total: 3, deleteCount: 1 },
    });

    useWizardStore.getState().applyTemplate('resource', 'mysql', templateFormData('resource', 'mysql')!);

    const state = useWizardStore.getState();
    expect(state.expertMode).toBe(false);
    expect(state.yamlError).toBeNull();
    expect(state.openapiOperationStats).toBeNull();
  });
});
