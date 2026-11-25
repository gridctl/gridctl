import { describe, it, expect } from 'vitest';
import {
  fieldLabel,
  formatNextReason,
  showPortField,
  subErrors,
  validateMCPServer,
  validateResource,
  validateStack,
} from '../lib/wizard-validation';
import type { MCPServerFormData } from '../lib/yaml-builder';

const none = { serverNames: [], resourceNames: [] };

function keys(errors: Record<string, string>): string[] {
  return Object.keys(errors).sort();
}

describe('validateMCPServer required fields', () => {
  it('reports name, image, and port for a blank container server', () => {
    const errors = validateMCPServer({ name: '', serverType: 'container' }, none);
    expect(keys(errors)).toEqual(['image', 'name', 'port']);
    expect(errors.name).toBe('Name is required');
    expect(errors.image).toBe('Image is required');
    expect(errors.port).toBe('Port is required');
  });

  it('does not report port for a stdio container', () => {
    const errors = validateMCPServer({ name: '', serverType: 'container', transport: 'stdio' }, none);
    expect(keys(errors)).toEqual(['image', 'name']);
  });

  it('rejects a port outside 1 to 65535', () => {
    const base: MCPServerFormData = { name: 'a', serverType: 'container', image: 'x' };
    expect(validateMCPServer({ ...base, port: 0 }, none).port).toMatch(/1 to 65535/);
    expect(validateMCPServer({ ...base, port: 70000 }, none).port).toMatch(/1 to 65535/);
    expect(validateMCPServer({ ...base, port: 8080 }, none)).toEqual({});
  });

  it.each<[string, MCPServerFormData, string[]]>([
    ['external', { name: 'a', serverType: 'external', transport: 'http' }, ['url']],
    ['local', { name: 'a', serverType: 'local', transport: 'stdio' }, ['command']],
    ['local with an empty first element', { name: 'a', serverType: 'local', command: ['', 'arg'] }, ['command']],
    ['ssh', { name: 'a', serverType: 'ssh' }, ['command', 'ssh.host', 'ssh.user']],
    ['openapi', { name: 'a', serverType: 'openapi' }, ['openapi.spec']],
    ['source with no source block (git)', { name: 'a', serverType: 'source' }, ['port', 'source.url']],
    ['source git', { name: 'a', serverType: 'source', port: 8080, source: { type: 'git' } }, ['source.url']],
    ['source local', { name: 'a', serverType: 'source', port: 8080, source: { type: 'local' } }, ['source.path']],
    ['source pypi', { name: 'a', serverType: 'source', transport: 'stdio', source: { type: 'pypi' } }, ['source.package', 'source.ref']],
  ])('reports exactly the required set for %s', (_label, data, expected) => {
    expect(keys(validateMCPServer(data, none))).toEqual(expected);
  });

  it('reports auth.token for external bearer auth without a token', () => {
    const errors = validateMCPServer(
      { name: 'a', serverType: 'external', url: 'https://x', auth: { type: 'bearer' } },
      none,
    );
    expect(keys(errors)).toEqual(['auth.token']);
  });

  it('reports auth.header and auth.value for external header auth', () => {
    const errors = validateMCPServer(
      { name: 'a', serverType: 'external', url: 'https://x', auth: { type: 'header' } },
      none,
    );
    expect(keys(errors)).toEqual(['auth.header', 'auth.value']);
  });

  it('requires nothing extra for OAuth', () => {
    expect(validateMCPServer({ name: 'a', serverType: 'external', url: 'https://x', auth: { type: 'oauth' } }, none)).toEqual({});
  });

  it('treats whitespace-only values as empty', () => {
    const errors = validateMCPServer({ name: '   ', serverType: 'container', image: ' \t', port: 80 }, none);
    expect(keys(errors)).toEqual(['image', 'name']);
  });

  it('does not report a hidden port for a stale stdio transport on another type', () => {
    expect(validateMCPServer({ name: 'a', serverType: 'external', url: 'https://x', transport: 'stdio' }, none)).toEqual({});
  });
});

describe('name collisions with names present in the stack store', () => {
  const ctx = { serverNames: ['demo'], resourceNames: ['db'] };

  it('flags an MCP server named like a server in the store', () => {
    const errors = validateMCPServer({ name: 'demo', serverType: 'external', url: 'https://x' }, ctx);
    expect(errors.name).toBe("An MCP server named 'demo' already exists in this stack");
  });

  it('flags an MCP server named like a resource in the store', () => {
    const errors = validateMCPServer({ name: 'db', serverType: 'external', url: 'https://x' }, ctx);
    expect(errors.name).toBe("A resource named 'db' already exists in this stack");
  });

  it('flags a resource named like a server or a resource in the store', () => {
    expect(validateResource({ name: 'demo', image: 'x' }, ctx).name).toMatch(/MCP server named 'demo'/);
    expect(validateResource({ name: 'db', image: 'x' }, ctx).name).toMatch(/resource named 'db'/);
  });

  it('reports nothing for a unique name', () => {
    expect(validateMCPServer({ name: 'fresh', serverType: 'external', url: 'https://x' }, ctx)).toEqual({});
    expect(validateResource({ name: 'fresh', image: 'x' }, ctx)).toEqual({});
  });

  it('reports required, not a collision, for an empty name', () => {
    expect(validateResource({ name: '', image: 'x' }, { serverNames: [''], resourceNames: [''] }).name).toBe('Name is required');
  });

  it('requires name and image on a resource', () => {
    expect(keys(validateResource({ name: '', image: '' }, none))).toEqual(['image', 'name']);
  });
});

describe('validateStack', () => {
  const server = (name: string): MCPServerFormData => ({ name, serverType: 'external', url: 'https://x' });

  it('requires the stack name and a gateway token when auth is set', () => {
    expect(keys(validateStack({ name: '' }))).toEqual(['name']);
    expect(keys(validateStack({ name: 's', gateway: { auth: { type: 'bearer', token: '' } } }))).toEqual(['gateway.auth.token']);
  });

  it('flags the later of two nested servers sharing a name', () => {
    const errors = validateStack({ name: 's', mcpServers: [server('a'), server('a')] });
    expect(keys(errors)).toEqual(['mcpServers.1.name']);
    expect(errors['mcpServers.1.name']).toMatch(/Another MCP server/);
  });

  it('flags duplicate resources and a resource named like a nested server', () => {
    const errors = validateStack({
      name: 's',
      mcpServers: [server('a')],
      resources: [{ name: 'db', image: 'x' }, { name: 'db', image: 'x' }, { name: 'a', image: 'x' }],
    });
    expect(keys(errors)).toEqual(['resources.1.name', 'resources.2.name']);
    expect(errors['resources.1.name']).toMatch(/Another resource/);
    expect(errors['resources.2.name']).toBe("An MCP server in this stack is named 'a'");
  });

  it('puts nested required fields under per-item keys', () => {
    const errors = validateStack({
      name: 's',
      mcpServers: [server('a'), { name: 'b', serverType: 'container', port: 80 }],
      resources: [{ name: '', image: '' }],
    });
    expect(keys(errors)).toEqual(['mcpServers.1.image', 'resources.0.image', 'resources.0.name']);
  });
});

describe('reason formatting', () => {
  it('uses the message itself for one error', () => {
    expect(formatNextReason({ name: 'Name is required' })).toBe('Name is required');
  });

  it('prefixes a nested item', () => {
    expect(formatNextReason({ 'mcpServers.1.image': 'Image is required' })).toBe('MCP server 2: Image is required');
    expect(fieldLabel('resources.0.name')).toBe('Resource 1: Name');
  });

  it('counts and lists labels for several errors', () => {
    expect(formatNextReason({ name: 'x', image: 'y', port: 'z' })).toBe('3 fields need attention: Name, Image, Port');
  });

  it('truncates a long list', () => {
    const errors = Object.fromEntries(['name', 'image', 'port', 'url', 'command', 'ssh.host'].map((k) => [k, 'x']));
    expect(formatNextReason(errors)).toBe('6 fields need attention: Name, Image, Port, URL, Command, and 1 more');
  });

  it('is empty with no errors', () => {
    expect(formatNextReason({})).toBe('');
  });
});

describe('helpers', () => {
  it('showPortField hides the port for stdio and non-container types', () => {
    expect(showPortField('container', 'http')).toBe(true);
    expect(showPortField('container', 'stdio')).toBe(false);
    expect(showPortField('external', 'http')).toBe(false);
  });

  it('subErrors strips the prefix', () => {
    expect(subErrors({ 'mcpServers.0.name': 'a', 'mcpServers.1.url': 'b', name: 'c' }, 'mcpServers.0.')).toEqual({ name: 'a' });
    expect(subErrors(undefined, 'x.')).toBeUndefined();
  });
});
