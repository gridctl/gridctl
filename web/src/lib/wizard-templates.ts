import type { MCPServerFormData, ResourceFormData, StackFormData } from './yaml-builder';

export interface WizardFormDataByType {
  stack: StackFormData;
  'mcp-server': MCPServerFormData;
  resource: ResourceFormData;
}

export type TemplateResourceType = keyof WizardFormDataByType;

export const defaultFormData: WizardFormDataByType = {
  stack: { name: '', version: '1' },
  'mcp-server': { name: '', serverType: 'container' },
  resource: { name: '', image: '' },
};

export interface ResourcePreset {
  id: string;
  name: string;
  data: ResourceFormData;
}

export const RESOURCE_PRESETS: readonly ResourcePreset[] = [
  {
    id: 'postgres',
    name: 'PostgreSQL',
    data: {
      name: 'postgres',
      image: 'postgres:16',
      env: { POSTGRES_DB: 'mydb', POSTGRES_USER: 'user', POSTGRES_PASSWORD: '${var:POSTGRES_PASSWORD}' },
      ports: ['5432:5432'],
    },
  },
  {
    id: 'redis',
    name: 'Redis',
    data: {
      name: 'redis',
      image: 'redis:7-alpine',
      ports: ['6379:6379'],
    },
  },
  {
    id: 'mysql',
    name: 'MySQL',
    data: {
      name: 'mysql',
      image: 'mysql:8',
      env: { MYSQL_DATABASE: 'mydb', MYSQL_ROOT_PASSWORD: '${var:MYSQL_ROOT_PASSWORD}' },
      ports: ['3306:3306'],
    },
  },
  {
    id: 'mongodb',
    name: 'MongoDB',
    data: {
      name: 'mongodb',
      image: 'mongo:7',
      ports: ['27017:27017'],
    },
  },
];

const mcpServerTemplates: Record<string, Partial<MCPServerFormData>> = {
  'blank': {},
  'container-http': { serverType: 'container', transport: 'http' },
  'container-stdio': { serverType: 'container', transport: 'stdio' },
  'external-url': { serverType: 'external', transport: 'sse' },
  'local-process': { serverType: 'local', transport: 'stdio' },
  'from-source': { serverType: 'source', transport: 'http', source: { type: 'git', dockerfile: 'Dockerfile' } },
  'python-package': { serverType: 'source', transport: 'stdio', source: { type: 'pypi', runtime: 'python' } },
};

const stackTemplates: Record<string, Partial<StackFormData>> = {
  'blank': {},
  // No network block: the backend defaults it to <name>-net with the bridge driver.
  'basic': { mcpServers: [{ name: '', serverType: 'container' }] },
};

const resourceTemplates: Record<string, Partial<ResourceFormData>> = {
  'blank': {},
  ...Object.fromEntries(RESOURCE_PRESETS.map((preset) => [preset.id, preset.data])),
};

const templatesByType: { [K in TemplateResourceType]: Record<string, Partial<WizardFormDataByType[K]>> } = {
  stack: stackTemplates,
  'mcp-server': mcpServerTemplates,
  resource: resourceTemplates,
};

function isTemplateType(type: string): type is TemplateResourceType {
  return Object.hasOwn(templatesByType, type);
}

// Returns a deep copy so no caller shares env maps or arrays with the
// definitions above or with another caller.
export function cloneFormData<T>(data: T): T {
  return structuredClone(data);
}

// templateFormData returns the complete form object a template card applies:
// the type's default plus the template's values. Null for an unknown id or a
// type without templates.
export function templateFormData<K extends TemplateResourceType>(type: K, templateId: string): WizardFormDataByType[K] | null;
export function templateFormData(type: string, templateId: string): WizardFormDataByType[TemplateResourceType] | null;
export function templateFormData(type: string, templateId: string): WizardFormDataByType[TemplateResourceType] | null {
  if (!isTemplateType(type)) return null;
  const templates = templatesByType[type] as Record<string, object>;
  if (!Object.hasOwn(templates, templateId)) return null;
  return cloneFormData({ ...defaultFormData[type], ...templates[templateId] }) as WizardFormDataByType[TemplateResourceType];
}
