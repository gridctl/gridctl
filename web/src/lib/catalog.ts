// catalog.ts — Catalog entry to wizard form data mapping
// Mirrors pkg/catalog's Entry.Server semantics so the wizard's YAML preview
// matches what `gridctl add` writes for the same entry: secrets become
// ${var:KEY} references, non-secret defaults land as literals, and env
// inputs are dropped for external URL servers.

import type { CatalogEntry, CatalogInput } from './api';
import type { MCPServerFormData } from './yaml-builder';

/**
 * Derive the stack server name: the part after the registry namespace
 * slash, with characters outside [a-zA-Z0-9_-] collapsed to dashes.
 * Mirrors pkg/catalog Entry.ServerName.
 */
export function catalogServerName(entry: CatalogEntry): string {
  const base = entry.name.includes('/')
    ? entry.name.slice(entry.name.lastIndexOf('/') + 1)
    : entry.name;
  return base.replace(/[^a-zA-Z0-9_-]+/g, '-').replace(/^-+|-+$/g, '');
}

/** The install-time value for one input, mirroring `gridctl add --yes`. */
function inputValue(input: CatalogInput): string {
  if (input.secret || input.auth) return `\${var:${input.name}}`;
  return input.default ?? '';
}

/**
 * Map a catalog entry onto the wizard's mcp-server form data. Returns a
 * full replacement (unused fields explicitly undefined) so a previously
 * edited form never leaks stale fields into the new selection.
 */
export function catalogEntryToFormData(entry: CatalogEntry): Partial<MCPServerFormData> {
  const base: Partial<MCPServerFormData> = {
    name: catalogServerName(entry),
    image: undefined,
    port: undefined,
    transport: undefined,
    command: undefined,
    url: undefined,
    auth: undefined,
    env: undefined,
    source: undefined,
    ssh: undefined,
    openapi: undefined,
  };

  const inputs = entry.inputs ?? [];
  const env: Record<string, string> = {};
  for (const input of inputs) {
    if (input.arg || input.auth) continue;
    const value = inputValue(input);
    // Optional inputs with no value are omitted, matching the CLI; required
    // ones stay visible (empty) so the user fills them in the form.
    if (value || input.required) env[input.name] = value;
  }
  const envOrUndefined = Object.keys(env).length > 0 ? env : undefined;

  switch (entry.install.type) {
    case 'image':
      return {
        ...base,
        serverType: 'container',
        image: entry.install.image ?? '',
        port: entry.install.port || undefined,
        transport: entry.install.transport,
        env: envOrUndefined,
      };
    case 'command': {
      const args = inputs
        .filter((i) => i.arg)
        .map((i) => inputValue(i))
        .filter(Boolean);
      return {
        ...base,
        serverType: 'local',
        command: [...(entry.install.command ?? []), ...args],
        transport: 'stdio',
        env: envOrUndefined,
      };
    }
    case 'url': {
      const authInput = inputs.find((i) => i.auth);
      const authRef = authInput ? `\${var:${authInput.name}}` : '';
      let auth: MCPServerFormData['auth'];
      if (entry.install.auth_type === 'bearer') {
        auth = { type: 'bearer', token: authRef };
      } else if (entry.install.auth_type === 'header') {
        auth = { type: 'header', header: entry.install.auth_header ?? '', value: authRef };
      }
      // Env inputs are not supported for external URL servers (the CLI
      // drops them with a warning); omit them here for YAML parity.
      return {
        ...base,
        serverType: 'external',
        url: entry.install.url ?? '',
        transport: entry.install.transport,
        auth,
      };
    }
  }
}

/** Human label for how the entry runs; mirrors Entry.InstallLabel. */
export function catalogInstallLabel(entry: CatalogEntry): string {
  if (entry.unsupported) return 'unsupported';
  switch (entry.install.type) {
    case 'image':
      return 'container image';
    case 'command':
      return `stdio via ${entry.install.command?.[0] ?? 'command'}`;
    case 'url':
      return `${entry.install.transport} url`;
  }
}
