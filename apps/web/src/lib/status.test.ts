import { describe, expect, it } from 'vitest';

import { uptimeLevel } from './status';

describe('uptimeLevel', () => {
  it.each([
    [null, 'none'],
    [100, 'good'],
    [99.9, 'good'],
    [99.89, 'warning'],
    [99, 'warning'],
    [98.99, 'serious'],
    [95, 'serious'],
    [94.9, 'critical'],
    [0, 'critical'],
  ] as const)('%s → %s', (pct, want) => {
    expect(uptimeLevel(pct)).toBe(want);
  });
});
