// yaml-builder.ts — Form state to YAML serialization
// Converts structured wizard form data into valid YAML strings.
// Never includes raw secrets — only ${vault:KEY} references.

export type ResourceType = 'stack' | 'mcp-server' | 'resource' | 'skill' | 'secret';

export type ServerType = 'container' | 'source' | 'external' | 'local' | 'ssh' | 'openapi';

export interface MCPServerFormData {
  name: string;
  serverType: ServerType;
  // Container
  image?: string;
  port?: number;
  transport?: string;
  command?: string[];
  // Source
  source?: {
    type: string;
    url?: string;
    ref?: string;
    path?: string;
    dockerfile?: string;
  };
  // External
  url?: string;
  // SSH
  ssh?: {
    host: string;
    user: string;
    port?: number;
    identityFile?: string;
    knownHostsFile?: string;
    jumpHost?: string;
  };
  // OpenAPI
  openapi?: {
    spec: string;
    baseUrl?: string;
    auth?: {
      type: string;
      tokenEnv?: string;
      header?: string;
      valueEnv?: string;
      paramName?: string;
      clientIdEnv?: string;
      clientSecretEnv?: string;
      tokenUrl?: string;
      scopes?: string[];
      usernameEnv?: string;
      passwordEnv?: string;
    };
    tls?: {
      certFile?: string;
      keyFile?: string;
      caFile?: string;
      insecureSkipVerify?: boolean;
    };
    operations?: {
      include?: string[];
      exclude?: string[];
    };
  };
  // Common
  env?: Record<string, string>;
  buildArgs?: Record<string, string>;
  tools?: string[];
  outputFormat?: string;
  network?: string;
  pinSchemas?: boolean;
}

export interface ResourceFormData {
  name: string;
  image: string;
  env?: Record<string, string>;
  ports?: string[];
  volumes?: string[];
  network?: string;
}

export interface StackFormData {
  name: string;
  version?: string;
  gateway?: {
    allowedOrigins?: string[];
    auth?: { type: string; token: string; header?: string };
    codeMode?: string;
    codeModeTimeout?: number;
    outputFormat?: string;
    maxToolResultBytes?: number;
    tracing?: {
      enabled?: boolean;
      sampling?: number;
      retention?: string;
      export?: string;
      endpoint?: string;
    };
    security?: {
      schemaPinning?: {
        enabled?: boolean;
        action?: string;
      };
    };
  };
  network?: { name: string; driver: string };
  secrets?: { sets: string[] };
  logging?: {
    file?: string;
    maxSizeMB?: number;
    maxAgeDays?: number;
    maxBackups?: number;
  };
  mcpServers?: MCPServerFormData[];
  resources?: ResourceFormData[];
}

export type WizardFormData =
  | { type: 'stack'; data: StackFormData }
  | { type: 'mcp-server'; data: MCPServerFormData }
  | { type: 'resource'; data: ResourceFormData };

// Serialize a value that might need quoting
function yamlValue(val: string | number | boolean): string {
  if (typeof val === 'number' || typeof val === 'boolean') return String(val);
  if (/^\$\{vault:/.test(val)) return `"${val}"`;
  if (/[:#{}[\],&*?|>!%@`]/.test(val) || val === '' || val === 'true' || val === 'false') {
    return `"${val.replace(/"/g, '\\"')}"`;
  }
  return val;
}

// Serialize a map to YAML key-value pairs
function serializeMap(map: Record<string, string>, indentLevel: number): string {
  const entries = Object.entries(map).filter(([k]) => k);
  if (entries.length === 0) return '';
  return entries.map(([k, v]) => `${' '.repeat(indentLevel)}${k}: ${yamlValue(v)}`).join('\n');
}

// Serialize a string array
function serializeArray(arr: string[], indentLevel: number): string {
  return arr.filter(Boolean).map((v) => `${' '.repeat(indentLevel)}- ${yamlValue(v)}`).join('\n');
}

function buildMCPServer(data: MCPServerFormData, indentLevel = 2): string {
  const pad = ' '.repeat(indentLevel);
  const lines: string[] = [];
  lines.push(`${pad}- name: ${yamlValue(data.name)}`);
  const inner = ' '.repeat(indentLevel + 2);

  switch (data.serverType) {
    case 'container':
      if (data.image) lines.push(`${inner}image: ${yamlValue(data.image)}`);
      if (data.port) lines.push(`${inner}port: ${data.port}`);
      if (data.transport) lines.push(`${inner}transport: ${data.transport}`);
      if (data.command?.length) {
        lines.push(`${inner}command:`);
        data.command.forEach((c) => lines.push(`${inner}  - ${yamlValue(c)}`));
      }
      break;
    case 'source':
      if (data.source) {
        lines.push(`${inner}source:`);
        lines.push(`${inner}  type: ${data.source.type}`);
        if (data.source.url) lines.push(`${inner}  url: ${yamlValue(data.source.url)}`);
        if (data.source.ref) lines.push(`${inner}  ref: ${data.source.ref}`);
        if (data.source.path) lines.push(`${inner}  path: ${data.source.path}`);
        if (data.source.dockerfile) lines.push(`${inner}  dockerfile: ${data.source.dockerfile}`);
      }
      if (data.port) lines.push(`${inner}port: ${data.port}`);
      if (data.transport) lines.push(`${inner}transport: ${data.transport}`);
      break;
    case 'external':
      if (data.url) lines.push(`${inner}url: ${yamlValue(data.url)}`);
      if (data.transport) lines.push(`${inner}transport: ${data.transport}`);
      break;
    case 'local':
      if (data.command?.length) {
        lines.push(`${inner}command:`);
        data.command.forEach((c) => lines.push(`${inner}  - ${yamlValue(c)}`));
      }
      lines.push(`${inner}transport: stdio`);
      break;
    case 'ssh':
      if (data.ssh) {
        lines.push(`${inner}ssh:`);
        lines.push(`${inner}  host: ${data.ssh.host}`);
        lines.push(`${inner}  user: ${data.ssh.user}`);
        if (data.ssh.port && data.ssh.port !== 22) lines.push(`${inner}  port: ${data.ssh.port}`);
        if (data.ssh.identityFile) lines.push(`${inner}  identityFile: ${data.ssh.identityFile}`);
        if (data.ssh.knownHostsFile) lines.push(`${inner}  knownHostsFile: ${data.ssh.knownHostsFile}`);
        if (data.ssh.jumpHost) lines.push(`${inner}  jumpHost: ${data.ssh.jumpHost}`);
      }
      if (data.command?.length) {
        lines.push(`${inner}command:`);
        data.command.forEach((c) => lines.push(`${inner}  - ${yamlValue(c)}`));
      }
      break;
    case 'openapi':
      if (data.openapi) {
        lines.push(`${inner}openapi:`);
        lines.push(`${inner}  spec: ${yamlValue(data.openapi.spec)}`);
        if (data.openapi.baseUrl) lines.push(`${inner}  baseUrl: ${yamlValue(data.openapi.baseUrl)}`);
        if (data.openapi.auth) {
          const auth = data.openapi.auth;
          lines.push(`${inner}  auth:`);
          lines.push(`${inner}    type: ${auth.type}`);
          if (auth.tokenEnv) lines.push(`${inner}    tokenEnv: ${auth.tokenEnv}`);
          if (auth.header) lines.push(`${inner}    header: ${auth.header}`);
          if (auth.valueEnv) lines.push(`${inner}    valueEnv: ${auth.valueEnv}`);
          if (auth.paramName) lines.push(`${inner}    paramName: ${auth.paramName}`);
          if (auth.clientIdEnv) lines.push(`${inner}    clientIdEnv: ${auth.clientIdEnv}`);
          if (auth.clientSecretEnv) lines.push(`${inner}    clientSecretEnv: ${auth.clientSecretEnv}`);
          if (auth.tokenUrl) lines.push(`${inner}    tokenUrl: ${yamlValue(auth.tokenUrl)}`);
          if (auth.scopes?.length) {
            lines.push(`${inner}    scopes:`);
            lines.push(serializeArray(auth.scopes, indentLevel + 6));
          }
          if (auth.usernameEnv) lines.push(`${inner}    usernameEnv: ${auth.usernameEnv}`);
          if (auth.passwordEnv) lines.push(`${inner}    passwordEnv: ${auth.passwordEnv}`);
        }
        if (data.openapi.tls) {
          const tls = data.openapi.tls;
          if (tls.certFile || tls.keyFile || tls.caFile || tls.insecureSkipVerify) {
            lines.push(`${inner}  tls:`);
            if (tls.certFile) lines.push(`${inner}    certFile: ${tls.certFile}`);
            if (tls.keyFile) lines.push(`${inner}    keyFile: ${tls.keyFile}`);
            if (tls.caFile) lines.push(`${inner}    caFile: ${tls.caFile}`);
            if (tls.insecureSkipVerify === true) lines.push(`${inner}    insecureSkipVerify: true`);
          }
        }
      }
      break;
  }

  if (data.env && Object.keys(data.env).length > 0) {
    lines.push(`${inner}env:`);
    lines.push(serializeMap(data.env, indentLevel + 4));
  }
  if (data.buildArgs && Object.keys(data.buildArgs).length > 0) {
    lines.push(`${inner}build_args:`);
    lines.push(serializeMap(data.buildArgs, indentLevel + 4));
  }
  if (data.tools?.length) {
    lines.push(`${inner}tools:`);
    lines.push(serializeArray(data.tools, indentLevel + 4));
  }
  if (data.outputFormat) lines.push(`${inner}output_format: ${data.outputFormat}`);
  if (data.network) lines.push(`${inner}network: ${data.network}`);
  if (data.pinSchemas !== undefined) lines.push(`${inner}pin_schemas: ${data.pinSchemas}`);

  return lines.join('\n');
}

function buildResource(data: ResourceFormData, indentLevel = 2): string {
  const pad = ' '.repeat(indentLevel);
  const inner = ' '.repeat(indentLevel + 2);
  const lines: string[] = [];
  lines.push(`${pad}- name: ${yamlValue(data.name)}`);
  lines.push(`${inner}image: ${yamlValue(data.image)}`);

  if (data.env && Object.keys(data.env).length > 0) {
    lines.push(`${inner}env:`);
    lines.push(serializeMap(data.env, indentLevel + 4));
  }
  if (data.ports?.length) {
    lines.push(`${inner}ports:`);
    lines.push(serializeArray(data.ports, indentLevel + 4));
  }
  if (data.volumes?.length) {
    lines.push(`${inner}volumes:`);
    lines.push(serializeArray(data.volumes, indentLevel + 4));
  }
  if (data.network) lines.push(`${inner}network: ${data.network}`);

  return lines.join('\n');
}

// stripListItem removes the leading "- " from a list-item YAML block and dedents
// all lines by 2 spaces so the result is valid root-level YAML.
function stripListItem(yaml: string): string {
  return yaml
    .replace(/^- /, '')
    .split('\n')
    .map((line) => line.replace(/^  /, ''))
    .join('\n');
}

export function buildYAML(form: WizardFormData): string {
  switch (form.type) {
    case 'mcp-server':
      return stripListItem(buildMCPServer(form.data, 0));
    case 'resource':
      return stripListItem(buildResource(form.data, 0));
    case 'stack':
      return buildStack(form.data);
  }
}

function buildStack(data: StackFormData): string {
  const lines: string[] = [];
  lines.push(`version: "${data.version || '1'}"`);
  lines.push(`name: ${yamlValue(data.name)}`);

  if (data.gateway) {
    lines.push('');
    lines.push('gateway:');
    if (data.gateway.allowedOrigins?.length) {
      lines.push('  allowed_origins:');
      data.gateway.allowedOrigins.forEach((o) => lines.push(`    - ${yamlValue(o)}`));
    }
    if (data.gateway.auth) {
      lines.push('  auth:');
      lines.push(`    type: ${data.gateway.auth.type}`);
      lines.push(`    token: ${yamlValue(data.gateway.auth.token)}`);
      if (data.gateway.auth.header) lines.push(`    header: ${data.gateway.auth.header}`);
    }
    if (data.gateway.codeMode) lines.push(`  code_mode: ${data.gateway.codeMode}`);
    if (data.gateway.codeModeTimeout) lines.push(`  code_mode_timeout: ${data.gateway.codeModeTimeout}`);
    if (data.gateway.outputFormat) lines.push(`  output_format: ${data.gateway.outputFormat}`);
    if (data.gateway.maxToolResultBytes) lines.push(`  maxToolResultBytes: ${data.gateway.maxToolResultBytes}`);

    const gw = data.gateway;
    if (gw.tracing) {
      const t = gw.tracing;
      if (t.enabled !== undefined || t.sampling !== undefined || t.retention || t.export || t.endpoint) {
        lines.push('  tracing:');
        if (t.enabled !== undefined) lines.push(`    enabled: ${t.enabled}`);
        if (t.sampling !== undefined && t.sampling !== 1.0) lines.push(`    sampling: ${t.sampling}`);
        if (t.retention && t.retention !== '24h') lines.push(`    retention: ${t.retention}`);
        if (t.export) lines.push(`    export: ${t.export}`);
        if (t.endpoint) lines.push(`    endpoint: ${yamlValue(t.endpoint)}`);
      }
    }

    if (gw.security?.schemaPinning?.enabled) {
      lines.push('  security:');
      lines.push('    schema_pinning:');
      lines.push(`      enabled: ${gw.security.schemaPinning.enabled}`);
      if (gw.security.schemaPinning.action) {
        lines.push(`      action: ${gw.security.schemaPinning.action}`);
      }
    }
  }

  if (data.secrets?.sets?.length) {
    lines.push('');
    lines.push('secrets:');
    lines.push('  sets:');
    data.secrets.sets.forEach((s) => lines.push(`    - ${yamlValue(s)}`));
  }

  if (data.network) {
    lines.push('');
    lines.push('network:');
    lines.push(`  name: ${data.network.name}`);
    lines.push(`  driver: ${data.network.driver}`);
  }

  if (data.logging?.file) {
    lines.push('');
    lines.push('logging:');
    lines.push(`  file: ${yamlValue(data.logging.file)}`);
    if (data.logging.maxSizeMB) lines.push(`  maxSizeMB: ${data.logging.maxSizeMB}`);
    if (data.logging.maxAgeDays) lines.push(`  maxAgeDays: ${data.logging.maxAgeDays}`);
    if (data.logging.maxBackups) lines.push(`  maxBackups: ${data.logging.maxBackups}`);
  }

  if (data.mcpServers?.length) {
    lines.push('');
    lines.push('mcp-servers:');
    data.mcpServers.forEach((s) => lines.push(buildMCPServer(s, 2)));
  }

  if (data.resources?.length) {
    lines.push('');
    lines.push('resources:');
    data.resources.forEach((r) => lines.push(buildResource(r, 2)));
  }

  return lines.join('\n') + '\n';
}

// Parse YAML string back to form data (best-effort for expert mode)
export function parseYAMLToForm(yaml: string, resourceType: ResourceType): WizardFormData | { error: string } {
  try {
    // Simple line-based YAML parser for the form data we generate
    // This is best-effort — complex YAML structures may not round-trip perfectly
    const lines = yaml.split('\n');
    const result: Record<string, unknown> = {};

    for (const line of lines) {
      const match = line.match(/^(\s*)(\w[\w-]*):\s*(.*)/);
      if (match) {
        const [, , key, value] = match;
        if (value) {
          result[key] = value.replace(/^["']|["']$/g, '');
        }
      }
    }

    // Return minimal parsed data based on type
    switch (resourceType) {
      case 'mcp-server':
        return {
          type: 'mcp-server',
          data: {
            name: (result.name as string) || '',
            serverType: 'container',
            image: result.image as string,
            transport: result.transport as string,
          },
        };
      case 'resource':
        return {
          type: 'resource',
          data: {
            name: (result.name as string) || '',
            image: (result.image as string) || '',
          },
        };
      case 'stack':
        return {
          type: 'stack',
          data: {
            name: (result.name as string) || '',
            version: (result.version as string) || '1',
          },
        };
      default:
        return { error: `Unsupported resource type: ${resourceType}` };
    }
  } catch (e) {
    return { error: e instanceof Error ? e.message : 'Failed to parse YAML' };
  }
}
