import { act, fireEvent, render, screen } from '@testing-library/react';
import { expect, it } from 'vitest';

import type { UptimeBar } from '../api/types';
import { UptimeBars, describeBar } from './UptimeBars';

const bars: UptimeBar[] = Array.from({ length: 90 }, (_, i) => ({
  day: new Date(Date.UTC(2026, 0, 1 + i)).toISOString().slice(0, 10),
  uptime: i === 89 ? 98.5 : i < 10 ? null : 100,
  total: i < 10 ? 0 : 1440,
  degraded: 0,
  incidents: i === 89 ? 2 : 0,
}));

it('renders one accessible cell per day', () => {
  render(<UptimeBars bars={bars} label="API" />);
  const list = screen.getByRole('list', { name: 'API: daily uptime history' });
  expect(list.querySelectorAll('li')).toHaveLength(90);
  expect(screen.getAllByLabelText(/no data/)).toHaveLength(10);
});

it('shows a tooltip on focus with uptime and incidents', () => {
  render(<UptimeBars bars={bars} label="API" />);
  const last = screen.getByLabelText(describeBar(bars[89]!));
  act(() => {
    fireEvent.focus(last);
  });
  const tip = screen.getByRole('tooltip');
  expect(tip).toHaveTextContent('98.5% uptime');
  expect(tip).toHaveTextContent('2 incidents opened');
});

it('describes bars for screen readers', () => {
  expect(describeBar({ day: '2026-03-04', uptime: null, total: 0, degraded: 0, incidents: 0 })).toBe('4 Mar 2026, no data');
  expect(describeBar({ day: '2026-03-04', uptime: 100, total: 10, degraded: 0, incidents: 1 })).toBe(
    '4 Mar 2026, 100% uptime, 1 incident',
  );
});
