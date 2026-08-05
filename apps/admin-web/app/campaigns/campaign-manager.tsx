'use client';

import { FormEvent, useEffect, useState } from 'react';
import { StatusBadge } from '../../components/status-badge';

type Campaign = {
  id: string;
  name: string;
  organisationId: string;
  status: string;
  maximumUniqueRecipients: number;
  eligibleAudienceCount: number;
  requestedStartAt?: string;
  completionDeadlineAt?: string;
  createdAt: string;
};

export function CampaignManager() {
  const [items, setItems] = useState<Campaign[]>([]);
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);

  async function load() {
    const response = await fetch('/api/v1/campaigns', { cache: 'no-store' });
    if (!response.ok) return;
    const payload = await response.json();
    setItems(payload.items ?? []);
  }

  useEffect(() => { void load(); }, []);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setMessage('');
    const data = new FormData(event.currentTarget);
    const start = String(data.get('requestedStartAt') ?? '');
    const deadline = String(data.get('completionDeadlineAt') ?? '');
    const payload = {
      organisationId: data.get('organisationId'),
      name: data.get('name'),
      purposeId: data.get('purposeId'),
      consentReviewId: data.get('consentReviewId'),
      requestedStartAt: start ? new Date(start).toISOString() : null,
      completionDeadlineAt: deadline ? new Date(deadline).toISOString() : null,
      maximumUniqueRecipients: Number(data.get('maximumUniqueRecipients')),
      maximumMessagesPerRecipient: 1,
      senderPool: data.get('senderPool'),
      createdBy: data.get('createdBy')
    };
    const response = await fetch('/api/v1/campaigns', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload)
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) setMessage(result.message ?? result.details?.detail ?? 'Campaign could not be created');
    else {
      setMessage('Campaign created in DRAFT. It cannot be sent until every approval gate is complete.');
      event.currentTarget.reset();
      await load();
    }
    setBusy(false);
  }

  return (
    <div className="split-layout">
      <section className="card">
        <h2>Campaign register</h2>
        {items.length === 0 ? <div className="empty-state"><strong>No campaigns</strong><p>Create the first campaign using the controlled form.</p></div> : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>Campaign</th><th>Status</th><th>Audience limit</th><th>Eligible</th><th>Window</th></tr></thead>
              <tbody>{items.map((item) => (
                <tr key={item.id}>
                  <td><strong>{item.name}</strong><small>{item.id}</small></td>
                  <td><StatusBadge status={item.status} /></td>
                  <td>{item.maximumUniqueRecipients.toLocaleString()}</td>
                  <td>{item.eligibleAudienceCount.toLocaleString()}</td>
                  <td>{item.requestedStartAt ? new Date(item.requestedStartAt).toLocaleString() : 'Not set'}<small>{item.completionDeadlineAt ? `Deadline: ${new Date(item.completionDeadlineAt).toLocaleString()}` : ''}</small></td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </section>

      <form className="card form-stack" onSubmit={create}>
        <h2>Create draft campaign</h2>
        <div className="alert alert-information">External organisations do not create or launch campaigns. Every identifier below refers to an internally reviewed record.</div>
        <label>Campaign name<input name="name" required /></label>
        <label>Organisation ID<input name="organisationId" required /></label>
        <label>Consent purpose ID<input name="purposeId" required /></label>
        <label>Approved consent-review ID<input name="consentReviewId" required /></label>
        <label>Maximum unique recipients<input name="maximumUniqueRecipients" type="number" min="1" required /></label>
        <label>Sender pool<input name="senderPool" required placeholder="EVENTS-NG" /></label>
        <div className="grid grid-2">
          <label>Requested start<input name="requestedStartAt" type="datetime-local" required /></label>
          <label>Completion deadline<input name="completionDeadlineAt" type="datetime-local" required /></label>
        </div>
        <label>Creator user ID<input name="createdBy" required /></label>
        <button className="primary" disabled={busy}>{busy ? 'Creating…' : 'Create draft campaign'}</button>
        {message ? <div className="alert">{message}</div> : null}
      </form>
    </div>
  );
}
