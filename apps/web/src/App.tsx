import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Link, Route, Routes } from 'react-router';

import { RequireAuth } from './components/AppLayout';
import { WakeBanner } from './components/WakeBanner';
import { ApiError } from './lib/api';
import { AuthProvider } from './lib/auth';
import { CheckDetailPage, EditCheckPage, NewCheckPage } from './pages/CheckPages';
import { IncidentDetailPage, IncidentsPage } from './pages/IncidentPages';
import { Landing } from './pages/Landing';
import { Overview } from './pages/Overview';
import { SettingsPage } from './pages/SettingsPage';
import { StatusPage } from './pages/StatusPage';

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        // The API client already retries cold starts; only retry other
        // transient failures once and never retry client errors.
        retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 1,
        refetchOnWindowFocus: true,
        staleTime: 5_000,
      },
    },
  });
}

function NotFound() {
  return (
    <main className="mx-auto flex min-h-dvh max-w-md flex-col items-center justify-center gap-3 px-4 text-center">
      <p className="text-sm font-semibold text-accent-strong">404</p>
      <h1 className="text-2xl font-semibold text-ink">Page not found</h1>
      <Link to="/" className="text-accent-strong hover:underline">
        Go home
      </Link>
    </main>
  );
}

export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<Landing />} />
      <Route path="/status/:slug" element={<StatusPage />} />
      <Route path="/app" element={<RequireAuth />}>
        <Route index element={<Overview />} />
        <Route path="checks/new" element={<NewCheckPage />} />
        <Route path="checks/:id" element={<CheckDetailPage />} />
        <Route path="checks/:id/edit" element={<EditCheckPage />} />
        <Route path="incidents" element={<IncidentsPage />} />
        <Route path="incidents/:id" element={<IncidentDetailPage />} />
        <Route path="settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<NotFound />} />
    </Routes>
  );
}

const queryClient = createQueryClient();

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <BrowserRouter>
          <WakeBanner />
          <AppRoutes />
        </BrowserRouter>
      </AuthProvider>
    </QueryClientProvider>
  );
}
