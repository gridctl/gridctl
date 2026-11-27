import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { isLiteralSecret, isSensitiveEnvName, maskYAMLSecrets, redactFormData } from '../lib/wizard-secrets';
import { buildYAML, type MCPServerFormData, type StackFormData } from '../lib/yaml-builder';

describe('isLiteralSecret', () => {
  it.each(['sk-abc', '${TOKEN:-sk-abc}', '${var:T:+sk-abc}', 'Bearer sk-abc'])('treats %s as a literal', (value) => {
    expect(isLiteralSecret(value)).toBe(true);
  });

  it.each([
    '',
    undefined,
    '${var:T}',
    '${vault:T}',
    '${secret:exec/x}',
    '${TOKEN}',
    '$TOKEN',
    'Bearer ${var:T}',
    '${TOKEN:-}',
  ])('treats %s as not a literal', (value) => {
    expect(isLiteralSecret(value)).toBe(false);
  });
});

describe('isSensitiveEnvName', () => {
  it('matches the export list case-insensitively', () => {
    for (const key of ['GITHUB_TOKEN', 'db_password', 'OPENAI_API_KEY', 'x-apikey', 'Authorization', 'AWS_ACCESS_KEY_ID', 'MY_CREDENTIALS', 'PRIVATE_KEY', 'CLIENT_SECRET']) {
      expect(isSensitiveEnvName(key)).toBe(true);
    }
    for (const key of ['LOG_LEVEL', 'PORT', 'HOST']) {
      expect(isSensitiveEnvName(key)).toBe(false);
    }
  });
});

describe('redactFormData', () => {
  it('blanks mcp-server credentials and keeps references and other fields', () => {
    const input: MCPServerFormData = {
      name: 'remote',
      serverType: 'external',
      auth: { type: 'oauth', token: 'sk-1', value: 'v-1', header: 'X-Key', clientId: 'client', clientSecret: 'cs-1' },
      headers: { 'X-Tenant': 'tenant-literal', 'X-Ref': '${var:REF}' },
      env: { GITHUB_TOKEN: 'ghp_literal', LOG_LEVEL: 'debug', API_KEY: '${var:API_KEY}' },
      buildArgs: { NPM_TOKEN: 'npm-literal', VERSION: '1.2' },
    };
    const snapshot = structuredClone(input);
    const { data, redacted } = redactFormData('mcp-server', input);
    expect(redacted).toBe(6);
    expect(data.auth).toEqual({ type: 'oauth', token: '', value: '', header: 'X-Key', clientId: 'client', clientSecret: '' });
    expect(data.headers).toEqual({ 'X-Tenant': '', 'X-Ref': '${var:REF}' });
    expect(data.env).toEqual({ GITHUB_TOKEN: '', LOG_LEVEL: 'debug', API_KEY: '${var:API_KEY}' });
    expect(data.buildArgs).toEqual({ NPM_TOKEN: '', VERSION: '1.2' });
    expect(input).toEqual(snapshot);
  });

  it('blanks the gateway token and recurses into servers and resources', () => {
    const input: StackFormData = {
      name: 'demo',
      gateway: { auth: { type: 'bearer', token: 'gw-literal' } },
      mcpServers: [{ name: 'a', serverType: 'external', auth: { type: 'bearer', token: 'srv-literal' } }],
      resources: [{ name: 'db', image: 'postgres', env: { POSTGRES_PASSWORD: 'pw', POSTGRES_DB: 'app' } }],
    };
    const { data, redacted } = redactFormData('stack', input);
    expect(redacted).toBe(3);
    expect(data.gateway?.auth?.token).toBe('');
    expect(data.mcpServers?.[0].auth?.token).toBe('');
    expect(data.resources?.[0].env).toEqual({ POSTGRES_PASSWORD: '', POSTGRES_DB: 'app' });
    expect(input.gateway?.auth?.token).toBe('gw-literal');
  });

  it('redacts resource env values', () => {
    const { data, redacted } = redactFormData('resource', { name: 'db', image: 'x', env: { SECRET_KEY: 'k', A: 'b' } });
    expect(redacted).toBe(1);
    expect(data.env).toEqual({ SECRET_KEY: '', A: 'b' });
  });

  it('reports zero for a form with only references', () => {
    const { redacted } = redactFormData('mcp-server', {
      name: 'x',
      serverType: 'external',
      auth: { type: 'bearer', token: '${var:TOKEN}' },
    });
    expect(redacted).toBe(0);
  });
});

describe('maskYAMLSecrets', () => {
  const server: MCPServerFormData = {
    name: 'remote',
    serverType: 'external',
    url: 'https://example.com/mcp',
    auth: { type: 'bearer', token: 'sk-literal-123' },
    env: { LOG_LEVEL: 'debug' },
  };

  it('changes only the token line of built server YAML', () => {
    const yaml = buildYAML({ type: 'mcp-server', data: server });
    const result = maskYAMLSecrets(yaml, '••••••••');
    expect(result?.masked).toBe(1);
    const before = yaml.split('\n');
    const after = result!.yaml.split('\n');
    expect(after).toHaveLength(before.length);
    const changed = before.map((line, i) => (line === after[i] ? null : i)).filter((i) => i !== null);
    expect(changed).toHaveLength(1);
    expect(before[changed[0]!]).toContain('token: sk-literal-123');
    expect(after[changed[0]!]).toBe('  token: ••••••••');
  });

  it('returns the identical string when there is nothing to mask', () => {
    const yaml = buildYAML({ type: 'mcp-server', data: { ...server, auth: { type: 'bearer', token: '${var:TOKEN}' } } });
    const result = maskYAMLSecrets(yaml, '""');
    expect(result).toEqual({ yaml, masked: 0 });
    expect(result!.yaml).toBe(yaml);
  });

  it('returns null for invalid YAML', () => {
    expect(maskYAMLSecrets('auth:\n  token: [unclosed\n', '""')).toBeNull();
  });

  it('masks a nested server inside a stack document', () => {
    const yaml = buildYAML({ type: 'stack', data: { name: 'demo', mcpServers: [server] } });
    const result = maskYAMLSecrets(yaml, '""');
    expect(result?.masked).toBe(1);
    expect(result!.yaml).not.toContain('sk-literal-123');
    const parsed = parse(result!.yaml) as { 'mcp-servers': Array<{ auth: { token: string } }> };
    expect(parsed['mcp-servers'][0].auth.token).toBe('');
  });

  it('preserves authored formatting outside the masked slices', () => {
    const longLine = 'this is a long plain scalar value with many spaces that runs well past the eighty column fold width';
    const yaml = [
      `description: ${longLine}`,
      'labels: {a: b,   c: d}',
      'auth:',
      '  type: bearer',
      '  token: sk-literal-123   # trailing comment',
      'headers: {X-Tenant: "tenant-secret"}',
      'env:',
      '  LOG_LEVEL: debug',
      '',
    ].join('\n');
    const result = maskYAMLSecrets(yaml, '""');
    expect(result?.masked).toBe(2);
    const before = yaml.split('\n');
    const after = result!.yaml.split('\n');
    expect(after).toHaveLength(before.length);
    for (let i = 0; i < before.length; i++) {
      if (i === 4) expect(after[i]).toBe('  token: ""   # trailing comment');
      else if (i === 5) expect(after[i]).toBe('headers: {X-Tenant: ""}');
      else expect(after[i]).toBe(before[i]);
    }
  });

  it('masks a double-quoted token and a block-scalar env value', () => {
    const yaml = 'auth:\n  type: bearer\n  token: "sk-quoted"\nenv:\n  MY_TOKEN: |\n    line-one\n    line-two\n  LOG_LEVEL: info\n';
    const result = maskYAMLSecrets(yaml, '""');
    expect(result?.masked).toBe(2);
    expect(result!.yaml).not.toContain('sk-quoted');
    expect(result!.yaml).not.toContain('line-one');
    const parsed = parse(result!.yaml) as { auth: { token: string }; env: Record<string, string> };
    expect(parsed.auth.token).toBe('');
    expect(parsed.env).toEqual({ MY_TOKEN: '', LOG_LEVEL: 'info' });
  });

  it('masks client_secret, auth value, and sensitive build args but not other keys', () => {
    const yaml = 'auth:\n  type: oauth\n  client_id: cid\n  client_secret: cs\n  value: v\nbuild_args:\n  NPM_TOKEN: n\n  VERSION: "1"\ntoken: top-level\n';
    const result = maskYAMLSecrets(yaml, 'X');
    expect(result?.masked).toBe(3);
    expect(result!.yaml).toBe('auth:\n  type: oauth\n  client_id: cid\n  client_secret: X\n  value: X\nbuild_args:\n  NPM_TOKEN: X\n  VERSION: "1"\ntoken: top-level\n');
  });

  it('leaves references and aliases unmasked', () => {
    const yaml = 'base: &tok sk-anchor\nauth:\n  token: *tok\n  value: "${var:V}"\n';
    expect(maskYAMLSecrets(yaml, '""')).toEqual({ yaml, masked: 0 });
  });
});
