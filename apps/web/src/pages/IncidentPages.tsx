import { useState, type FormEvent } from 'react';
import { Link, useParams, useSearchParams } from 'react-router';

import { useAcknowledge, useAddNote, useIncident, useIncidents, useResolve, type IncidentFilter } from '../api/hooks';
import type { Incident, IncidentEvent, IncidentEventKind } from '../api/types';
import {
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  Field,
  InlineAlert,
  LoadingState,
  StatusBadge,
  cx,
} from '../components/ui';
import { ApiError, errorMessage } from '../lib/api';
import { formatDateTime, formatDuration, formatRelative } from '../lib/format';
import { incidentStatusMeta } from '../lib/status';

const filters: { value: IncidentFilter; label: string }[] = [
  { value: 'active', label: 'Active' },
  { value: 'resolved', label: 'Resolved' },
  { value: 'all', label: 'All' },
];

export function incidentDuration(inc: Pick<Incident, 'openedAt' | 'resolvedAt'>, now = new Date()): string {
  const end = inc.resolvedAt ? new Date(inc.resolvedAt) : now;
  return formatDuration(end.getTime() - new Date(inc.openedAt).getTime());
}

export function IncidentsPage() {
  const [params, setParams] = useSearchParams();
  const raw = params.get('status');
  const filter: IncidentFilter = raw === 'resolved' || raw === 'all' ? raw : 'active';
  const incidents = useIncidents(filter);

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold text-ink">Incidents</h1>
        <p className="text-sm text-muted">Opened automatically by alert rules; acknowledge and resolve them here.</p>
      </div>
      <div role="group" aria-label="Filter incidents" className="flex w-fit gap-1 rounded-lg bg-wash p-1">
        {filters.map((f) => (
          <button
            key={f.value}
            type="button"
            aria-pressed={filter === f.value}
            onClick={() => setParams(f.value === 'active' ? {} : { status: f.value })}
            className={cx(
              'min-h-9 rounded-md px-4 text-sm font-medium',
              filter === f.value ? 'bg-raised text-ink shadow-sm' : 'text-ink-2 hover:text-ink',
            )}
          >
            {f.label}
          </button>
        ))}
      </div>
      <Card>
        {incidents.isPending ? (
          <LoadingState label="Loading incidents…" />
        ) : incidents.isError ? (
          <ErrorState message={errorMessage(incidents.error)} onRetry={() => void incidents.refetch()} />
        ) : incidents.data.length === 0 ? (
          <EmptyState
            title={filter === 'active' ? 'No active incidents' : 'No incidents'}
            body={filter === 'active' ? 'Everything is healthy. Try “Simulate outage” on a check to see the full flow.' : undefined}
          />
        ) : (
          <ul className="divide-y divide-line">
            {incidents.data.map((inc) => (
              <li key={inc.id}>
                <Link
                  to={`/app/incidents/${inc.id}`}
                  className="flex flex-col gap-1 px-4 py-3 hover:bg-wash sm:flex-row sm:items-center sm:gap-4 sm:px-5"
                >
                  <div className="flex items-center gap-2 sm:w-36">
                    <StatusBadge tone={incidentStatusMeta[inc.status].tone}>{incidentStatusMeta[inc.status].label}</StatusBadge>
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="font-medium text-ink">{inc.title}</div>
                    <div className="text-xs text-muted">
                      #{inc.id} · {inc.cause === 'down' ? 'Outage' : 'Degraded performance'}
                      {inc.acknowledgedBy && ` · acknowledged by ${inc.acknowledgedBy}`}
                    </div>
                  </div>
                  <div className="text-sm text-muted sm:text-right">
                    <div>{formatRelative(inc.openedAt)}</div>
                    <div className="text-xs">{inc.resolvedAt ? `lasted ${incidentDuration(inc)}` : `ongoing ${incidentDuration(inc)}`}</div>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

const eventStyle: Record<IncidentEventKind, { label: string; dot: string }> = {
  opened: { label: 'Opened', dot: 'bg-critical' },
  escalated: { label: 'Escalated', dot: 'bg-critical' },
  acknowledged: { label: 'Acknowledged', dot: 'bg-warning' },
  resolved: { label: 'Resolved', dot: 'bg-good' },
  auto_resolved: { label: 'Auto-resolved', dot: 'bg-good' },
  note: { label: 'Note', dot: 'bg-accent' },
  notified: { label: 'Notification sent', dot: 'bg-line-strong' },
  notify_failed: { label: 'Notification failed', dot: 'bg-serious' },
};

export function IncidentDetailPage() {
  const id = Number(useParams().id);
  const q = useIncident(id);

  if (q.isPending) return <LoadingState label="Loading incident…" />;
  if (q.isError) {
    if (q.error instanceof ApiError && q.error.status === 404) {
      return <EmptyState title="Incident not found" action={<Link to="/app/incidents" className="text-accent-strong">Back to incidents</Link>} />;
    }
    return <ErrorState message={errorMessage(q.error)} onRetry={() => void q.refetch()} />;
  }
  const { incident, events } = q.data;
  const meta = incidentStatusMeta[incident.status];

  return (
    <div className="flex flex-col gap-6">
      <Link to="/app/incidents" className="text-sm font-medium text-accent-strong hover:underline">
        ← Incidents
      </Link>
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-2xl font-semibold text-ink">{incident.title}</h1>
            <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
          </div>
          <p className="mt-1 text-sm text-muted">
            Incident #{incident.id} · {incident.cause === 'down' ? 'Outage' : 'Degraded performance'} ·{' '}
            {incident.checkId ? (
              <Link to={`/app/checks/${incident.checkId}`} className="text-accent-strong hover:underline">
                {incident.checkName}
              </Link>
            ) : (
              `${incident.checkName} (deleted)`
            )}
          </p>
        </div>
        <IncidentActions incident={incident} />
      </div>

      <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          ['Opened', formatDateTime(incident.openedAt)],
          ['Acknowledged', incident.acknowledgedAt ? `${formatDateTime(incident.acknowledgedAt)}${incident.acknowledgedBy ? ` by ${incident.acknowledgedBy}` : ''}` : '—'],
          ['Resolved', incident.resolvedAt ? `${formatDateTime(incident.resolvedAt)}${incident.resolvedBy ? ` by ${incident.resolvedBy}` : ' (auto)'}` : '—'],
          [incident.resolvedAt ? 'Duration' : 'Ongoing for', incidentDuration(incident)],
        ].map(([label, value]) => (
          <div key={label} className="rounded-xl border border-line bg-surface px-4 py-3">
            <dt className="text-sm text-muted">{label}</dt>
            <dd className="mt-0.5 text-sm font-medium text-ink">{value}</dd>
          </div>
        ))}
      </dl>

      <Card>
        <CardHeader title="Timeline" description="Every state change, notification and note, oldest first." />
        <Timeline events={events} />
        <NoteForm incidentId={incident.id} />
      </Card>
    </div>
  );
}

function Timeline({ events }: { events: IncidentEvent[] }) {
  return (
    <ol className="px-4 py-5 sm:px-6">
      {events.map((ev, i) => {
        const style = eventStyle[ev.kind] ?? { label: ev.kind, dot: 'bg-line-strong' };
        const last = i === events.length - 1;
        return (
          <li key={ev.id} className="relative flex gap-4 pb-6 last:pb-0">
            {!last && <span className="absolute top-4 left-[5px] h-full w-px bg-line" aria-hidden="true" />}
            <span className={cx('relative mt-1.5 size-3 shrink-0 rounded-full ring-4 ring-surface', style.dot)} aria-hidden="true" />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-baseline gap-x-2">
                <span className="text-sm font-semibold text-ink">{style.label}</span>
                {ev.actor && <span className="text-sm text-ink-2">by {ev.actor}</span>}
                <time dateTime={ev.at} className="text-xs text-muted">
                  {formatDateTime(ev.at)}
                </time>
              </div>
              <p className={cx('mt-0.5 text-sm break-words', ev.kind === 'note' ? 'text-ink' : 'text-ink-2')}>{ev.message}</p>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function IncidentActions({ incident }: { incident: Incident }) {
  const ack = useAcknowledge(incident.id);
  const resolve = useResolve(incident.id);
  const [resolving, setResolving] = useState(false);
  const [note, setNote] = useState('');
  const error = ack.error ?? resolve.error;

  if (incident.status === 'resolved') return null;

  return (
    <div className="flex flex-col gap-2 lg:items-end">
      {!resolving ? (
        <div className="flex flex-wrap gap-2">
          {incident.status === 'open' && (
            <Button variant="primary" loading={ack.isPending} onClick={() => ack.mutate()}>
              Acknowledge
            </Button>
          )}
          <Button onClick={() => setResolving(true)}>Resolve…</Button>
        </div>
      ) : (
        <form
          className="flex w-full flex-col gap-2 sm:w-96"
          onSubmit={(e) => {
            e.preventDefault();
            resolve.mutate(note.trim(), { onSuccess: () => setResolving(false) });
          }}
        >
          <Field label="Resolution note (optional)">
            {(p) => (
              <textarea
                {...p}
                value={note}
                onChange={(e) => setNote(e.target.value)}
                rows={2}
                maxLength={2000}
                placeholder="What fixed it?"
                className="w-full rounded-lg border border-line-strong bg-raised px-3 py-2 text-sm text-ink"
              />
            )}
          </Field>
          <div className="flex gap-2 sm:justify-end">
            <Button variant="ghost" onClick={() => setResolving(false)}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" loading={resolve.isPending}>
              Resolve incident
            </Button>
          </div>
        </form>
      )}
      {error && <InlineAlert>{errorMessage(error)}</InlineAlert>}
    </div>
  );
}

function NoteForm({ incidentId }: { incidentId: number }) {
  const add = useAddNote(incidentId);
  const [message, setMessage] = useState('');
  const [error, setError] = useState<string | undefined>();

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const text = message.trim();
    if (!text) {
      setError('Write a note first');
      return;
    }
    setError(undefined);
    add.mutate(text, {
      onSuccess: () => setMessage(''),
      onError: (err) => setError(errorMessage(err)),
    });
  };

  return (
    <form onSubmit={submit} className="flex flex-col gap-2 border-t border-line px-4 py-4 sm:flex-row sm:items-end sm:px-6">
      <div className="flex-1">
        <Field label="Add a note to the timeline" error={error}>
          {(p) => (
            <input
              {...p}
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              maxLength={2000}
              placeholder="e.g. Rolled back deploy 4f2a1c"
              className="min-h-10 w-full rounded-lg border border-line-strong bg-raised px-3 py-2 text-sm text-ink"
            />
          )}
        </Field>
      </div>
      <Button type="submit" loading={add.isPending}>
        Add note
      </Button>
    </form>
  );
}
