'use client';

import Link from 'next/link';
import { useEffect, useState } from 'react';
import { apiRequest, collectBoundedPages, type ListEnvelope } from '../../../lib/api';

type Job = { campaignId: string; segmentId?: string; snapshotId?: string; status: string; processedCount: number };

export function FrozenAudiencePicker({ campaignId, maximum, busy, validate }: {
  campaignId: string; maximum: number; busy: boolean; validate: (id: string) => Promise<void>
}) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [chosen, setChosen] = useState('');
  const [revision, setRevision] = useState(0);
  const [message, setMessage] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    void collectBoundedPages<Job>(cursor => apiRequest<ListEnvelope<Job>>('/v1/campaigns/' + encodeURIComponent(campaignId) +
      '/audience-materialisations?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''), { signal: controller.signal }), 20)
      .then(items => {
        if (!controller.signal.aborted) {
          const completed = items.filter(j => j.campaignId === campaignId && j.status === 'COMPLETED' && j.snapshotId);
          setJobs(completed);
          setMessage(completed.length ? '' : 'No completed snapshots for this campaign.');
        }
      }).catch(() => { if (!controller.signal.aborted) setMessage('Could not load snapshots. Refresh to retry.'); });
    return () => controller.abort();
  }, [campaignId, revision]);
  const job = jobs.find(j => j.snapshotId === chosen);
  return <div className="form-stack">
    <p>In <Link href="/audiences/builder">Audience builder</Link>, estimate the saved cohort and schedule its snapshot for this campaign.</p>
    <label>Completed campaign snapshot<select value={chosen} onChange={e => setChosen(e.target.value)}>
      <option value="">Select the snapshot you reviewed</option>{jobs.map(j => <option key={j.snapshotId} value={j.snapshotId}>{j.snapshotId} · {j.processedCount} contacts</option>)}
    </select></label>
    <button type="button" onClick={() => { setJobs([]); setChosen(''); setMessage('Loading snapshots…'); setRevision(v => v + 1); }}>Refresh snapshots</button>
    <button type="button" disabled={busy || !job || job.processedCount <= 0 || job.processedCount > maximum}
      onClick={() => { if (job?.snapshotId) void validate(job.snapshotId); }}>Validate selected audience</button>
    {message && <p role="status">{message}</p>}
  </div>;
}

const counters = [
  ['Queued', 'queuedTotal'], ['Submitted', 'submittedTotal'], ['Gateway accepted', 'gatewayAcceptedTotal'],
  ['Sent', 'sentTotal'], ['Delivered', 'deliveredTotal'], ['Read', 'readTotal'], ['Failed', 'failedTotal'],
  ['Unknown', 'unknownTotal'], ['Suppressed', 'suppressedTotal'], ['Excluded before release', 'excludedFinalCheckTotal']
] as const;

export function DeliveryMonitor({ campaignId, pool, status, initial }: {
  campaignId: string; pool?: string; status: string; initial?: Record<string, number>
}) {
  const [reading, setReading] = useState<{ value?: Record<string, number> }>();
  const metrics = reading ? reading.value : initial;
  const [refresh, setRefresh] = useState(0);
  const [notice, setNotice] = useState('');
  const live = status === 'DISPATCHING' || status === 'PAUSED' || status === 'SCHEDULED';
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function poll() {
      try {
        const next = await apiRequest<Record<string, number>>('/v1/campaigns/' + encodeURIComponent(campaignId) + '/metrics', { signal: controller.signal });
        if (!controller.signal.aborted) { setReading({ value: next }); setNotice('Updated ' + new Date().toLocaleTimeString()); }
      } catch {
        if (!controller.signal.aborted) { setReading({}); setNotice('Delivery counts unavailable. Refresh to retry.'); }
      } finally {
        if (live && !controller.signal.aborted) timer = setTimeout(() => void poll(), 15000);
      }
    }
    if (refresh) void poll();
    else if (live) timer = setTimeout(() => void poll(), 15000);
    return () => { controller.abort(); clearTimeout(timer); };
  }, [campaignId, live, refresh]);
  return <section className="card" aria-labelledby="sending-progress"><h2 id="sending-progress">Sending progress</h2>
    <p>Sender pool: {pool || 'Not assigned'}. Counts show each recipient’s current delivery state.{live && ' Refreshes every 15 seconds.'}</p>
    <dl className="definition-list">{counters.map(([label, key]) => <div key={key}><dt>{label}</dt><dd>{typeof metrics?.[key] === 'number' ? metrics[key].toLocaleString() : 'Unavailable'}</dd></div>)}</dl>
    <button type="button" onClick={() => setRefresh(v => v + 1)}>Refresh delivery counts</button>
    {notice && <small role="status">{notice}</small>}
  </section>;
}
