import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { request } from '../lib/api';
import type {
  Check,
  CheckDetail,
  CheckInput,
  CheckSummary,
  Incident,
  IncidentEvent,
  NotificationLog,
  PublicStatus,
  Session,
  Settings,
  User,
  Workspace,
} from './types';

export const keys = {
  me: ['me'] as const,
  checks: ['checks'] as const,
  check: (id: number) => ['checks', id] as const,
  incidents: (status: string) => ['incidents', status] as const,
  incident: (id: number) => ['incident', id] as const,
  settings: ['settings'] as const,
  notifications: ['notifications'] as const,
  publicStatus: (slug: string) => ['public-status', slug] as const,
};

// --- auth -----------------------------------------------------------------

export const authApi = {
  demo: () => request<Session>('/api/auth/demo', { method: 'POST', auth: false, retry: true }),
  login: (email: string, password: string) =>
    request<Session>('/api/auth/login', { method: 'POST', auth: false, retry: true, body: { email, password } }),
  register: (body: { name: string; email: string; password: string; workspaceName: string }) =>
    request<Session>('/api/auth/register', { method: 'POST', auth: false, body }),
};

export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: () => request<{ user: User; workspace: Workspace }>('/api/me'),
    staleTime: 60_000,
  });
}

// --- checks ---------------------------------------------------------------

export function useChecks() {
  return useQuery({
    queryKey: keys.checks,
    queryFn: () => request<{ checks: CheckSummary[] }>('/api/checks').then((r) => r.checks),
    refetchInterval: 15_000,
  });
}

export function useCheck(id: number) {
  return useQuery({
    queryKey: keys.check(id),
    queryFn: () => request<CheckDetail>(`/api/checks/${id}`),
    refetchInterval: 15_000,
  });
}

function useInvalidateChecks() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: keys.checks });
    void qc.invalidateQueries({ queryKey: ['incidents'] });
  };
}

export function useCreateCheck() {
  const invalidate = useInvalidateChecks();
  return useMutation({
    mutationFn: (input: CheckInput) =>
      request<{ check: Check }>('/api/checks', { method: 'POST', body: input }).then((r) => r.check),
    onSuccess: invalidate,
  });
}

export function useUpdateCheck(id: number) {
  const invalidate = useInvalidateChecks();
  return useMutation({
    mutationFn: (input: CheckInput) =>
      request<{ check: Check }>(`/api/checks/${id}`, { method: 'PUT', body: input }).then((r) => r.check),
    onSuccess: invalidate,
  });
}

export function useDeleteCheck() {
  const invalidate = useInvalidateChecks();
  return useMutation({
    mutationFn: (id: number) => request<void>(`/api/checks/${id}`, { method: 'DELETE' }),
    onSuccess: invalidate,
  });
}

export function useRunCheck(id: number) {
  const invalidate = useInvalidateChecks();
  return useMutation({
    mutationFn: () => request<{ check: Check }>(`/api/checks/${id}/run`, { method: 'POST' }),
    onSuccess: invalidate,
  });
}

export function useDrill(id: number) {
  const invalidate = useInvalidateChecks();
  return useMutation({
    mutationFn: (durationSeconds: number | null) =>
      durationSeconds === null
        ? request<{ check: Check }>(`/api/checks/${id}/drill`, { method: 'DELETE' })
        : request<{ check: Check }>(`/api/checks/${id}/drill`, { method: 'POST', body: { durationSeconds } }),
    onSuccess: invalidate,
  });
}

// --- incidents ------------------------------------------------------------

export type IncidentFilter = 'active' | 'resolved' | 'all';

export function useIncidents(status: IncidentFilter) {
  return useQuery({
    queryKey: keys.incidents(status),
    queryFn: () => request<{ incidents: Incident[] }>(`/api/incidents?status=${status}`).then((r) => r.incidents),
    refetchInterval: 15_000,
  });
}

export function useIncident(id: number) {
  return useQuery({
    queryKey: keys.incident(id),
    queryFn: () => request<{ incident: Incident; events: IncidentEvent[] }>(`/api/incidents/${id}`),
    refetchInterval: 10_000,
  });
}

function useIncidentMutation<TVars>(id: number, fn: (vars: TVars) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.incident(id) });
      void qc.invalidateQueries({ queryKey: ['incidents'] });
      void qc.invalidateQueries({ queryKey: keys.checks });
    },
  });
}

export function useAcknowledge(id: number) {
  return useIncidentMutation<void>(id, () => request(`/api/incidents/${id}/acknowledge`, { method: 'POST' }));
}

export function useResolve(id: number) {
  return useIncidentMutation<string>(id, (note) =>
    request(`/api/incidents/${id}/resolve`, { method: 'POST', body: { note } }),
  );
}

export function useAddNote(id: number) {
  return useIncidentMutation<string>(id, (message) =>
    request(`/api/incidents/${id}/notes`, { method: 'POST', body: { message } }),
  );
}

// --- settings & notifications ---------------------------------------------

export function useSettings() {
  return useQuery({ queryKey: keys.settings, queryFn: () => request<Settings>('/api/settings') });
}

export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { workspaceName: string; slackWebhookUrl?: string | null }) =>
      request<Settings>('/api/settings', { method: 'PUT', body }),
    onSuccess: (data) => {
      qc.setQueryData(keys.settings, data);
      void qc.invalidateQueries({ queryKey: keys.me });
    },
  });
}

export function useTestNotification() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => request('/api/settings/test-notification', { method: 'POST' }),
    onSuccess: () => {
      // The dispatcher delivers within a couple of seconds.
      setTimeout(() => void qc.invalidateQueries({ queryKey: keys.notifications }), 2500);
    },
  });
}

export function useNotifications() {
  return useQuery({
    queryKey: keys.notifications,
    queryFn: () =>
      request<{ notifications: NotificationLog[] }>('/api/notifications?limit=50').then((r) => r.notifications),
    refetchInterval: 15_000,
  });
}

// --- public ---------------------------------------------------------------

export function usePublicStatus(slug: string) {
  return useQuery({
    queryKey: keys.publicStatus(slug),
    queryFn: () => request<PublicStatus>(`/api/public/status/${encodeURIComponent(slug)}`, { auth: false }),
    refetchInterval: 60_000,
  });
}
