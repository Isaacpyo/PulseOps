import { QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { createQueryClient } from '../App';
import { AuthProvider } from '../lib/auth';
import { Landing } from './Landing';

const session = {
  token: 'demo-token',
  expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
  user: { id: 1, workspaceId: 1, email: 'demo@pulseops.dev', name: 'Demo Operator', createdAt: '' },
  workspace: { id: 1, name: 'Acme Cloud', slug: 'demo', isDemo: true, createdAt: '' },
};

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

function renderLanding() {
  return render(
    <QueryClientProvider client={createQueryClient()}>
      <AuthProvider>
        <MemoryRouter initialEntries={['/']}>
          <Routes>
            <Route path="/" element={<Landing />} />
            <Route path="/app" element={<p>Console for {session.workspace.name}</p>} />
          </Routes>
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  );
}

it('"Try the demo" signs into the demo account and opens the console', async () => {
  fetchMock.mockImplementation(async (input) => {
    const url = String(input);
    if (url.endsWith('/api/auth/demo')) return new Response(JSON.stringify(session), { status: 200 });
    return new Response(JSON.stringify({ status: 'ok' }), { status: 200 });
  });
  renderLanding();
  await userEvent.click(screen.getByRole('button', { name: 'Try the demo' }));
  expect(await screen.findByText('Console for Acme Cloud')).toBeInTheDocument();
  expect(JSON.parse(localStorage.getItem('pulseops.session')!)).toMatchObject({ token: 'demo-token' });
});

it('shows a friendly error when the demo is unavailable', async () => {
  fetchMock.mockImplementation(async (input) => {
    if (String(input).endsWith('/api/auth/demo')) {
      return new Response(JSON.stringify({ error: { code: 'demo_disabled', message: 'The demo account is not enabled on this server.' } }), {
        status: 404,
      });
    }
    return new Response('{}', { status: 200 });
  });
  renderLanding();
  await userEvent.click(screen.getByRole('button', { name: 'Try the demo' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('not enabled');
});

it('validates the sign-in form before calling the API', async () => {
  fetchMock.mockResolvedValue(new Response('{}', { status: 200 }));
  renderLanding();
  await userEvent.click(screen.getByRole('button', { name: 'Sign in', pressed: undefined, selected: undefined, exact: true } as never));
  expect(await screen.findByText('Enter a valid email address')).toBeInTheDocument();
  expect(fetchMock.mock.calls.some(([u]) => String(u).includes('/api/auth/login'))).toBe(false);
});
