'use client';

import { FormEvent, useState } from 'react';
import { useRouter } from 'next/navigation';

import { APIError, apiRequest } from '../../lib/api';
import { safeReturnPath } from '../../lib/session';

function currentReturnPath(): string {
  if (typeof window === 'undefined') return '/';
  return safeReturnPath(new URLSearchParams(window.location.search).get('returnTo'));
}

export default function StepUpPage() {
  const router = useRouter();
  const [code, setCode] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string>();

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setError(undefined);
    try {
      await apiRequest('/v1/auth/step-up', { method: 'POST', body: JSON.stringify({ code: code.trim() }) });
      router.replace(currentReturnPath());
    } catch (cause) {
      setError(cause instanceof APIError && (cause.status === 401 || cause.status === 403)
        ? 'The code was not accepted or the session no longer permits step-up.'
        : cause instanceof Error ? cause.message : 'Step-up verification failed.');
      setSubmitting(false);
    }
  }

  return (
    <section className="auth-card auth-card-compact" aria-labelledby="step-up-title">
      <div className="auth-heading">
        <span className="eyebrow">Sensitive action</span>
        <h1 id="step-up-title">Confirm recent MFA</h1>
        <p>This operation needs fresh multi-factor assurance before it can continue.</p>
      </div>
      {error ? <div className="alert alert-danger" role="alert">{error}</div> : null}
      <form className="form-stack" onSubmit={submit}>
        <label>Authentication code<input className="otp-input" inputMode="numeric" pattern="[0-9]{6}" maxLength={6} autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, '').slice(0, 6))} required /></label>
        <button className="primary" type="submit" disabled={submitting || code.length !== 6}>{submitting ? 'Confirming…' : 'Confirm and return'}</button>
      </form>
    </section>
  );
}
