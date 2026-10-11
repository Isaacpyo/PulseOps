import { useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router';

import { useNotifications, useSettings, useTestNotification, useUpdateSettings } from '../api/hooks';
import type { NotificationLog, Settings } from '../api/types';
import {
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  Field,
  InlineAlert,
  Kbd,
  LoadingState,
  StatusBadge,
  TextInput,
} from '../components/ui';
import { ApiError, errorMessage } from '../lib/api';
import { formatDateTime } from '../lib/format';

export function SettingsPage() {
  const settings = useSettings();
  if (settings.isPending) return <LoadingState label="Loading settings…" />;
  if (settings.isError) return <ErrorState message={errorMessage(settings.error)} onRetry={() => void settings.refetch()} />;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold text-ink">Settings</h1>
        <p className="text-sm text-muted">Workspace details and where alerts are delivered.</p>
      </div>
      {settings.data.notifications.readOnly && (
        <InlineAlert tone="info">
          You’re in the shared demo workspace, so settings are read-only. Test notifications still work — they’re delivered by
          the mock provider and appear in the log below.
        </InlineAlert>
      )}
      <div className="grid gap-6 lg:grid-cols-2">
        <SettingsForm settings={settings.data} />
        <DeliveryCard settings={settings.data} />
      </div>
      <NotificationLogCard />
    </div>
  );
}

function SettingsForm({ settings }: { settings: Settings }) {
  const update = useUpdateSettings();
  const readOnly = settings.notifications.readOnly;
  const [name, setName] = useState(settings.workspace.name);
  const [webhook, setWebhook] = useState('');
  const [fields, setFields] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState(false);

  useEffect(() => setName(settings.workspace.name), [settings.workspace.name]);

  const save = (body: { workspaceName: string; slackWebhookUrl?: string | null }) => {
    setSaved(false);
    setFields({});
    update.mutate(body, {
      onSuccess: () => {
        setSaved(true);
        setWebhook('');
      },
      onError: (err) => {
        if (err instanceof ApiError) setFields(err.fields);
      },
    });
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) {
      setFields({ workspaceName: 'is required' });
      return;
    }
    save({ workspaceName: name.trim(), ...(webhook.trim() ? { slackWebhookUrl: webhook.trim() } : {}) });
  };

  return (
    <Card>
      <CardHeader title="Workspace" />
      <form onSubmit={submit} noValidate className="flex flex-col gap-4 p-4 sm:p-5">
        {update.isError && Object.keys(fields).length === 0 && <InlineAlert>{errorMessage(update.error)}</InlineAlert>}
        {saved && <InlineAlert tone="good">Settings saved.</InlineAlert>}
        <Field label="Workspace name" error={fields.workspaceName && `Workspace name ${fields.workspaceName}`}>
          {(p) => <TextInput {...p} value={name} onChange={(e) => setName(e.target.value)} disabled={readOnly} maxLength={80} />}
        </Field>
        <div className="text-sm">
          <span className="font-medium text-ink">Public status page</span>
          <p>
            <a href={settings.statusPageUrl} className="break-all text-accent-strong hover:underline" target="_blank" rel="noreferrer">
              {settings.statusPageUrl}
            </a>
          </p>
        </div>
        <Field
          label="Slack incoming webhook URL"
          error={fields.slackWebhookUrl && `Webhook ${fields.slackWebhookUrl}`}
          hint={
            settings.notifications.webhookConfigured ? (
              <>Current: <Kbd>{settings.notifications.webhookPreview}</Kbd>. Leave blank to keep it.</>
            ) : (
              <>Create one in Slack under Apps → Incoming Webhooks. Only <Kbd>hooks.slack.com</Kbd> URLs are accepted.</>
            )
          }
        >
          {(p) => (
            <TextInput
              {...p}
              type="url"
              value={webhook}
              onChange={(e) => setWebhook(e.target.value)}
              placeholder="https://hooks.slack.com/services/…"
              disabled={readOnly}
              autoComplete="off"
              spellCheck={false}
            />
          )}
        </Field>
        <div className="flex flex-wrap justify-end gap-2">
          {settings.notifications.webhookConfigured && !readOnly && (
            <Button
              variant="danger"
              disabled={update.isPending}
              onClick={() => save({ workspaceName: settings.workspace.name, slackWebhookUrl: null })}
            >
              Remove webhook
            </Button>
          )}
          <Button type="submit" variant="primary" loading={update.isPending} disabled={readOnly}>
            Save
          </Button>
        </div>
      </form>
    </Card>
  );
}

function DeliveryCard({ settings }: { settings: Settings }) {
  const test = useTestNotification();
  const n = settings.notifications;
  const provider = n.effectiveProvider === 'slack' ? 'Slack' : 'Mock provider';

  return (
    <Card>
      <CardHeader title="Alert delivery" />
      <div className="flex flex-col gap-4 p-4 text-sm sm:p-5">
        <div className="flex items-center gap-3">
          <StatusBadge tone={n.effectiveProvider === 'slack' ? 'good' : 'info'}>{provider}</StatusBadge>
          <span className="text-ink-2">
            Server mode <Kbd>NOTIFIER={n.mode}</Kbd>
          </span>
        </div>
        <p className="text-ink-2">
          {n.effectiveProvider === 'slack'
            ? n.usingServerWebhook
              ? 'Alerts go to the server’s default Slack webhook. Add your own above to route them to your channel.'
              : 'Alerts go to your workspace’s Slack webhook.'
            : n.mode === 'mock'
              ? 'This server runs with the mock notifier: alerts are recorded in the log below instead of being posted to Slack. Set NOTIFIER=slack on the API to deliver for real.'
              : 'No Slack webhook is configured yet, so alerts fall back to the mock provider. Add a webhook to deliver to Slack.'}
        </p>
        <p className="text-ink-2">
          PulseOps sends a message when an incident opens, escalates, is acknowledged and resolves. Delivery uses an outbox with
          retries (15s → 10m backoff, 5 attempts), and each delivery is recorded on the incident timeline.
        </p>
        <div>
          <Button loading={test.isPending} onClick={() => test.mutate()}>
            Send test notification
          </Button>
          {test.isSuccess && <p role="status" className="mt-2 text-good-ink">Queued — it will appear in the log within a few seconds.</p>}
          {test.isError && <p role="alert" className="mt-2 text-critical-ink">{errorMessage(test.error)}</p>}
        </div>
      </div>
    </Card>
  );
}

const statusTone = { sent: 'good', pending: 'warning', failed: 'critical' } as const;

function NotificationLogCard() {
  const log = useNotifications();
  return (
    <Card>
      <CardHeader title="Notification log" description="Most recent 50 deliveries" />
      {log.isPending ? (
        <LoadingState />
      ) : log.isError ? (
        <ErrorState message={errorMessage(log.error)} onRetry={() => void log.refetch()} />
      ) : log.data.length === 0 ? (
        <EmptyState title="No notifications yet" body="They appear here when incidents change state." />
      ) : (
        <ul className="divide-y divide-line">
          {log.data.map((n) => (
            <NotificationRow key={n.id} n={n} />
          ))}
        </ul>
      )}
    </Card>
  );
}

function NotificationRow({ n }: { n: NotificationLog }) {
  return (
    <li className="flex flex-col gap-1 px-4 py-3 sm:flex-row sm:items-start sm:gap-4 sm:px-5">
      <div className="flex shrink-0 items-center gap-2 sm:w-44">
        <StatusBadge tone={statusTone[n.status]}>{n.status === 'sent' ? `Sent · ${n.provider ?? ''}` : n.status}</StatusBadge>
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium break-words text-ink">{n.payload.title}</div>
        {n.payload.text && <div className="text-xs break-words text-muted">{n.payload.text}</div>}
        {n.lastError && <div className="text-xs text-critical-ink">Last error: {n.lastError}</div>}
      </div>
      <div className="shrink-0 text-xs text-muted sm:text-right">
        <div>{formatDateTime(n.createdAt)}</div>
        {n.incidentId && (
          <Link to={`/app/incidents/${n.incidentId}`} className="text-accent-strong hover:underline">
            Incident #{n.incidentId}
          </Link>
        )}
      </div>
    </li>
  );
}
