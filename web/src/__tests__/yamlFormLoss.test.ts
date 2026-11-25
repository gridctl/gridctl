import { describe, expect, it } from 'vitest';
import {
  buildYAML,
  yamlToFormWithLoss,
  type MCPServerFormData,
  type ResourceFormData,
  type StackFormData,
} from '../lib/yaml-builder';

function serverYAML(data: MCPServerFormData): string {
  return buildYAML({ type: 'mcp-server', data });
}

function stackYAML(data: StackFormData): string {
  return buildYAML({ type: 'stack', data });
}

function check(yaml: string, type: 'stack' | 'mcp-server' | 'resource', current: object) {
  const result = yamlToFormWithLoss(yaml, type, current as Record<string, unknown>);
  if ('error' in result) throw new Error(`unexpected error: ${result.error}`);
  return result;
}

const openapiForm: MCPServerFormData = {
  name: 'petstore',
  serverType: 'openapi',
  openapi: {
    spec: 'https://petstore.example.com/openapi.json',
    baseUrl: 'https://petstore.example.com',
    auth: { type: 'bearer', tokenEnv: 'PETSTORE_TOKEN' },
  },
};

const externalForm: MCPServerFormData = {
  name: 'remote',
  serverType: 'external',
  url: 'https://mcp.example.com/sse',
  transport: 'sse',
  auth: { type: 'bearer', token: '${var:REMOTE_TOKEN}' },
};

const sshForm: MCPServerFormData = {
  name: 'remote-shell',
  serverType: 'ssh',
  ssh: { host: 'box.example.com', user: 'ops', identityFile: '~/.ssh/id_ed25519' },
  command: ['mcp-server'],
};

const containerForm: MCPServerFormData = {
  name: 'github',
  serverType: 'container',
  image: 'ghcr.io/example/github-mcp:1.2.3',
  port: 3000,
  transport: 'http',
  env: { LOG_LEVEL: 'info' },
};

const sourceForm: MCPServerFormData = {
  name: 'from-git',
  serverType: 'source',
  transport: 'http',
  port: 8080,
  source: { type: 'git', url: 'https://github.com/example/server', ref: 'main', dockerfile: 'Dockerfile' },
};

const localForm: MCPServerFormData = {
  name: 'local',
  serverType: 'local',
  command: ['npx', '-y', 'some-mcp'],
  env: { DEBUG: '1' },
};

const stackForm: StackFormData = {
  name: 'demo',
  version: '1',
  gateway: {
    codeMode: 'on',
    tracing: { enabled: true, export: 'otlp', endpoint: 'http://localhost:4318' },
  },
  mcpServers: [containerForm],
  resources: [{ name: 'db', image: 'postgres:16', env: { POSTGRES_PASSWORD: '${var:PG}' } }],
};

describe('yamlToFormWithLoss', () => {
  it('reports an edited openapi.baseUrl', () => {
    const yaml = serverYAML(openapiForm).replace(/baseUrl: .*/, 'baseUrl: https://other.example.com');
    expect(yaml).toContain('other.example.com');
    expect(check(yaml, 'mcp-server', openapiForm).lostKeys).toEqual(['openapi.baseUrl']);
  });

  it('handles stack edits to read and unread keys', () => {
    const form: StackFormData = { name: 'demo', version: '1', gateway: { codeMode: 'on' }, mcpServers: [containerForm] };
    const yaml = stackYAML(form);
    expect(check(yaml, 'stack', form).lostKeys).toEqual([]);

    const renamed = check(yaml.replace('name: demo', 'name: renamed'), 'stack', form);
    expect(renamed.lostKeys).toEqual([]);
    expect(renamed.data.name).toBe('renamed');

    expect(check(yaml.replace('code_mode: on', 'code_mode: off'), 'stack', form).lostKeys).toEqual(['gateway.code_mode']);
    expect(
      check(yaml.replace('ghcr.io/example/github-mcp:1.2.3', 'ghcr.io/example/github-mcp:2.0.0'), 'stack', form).lostKeys,
    ).toEqual(['mcp-servers[0].image']);
  });

  it('reports a gateway block added to a stack without one', () => {
    const form: StackFormData = { name: 'demo', version: '1' };
    const yaml = `${stackYAML(form)}\ngateway:\n  code_mode: on\n`;
    expect(check(yaml, 'stack', form).lostKeys).toEqual(['gateway']);
  });

  it('reports edited external auth and ssh hosts', () => {
    const ext = serverYAML(externalForm).replace('${var:REMOTE_TOKEN}', '${var:OTHER_TOKEN}');
    expect(check(ext, 'mcp-server', externalForm).lostKeys).toEqual(['auth.token']);
    const ssh = serverYAML(sshForm).replace('host: box.example.com', 'host: other.example.com');
    expect(check(ssh, 'mcp-server', sshForm).lostKeys).toEqual(['ssh.host']);
  });

  it('reports an unknown resource key', () => {
    const form: ResourceFormData = { name: 'db', image: 'postgres:16' };
    const yaml = `${buildYAML({ type: 'resource', data: form })}\nrestart: always\n`;
    expect(check(yaml, 'resource', form).lostKeys).toEqual(['restart']);
  });

  it.each([
    ['container', containerForm],
    ['source', sourceForm],
    ['external', externalForm],
    ['local', localForm],
    ['ssh', sshForm],
    ['openapi', openapiForm],
  ])('reports nothing for unedited %s builder output', (_label, form) => {
    expect(check(serverYAML(form), 'mcp-server', form).lostKeys).toEqual([]);
  });

  it('reports nothing for an unedited stack with a gateway, servers, and resources', () => {
    expect(check(stackYAML(stackForm), 'stack', stackForm).lostKeys).toEqual([]);
  });

  it('reports nothing for an unedited resource', () => {
    const form: ResourceFormData = { name: 'cache', image: 'redis:7', ports: ['6379:6379'], network: 'bridge' };
    expect(check(buildYAML({ type: 'resource', data: form }), 'resource', form).lostKeys).toEqual([]);
  });

  describe('deletions that the merge would restore', () => {
    it('reports a removed external auth block', () => {
      const yaml = serverYAML(externalForm).replace(/auth:\n {2}type: bearer\n {2}token: "\$\{var:REMOTE_TOKEN\}"\n?/, '');
      expect(yaml).not.toContain('auth:');
      expect(check(yaml, 'mcp-server', externalForm).lostKeys).toEqual(['auth']);
    });

    it('reports a removed gateway block', () => {
      const form: StackFormData = { name: 'demo', version: '1', gateway: { codeMode: 'on' } };
      const yaml = stackYAML(form).replace(/\ngateway:\n {2}code_mode: on\n/, '\n');
      expect(yaml).not.toContain('gateway');
      expect(check(yaml, 'stack', form).lostKeys).toEqual(['gateway']);
    });

    it('reports a removed gateway.tracing block', () => {
      const yaml = stackYAML(stackForm).replace(/ {2}tracing:\n(?: {4}.*\n)+/, '');
      expect(yaml).not.toContain('tracing');
      expect(check(yaml, 'stack', stackForm).lostKeys).toEqual(['gateway.tracing']);
    });

    it('reports a removed resources block', () => {
      const yaml = stackYAML(stackForm).replace(/\nresources:\n[\s\S]*$/, '\n');
      expect(yaml).not.toContain('resources');
      expect(check(yaml, 'stack', stackForm).lostKeys).toEqual(['resources']);
    });

    it('reports a removed container image', () => {
      const yaml = serverYAML(containerForm).replace(/image: .*\n/, '');
      expect(yaml).not.toContain('image');
      expect(check(yaml, 'mcp-server', containerForm).lostKeys).toEqual(['image']);
    });
  });

  describe('deletions that switch', () => {
    it('drops env removed from a local server', () => {
      const yaml = serverYAML(localForm).replace(/env:\n {2}DEBUG: .*\n?/, '');
      const result = check(yaml, 'mcp-server', localForm);
      expect(result.lostKeys).toEqual([]);
      expect(result.data.env).toBeUndefined();
    });

    it('switches type when the openapi block is removed', () => {
      const yaml = 'name: petstore\n';
      const result = check(yaml, 'mcp-server', openapiForm);
      expect(result.lostKeys).toEqual([]);
      expect(result.data.serverType).not.toBe('openapi');
    });

    it('switches type when the ssh block is removed', () => {
      const yaml = serverYAML(sshForm).replace(/ssh:\n(?: {2}.*\n)+/, '');
      expect(yaml).not.toContain('ssh:');
      const result = check(yaml, 'mcp-server', sshForm);
      expect(result.lostKeys).toEqual([]);
      expect(result.data.serverType).not.toBe('ssh');
    });
  });

  it('does not report builder defaults the YAML omits', () => {
    const form: MCPServerFormData = { name: 'local', serverType: 'local', command: ['run-me'] };
    const local = serverYAML(form).replace(/transport: stdio\n?/, '');
    expect(local).not.toContain('transport');
    expect(check(local, 'mcp-server', form).lostKeys).toEqual([]);

    const stack = stackYAML(stackForm).replace(/version: "1"\n/, '');
    expect(stack).not.toContain('version');
    expect(check(stack, 'stack', stackForm).lostKeys).toEqual([]);
  });

  it('returns a parse error unchanged', () => {
    const result = yamlToFormWithLoss('name: [unclosed', 'stack', { name: '' });
    expect('error' in result).toBe(true);
  });
});
