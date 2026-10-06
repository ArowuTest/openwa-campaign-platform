'use client';

import { FormEvent, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';

import { useAuth } from '../../components/auth-provider';
import { APIError, apiRequest } from '../../lib/api';
import { defaultLandingPath, permissionForPath } from '../../lib/navigation';
import { hasPermission, safeReturnPath } from '../../lib/session';

type LoginResult = {
  mfaRequired: boolean;
  challengeToken?: string;
};

function permittedReturnPath(path: string, permissions: readonly string[] | undefined) {
  const required = permissionForPath(path);
  return required && !hasPermission(permissions, required) ? defaultLandingPath(permissions) : path;
}

function currentReturnPath(): string {
  if (typeof window === 'undefined') return '/';
  return safeReturnPath(new URLSearchParams(window.location.search).get('returnTo'));
}

export default function LoginPage() {
  const router = useRouter();
  const { status, session, refresh } = useAuth();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string>();

  useEffect(() => {
    if (status === 'authenticated' && session) {
      router.replace(permittedReturnPath(currentReturnPath(), session.permissions));
    }
  }, [router, session, status]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    const returnTo = currentReturnPath();
    setSubmitting(true);
    setError(undefined);
    try {
      const result = await apiRequest<LoginResult>('/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify({ email: email.trim(), password })
      });
      if (result.mfaRequired) {
        if (!result.challengeToken) throw new Error('The MFA challenge was not returned.');
        sessionStorage.setItem('campaign_mfa_challenge', result.challengeToken);
        router.push(`/mfa?returnTo=${encodeURIComponent(returnTo)}`);
        return;
      }
      const next = await refresh();
      router.replace(permittedReturnPath(returnTo, next?.permissions));
    } catch (cause) {
      if (cause instanceof APIError && cause.status === 429) {
        setError('This account is temporarily locked after repeated failed sign-in attempts.');
      } else if (cause instanceof APIError && cause.status === 401) {
        setError('The email or password could not be verified.');
      } else {
        setError(cause instanceof Error ? cause.message : 'Sign in could not be completed.');
      }
      setSubmitting(false);
    }
  }

  return (
    <section className="auth-card" aria-labelledby="sign-in-title">
      <div className="auth-brand"><span className="brand-mark" aria-hidden="true">CO</span><span><strong>Campaign Ops</strong><small>Internal control plane</small></span></div>
      <div className="auth-heading">
        <span className="eyebrow">Authorised operators only</span>
        <h1 id="sign-in-title">Sign in</h1>
        <p>Use your individual operations account. Shared accounts are not permitted.</p>
      </div>
      {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}
      <form className="form-stack" onSubmit={submit}>
        <label>Email<input type="email" name="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required /></label>
        <label>Password<input type="password" name="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required /></label>
        <button className="primary auth-submit" type="submit" disabled={submitting}>{submitting ? 'Verifying…' : 'Continue securely'}</button>
      </form>
      <div className="auth-footnote">Activity is attributable to your operator identity and may be audited.</div>
    </section>
  );
}
