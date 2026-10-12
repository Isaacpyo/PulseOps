import { useEffect, useState, type FormEvent } from 'react';
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router';

import { authApi } from '../api/hooks';
import type { Session } from '../api/types';
import { Logo } from '../components/Logo';
import { Button, Field, InlineAlert, TextInput, cx } from '../components/ui';
import { ApiError, errorMessage, prewarm } from '../lib/api';
import { useAuth } from '../lib/auth';

const features = [
  {
    title: 'HTTP & TCP checks',
    body: 'Go workers probe every target on its own interval with an SSRF-safe dialer and record latency, status and errors.',
  },
  {
    title: 'Alert rules → incidents',
    body: 'Consecutive-failure and latency-threshold rules open, escalate and auto-resolve incidents with a full timeline.',
  },
  {
    title: 'Slack notifications',
    body: 'A transactional outbox delivers alerts to Slack with retries — or to a mock provider when no webhook is set.',
  },
  {
    title: 'Status page & metrics',
    body: 'A public page with 90-day uptime bars, plus Prometheus metrics and Grafana dashboards for the platform itself.',
  },
];

function safeNext(next: string | null): string {
  return next && next.startsWith('/app') ? next : '/app';
}

export function Landing() {
  const { session, signIn } = useAuth();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const next = safeNext(params.get('next'));
  const [demoLoading, setDemoLoading] = useState(false);
  const [demoError, setDemoError] = useState<string | null>(null);

  useEffect(() => {
    // Start waking the free-tier API as soon as someone lands here.
    prewarm();
  }, []);

  if (session) return <Navigate to={next} replace />;

  const finish = (s: Session) => {
    signIn(s);
    navigate(next, { replace: true });
  };

  const tryDemo = async () => {
    setDemoError(null);
    setDemoLoading(true);
    try {
      finish(await authApi.demo());
    } catch (err) {
      setDemoError(errorMessage(err));
      setDemoLoading(false);
    }
  };

  return (
    <div className="min-h-dvh">
      <header className="mx-auto flex max-w-6xl items-center justify-between px-4 py-5">
        <span className="flex items-center gap-2 text-lg font-semibold text-ink">
          <Logo />
          PulseOps
        </span>
        <Link to="/status/demo" className="text-sm font-medium text-accent-strong hover:underline">
          Demo status page
        </Link>
      </header>

      <main className="mx-auto grid max-w-6xl gap-10 px-4 pt-6 pb-16 lg:grid-cols-[1.15fr_1fr] lg:gap-16 lg:pt-14">
        <section>
          <p className="text-sm font-semibold tracking-wide text-accent-strong uppercase">Monitoring &amp; incident response</p>
          <h1 className="mt-3 text-4xl font-bold tracking-tight text-ink sm:text-5xl">
            Know it’s down before your users do.
          </h1>
          <p className="mt-5 max-w-xl text-lg text-ink-2">
            PulseOps probes your HTTP and TCP endpoints, turns failures into incidents with acknowledge/resolve
            workflows, pings Slack, and publishes a status page with 90-day uptime history.
          </p>
          <div className="mt-8 flex flex-col gap-3 sm:flex-row sm:items-center">
            <Button variant="primary" className="min-h-12 px-6 text-base" onClick={tryDemo} loading={demoLoading}>
              {demoLoading ? 'Opening the demo…' : 'Try the demo'}
            </Button>
            <span className="text-sm text-muted">One click, no sign-up. Uses a shared, seeded workspace.</span>
          </div>
          {demoError && (
            <div className="mt-4 max-w-xl">
              <InlineAlert>{demoError}</InlineAlert>
            </div>
          )}

          <ul className="mt-12 grid gap-6 sm:grid-cols-2">
            {features.map((f) => (
              <li key={f.title} className="border-l-2 border-accent/40 pl-4">
                <h2 className="font-semibold text-ink">{f.title}</h2>
                <p className="mt-1 text-sm text-ink-2">{f.body}</p>
              </li>
            ))}
          </ul>
        </section>

        <AuthCard onSession={finish} />
      </main>
    </div>
  );
}

function AuthCard({ onSession }: { onSession: (s: Session) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [name, setName] = useState('');
  const [workspaceName, setWorkspaceName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [fields, setFields] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    const local: Record<string, string> = {};
    if (mode === 'register' && !name.trim()) local.name = 'Name is required';
    if (!email.includes('@')) local.email = 'Enter a valid email address';
    if (password.length < 8) local.password = 'Password must be at least 8 characters';
    setFields(local);
    if (Object.keys(local).length) return;

    setLoading(true);
    try {
      const s =
        mode === 'login'
          ? await authApi.login(email.trim(), password)
          : await authApi.register({ name: name.trim(), email: email.trim(), password, workspaceName: workspaceName.trim() });
      onSession(s);
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fields).length) {
        setFields(Object.fromEntries(Object.entries(err.fields).map(([k, v]) => [k, `${capitalise(k)} ${v}`])));
      } else {
        setError(errorMessage(err));
      }
      setLoading(false);
    }
  };

  return (
    <section aria-labelledby="auth-title" className="h-fit rounded-2xl border border-line bg-surface p-5 shadow-sm sm:p-7">
      <div role="tablist" aria-label="Account" className="mb-6 grid grid-cols-2 rounded-lg bg-wash p-1">
        {(['login', 'register'] as const).map((m) => (
          <button
            key={m}
            type="button"
            role="tab"
            aria-selected={mode === m}
            onClick={() => {
              setMode(m);
              setFields({});
              setError(null);
            }}
            className={cx(
              'min-h-9 rounded-md text-sm font-medium',
              mode === m ? 'bg-raised text-ink shadow-sm' : 'text-ink-2 hover:text-ink',
            )}
          >
            {m === 'login' ? 'Sign in' : 'Create account'}
          </button>
        ))}
      </div>
      <h2 id="auth-title" className="sr-only">
        {mode === 'login' ? 'Sign in' : 'Create an account'}
      </h2>
      <form onSubmit={submit} noValidate className="flex flex-col gap-4">
        {error && <InlineAlert>{error}</InlineAlert>}
        {mode === 'register' && (
          <>
            <Field label="Your name" error={fields.name}>
              {(p) => <TextInput {...p} value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />}
            </Field>
            <Field label="Workspace name" error={fields.workspaceName} hint="Shown on your public status page. Optional.">
              {(p) => <TextInput {...p} value={workspaceName} onChange={(e) => setWorkspaceName(e.target.value)} autoComplete="organization" />}
            </Field>
          </>
        )}
        <Field label="Email" error={fields.email}>
          {(p) => <TextInput {...p} type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" />}
        </Field>
        <Field label="Password" error={fields.password} hint={mode === 'register' ? 'At least 8 characters.' : undefined}>
          {(p) => (
            <TextInput
              {...p}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
            />
          )}
        </Field>
        <Button type="submit" variant="primary" loading={loading} className="mt-1">
          {mode === 'login' ? 'Sign in' : 'Create account'}
        </Button>
      </form>
    </section>
  );
}

function capitalise(field: string): string {
  const label = field === 'workspaceName' ? 'workspace name' : field;
  return label.charAt(0).toUpperCase() + label.slice(1);
}
