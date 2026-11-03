import { describe, expect, it } from 'vitest';
import { HTTPError } from '../../lib/api';
import { credentialRefusalText, splitEvidence } from './pinApproval';

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
