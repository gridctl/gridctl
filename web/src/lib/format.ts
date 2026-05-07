/**
 * Format a number into a compact human-readable string.
 * 1000 -> "1k", 1500 -> "1.5k", 1500000 -> "1.5M"
 */
export function formatCompactNumber(n: number): string {
  if (n < 1000) return n.toString();
  if (n < 1_000_000) {
    const k = n / 1000;
    return k % 1 === 0 ? `${k}k` : `${k.toFixed(1)}k`;
  }
  const m = n / 1_000_000;
  return m % 1 === 0 ? `${m}M` : `${m.toFixed(1)}M`;
}

/**
 * Format a USD amount with precision that matches its magnitude. Tiny
 * per-call costs need 4 decimals to be readable; aggregates round
 * naturally. Anything above $10k compacts (k/M) so KPI cards never
 * overflow.
 */
export function formatUSD(usd: number): string {
  if (!Number.isFinite(usd)) return '—';
  if (usd === 0) return '$0.00';
  const abs = Math.abs(usd);
  if (abs < 0.01) return `$${usd.toFixed(4)}`;
  if (abs < 1) return `$${usd.toFixed(3)}`;
  if (abs < 1000) return `$${usd.toFixed(2)}`;
  if (abs < 1_000_000) return `$${(usd / 1000).toFixed(2)}k`;
  return `$${(usd / 1_000_000).toFixed(2)}M`;
}
