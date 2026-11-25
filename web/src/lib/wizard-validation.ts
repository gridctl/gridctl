import type { MCPServerFormData, ResourceFormData, ServerType, StackFormData } from './yaml-builder';

// Client-side checks for the Configure step. They mirror only the "is
// required" and name-uniqueness rules in pkg/config/validate.go so the wizard
// can never be stricter than the server, which stays authoritative.

export type WizardErrors = Record<string, string>;

export interface NameContext {
  serverNames: string[];
  resourceNames: string[];
}

export const TRANSPORT_DEFAULTS: Record<ServerType, string> = {
  container: 'http',
  source: 'http',
  external: 'http',
  local: 'stdio',
  ssh: 'stdio',
  openapi: '',
};

export function showPortField(serverType: ServerType, transport: string): boolean {
  if (serverType === 'external' || serverType === 'local' || serverType === 'ssh' || serverType === 'openapi') return false;
  return transport !== 'stdio';
}

const FIELD_LABELS: Record<string, string> = {
  name: 'Name',
  image: 'Image',
  url: 'URL',
  port: 'Port',
  command: 'Command',
  'source.package': 'Project name',
  'source.ref': 'Exact version',
  'source.url': 'Repository URL',
  'source.path': 'Local root',
  'ssh.host': 'Host',
  'ssh.user': 'User',
  'openapi.spec': 'Spec',
  'auth.token': 'Token',
  'auth.header': 'Header name',
  'auth.value': 'Value',
  'gateway.auth.token': 'Gateway token',
};

function isEmpty(value: string | undefined | null): boolean {
  return value == null || value.trim() === '';
}

function required(errors: WizardErrors, key: string, value: string | undefined | null) {
  if (isEmpty(value)) errors[key] = `${FIELD_LABELS[key]} is required`;
}

function serverRequiredErrors(data: MCPServerFormData): WizardErrors {
  const errors: WizardErrors = {};
  required(errors, 'name', data.name);

  switch (data.serverType) {
    case 'container':
      required(errors, 'image', data.image);
      break;
    case 'source': {
      const source = data.source ?? { type: 'git' };
      if (source.type === 'pypi') {
        required(errors, 'source.package', source.package);
        required(errors, 'source.ref', source.ref);
      } else if (source.type === 'git') {
        required(errors, 'source.url', source.url);
      } else {
        required(errors, 'source.path', source.path);
      }
      break;
    }
    case 'external':
      required(errors, 'url', data.url);
      if (data.auth?.type === 'bearer') {
        required(errors, 'auth.token', data.auth.token);
      } else if (data.auth?.type === 'header') {
        required(errors, 'auth.header', data.auth.header);
        required(errors, 'auth.value', data.auth.value);
      }
      break;
    case 'local':
      required(errors, 'command', data.command?.[0]);
      break;
    case 'ssh':
      required(errors, 'ssh.host', data.ssh?.host);
      required(errors, 'ssh.user', data.ssh?.user);
      required(errors, 'command', data.command?.[0]);
      break;
    case 'openapi':
      required(errors, 'openapi.spec', data.openapi?.spec);
      break;
  }

  const transport = data.transport ?? TRANSPORT_DEFAULTS[data.serverType];
  if ((data.serverType === 'container' || data.serverType === 'source') && showPortField(data.serverType, transport)) {
    const port = data.port;
    if (port === undefined || port === null || Number.isNaN(port)) {
      errors.port = 'Port is required';
    } else if (!Number.isInteger(port) || port < 1 || port > 65535) {
      errors.port = 'Port must be a whole number from 1 to 65535';
    }
  }
  return errors;
}

function collision(name: string, ctx: NameContext): string | undefined {
  if (ctx.serverNames.includes(name)) return `An MCP server named '${name}' already exists in this stack`;
  if (ctx.resourceNames.includes(name)) return `A resource named '${name}' already exists in this stack`;
  return undefined;
}

export function validateMCPServer(data: MCPServerFormData, ctx: NameContext): WizardErrors {
  const errors = serverRequiredErrors(data);
  if (!errors.name) {
    const clash = collision(data.name, ctx);
    if (clash) errors.name = clash;
  }
  return errors;
}

function resourceRequiredErrors(data: ResourceFormData): WizardErrors {
  const errors: WizardErrors = {};
  required(errors, 'name', data.name);
  required(errors, 'image', data.image);
  return errors;
}

export function validateResource(data: ResourceFormData, ctx: NameContext): WizardErrors {
  const errors = resourceRequiredErrors(data);
  if (!errors.name) {
    const clash = collision(data.name, ctx);
    if (clash) errors.name = clash;
  }
  return errors;
}

export function validateStack(data: StackFormData): WizardErrors {
  const errors: WizardErrors = {};
  required(errors, 'name', data.name);
  if (data.gateway?.auth?.type) required(errors, 'gateway.auth.token', data.gateway.auth.token);

  const servers = data.mcpServers ?? [];
  const serverNames = new Set<string>();
  servers.forEach((server, i) => {
    const itemErrors = serverRequiredErrors(server);
    if (!itemErrors.name) {
      if (serverNames.has(server.name)) {
        itemErrors.name = `Another MCP server in this stack is named '${server.name}'`;
      } else {
        serverNames.add(server.name);
      }
    }
    for (const [key, message] of Object.entries(itemErrors)) errors[`mcpServers.${i}.${key}`] = message;
  });

  const resourceNames = new Set<string>();
  (data.resources ?? []).forEach((resource, i) => {
    const itemErrors = resourceRequiredErrors(resource);
    if (!itemErrors.name) {
      if (resourceNames.has(resource.name)) {
        itemErrors.name = `Another resource in this stack is named '${resource.name}'`;
      } else if (serverNames.has(resource.name)) {
        itemErrors.name = `An MCP server in this stack is named '${resource.name}'`;
      } else {
        resourceNames.add(resource.name);
      }
    }
    for (const [key, message] of Object.entries(itemErrors)) errors[`resources.${i}.${key}`] = message;
  });
  return errors;
}

// fieldLabel names an error key for the Next reason; nested stack items carry
// their position, for example "MCP server 2: Image".
export function fieldLabel(key: string): string {
  const nested = /^(mcpServers|resources)\.(\d+)\.(.+)$/.exec(key);
  if (nested) {
    const kind = nested[1] === 'mcpServers' ? 'MCP server' : 'Resource';
    return `${kind} ${Number(nested[2]) + 1}: ${FIELD_LABELS[nested[3]] ?? nested[3]}`;
  }
  return FIELD_LABELS[key] ?? key;
}

function itemPrefix(key: string): string {
  const label = fieldLabel(key);
  const colon = label.indexOf(': ');
  return colon === -1 ? '' : label.slice(0, colon + 2);
}

const REASON_LABELS_SHOWN = 5;

// formatNextReason explains why Next does not advance: the message itself for
// one error, otherwise a count and the affected field labels.
export function formatNextReason(errors: WizardErrors): string {
  const keys = Object.keys(errors);
  if (keys.length === 0) return '';
  if (keys.length === 1) return `${itemPrefix(keys[0])}${errors[keys[0]]}`;
  const shown = keys.slice(0, REASON_LABELS_SHOWN).map(fieldLabel).join(', ');
  const more = keys.length > REASON_LABELS_SHOWN ? `, and ${keys.length - REASON_LABELS_SHOWN} more` : '';
  return `${keys.length} fields need attention: ${shown}${more}`;
}

// subErrors returns the errors under a key prefix with the prefix removed.
export function subErrors(errors: WizardErrors | undefined, prefix: string): WizardErrors | undefined {
  if (!errors) return undefined;
  const out: WizardErrors = {};
  for (const [key, message] of Object.entries(errors)) {
    if (key.startsWith(prefix)) out[key.slice(prefix.length)] = message;
  }
  return out;
}
