'use client';

import { FormEvent, useEffect, useMemo, useState } from 'react';
import { StatusBadge } from '../../components/status-badge';
import { useAuth } from '../../components/auth-provider';
import { APIError, apiRequest, collectBoundedPages, type ListEnvelope } from '../../lib/api';
import { hasPermission } from '../../lib/session';

type Organisation = {
  id: string;
  legalName: string;
  tradingName?: string;
  countryISO2?: string;
  status: 'ACTIVE' | 'SUSPENDED' | 'UNDER_REVIEW' | 'CLOSED' | string;
  primaryContactName?: string;
  primaryContactEmail?: string;
  internalNotes?: string;
  createdAt: string;
  updatedAt?: string;
  version: number;
};

type OrganisationForm = {
  legalName: string;
  tradingName: string;
  countryISO2: string;
  primaryContactName: string;
  primaryContactEmail: string;
  internalNotes: string;
};

const emptyForm: OrganisationForm = {
  legalName: '',
  tradingName: '',
  countryISO2: 'NG',
  primaryContactName: '',
  primaryContactEmail: '',
  internalNotes: ''
};

const statusOptions: Organisation['status'][] = ['UNDER_REVIEW', 'ACTIVE', 'SUSPENDED', 'CLOSED'];

function formFromOrganisation(value: Organisation): OrganisationForm {
  return {
    legalName: value.legalName ?? '',
    tradingName: value.tradingName ?? '',
    countryISO2: value.countryISO2 ?? '',
    primaryContactName: value.primaryContactName ?? '',
    primaryContactEmail: value.primaryContactEmail ?? '',
    internalNotes: value.internalNotes ?? ''
  };
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError && error.status === 409) return 'This organisation changed in another session. Reload it before saving.';
  return error instanceof Error ? error.message : fallback;
}

export function OrganisationManager() {
  const { session } = useAuth();
  const canWrite = hasPermission(session?.permissions, 'organisation.write');
  const canApprove = hasPermission(session?.permissions, 'organisation.approve');
  const [items, setItems] = useState<Organisation[]>([]);
  const [selected, setSelected] = useState<Organisation | null>(null);
  const [form, setForm] = useState<OrganisationForm>(emptyForm);
  const [createForm, setCreateForm] = useState<OrganisationForm>(emptyForm);
  const [reason, setReason] = useState('');
  const [statusReason, setStatusReason] = useState('');
  const [statusValue, setStatusValue] = useState('ACTIVE');
  const [state, setState] = useState<'idle' | 'loading' | 'saving' | 'error'>('loading');
  const [message, setMessage] = useState('');

  async function load() {
    setState('loading');
    try {
      const values = await collectBoundedPages<Organisation>(
        (cursor) => apiRequest<ListEnvelope<Organisation>>('/v1/organisations?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')),
        20
      );
      setItems(values);
      setState('idle');
    } catch (error) {
      setMessage(errorMessage(error, 'Unable to load organisations'));
      setState('error');
    }
  }

  useEffect(() => {
    let cancelled = false;
    void collectBoundedPages<Organisation>(
      (cursor) => apiRequest<ListEnvelope<Organisation>>('/v1/organisations?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')),
      20
    ).then((values) => {
      if (cancelled) return;
      setItems(values);
      setState('idle');
    }).catch((error) => {
      if (cancelled) return;
      setMessage(errorMessage(error, 'Unable to load organisations'));
      setState('error');
    });
    return () => { cancelled = true; };
  }, []);

  async function selectOrganisation(item: Organisation) {
    setMessage('');
    try {
      const value = await apiRequest<Organisation>('/v1/organisations/' + encodeURIComponent(item.id));
      setSelected(value);
      setForm(formFromOrganisation(value));
      setStatusValue(value.status);
      setReason('');
      setStatusReason('');
    } catch (error) {
      setMessage(errorMessage(error, 'Unable to load organisation detail'));
    }
  }

  async function submitCreate(event: FormEvent) {
    event.preventDefault();
    if (!canWrite) return;
    setState('saving');
    setMessage('');
    try {
      await apiRequest<Organisation>('/v1/organisations', { method: 'POST', body: JSON.stringify(createForm) });
      setCreateForm(emptyForm);
      setMessage('Organisation created in UNDER_REVIEW. Complete offline vetting before activating it.');
      await load();
    } catch (error) {
      setMessage(errorMessage(error, 'Unable to create organisation'));
      setState('error');
    }
  }

  async function saveOrganisation(event: FormEvent) {
    event.preventDefault();
    if (!selected || !canWrite) return;
    const trimmedReason = reason.trim();
    if (trimmedReason.length < 1) {
      setMessage('Enter a reason for the organisation change.');
      return;
    }
    setState('saving');
    setMessage('');
    try {
      const value = await apiRequest<Organisation>('/v1/organisations/' + encodeURIComponent(selected.id), {
        method: 'PUT',
        body: JSON.stringify({ ...form, expectedVersion: selected.version, reason: trimmedReason })
      });
      setSelected(value);
      setForm(formFromOrganisation(value));
      setReason('');
      setMessage('Organisation details saved.');
      await load();
      setState('idle');
    } catch (error) {
      setMessage(errorMessage(error, 'Unable to save organisation'));
      setState('error');
    }
  }

  async function changeStatus() {
    if (!selected || !canApprove) return;
    const trimmedReason = statusReason.trim();
    if (trimmedReason.length < 1) {
      setMessage('Enter a reason for the status change.');
      return;
    }
    setState('saving');
    setMessage('');
    try {
      const value = await apiRequest<Organisation>('/v1/organisations/' + encodeURIComponent(selected.id) + '/status', {
        method: 'POST',
        body: JSON.stringify({ expectedVersion: selected.version, status: statusValue, reason: trimmedReason })
      });
      setSelected(value);
      setForm(formFromOrganisation(value));
      setStatusValue(value.status);
      setStatusReason('');
      setMessage('Organisation status updated.');
      await load();
      setState('idle');
    } catch (error) {
      setMessage(errorMessage(error, 'Unable to update organisation status'));
      setState('error');
    }
  }

  const selectedLabel = useMemo(() => selected ? (selected.tradingName || selected.legalName) : '', [selected]);

  return (
    <div className="workspace-stack">
      {message ? <div className={state === 'error' ? 'alert alert-danger' : 'alert alert-information'} role="status">{message}</div> : null}

      <section className="card">
        <div className="section-heading">
          <div><h2>Organisation register</h2><p className="muted">Internal records for offline-vetted clients. Only operators can access this register.</p></div>
          <span className="pill">{items.length} organisations</span>
        </div>
        {state === 'loading' ? <p className="muted">Loading organisations…</p> : null}
        {state !== 'loading' && items.length === 0 ? (
          <div className="empty-state"><strong>No organisations recorded</strong><p>Create the first internal organisation record using the form.</p></div>
        ) : null}
        {items.length > 0 ? (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Organisation</th><th>Country</th><th>Contact</th><th>Status</th><th>Version</th><th>Created</th></tr></thead>
              <tbody>
                {items.map((item) => (
                  <tr key={item.id} className={selected?.id === item.id ? 'selected-row' : undefined}>
                    <td><button type="button" className="table-link" onClick={() => void selectOrganisation(item)}>{item.tradingName || item.legalName}</button><small>{item.legalName}</small></td>
                    <td>{item.countryISO2 || '—'}</td>
                    <td>{item.primaryContactName || '—'}<small>{item.primaryContactEmail || ''}</small></td>
                    <td><StatusBadge status={item.status} /></td>
                    <td>{item.version}</td>
                    <td>{item.createdAt ? new Date(item.createdAt).toLocaleDateString() : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>

      <section className="grid grid-2">
        <form className="card form-stack" onSubmit={submitCreate}>
          <h2>Create organisation</h2>
          <p className="muted">New records start UNDER_REVIEW. Activate only after offline client vetting and approval evidence are complete.</p>
          <label>Legal name<input required value={createForm.legalName} onChange={(e) => setCreateForm({ ...createForm, legalName: e.target.value })} /></label>
          <label>Trading name<input value={createForm.tradingName} onChange={(e) => setCreateForm({ ...createForm, tradingName: e.target.value })} /></label>
          <label>Country<select value={createForm.countryISO2} onChange={(e) => setCreateForm({ ...createForm, countryISO2: e.target.value })}><option value="NG">Nigeria</option><option value="GH">Ghana</option><option value="GB">United Kingdom</option></select></label>
          <label>Primary contact name<input value={createForm.primaryContactName} onChange={(e) => setCreateForm({ ...createForm, primaryContactName: e.target.value })} /></label>
          <label>Primary contact email<input type="email" value={createForm.primaryContactEmail} onChange={(e) => setCreateForm({ ...createForm, primaryContactEmail: e.target.value })} /></label>
          <label>Internal notes<textarea rows={4} value={createForm.internalNotes} onChange={(e) => setCreateForm({ ...createForm, internalNotes: e.target.value })} /></label>
          <button className="primary" disabled={state === 'saving' || !canWrite}>{state === 'saving' ? 'Saving…' : 'Create organisation'}</button>
        </form>

        <article className="card form-stack">
          <div className="section-heading"><div><h2>Organisation detail</h2><p className="muted">{selected ? selectedLabel + ' · version ' + selected.version : 'Select an organisation to edit its vetted record.'}</p></div>{selected ? <StatusBadge status={selected.status} /> : null}</div>
          {!selected ? <div className="empty-state"><strong>No organisation selected</strong><p>Choose a register row to review or update it.</p></div> : (
            <>
              <form className="form-stack" onSubmit={saveOrganisation}>
                <label>Legal name<input required disabled={!canWrite} value={form.legalName} onChange={(e) => setForm({ ...form, legalName: e.target.value })} /></label>
                <label>Trading name<input disabled={!canWrite} value={form.tradingName} onChange={(e) => setForm({ ...form, tradingName: e.target.value })} /></label>
                <label>Country<select disabled={!canWrite} value={form.countryISO2} onChange={(e) => setForm({ ...form, countryISO2: e.target.value })}><option value="">Select country</option><option value="NG">Nigeria</option><option value="GH">Ghana</option><option value="GB">United Kingdom</option></select></label>
                <label>Primary contact name<input disabled={!canWrite} value={form.primaryContactName} onChange={(e) => setForm({ ...form, primaryContactName: e.target.value })} /></label>
                <label>Primary contact email<input disabled={!canWrite} type="email" value={form.primaryContactEmail} onChange={(e) => setForm({ ...form, primaryContactEmail: e.target.value })} /></label>
                <label>Internal notes<textarea disabled={!canWrite} rows={3} value={form.internalNotes} onChange={(e) => setForm({ ...form, internalNotes: e.target.value })} /></label>
                <label>Change reason<input required={canWrite} disabled={!canWrite} value={reason} onChange={(e) => setReason(e.target.value)} /></label>
                <button className="secondary" disabled={!canWrite || state === 'saving'}>Save organisation</button>
              </form>
              <fieldset>
                <legend>Governance status</legend>
                <label>Target status<select disabled={!canApprove} value={statusValue} onChange={(e) => setStatusValue(e.target.value)}>{statusOptions.map((value) => <option key={value} value={value}>{value}</option>)}</select></label>
                <label>Status reason<textarea aria-label="Status reason" rows={2} disabled={!canApprove} value={statusReason} onChange={(e) => setStatusReason(e.target.value)} /></label>
                <button type="button" className="primary" disabled={!canApprove || !statusReason.trim() || state === 'saving'} onClick={() => void changeStatus()}>Set {statusValue}</button>
              </fieldset>
            </>
          )}
        </article>
      </section>
    </div>
  );
}
