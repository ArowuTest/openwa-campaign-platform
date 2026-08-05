'use client';

import { FormEvent, useState } from 'react';

export function ConsentReviewManager() {
  const [message, setMessage] = useState('');
  const [saving, setSaving] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setMessage('');
    const data = new FormData(event.currentTarget);
    const payload = {
      organisationId: data.get('organisationId'),
      name: data.get('name'),
      purposeDescription: data.get('purposeDescription'),
      channel: 'WHATSAPP',
      consentSource: data.get('consentSource'),
      wordingVersion: data.get('wordingVersion'),
      evidenceObjectKeys: [],
      privacyNoticeReviewed: data.get('privacyNoticeReviewed') === 'on',
      optOutProcessReviewed: data.get('optOutProcessReviewed') === 'on',
      sampleRecordsReviewed: data.get('sampleRecordsReviewed') === 'on',
      permittedCountries: data.getAll('permittedCountries'),
      permittedMessageCategory: data.get('permittedMessageCategory'),
      restrictions: data.get('restrictions')
    };
    const response = await fetch('/api/v1/consent-reviews', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload)
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) {
      setMessage(result.message ?? 'Consent review could not be created');
    } else {
      setMessage(`Review ${result.name} created and awaiting a compliance decision.`);
      event.currentTarget.reset();
    }
    setSaving(false);
  }

  return (
    <div className="split-layout">
      <form className="card form-stack" onSubmit={submit}>
        <h2>Record consent review</h2>
        <div className="alert alert-warning">
          Creating this record does not approve consent. A separate authorised reviewer must complete the decision.
        </div>
        <label>Organisation ID<input name="organisationId" required /></label>
        <label>Review name<input name="name" required placeholder="e.g. Festival registration consent – August 2026" /></label>
        <label>Permitted marketing purpose<textarea name="purposeDescription" rows={3} required /></label>
        <label>Consent collection source<input name="consentSource" required placeholder="Website, registration form or source system" /></label>
        <label>Consent wording version<input name="wordingVersion" required /></label>
        <label>Message category<input name="permittedMessageCategory" placeholder="Entertainment and event promotions" /></label>
        <fieldset>
          <legend>Permitted countries</legend>
          <label className="check"><input type="checkbox" name="permittedCountries" value="NG" /> Nigeria</label>
          <label className="check"><input type="checkbox" name="permittedCountries" value="GH" /> Ghana</label>
          <label className="check"><input type="checkbox" name="permittedCountries" value="GB" /> United Kingdom</label>
        </fieldset>
        <fieldset>
          <legend>Mandatory review checks</legend>
          <label className="check"><input type="checkbox" name="privacyNoticeReviewed" /> Privacy notice reviewed</label>
          <label className="check"><input type="checkbox" name="optOutProcessReviewed" /> Opt-out process reviewed</label>
          <label className="check"><input type="checkbox" name="sampleRecordsReviewed" /> Sample consent records reviewed</label>
        </fieldset>
        <label>Restrictions and review notes<textarea name="restrictions" rows={4} /></label>
        <button className="primary" disabled={saving}>{saving ? 'Saving…' : 'Submit for review'}</button>
        {message ? <div className="alert">{message}</div> : null}
      </form>
      <aside className="card">
        <h2>Approval safeguard</h2>
        <p>An approval cannot be recorded unless all mandatory checks are complete and a future expiry date is supplied.</p>
        <ol className="process-list">
          <li>Review collection process and exact wording.</li>
          <li>Verify organisation and permitted marketing purpose.</li>
          <li>Review evidence, privacy notice and opt-out route.</li>
          <li>Record restrictions and approval expiry.</li>
          <li>Link every import and campaign to the approved review.</li>
        </ol>
      </aside>
    </div>
  );
}
