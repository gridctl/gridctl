import { describe, expect, it } from 'vitest';
import { HTTPError, type PinsDiff, type PinsToolDiff } from '../../lib/api';
import { MAX_DIFF_LINES } from '../../lib/diff';
import {
  APPROVE_CONFIRM_CHANGES,
  APPROVE_CONFIRM_LINES,
  approvalRisk,
  credentialRefusalText,
  serverDiffTruncated,
  splitEvidence,
} from './pinApproval';

describe('approvalRisk', () => {
  it('approves a small clean change without confirmation', () => {
    expect(approvalRisk({ truncated: false, changes: 1, changedLines: 3 })).toEqual({
      needsConfirm: false,
      alertFindings: 0,
      truncated: false,
      large: false,
    });
  });

  it('confirms on a warn or critical finding', () => {
    const risk = approvalRisk({ findings: [{ severity: 'warn' }, { severity: 'info' }], truncated: false });
    expect(risk.needsConfirm).toBe(true);
    expect(risk.alertFindings).toBe(1);
    expect(approvalRisk({ findings: [{ severity: 'critical' }], truncated: false }).needsConfirm).toBe(true);
  });

  it('does not confirm on info-only findings', () => {
    const risk = approvalRisk({ findings: [{ severity: 'info' }, { severity: 'info' }], truncated: false });
    expect(risk).toMatchObject({ needsConfirm: false, alertFindings: 0 });
  });

  it('confirms a truncated diff', () => {
    expect(approvalRisk({ truncated: true })).toMatchObject({ needsConfirm: true, truncated: true, large: false });
  });

  it('confirms at five changes, not four', () => {
    expect(APPROVE_CONFIRM_CHANGES).toBe(5);
    expect(approvalRisk({ truncated: false, changes: 4 }).needsConfirm).toBe(false);
    expect(approvalRisk({ truncated: false, changes: 5 })).toMatchObject({ needsConfirm: true, large: true });
  });

  it('confirms at 50 changed lines, not 49', () => {
    expect(APPROVE_CONFIRM_LINES).toBe(50);
    expect(approvalRisk({ truncated: false, changedLines: 49 }).needsConfirm).toBe(false);
    expect(approvalRisk({ truncated: false, changedLines: 50 })).toMatchObject({ needsConfirm: true, large: true });
  });
});

describe('serverDiffTruncated', () => {
  const bigSchema = (tag: string) =>
    JSON.stringify({
      type: 'object',
      properties: Object.fromEntries(
        Array.from({ length: 800 }, (_, i) => [`${tag}${i}`, { type: 'string' }]),
      ),
    });
  const tool = (over: Partial<PinsToolDiff>): PinsToolDiff => ({
    name: 't',
    old_hash: 'a',
    new_hash: 'b',
    old_description: '',
    new_description: '',
    ...over,
  });
  const diff = (tools: PinsToolDiff[]): PinsDiff => ({
    server: 's',
    status: 'drift',
    live_server_hash: 'h',
    modified_tools: tools,
    new_tools: [],
    removed_tools: [],
  });

  it('is false for an ordinary schema change', () => {
    const d = diff([
      tool({
        change_kinds: ['input_schema'],
        old_input_schema: '{"type":"object","properties":{"a":{"type":"string"}}}',
        new_input_schema: '{"type":"object","properties":{"b":{"type":"string"}}}',
      }),
    ]);
    expect(serverDiffTruncated(d)).toBe(false);
  });

  it('is true when a pretty-printed schema diff passes the cap', () => {
    const d = diff([tool({ change_kinds: ['input_schema'], old_input_schema: '{}', new_input_schema: bigSchema('p') })]);
    expect(serverDiffTruncated(d)).toBe(true);
  });

  it('applies prettySchema before counting', () => {
    // Canonical JSON is one line; only the pretty form passes the cap.
    expect(bigSchema('p').split('\n')).toHaveLength(1);
    const d = diff([
      tool({ change_kinds: ['output_schema'], old_output_schema: '{}', new_output_schema: bigSchema('o') }),
    ]);
    expect(serverDiffTruncated(d)).toBe(true);
  });

  it('ignores schemas whose change kind is not listed', () => {
    const d = diff([tool({ change_kinds: ['description'], old_input_schema: '{}', new_input_schema: bigSchema('p') })]);
    expect(serverDiffTruncated(d)).toBe(false);
  });

  it('checks the new schema of an uncaptured pin against the cap', () => {
    expect(serverDiffTruncated(diff([tool({ change_kinds: ['schema_uncaptured'], new_input_schema: bigSchema('u') })]))).toBe(
      true,
    );
    const small = JSON.stringify({ type: 'object' });
    expect(serverDiffTruncated(diff([tool({ change_kinds: ['schema_uncaptured'], new_output_schema: small })]))).toBe(false);
    expect(MAX_DIFF_LINES).toBe(2000);
  });
});

describe('splitEvidence', () => {
  it('splits on newlines, trims, drops blanks, and keeps the first duplicate', () => {
    expect(splitEvidence('  https://example.test/pr/1 \n\nhttps://example.test/pr/1\nplan/review.md  ')).toEqual([
      'https://example.test/pr/1',
      'plan/review.md',
    ]);
    expect(splitEvidence('\n  \n')).toEqual([]);
  });
});

describe('credentialRefusalText', () => {
  it('returns the server text for any approve 400', () => {
    expect(credentialRefusalText(new HTTPError(400, 'approver is longer than 256 bytes; shorten it and retry'))).toMatch(
      /longer than 256/,
    );
    expect(credentialRefusalText(new HTTPError(400, 'evidence reference 1: URL carries userinfo; remove it and retry'))).toMatch(
      /userinfo/,
    );
  });

  it('ignores other statuses and plain errors', () => {
    expect(credentialRefusalText(new HTTPError(409, 'changed'))).toBeNull();
    expect(credentialRefusalText(new Error('evidence reference looks like a bearer token'))).toBeNull();
    expect(credentialRefusalText(new HTTPError(400, '   '))).toBeNull();
  });
});
