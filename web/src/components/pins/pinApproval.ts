import type { PinApproval } from '../../lib/api';

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

export function latestApprovals(record?: PinApproval[], fallback?: PinApproval): PinApproval[] | undefined {
  if (record && record.length > 0) return record;
  if (fallback) return [fallback];
  return undefined;
}

export function credentialRefusalText(err: unknown): string | null {
  if (!err || typeof err !== 'object' || !('status' in err) || !('message' in err)) return null;
  const status = (err as { status?: number }).status;
  const message = String((err as { message?: string }).message ?? '');
  if (status !== 400) return null;
  if (
    message.includes('URL carries userinfo') ||
    message.includes('bearer token') ||
    message.includes('key=value secret') ||
    message.includes('credential-shaped')
  ) {
    return message;
  }
  return null;
}
