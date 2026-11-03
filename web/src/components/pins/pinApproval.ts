import { HTTPError } from '../../lib/api';

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
