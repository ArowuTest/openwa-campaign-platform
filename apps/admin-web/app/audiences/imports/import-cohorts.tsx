'use client';

import Link from 'next/link';
import { useEffect, useRef, useState } from 'react';
import { apiRequest, collectBoundedPages, type ListEnvelope } from '../../../lib/api';
import { filterGroupsEqual, type FilterGroup } from '../../../lib/audience-builder-model';

type Source = { id: string; organisationId: string; sourceName: string };
type Segment = { id: string; organisationId: string; name: string; status: string; definition: FilterGroup };

export function ImportCohorts({ source }: { source: Source }) {
  const [count, setCount] = useState('1');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [saved, setSaved] = useState<string[]>([]);
  const active = useRef(true);
  const working = useRef(false);
  useEffect(() => {
    active.current = true;
    return () => { active.current = false; };
  }, []);

  async function prepare() {
    const total = Number(count);
    if (!Number.isInteger(total) || total < 1 || total > 32 || working.current) return;
    working.current = true;
    setBusy(true);
    setMessage('');
    setSaved([]);
    const confirmed: string[] = [];
    try {
      const inventory = await collectBoundedPages<Segment>((cursor) =>
        apiRequest<ListEnvelope<Segment>>('/v1/segments?organisationId=' + encodeURIComponent(source.organisationId) +
          '&limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20);
      for (let part = 1; part <= total && active.current; part++) {
        // The first name serialises competing split sizes through the server's
        // unique active segment name constraint. Never overwrite an existing rule.
        const name = source.sourceName.slice(0, 45) + ' · ' + source.id + ' · group ' + part;
        const definition: FilterGroup = { join: 'AND', rules: [{ definitionCode: 'IMPORT_COHORT',
          operator: 'equals', values: [source.id + '/' + part + '/' + total] }] };
        const matches = (item: Segment) => item.organisationId === source.organisationId &&
          item.name === name && item.status === 'ACTIVE' && filterGroupsEqual(item.definition, definition);
        const found = inventory.find(item => item.organisationId === source.organisationId &&
          item.name === name && item.status !== 'ARCHIVED');
        if (found && !matches(found)) throw new Error('A saved group has a different split or was edited. Review it in Audience builder before continuing.');
        const result = found ?? await apiRequest<Segment>('/v1/segments', { method: 'POST', body: JSON.stringify({
          organisationId: source.organisationId, name, description: 'Group ' + part + ' of ' + total + ' from reviewed upload ' + source.id, definition
        }) });
        if (!matches(result)) throw new Error('The saved group did not match the requested upload. Review the segment in Audience builder.');
        confirmed.push(result.name);
        if (active.current) setSaved([...confirmed]);
      }
      if (active.current) setMessage('All ' + total + ' groups are saved. No messages have been sent.');
    } catch (cause) {
      if (active.current) setMessage((cause instanceof Error ? cause.message : 'Could not prepare groups.') +
        ' Confirmed ' + confirmed.length + ' of ' + total + '. Retry with the same group count to reuse saved groups.');
    } finally {
      working.current = false;
      if (active.current) setBusy(false);
    }
  }

  return <section className="card form-stack" aria-labelledby="upload-cohort-heading">
    <h2 id="upload-cohort-heading">Prepare sending cohorts</h2>
    <p>Keep this upload as one cohort, or divide it into stable, non-overlapping groups. Sizes may differ. Consent, suppression and frequency checks still apply when you build the audience.</p>
    <label>Number of groups<input type="number" min="1" max="32" step="1" value={count} disabled={busy}
      onChange={e => { setCount(e.target.value); setSaved([]); setMessage(''); }} /></label>
    <button className="primary" type="button" disabled={busy || !Number.isInteger(Number(count)) || Number(count) < 1 || Number(count) > 32}
      onClick={() => void prepare()}>{busy ? 'Preparing groups…' : 'Prepare groups'}</button>
    {message && <p role="status">{message}</p>}
    {saved.length > 0 && <><ul>{saved.map(name => <li key={name}>{name}</li>)}</ul>
      <p><Link href="/campaigns">Create a draft campaign</Link> for each group, choose its sender pool, then move the draft to audience building. In <Link href="/audiences/builder">Audience builder</Link>, open the matching group, estimate it and attach its snapshot to that campaign.</p>
      <p>After approval, release each campaign from its workspace and monitor its send, delivery and failure counts there.</p></>}
  </section>;
}
