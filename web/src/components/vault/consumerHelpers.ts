import type { Consumer } from '../../lib/api';

// A consumer is navigable to a canvas node only when it is a server or a
// resource (those are the kinds the graph renders as nodes). Synthetic
// secrets-set consumers have no single YAML site and are never navigable.
export function isNavigable(c: Consumer): boolean {
  return c.kind === 'mcp-server' || c.kind === 'resource';
}

// consumerLabel renders the compact one-line form shown in consumer rows.
// Explicit references keep the raw "<site> · <field>" YAML path (usable
// verbatim to locate the reference in stack.yaml); set injections describe
// what actually happens since they have no single site.
export function consumerLabel(c: Consumer): string {
  if (c.kind === 'secrets-set') {
    return `set: ${c.name} · injected into server env`;
  }
  return `${c.name || c.kind} · ${c.field}`;
}

// describeConsumer produces the human tooltip for a consumer row, translating
// raw YAML field paths like "command[4]" into plain language. Unknown shapes
// fall back to the raw path.
export function describeConsumer(c: Consumer): string {
  if (c.kind === 'secrets-set') {
    return `Injected into every MCP server and resource env via the "${c.name}" set (secrets.sets in stack.yaml)`;
  }
  const site = c.name ? `${c.kind} "${c.name}"` : c.kind;
  return `${describeField(c.field)} of ${site} in stack.yaml`;
}

function describeField(field: string): string {
  const command = field.match(/^command\[(\d+)\]$/);
  if (command) {
    return `argument ${Number(command[1]) + 1} of the server command`;
  }
  const env = field.match(/^env\.(.+)$/);
  if (env) return `environment variable ${env[1]}`;
  const origin = field.match(/^allowed_origins\[(\d+)\]$/);
  if (origin) return `allowed origin ${Number(origin[1]) + 1}`;
  if (field === 'auth.token') return 'auth token';
  if (field === 'name') return 'name';
  return `field ${field}`;
}
