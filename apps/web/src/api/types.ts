// Mirrors the JSON shapes served by apps/api (internal/store/models.go and
// internal/httpapi). Keep in sync when the API changes.

export type CheckKind = 'http' | 'tcp';
export type CheckStatus = 'pending' | 'up' | 'degraded' | 'down';
export type IncidentStatus = 'open' | 'acknowledged' | 'resolved';
export type IncidentCause = 'down' | 'degraded';
export type ComponentState = 'operational' | 'degraded' | 'partial_outage' | 'major_outage' | 'paused' | 'pending';

export interface User {
  id: number;
  workspaceId: number;
  email: string;
  name: string;
  createdAt: string;
}

export interface Workspace {
  id: number;
  name: string;
  slug: string;
  isDemo: boolean;
  createdAt: string;
}

export interface Session {
  token: string;
  expiresAt: string;
  user: User;
  workspace: Workspace;
}

export interface Check {
  id: number;
  name: string;
  kind: CheckKind;
  target: string;
  intervalSeconds: number;
  timeoutMs: number;
  expectedStatus: number | null;
  latencyThresholdMs: number | null;
  failureThreshold: number;
  recoveryThreshold: number;
  enabled: boolean;
  isPublic: boolean;
  status: CheckStatus;
  consecutiveFailures: number;
  consecutiveSlow: number;
  consecutiveHealthy: number;
  consecutiveReachable: number;
  lastCheckedAt: string | null;
  lastLatencyMs: number | null;
  lastError: string | null;
  nextRunAt: string;
  drillUntil: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface ResultPoint {
  at: string;
  outcome: CheckStatus;
  latencyMs: number;
}

export interface CheckSummary extends Check {
  uptime24h: number | null;
  avgLatency24h: number | null;
  recent: ResultPoint[];
  activeIncident: { id: number; cause: IncidentCause; status: IncidentStatus } | null;
}

export interface CheckResult {
  id: number;
  checkedAt: string;
  outcome: Exclude<CheckStatus, 'pending'>;
  latencyMs: number;
  statusCode: number | null;
  error: string | null;
}

export interface UptimeBar {
  day: string;
  uptime: number | null;
  total: number;
  degraded: number;
  incidents: number;
}

export interface Incident {
  id: number;
  checkId: number | null;
  checkName: string;
  title: string;
  cause: IncidentCause;
  status: IncidentStatus;
  openedAt: string;
  acknowledgedAt: string | null;
  acknowledgedBy: string | null;
  resolvedAt: string | null;
  resolvedBy: string | null;
}

export type IncidentEventKind =
  | 'opened'
  | 'escalated'
  | 'acknowledged'
  | 'resolved'
  | 'auto_resolved'
  | 'note'
  | 'notified'
  | 'notify_failed';

export interface IncidentEvent {
  id: number;
  at: string;
  kind: IncidentEventKind;
  message: string;
  actor: string | null;
}

export interface CheckDetail {
  check: Check;
  results: CheckResult[];
  uptime: { overall: number | null; bars: UptimeBar[] };
  incidents: Incident[];
}

export interface CheckInput {
  name: string;
  kind: CheckKind;
  target: string;
  intervalSeconds: number;
  timeoutMs: number;
  expectedStatus: number | null;
  latencyThresholdMs: number | null;
  failureThreshold: number;
  recoveryThreshold: number;
  enabled: boolean;
  isPublic: boolean;
}

export interface NotificationPayload {
  event: string;
  severity: 'critical' | 'warning' | 'resolved' | 'info';
  title: string;
  text: string;
  checkName?: string;
  incidentId?: number;
  url?: string;
}

export interface NotificationLog {
  id: number;
  incidentId: number | null;
  event: string;
  provider: string | null;
  status: 'pending' | 'sent' | 'failed';
  attempts: number;
  payload: NotificationPayload;
  lastError: string | null;
  createdAt: string;
  sentAt: string | null;
}

export interface Settings {
  workspace: Workspace;
  statusPageUrl: string;
  notifications: {
    mode: 'mock' | 'slack';
    effectiveProvider: 'mock' | 'slack';
    webhookConfigured: boolean;
    webhookPreview: string;
    usingServerWebhook: boolean;
    readOnly: boolean;
  };
}

export interface PublicIncident {
  id: number;
  checkName: string;
  title: string;
  cause: IncidentCause;
  status: IncidentStatus;
  openedAt: string;
  resolvedAt: string | null;
}

export interface PublicComponent {
  id: number;
  name: string;
  kind: CheckKind;
  state: ComponentState;
  uptime90: number | null;
  bars: UptimeBar[];
}

export interface PublicStatus {
  workspace: { name: string; slug: string };
  state: ComponentState;
  components: PublicComponent[];
  activeIncidents: PublicIncident[];
  recentIncidents: PublicIncident[];
  generatedAt: string;
}
