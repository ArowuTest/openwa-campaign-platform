'use client';

import { useEffect, useRef, useState } from 'react';
import { useAuth } from '../../../components/auth-provider';
import { APIError } from '../../../lib/api';
import type { CampaignDetail } from '../../../lib/campaign-preparation-model';
import { getCampaignReadiness, type CampaignReadiness } from '../../../lib/campaign-readiness-model';
import { hasPermission } from '../../../lib/session';

export function CampaignReadinessPanel({ campaign }: { campaign: CampaignDetail }) {
 const { session } = useAuth();
 const generation = JSON.stringify([campaign.id, campaign.version, campaign.status, session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
 if (!hasPermission(session?.permissions, 'campaign.read')) return <p>Campaign readiness requires campaign.read permission.</p>;
 return <ReadinessGeneration key={generation} campaign={campaign} />;
}
const labels: Record<string,string> = { organisation:'Organisation', purpose:'Purpose', consent:'Consent review', audience:'Frozen audience', message:'Message and media', transport:'Saved transport', schedule:'Schedule', capacity:'Live capacity', pilot:'Pilot evidence', commercial:'Commercial approval', maintenance:'Maintenance', finalReview:'Independent final review' };
function ReadinessGeneration({ campaign }: { campaign: CampaignDetail }) {
 const [value,setValue] = useState<CampaignReadiness>();
 const [error,setError] = useState('');
 const [busy,setBusy] = useState(false);
 const active = useRef<AbortController | undefined>(undefined);
 const mounted = useRef(false);
 useEffect(() => { mounted.current = true; return () => { mounted.current = false; active.current?.abort(); }; }, []);
 async function refresh() {
  active.current?.abort();
  const controller = new AbortController(); active.current = controller;
  const current = () => mounted.current && active.current === controller && !controller.signal.aborted;
  setValue(undefined); setError(''); setBusy(true);
  try {
   const response = await getCampaignReadiness(campaign.id, controller.signal);
   if (!current()) return;
   if (response.campaignId !== campaign.id || response.campaignVersion !== campaign.version || response.campaignStatus !== campaign.status) {
    setError('The saved campaign changed. Reload its detail before refreshing readiness.'); return;
   }
   setValue(response);
  } catch (cause) {
   if (!current()) return;
   setError(cause instanceof APIError && cause.code === 'CAMPAIGN_READINESS_STAGE_UNSUPPORTED'
    ? 'Use the existing execution evidence for this campaign stage.'
    : cause instanceof APIError && cause.code === 'CAMPAIGN_VERSION_CONFLICT'
     ? 'The saved campaign changed. Reload its detail before refreshing readiness.'
     : 'Readiness could not be verified. Refresh again when the evidence services are available.');
  } finally { if (current()) setBusy(false); }
 }
 const capacity=value?.capacity;
 return <section className="panel" aria-labelledby="campaign-readiness-heading">
  <div className="section-heading"><div><h2 id="campaign-readiness-heading">Preparation readiness</h2><p>Assess the saved campaign before independent final review.</p></div>
   <button type="button" onClick={() => void refresh()} aria-disabled={busy}>{busy ? 'Refreshing readiness…' : 'Refresh readiness'}</button>
  </div>
  <p>Readiness is advisory; final approval revalidates current evidence.</p>
  <p>Saved sender pool: <code>{campaign.transport.senderPoolId || 'Not bound'}</code> · Gateway pool: <code>{campaign.transport.gatewayPoolId || 'Not bound'}</code></p>
  {error && <p role="alert">{error}</p>}
  <div aria-live="polite" aria-busy={busy}>
   {!value && !error && <p>{busy ? 'Reading current evidence…' : 'Refresh to assess this saved version.'}</p>}
   {value && <>
    <h3>{value.state === 'READY_FOR_FINAL_REVIEW' ? 'Ready for independent final review' : value.state === 'REQUIRES_REVIEW' ? 'Independent review required' : 'Preparation blocked'}</h3>
    <p>Assessment for <code>{value.campaignId}</code>, saved version {value.campaignVersion}. Assessed at <time dateTime={value.assessedAt}>{value.assessedAt}</time>.</p>
    <ul>{value.checks.map(check => <li key={check.key}>
     <h4>{labels[check.key]} — {check.status.replaceAll('_',' ')}</h4><p>{check.message}</p><p>{check.remediation}</p>
     <small>{check.code}</small>
     {check.evidence.length > 0 && <ul aria-label={labels[check.key] + ' evidence'}>{check.evidence.map((e,index) => <li key={e.kind + e.id + index}><span>{e.kind}: </span><code>{e.id}</code>{e.version !== undefined && <span> · version {e.version}</span>}{e.status && <span> · {e.status}</span>}{e.hash && <span> · hash <code>{e.hash}</code></span>}</li>)}</ul>}
    </li>)}</ul>
    {capacity && <section aria-label="Advisory gross capacity"><h3>Advisory gross capacity</h3>
     <p>{capacity.healthySessions} healthy sessions · {capacity.healthyNodes} healthy nodes · minimum {capacity.minimumHealthyNodes} healthy nodes</p>
     <p>{capacity.availableMessagesPerMinute} measured messages per minute · {capacity.availableHourlyUnits} hourly units · {capacity.availableDailyUnits} daily units after static reserve deduction</p>
     <p>Basis: {capacity.basisRecipientCount} recipients ({capacity.basis === 'SNAPSHOT' ? 'immutable snapshot' : 'authorised maximum'}). Assessment margin: {capacity.safetyMarginPercent}%.</p>
     <p>Measured at <time dateTime={capacity.measurementAsOf}>{capacity.measurementAsOf}</time>. Classification: {capacity.classification.replaceAll('_',' ')}.</p>
     {capacity.projectedCompletionAt && <p>Gross advisory completion: <time dateTime={capacity.projectedCompletionAt}>{capacity.projectedCompletionAt}</time>. This is not a deadline guarantee.</p>}
     <ul>{capacity.reasons.map(reason => <li key={reason}>{reason.replaceAll('_',' ')}</li>)}</ul>
    </section>}
    <ul aria-label="Readiness limitations">{value.limitations.map(text => <li key={text}>{text}</li>)}</ul>
   </>}
  </div>
 </section>;
}
