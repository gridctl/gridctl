import type { OpenAPIOperation } from './api';

// Pure logic behind the wizard's OpenAPI operations picker. Kept out of the
// component so the counting and filter-shaping rules — the parts that decide
// what actually lands in stack.yaml — are unit-testable without rendering.

// The three-way control maps 1:1 onto the config: no operations block, an
// include list, or an exclude list.
export type OperationsMode = 'all' | 'include' | 'exclude';

// The form's operations block. Undefined means no filter at all.
export interface OperationsFilter {
  include?: string[];
  exclude?: string[];
}

/**
 * Which mode an existing form value represents.
 *
 * A defined-but-empty list reads as its mode rather than as "all": the user
 * picked that mode and has not chosen operations yet. What never happens is
 * that empty list reaching YAML — see buildOperationsFilter.
 */
export function deriveOperationsMode(ops: OperationsFilter | undefined): OperationsMode {
  if (ops?.include) return 'include';
  if (ops?.exclude) return 'exclude';
  return 'all';
}

/**
 * Shape a mode plus a selection into the form's operations block.
 *
 * An empty selection collapses to undefined in every mode. `include: []` means
 * "expose everything" to the backend while reading as a whitelist, so it must
 * never be written; `exclude: []` is merely meaningless. Callers get back
 * exactly what should be persisted, with no empty-array middle ground.
 */
export function buildOperationsFilter(
  mode: OperationsMode,
  operationIds: string[],
): OperationsFilter | undefined {
  if (mode === 'all' || operationIds.length === 0) return undefined;
  return mode === 'include' ? { include: [...operationIds] } : { exclude: [...operationIds] };
}

/** The operation IDs currently held by a filter, whichever mode it is in. */
export function selectedOperationIds(ops: OperationsFilter | undefined): string[] {
  return ops?.include ?? ops?.exclude ?? [];
}

/**
 * Operations that can actually become tools. Skipped rows (no operationId, or
 * a sanitized name that comes out empty) are excluded from every count: they
 * are not selectable and they never become tools, so counting them would
 * overstate what deploy produces.
 */
export function selectableOperations(operations: OpenAPIOperation[]): OpenAPIOperation[] {
  return operations.filter((op) => !op.skipped);
}

/**
 * The picker's header count. States the real outcome in every mode rather than
 * just the selection size, because "12 selected" alone does not tell an
 * operator how many tools they are about to create.
 */
export function formatOperationsCount(
  mode: OperationsMode,
  selectedCount: number,
  total: number,
): string {
  const plural = total === 1 ? 'operation' : 'operations';
  if (mode === 'all') return `All ${total} ${plural} will become tools`;
  if (mode === 'include') return `${selectedCount} of ${total} selected`;
  const remaining = Math.max(0, total - selectedCount);
  return `${selectedCount} of ${total} excluded, ${remaining} become tools`;
}

/**
 * The Review step's one-line outcome. The total is unknown when the operator
 * never loaded the spec (manual entry, or an unreachable spec at authoring
 * time), so every mode has a form that degrades without it.
 */
export function formatOperationsSummary(
  ops: OperationsFilter | undefined,
  total: number | null,
): string {
  const mode = deriveOperationsMode(ops);
  const count = selectedOperationIds(ops).length;

  if (mode === 'all' || count === 0) {
    return total === null ? 'All operations' : `All ${total}`;
  }
  if (mode === 'include') {
    return total === null ? `${count} selected (include)` : `${count} of ${total} (include)`;
  }
  return total === null ? `${count} excluded (exclude)` : `${count} of ${total} excluded (exclude)`;
}

// Method colors follow the Swagger convention as closely as the theme palette
// allows. The method name itself is the primary channel; color is redundant.
const METHOD_COLORS: Record<string, string> = {
  GET: 'text-secondary-light',
  POST: 'text-status-running',
  PUT: 'text-primary',
  PATCH: 'text-tertiary-light',
  DELETE: 'text-status-error',
};

export function methodColorClass(method: string): string {
  return METHOD_COLORS[method.toUpperCase()] ?? 'text-text-muted';
}

/**
 * Accessible row label. Composes method, path, and summary so a screen reader
 * announces what the operation is, not just an opaque identifier.
 */
export function operationRowLabel(op: OpenAPIOperation): string {
  const base = `${op.method.toUpperCase()} ${op.path}`;
  return op.summary ? `${base} — ${op.summary}` : base;
}

/** Distinct method names present in a spec, in a stable order. */
export function collectMethods(operations: OpenAPIOperation[]): string[] {
  const seen = new Set<string>();
  for (const op of operations) seen.add(op.method.toUpperCase());
  return [...seen].sort();
}

/** Distinct tags present in a spec, in a stable order. */
export function collectTags(operations: OpenAPIOperation[]): string[] {
  const seen = new Set<string>();
  for (const op of operations) {
    for (const tag of op.tags ?? []) seen.add(tag);
  }
  return [...seen].sort();
}

// Copy for the skip reasons the backend reports. Unknown reasons fall through
// to the raw string rather than being swallowed.
const SKIP_REASONS: Record<string, string> = {
  no_operation_id: 'no operationId in the spec',
  unusable_tool_name: 'operationId cannot be sanitized into a tool name',
};

export function describeSkipReason(reason: string | undefined): string {
  if (!reason) return 'not usable as a tool';
  return SKIP_REASONS[reason] ?? reason;
}
