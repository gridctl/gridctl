import { describe, it, expect, vi, afterEach } from 'vitest';
import { formatRelativeTime, formatRelativeTimeFine } from '../lib/time';

const NOW = new Date('2026-07-25T12:00:00Z').getTime();

const ago = (ms: number) => new Date(NOW - ms);
const HOUR = 3_600_000;
const DAY = 24 * HOUR;

afterEach(() => {
  vi.useRealTimers();
});

describe('formatRelativeTime', () => {
  it('keeps the fine-grained tiers under 48 hours', () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    expect(formatRelativeTime(ago(5_000))).toBe('just now');
    expect(formatRelativeTime(ago(30_000))).toBe('30s ago');
    expect(formatRelativeTime(ago(5 * 60_000))).toBe('5m ago');
    expect(formatRelativeTime(ago(47 * HOUR))).toBe('47h ago');
  });

  it('reads as days from 48 hours', () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    expect(formatRelativeTime(ago(48 * HOUR))).toBe('2d ago');
    expect(formatRelativeTime(ago(13 * DAY))).toBe('13d ago');
  });

  it('reads as an absolute date from 14 days', () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    expect(formatRelativeTime(ago(14 * DAY))).toMatch(/^[A-Z][a-z]{2} \d{1,2}, \d{4}$/);
    // 853h (the motivating regression) is well past two weeks.
    expect(formatRelativeTime(ago(853 * HOUR))).toMatch(/^[A-Z][a-z]{2} \d{1,2}, \d{4}$/);
  });
});

describe('formatRelativeTimeFine', () => {
  it('keeps second granularity under a minute', () => {
    expect(formatRelativeTimeFine(ago(3_000), NOW)).toBe('3s ago');
    expect(formatRelativeTimeFine(ago(0), NOW)).toBe('now');
  });

  it('mirrors the day and date tiers', () => {
    expect(formatRelativeTimeFine(ago(47 * HOUR), NOW)).toBe('47h ago');
    expect(formatRelativeTimeFine(ago(3 * DAY), NOW)).toBe('3d ago');
    expect(formatRelativeTimeFine(ago(20 * DAY), NOW)).toMatch(/^[A-Z][a-z]{2} \d{1,2}, \d{4}$/);
  });
});
