import type { CheckStatus, ComponentState, IncidentStatus } from '../api/types';

export type Tone = 'good' | 'warning' | 'serious' | 'critical' | 'neutral' | 'info';

export type UptimeLevel = 'none' | 'good' | 'warning' | 'serious' | 'critical';

/**
 * Classifies a day's uptime for the status-page bars. Thresholds mirror common
 * SLO tiers: 99.9% (three nines) reads as healthy.
 */
export function uptimeLevel(p: number | null | undefined): UptimeLevel {
  if (p === null || p === undefined) return 'none';
  if (p >= 99.9) return 'good';
  if (p >= 99) return 'warning';
  if (p >= 95) return 'serious';
  return 'critical';
}

export const UPTIME_LEVELS: { level: UptimeLevel; label: string }[] = [
  { level: 'good', label: '≥ 99.9%' },
  { level: 'warning', label: '≥ 99%' },
  { level: 'serious', label: '≥ 95%' },
  { level: 'critical', label: '< 95%' },
  { level: 'none', label: 'No data' },
];

export const checkStatusMeta: Record<CheckStatus | 'paused', { label: string; tone: Tone }> = {
  up: { label: 'Up', tone: 'good' },
  degraded: { label: 'Degraded', tone: 'warning' },
  down: { label: 'Down', tone: 'critical' },
  pending: { label: 'Pending', tone: 'neutral' },
  paused: { label: 'Paused', tone: 'neutral' },
};

export const incidentStatusMeta: Record<IncidentStatus, { label: string; tone: Tone }> = {
  open: { label: 'Open', tone: 'critical' },
  acknowledged: { label: 'Acknowledged', tone: 'warning' },
  resolved: { label: 'Resolved', tone: 'good' },
};

export const componentStateMeta: Record<ComponentState, { label: string; tone: Tone }> = {
  operational: { label: 'Operational', tone: 'good' },
  degraded: { label: 'Degraded performance', tone: 'warning' },
  partial_outage: { label: 'Partial outage', tone: 'serious' },
  major_outage: { label: 'Major outage', tone: 'critical' },
  paused: { label: 'Paused', tone: 'neutral' },
  pending: { label: 'Awaiting data', tone: 'neutral' },
};

export const overallHeadline: Record<ComponentState, string> = {
  operational: 'All systems operational',
  degraded: 'Some systems are degraded',
  partial_outage: 'Partial system outage',
  major_outage: 'Major system outage',
  paused: 'Monitoring paused',
  pending: 'Collecting data',
};
