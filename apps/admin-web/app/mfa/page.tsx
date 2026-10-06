'use client';

import { FormEvent, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';

import { useAuth } from '../../components/auth-provider';
import { APIError, apiRequest } from '../../lib/api';
import { defaultLandingPath, permissionForPath } from '../../lib/navigation';
import { hasPermission, safeReturnPath } from '../../lib/session';

function permittedReturnPath(path: string, permissions: readonly string[] | undefined) {
  const required = permissionForPath(path);
  return required && !hasPermission(permissions, required) ? defaultLandingPath(permissions) : path;
}

function currentReturnPath(): string {
  if (typeof window === 'undefined') return '/';
  return safeReturnPath(new URLSearchParams(window.location.search).get('returnTo'));
}

export default function MFAPage() {
  const router = useRouter();
  const { refresh } = useAuth();
  const [code, setCode] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string>();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const challenge = sessionStorage.getItem('campaign_mfa_challenge') || '';
    if (!challenge) {
      setError('The sign-in challenge has expired. Start sign in again.');
      return;
    }
    setSubmitting(true);
    setError(undefined);
    try {
      await apiRequest('/v1/auth/mfa/verify', {
        method: 'POST',
        body: JSON.stringify({ challengeToken: challenge, code: code.trim() })
      });
      sessionStorage.removeItem('campaign_mfa_challenge');
      const session = await refresh();
      router.replace(permittedReturnPath(currentReturnPath(), session?.permissions));
    } catch (cause) {
      setError(cause instanceof APIError && cause.status === 401
        ? 'That verification code could not be accepted. Check the current code and try again.'
        : cause instanceof Error ? cause.message : 'Verification could not be completed.');
      setSubmitting(false);
    }
  }

  return (
    <section className="auth-card" aria-labelledby="mfa-title">
      <div className="auth-brand"><span className="brand-mark" aria-hidden="true">CO</span><span><strong>Campaign Ops</strong><small>Internal control plane</small></span></div>
      <div className="auth-heading">
        <span className="eyebrow">Multi-factor verification</span>
        <h1 id="mfa-title">Verify it’s you</h1>
        <p>Enter the current six-digit code from your authenticator.</p>
      </div>
      {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}
      <form className="form-stack" onSubmit={submit}>
        <label>Authentication code<input className="otp-input" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, '').slice(0, 6))} required /></label>
        <button className="primary auth-submit" type="submit" disabled={submitting || code.length !== 6}>{submitting ? 'Verifying…' : 'Verify and continue'}</button>
      </form>
      <Link className="auth-secondary-link" href="/login">Use a different account</Link>
    </section>
  );
}
