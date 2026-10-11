'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import { APIError, apiRequest } from '../lib/api';
import type { SessionContext } from '../lib/session';

type AuthStatus = 'loading' | 'authenticated' | 'anonymous';

type AuthContextValue = {
  status: AuthStatus;
  session?: SessionContext;
  error?: string;
  refresh: () => Promise<SessionContext | undefined>;
  logout: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('loading');
  const [session, setSession] = useState<SessionContext>();
  const [error, setError] = useState<string>();

  const refresh = useCallback(async () => {
    try {
      const next = await apiRequest<SessionContext>('/v1/auth/me');
      setSession(next);
      setStatus('authenticated');
      setError(undefined);
      return next;
    } catch (cause) {
      setSession(undefined);
      if (cause instanceof APIError && cause.status === 401) {
        setStatus('anonymous');
        setError(undefined);
        return undefined;
      }
      setStatus('anonymous');
      setError(cause instanceof Error ? cause.message : 'Authentication context is unavailable.');
      return undefined;
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    void apiRequest<SessionContext>('/v1/auth/me')
      .then((next) => {
        if (cancelled) return;
        setSession(next);
        setStatus('authenticated');
        setError(undefined);
      })
      .catch((cause) => {
        if (cancelled) return;
        setSession(undefined);
        setStatus('anonymous');
        setError(cause instanceof APIError && cause.status === 401
          ? undefined
          : cause instanceof Error ? cause.message : 'Authentication context is unavailable.');
      });
    return () => { cancelled = true; };
  }, []);

  const logout = useCallback(async () => {
    await apiRequest<void>('/v1/auth/logout', { method: 'POST' });
    setSession(undefined);
    setStatus('anonymous');
  }, []);

  const value = useMemo<AuthContextValue>(() => ({ status, session, error, refresh, logout }), [status, session, error, refresh, logout]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error('useAuth must be used inside AuthProvider');
  return value;
}
