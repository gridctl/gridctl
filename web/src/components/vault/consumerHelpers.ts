import type { Consumer } from '../../lib/api';

// A consumer is navigable to a canvas node when it points at a server or a
// resource. Explicit references carry that in `kind`; a scoped secrets.sets
// consumer carries it in `targetKind`, naming the single workload the set
// actually injects into. An unscoped set injection reaches everything at once
// and has no single node to open.
export function isNavigable(c: Consumer): boolean {
  if (c.kind === 'secrets-set') {
    return c.targetKind === 'mcp-server' || c.targetKind === 'resource';
  }
  return c.kind === 'mcp-server' || c.kind === 'resource';
}

// navigationTarget returns the node a consumer opens, or null when it has
// none. Callers use this instead of re-deriving the scoped/unscoped split.
export function navigationTarget(
  c: Consumer,
): { kind: 'mcp-server' | 'resource'; name: string } | null {
  if (c.kind === 'secrets-set') {
    if (
      (c.targetKind === 'mcp-server' || c.targetKind === 'resource') &&
      c.target
    ) {
      return { kind: c.targetKind, name: c.target };
    }
    return null;
  }
  if ((c.kind === 'mcp-server' || c.kind === 'resource') && c.name) {
    return { kind: c.kind, name: c.name };
  }
  return null;
}

// consumerLabel renders the compact one-line form shown in consumer rows.
// Explicit references keep the raw "<site> · <field>" YAML path (usable
// verbatim to locate the reference in stack.yaml); set injections describe
// what actually happens, naming the workload when the set is scoped.
export function consumerLabel(c: Consumer): string {
  if (c.kind === 'secrets-set') {
    if (c.target) return `set: ${c.name} · into ${c.target}`;
    return `set: ${c.name} · injected into server env`;
  }
  return `${c.name || c.kind} · ${c.field}`;
}

// describeConsumer produces the human tooltip for a consumer row, translating
// raw YAML field paths like "command[4]" into plain language. Unknown shapes
// fall back to the raw path.
export function describeConsumer(c: Consumer): string {
  if (c.kind === 'secrets-set') {
    if (c.target) {
      const kind = c.targetKind === 'resource' ? 'resource' : 'server';
      return `Injected into the ${kind} "${c.target}" env by the "${c.name}" set, which is scoped to named workloads (secrets.sets in stack.yaml)`;
    }
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

// consumerReachesWorkload reports whether a consumer means "this variable
// reaches the named server or resource". It backs the Stack deep-link
// (?filter=server:<name>), which asks what a workload actually consumes.
//
// Three ways that is true, and missing any of them under-reports:
//   - an explicit ${var:KEY} written on that workload
//   - a scoped secrets.sets entry naming it as a target
//   - an unscoped secrets.sets entry, which reaches every workload
//
// Stack-level sites (gateway, network, stack) belong to no workload.
export function consumerReachesWorkload(c: Consumer, workload: string): boolean {
  if (c.kind === 'secrets-set') {
    return c.target ? c.target === workload : true;
  }
  return (
    (c.kind === 'mcp-server' || c.kind === 'resource') && c.name === workload
  );
}

// consumerSearchText is the text a consumer contributes to list search: the
// site or set name, the workload a scoped entry targets, and the YAML field
// path. Without the target, searching a server name would miss variables that
// only reach it through a scoped set.
export function consumerSearchText(c: Consumer): string {
  return [c.name, c.target, c.field].filter(Boolean).join(' ').toLowerCase();
}

// consumerCount is the "used by N" figure. It counts a scoped set once rather
// than once per workload it reaches, so tightening a set's scope never makes a
// variable look more heavily used than leaving it to fan out.
export function consumerCount(consumers: Consumer[] | undefined): number {
  return groupConsumers(consumers ?? []).length;
}

// describeSetInjection returns the delete-confirmation phrase for bulk
// injection, or null when no set injects this variable. The caller appends
// "via secrets.sets", so the phrase must not carry its own preposition.
//
// A scoped set states the reach it actually has. Claiming "every server" for a
// set scoped to one would overstate the blast radius of a delete, which is the
// same class of dishonesty the scoping feature exists to remove.
export function describeSetInjection(
  consumers: Consumer[] | undefined,
): string | null {
  const sets = (consumers ?? []).filter((c) => c.kind === 'secrets-set');
  if (sets.length === 0) return null;
  if (sets.some((c) => !c.target)) {
    return 'This variable is injected into every server env';
  }
  const targets = new Set(sets.map((c) => c.target));
  if (targets.size === 1) {
    return `This variable is injected into ${[...targets][0]}`;
  }
  return `This variable is injected into ${targets.size} workloads`;
}

// A rendered entry: either one consumer, or a scoped secrets.sets entry that
// reaches several workloads and folds into a single expandable row.
export type Entry =
  | { kind: 'one'; consumer: Consumer }
  | { kind: 'setGroup'; setName: string; consumers: Consumer[] };

// groupConsumers folds a scoped set's per-workload consumers into one entry.
// Without this, a set scoped to eight servers renders as eight near-identical
// rows. Unscoped set injections and explicit references pass through
// untouched, and a scope that reaches exactly one workload stays a plain row
// (a group of one has nothing to expand).
export function groupConsumers(consumers: Consumer[]): Entry[] {
  const entries: Entry[] = [];
  const groupIndex = new Map<string, number>();

  for (const c of consumers) {
    if (c.kind !== 'secrets-set' || !c.target) {
      entries.push({ kind: 'one', consumer: c });
      continue;
    }
    const setName = c.name ?? '';
    const at = groupIndex.get(setName);
    if (at === undefined) {
      groupIndex.set(setName, entries.length);
      entries.push({ kind: 'setGroup', setName, consumers: [c] });
      continue;
    }
    const existing = entries[at];
    if (existing.kind === 'setGroup') existing.consumers.push(c);
  }

  return entries.map((e) =>
    e.kind === 'setGroup' && e.consumers.length === 1
      ? { kind: 'one', consumer: e.consumers[0] }
      : e,
  );
}
