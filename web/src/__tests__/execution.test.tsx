import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { parse } from 'yaml';
import { buildYAML, parseYAMLToForm, type WizardFormData } from '../lib/yaml-builder';
import { ExecutionDetails } from '../components/sidebar/ExecutionDetails';
import type { MCPServerStatus } from '../types';

describe('execution preservation and reporting', () => {
  it('preserves nested empty lists, false exceptions, and secret references', () => {
    const input = 'name: fixture\nimage: alpine\ntransport: stdio\nexecution:\n  mode: hardened\n  uid: 1000\n  gid: 1000\n  read_only: false\n  drop_capabilities: []\n  mounts: []\n  tmpfs: []\nenv:\n  TOKEN: ${var:TOKEN}\n';
    const form = parseYAMLToForm(input, 'mcp-server');
    expect(form).not.toHaveProperty('error');
    const output = parse(buildYAML(form as WizardFormData));
    expect(output.execution).toEqual(parse(input).execution);
    expect(output.env.TOKEN).toBe('${var:TOKEN}');
  });

  it('blocks a lossy whole-stack transition', () => {
    expect(parseYAMLToForm('name: fixture\nmcp-servers:\n- name: local\n  command: [/bin/cat]\n  execution: {mode: local, inherit: []}\n', 'stack')).toHaveProperty('error');
  });

  it('does not turn healthy MCP into protected evidence', () => {
    const server = { name: 'fixture', replicas: [{ replicaId: 0, healthy: true, execution: { mode: 'hardened', outcome: 'mismatch', eligible: false, controls: [{ field: 'memory_bytes', requested: '268435456', outcome: 'mismatch', source: 'kernel' }] } }] } as MCPServerStatus;
    render(<ExecutionDetails server={server} />);
    expect(screen.getByText(/0 of 1 active replicas/)).toBeTruthy();
    expect(screen.getByText(/MCP healthy; execution mismatch/)).toBeTruthy();
  });

  it('shows no current evidence for idle replicas', () => {
    render(<ExecutionDetails server={{ name: 'fixture', replicas: [] } as unknown as MCPServerStatus} />);
    expect(screen.getByText(/No current active execution evidence/)).toBeTruthy();
  });
});
