import { useEffect } from 'react';
import { Link, NavLink, Navigate, Outlet, useLocation, useNavigate } from 'react-router';

import { useIncidents, useMe } from '../api/hooks';
import { useAuth } from '../lib/auth';
import { Logo } from './Logo';
import { Button, cx } from './ui';

const nav = [
  { to: '/app', label: 'Overview', end: true },
  { to: '/app/incidents', label: 'Incidents', end: false },
  { to: '/app/settings', label: 'Settings', end: false },
];

export function RequireAuth() {
  const { session } = useAuth();
  const location = useLocation();
  if (!session) {
    return <Navigate to={`/?next=${encodeURIComponent(location.pathname)}`} replace />;
  }
  return <AppLayout />;
}

function AppLayout() {
  const { session, signOut, updateIdentity } = useAuth();
  const navigate = useNavigate();
  const me = useMe();
  const active = useIncidents('active');
  const activeCount = active.data?.length ?? 0;

  useEffect(() => {
    if (me.data) updateIdentity(me.data.user, me.data.workspace);
    // updateIdentity is stable enough; only react to fresh data.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [me.data]);

  if (!session) return null;
  const { workspace, user } = session;

  return (
    <div className="min-h-dvh">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-lg focus:bg-raised focus:px-3 focus:py-2"
      >
        Skip to content
      </a>
      <header className="border-b border-line bg-surface">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
          <Link to="/app" className="flex items-center gap-2 font-semibold text-ink">
            <Logo />
            PulseOps
          </Link>
          <nav aria-label="Main" className="order-3 -mx-1 flex w-full gap-1 overflow-x-auto sm:order-none sm:w-auto">
            {nav.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  cx(
                    'flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium whitespace-nowrap',
                    isActive ? 'bg-accent-wash text-accent-strong' : 'text-ink-2 hover:bg-wash hover:text-ink',
                  )
                }
              >
                {item.label}
                {item.label === 'Incidents' && activeCount > 0 && (
                  <span className="rounded-full bg-critical px-1.5 text-xs font-semibold text-white tabular">
                    {activeCount}
                    <span className="sr-only"> active</span>
                  </span>
                )}
              </NavLink>
            ))}
            <a
              href={`/status/${workspace.slug}`}
              target="_blank"
              rel="noreferrer"
              className="rounded-lg px-3 py-2 text-sm font-medium whitespace-nowrap text-ink-2 hover:bg-wash hover:text-ink"
            >
              Status page ↗<span className="sr-only"> (opens in a new tab)</span>
            </a>
          </nav>
          <div className="ml-auto flex items-center gap-3">
            <div className="hidden text-right text-xs leading-tight md:block">
              <div className="font-medium text-ink">{workspace.name}</div>
              <div className="text-muted">{user.name}</div>
            </div>
            {workspace.isDemo && (
              <span className="rounded-full border border-accent/30 bg-accent-wash px-2 py-0.5 text-xs font-medium text-accent-strong">
                Demo
              </span>
            )}
            <Button
              variant="ghost"
              className="min-h-9 px-3"
              onClick={() => {
                signOut();
                navigate('/');
              }}
            >
              Sign out
            </Button>
          </div>
        </div>
      </header>
      <main id="main" className="mx-auto max-w-6xl px-4 py-6 sm:py-8">
        <Outlet />
      </main>
    </div>
  );
}
