import type {
  AgentProjectionStatus,
  ClientStatus,
  ModelsTargetStatus,
  SessionEntry,
  WiringRow,
} from '../../types';
import type { ContextClientStatus } from '../../lib/api';

// Desired connection state per slug, staged locally until Apply. Only
// slugs whose desired state differs from the current state are present.
export type StagedChanges = Record<string, boolean>;

// The toggle reflects "connected in any sense": linked (an entry exists in
// the client config, however it got there) or declared in the stack's
// link: block. Toggling ON an already-linked client adopts it into link:;
// the Declared badge still distinguishes declared from merely linked.
export function isConnected(c: ClientStatus): boolean {
  return c.linked || Boolean(c.declared);
}

// Wiring states that need a human decision. `missing` is excluded: a
// detected-but-never-linked client is an opportunity, not a problem.
const WIRING_ATTENTION = new Set(['stale', 'drifted', 'target-missing', 'foreign']);
const CONTEXT_ATTENTION = new Set(['stale', 'drifted', 'target-missing']);
const AGENT_ATTENTION = new Set(['stale', 'drifted', 'target-missing']);
// Model routing: restart-pending is an annotation and never counts,
// matching the engine's NeedsAttention. Never-synced is deliberately
// looser than the engine (whose exit-code contract counts it): here it
// is an invitation, not a fault, matching the sets above.
const MODELS_ATTENTION = new Set(['stale', 'drifted', 'target-missing']);

/** One client's joined ownership health across the projection domains.
 *  Ownership only — live connectivity is a separate axis and must never
 *  fold into this. */
export interface ClientHealth {
  attention: boolean;
  /** Human-readable reasons, one per drifting domain, in display order. */
  reasons: string[];
}

/**
 * clientHealth joins wiring rows, context client states, agent
 * projection rows, and model routing targets for one slug. The sources
 * use different keys (wiring/context by provisioner slug; agent targets
 * have one documented non-provisioner exception, copilot), so the join
 * is by string slug and rows that fail to join for OTHER slugs are the
 * caller's concern (surfaced via unjoinedAgentSlugs), never silently
 * dropped. Model routing's LiteLLM targets carry client "litellm",
 * which is not a provisioner slug, so they never join a rail row by
 * construction: their drift lives in the Model routing dialog alone.
 */
export function clientHealth(
  slug: string,
  wiring: WiringRow[] | null,
  contextClients: ContextClientStatus[] | null,
  agentStatuses: AgentProjectionStatus[] | null,
  modelsTargets: ModelsTargetStatus[] | null = null,
): ClientHealth {
  const reasons: string[] = [];

  const wiringHits = (wiring ?? []).filter(
    (r) => r.client === slug && WIRING_ATTENTION.has(r.state),
  );
  if (wiringHits.length > 0) {
    reasons.push(`wiring ${wiringHits[0].state}`);
  }

  const ctx = (contextClients ?? []).find((c) => c.slug === slug);
  if (ctx && CONTEXT_ATTENTION.has(ctx.state)) {
    reasons.push(`context ${ctx.state}`);
  }

  const agentHits = (agentStatuses ?? []).filter(
    (s) => s.client === slug && AGENT_ATTENTION.has(s.state),
  );
  if (agentHits.length > 0) {
    reasons.push(
      `${agentHits.length} agent projection${agentHits.length === 1 ? '' : 's'} ${agentHits[0].state}`,
    );
  }

  const modelsHits = (modelsTargets ?? []).filter(
    (t) => t.client === slug && MODELS_ATTENTION.has(t.state),
  );
  if (modelsHits.length > 0) {
    reasons.push(`model routing ${modelsHits[0].state}`);
  }

  return { attention: reasons.length > 0, reasons };
}

/** Agent-status slugs that join no known client slug. The copilot target
 *  is a documented non-provisioner surface and lands here by design;
 *  anything else appearing is vocabulary drift worth surfacing. */
export function unjoinedAgentSlugs(
  agentStatuses: AgentProjectionStatus[] | null,
  clientSlugs: Set<string>,
): string[] {
  const out = new Set<string>();
  for (const s of agentStatuses ?? []) {
    if (!clientSlugs.has(s.client)) out.add(s.client);
  }
  return [...out].sort();
}

/** Rail ordering: attention first, then connected, then detected, then
 *  the rest; name-alphabetical within each tier. */
export function sortClients(
  clients: ClientStatus[],
  healthOf: (slug: string) => ClientHealth,
): ClientStatus[] {
  const tier = (c: ClientStatus) => {
    if (healthOf(c.slug).attention) return 0;
    if (isConnected(c)) return 1;
    if (c.detected) return 2;
    return 3;
  };
  return [...clients].sort(
    (a, b) => tier(a) - tier(b) || a.name.localeCompare(b.name),
  );
}

export interface AttributedSessions {
  /** Sessions per client slug, joined on the normalized accessId. */
  bySlug: Map<string, SessionEntry[]>;
  /** Sessions whose identity matches no known client slug. Presented as
   *  their own bucket, never force-matched to a guess. */
  unattributed: SessionEntry[];
}

/** attributeSessions joins live session entries to client slugs by the
 *  backend-normalized accessId. */
export function attributeSessions(
  entries: SessionEntry[] | null,
  clientSlugs: Set<string>,
): AttributedSessions {
  const bySlug = new Map<string, SessionEntry[]>();
  const unattributed: SessionEntry[] = [];
  for (const e of entries ?? []) {
    if (e.accessId && clientSlugs.has(e.accessId)) {
      const list = bySlug.get(e.accessId);
      if (list) list.push(e);
      else bySlug.set(e.accessId, [e]);
    } else {
      unattributed.push(e);
    }
  }
  return { bySlug, unattributed };
}

/** Synthesized display identity for a session the UI cannot attribute:
 *  clientInfo when the client supplied one, else generation plus a short
 *  ID — never a bare "unknown". */
export function sessionIdentity(e: SessionEntry): string {
  if (e.clientName) {
    return e.clientVersion ? `${e.clientName} ${e.clientVersion}` : e.clientName;
  }
  return `${e.generation} · ${e.id.slice(0, 8)}`;
}
