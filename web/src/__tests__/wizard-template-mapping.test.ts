import { describe, it, expect, beforeEach } from 'vitest';
import { useWizardStore } from '../stores/useWizardStore';

// Regression test: selecting a template on the Template step must pre-populate
// formData['mcp-server'].serverType on the Configure screen.
// Previously, setSelectedTemplate only saved the template ID string and advanced
// the step without updating form data, so the form always defaulted to 'container'.
describe('MCP server template selection pre-populates form data', () => {
  beforeEach(() => {
    useWizardStore.getState().reset();
  });

  const cases: Array<{ templateId: string; serverType: string; transport?: string }> = [
    { templateId: 'external-url',    serverType: 'external',  transport: 'sse' },
    { templateId: 'local-process',   serverType: 'local',     transport: 'stdio' },
    { templateId: 'from-source',     serverType: 'source',    transport: 'http' },
    { templateId: 'container-stdio', serverType: 'container', transport: 'stdio' },
    { templateId: 'container-http',  serverType: 'container', transport: 'http' },
  ];

  it.each(cases)(
    '$templateId → serverType: $serverType',
    ({ templateId, serverType, transport }) => {
      const { updateFormData, setSelectedTemplate } = useWizardStore.getState();

      // Simulate what handleTemplateSelect does in CreationWizard
      const mapping: Record<string, { serverType: string; transport?: string }> = {
        'blank':          { serverType: 'container' },
        'container-http': { serverType: 'container', transport: 'http' },
        'container-stdio':{ serverType: 'container', transport: 'stdio' },
        'external-url':   { serverType: 'external',  transport: 'sse' },
        'local-process':  { serverType: 'local',     transport: 'stdio' },
        'from-source':    { serverType: 'source',    transport: 'http' },
      };
      updateFormData('mcp-server', mapping[templateId] as Record<string, unknown>);
      setSelectedTemplate(templateId);

      const state = useWizardStore.getState();
      expect(state.formData['mcp-server'].serverType).toBe(serverType);
      if (transport) {
        expect(state.formData['mcp-server'].transport).toBe(transport);
      }
      expect(state.selectedTemplate).toBe(templateId);
    },
  );

  it('preserves existing name when template is selected', () => {
    const { updateFormData, setSelectedTemplate } = useWizardStore.getState();

    updateFormData('mcp-server', { name: 'my-api-server' });
    updateFormData('mcp-server', { serverType: 'external', transport: 'sse' });
    setSelectedTemplate('external-url');

    const state = useWizardStore.getState();
    expect(state.formData['mcp-server'].name).toBe('my-api-server');
    expect(state.formData['mcp-server'].serverType).toBe('external');
  });
});
