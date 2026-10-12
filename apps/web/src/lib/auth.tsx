import { useQueryClient } from '@tanstack/react-query';
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';

import type { Session, User, Workspace } from '../api/types';
import { configureApi, request } from './api';
import { loadSession, saveSession } from './session';

type AuthContextValue = {
  session: Session | null;
  signIn: (s: Session) => void;
  signOut: (opts?: { remote?: boolean }) => void;
  updateIdentity: (user: User, workspace: Workspace) => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

let currentToken: string | null = null;

export function AuthProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [session, setSession] = useState<Session | null>(() => {
    const s = loadSession();
    currentToken = s?.token ?? null;
    return s;
  });

  const apply = useCallback((s: Session | null) => {
    currentToken = s?.token ?? null;
    saveSession(s);
    setSession(s);
  }, []);

  const signOut = useCallback(
    ({ remote = true }: { remote?: boolean } = {}) => {
      if (remote && currentToken) {
        void request('/api/auth/logout', { method: 'POST' }).catch(() => {});
      }
      apply(null);
      queryClient.clear();
    },
    [apply, queryClient],
  );

  // Configure the API client during render (idempotent) so child effects
  // never fire requests before the token provider is wired up.
  configureApi({
    getToken: () => currentToken,
    onUnauthorized: () => {
      if (currentToken) signOut({ remote: false });
    },
  });

  const value = useMemo<AuthContextValue>(
    () => ({
      session,
      signIn: (s) => {
        queryClient.clear();
        apply(s);
      },
      signOut,
      updateIdentity: (user, workspace) => {
        setSession((prev) => {
          if (!prev) return prev;
          const next = { ...prev, user, workspace };
          saveSession(next);
          return next;
        });
      },
    }),
    [session, apply, signOut, queryClient],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>');
  return ctx;
}
