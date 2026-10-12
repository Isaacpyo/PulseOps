import { expect, it } from 'vitest';

import type { CheckSummary } from '../api/types';
import { summarise } from './Overview';

const base = { enabled: true, uptime24h: 100, avgLatency24h: 100 } as CheckSummary;

it('summarises enabled checks and ignores paused ones in the headline figures', () => {
  const s = summarise([
    { ...base, status: 'up' },
    { ...base, status: 'degraded', uptime24h: 99, avgLatency24h: 300 },
    { ...base, status: 'down', uptime24h: 80, avgLatency24h: null },
    { ...base, status: 'up', enabled: false, uptime24h: 0, avgLatency24h: 9999 },
  ]);
  expect(s).toEqual({ total: 4, up: 1, degraded: 1, down: 1, paused: 1, uptime: 93, latency: 200 });
});

it('handles no data', () => {
  expect(summarise([])).toMatchObject({ total: 0, uptime: null, latency: null });
});
