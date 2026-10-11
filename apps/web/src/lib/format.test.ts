import { describe, expect, it } from 'vitest';

import { formatDuration, formatMs, formatPercent, formatRelative } from './format';

describe('formatDuration', () => {
  it.each([
    [0, '0s'],
    [45_000, '45s'],
    [12 * 60_000, '12m'],
    [3 * 3_600_000, '3h'],
    [3 * 3_600_000 + 5 * 60_000, '3h 5m'],
    [2 * 86_400_000 + 4 * 3_600_000, '2d 4h'],
    [-5, '0s'],
  ])('%i ms → %s', (ms, want) => {
    expect(formatDuration(ms)).toBe(want);
  });
});

describe('formatPercent', () => {
  it('distinguishes near-perfect uptime without rounding up to 100', () => {
    expect(formatPercent(100)).toBe('100%');
    expect(formatPercent(99.999)).toBe('99.99%');
    expect(formatPercent(99.95)).toBe('99.95%');
    expect(formatPercent(97.46)).toBe('97.4%');
  });
  it('renders a dash for missing data', () => {
    expect(formatPercent(null)).toBe('—');
    expect(formatPercent(undefined)).toBe('—');
  });
});

describe('formatMs', () => {
  it('formats milliseconds and switches to seconds for slow probes', () => {
    expect(formatMs(87.4)).toBe('87 ms');
    expect(formatMs(1234)).toBe('1,234 ms');
    expect(formatMs(12_500)).toBe('12.5 s');
    expect(formatMs(null)).toBe('—');
  });
});

describe('formatRelative', () => {
  const now = new Date('2026-05-01T12:00:00Z');
  it.each([
    [null, 'never'],
    ['2026-05-01T11:59:55Z', 'just now'],
    ['2026-05-01T11:59:30Z', '30 seconds ago'],
    ['2026-05-01T11:45:00Z', '15 minutes ago'],
    ['2026-05-01T09:00:00Z', '3 hours ago'],
    ['2026-04-29T12:00:00Z', '2 days ago'],
  ])('%s → %s', (iso, want) => {
    expect(formatRelative(iso, now)).toBe(want);
  });
});
