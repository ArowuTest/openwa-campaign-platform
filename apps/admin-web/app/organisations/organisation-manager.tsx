'use client';

import { FormEvent, useEffect, useState } from 'react';
import { StatusBadge } from '../../components/status-badge';

type Organisation = {
  id: string;
  legalName: string;
  tradingName?: string;
  countryISO2?: string;
  status: string;
  primaryContactName?: string;
  primaryContactEmail?: string;
  createdAt: string;
};

const emptyForm = {
  legalName: '',
  tradingName: '',
  countryISO2: 'NG',
  primaryContactName: '',
  primaryContactEmail: '',
  internalNotes: ''
};

export function OrganisationManager() {
  const [items, setItems] = useState<Organisation[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [state, setState] = useState<'idle' | 'loading' | 'saving' | 'error'>('loading');
  const [message, setMessage] = useState('');

  async function load() {
    setState('loading');
    try {
      const response = await fetch('/api/v1/organisations', { cache: 'no-store' });
      if (!response.ok) throw new Error('Unable to load organisations');
      const payload = await response.json();
      setItems(payload.items ?? []);
      setState('idle');
    } catch (error) {
      setMessage(error instanceof Error ? error.message : 'Unable to load organisations');
      setState('error');
    }
  }

  useEffect(() => { void load(); }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setState('saving');
    setMessage('');
    const response = await fetch('/api/v1/organisations', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form)
    });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({}));
      setMessage(payload.message ?? 'Unable to create organisation');
      setState('error');
      return;
    }
    setForm(emptyForm);
    await load();
  }

  return (
    <div className="split-layout">
      <section className="card">
        <h2>Organisation register</h2>
        {state === 'loading' ? <p className="muted">Loading organisations…</p> : null}
        {state === 'error' ? <div className="alert alert-danger">{message}</div> : null}
        {state !== 'loading' && items.length === 0 ? (
          <div className="empty-state">
            <strong>No organisations recorded</strong>
            <p>Create the first internal organisation record using the form.</p>
          </div>
        ) : null}
        {items.length > 0 ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Organisation</th><th>Country</th><th>Contact</th><th>Status</th><th>Created</th></tr></thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.id}>
                    <td><strong>{item.tradingName || item.legalName}</strong><small>{item.legalName}</small></td>
                    <td>{item.countryISO2 || '—'}</td>
                    <td>{item.primaryContactName || '—'}<small>{item.primaryContactEmail}</small></td>
                    <td><StatusBadge status={item.status} /></td>
                    <td>{new Date(item.createdAt).toLocaleDateString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>

      <form className="card form-stack" onSubmit={submit}>
        <h2>Create organisation</h2>
        <label>Legal name<input required value={form.legalName} onChange={(e) => setForm({ ...form, legalName: e.target.value })} /></label>
        <label>Trading name<input value={form.tradingName} onChange={(e) => setForm({ ...form, tradingName: e.target.value })} /></label>
        <label>Country
          <select value={form.countryISO2} onChange={(e) => setForm({ ...form, countryISO2: e.target.value })}>
            <option value="NG">Nigeria</option><option value="GH">Ghana</option><option value="GB">United Kingdom</option>
          </select>
        </label>
        <label>Primary contact name<input value={form.primaryContactName} onChange={(e) => setForm({ ...form, primaryContactName: e.target.value })} /></label>
        <label>Primary contact email<input type="email" value={form.primaryContactEmail} onChange={(e) => setForm({ ...form, primaryContactEmail: e.target.value })} /></label>
        <label>Internal notes<textarea rows={4} value={form.internalNotes} onChange={(e) => setForm({ ...form, internalNotes: e.target.value })} /></label>
        <button className="primary" disabled={state === 'saving'}>{state === 'saving' ? 'Creating…' : 'Create organisation'}</button>
      </form>
    </div>
  );
}
