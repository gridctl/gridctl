import { HTTPError, type PinFinding, type PinsDiff } from '../../lib/api';
import { lineDiffExceedsCap, MAX_DIFF_LINES, prettySchema } from '../../lib/diff';

// Every approve surface (server, skill, and context drift, and proposals)
// interposes a confirmation past either threshold, on any warn-or-critical
// finding, or when the rendered diff is truncated.
export const APPROVE_CONFIRM_CHANGES = 5;
export const APPROVE_CONFIRM_LINES = 50;

export interface ApprovalRisk {
  needsConfirm: boolean;
  alertFindings: number;
  truncated: boolean;
  large: boolean;
}

export function approvalRisk(input: {
  findings?: Pick<PinFinding, 'severity'>[] | null;
  truncated: boolean;
  changes?: number;
  changedLines?: number;
}): ApprovalRisk {
  const alertFindings = (input.findings ?? []).filter((f) => f.severity !== 'info').length;
  const large =
    (input.changes ?? 0) >= APPROVE_CONFIRM_CHANGES ||
    (input.changedLines ?? 0) >= APPROVE_CONFIRM_LINES;
  return {
    needsConfirm: alertFindings > 0 || input.truncated || large,
    alertFindings,
    truncated: input.truncated,
    large,
  };
}

// serverDiffTruncated mirrors what the drift panels render for modified
// tools: a schema diff past the line cap, or an uncaptured-old schema that
// pretty-prints past it.
export function serverDiffTruncated(diff: PinsDiff): boolean {
  return diff.modified_tools.some((t) => {
    const kinds = t.change_kinds ?? [];
    if (
      kinds.includes('input_schema') &&
      lineDiffExceedsCap(prettySchema(t.old_input_schema ?? ''), prettySchema(t.new_input_schema ?? ''))
    ) {
      return true;
    }
    if (
      kinds.includes('output_schema') &&
      lineDiffExceedsCap(prettySchema(t.old_output_schema ?? ''), prettySchema(t.new_output_schema ?? ''))
    ) {
      return true;
    }
    if (kinds.includes('schema_uncaptured')) {
      return [t.new_input_schema, t.new_output_schema].some(
        (s) => prettySchema(s ?? '').split('\n').length > MAX_DIFF_LINES,
      );
    }
    return false;
  });
}

export function splitEvidence(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const line of text.split('\n')) {
    const trimmed = line.trim();
    if (trimmed === '' || seen.has(trimmed)) continue;
    seen.add(trimmed);
    out.push(trimmed);
  }
  return out;
}

// A 400 from an approve call is an input refusal. Show the server text.
// Branch on the status, not on message substrings.
export function credentialRefusalText(err: unknown): string | null {
  if (!(err instanceof HTTPError) || err.status !== 400) return null;
  const message = err.message.trim();
  return message === '' ? null : message;
}
