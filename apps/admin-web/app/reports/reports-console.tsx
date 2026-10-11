'use client';

import { FormEvent, useEffect, useMemo, useState } from 'react';

import { StatusBadge } from '../../components/status-badge';
import { apiRequest, collectBoundedPages, type ListEnvelope } from '../../lib/api';

type Campaign = { id: string; name: string; organisationId: string; status: string };
type Organisation = { id: string; legalName: string; tradingName?: string; status: string };

type PrivacyEvidence = {
  policyId: string;
  policyVersion: number;
  minimumCohortSize: number;
  suppressionLabel: string;
  suppressedCellCount: number;
  appliedAt: string;
};

type CampaignReport = {
  campaignId: string;
  organisationId: string;
  name: string;
  purpose: string;
  status: string;
  audience: Record<string, number>;
  delivery: Record<string, number>;
  engagement: Record<string, number>;
  exceptions: Record<string, number>;
  warnings: string[];
  breakdowns?: Record<string, Array<{ label: string; count?: number; suppressed: boolean }>>;
  privacy: PrivacyEvidence;
  generatedAt: string;
};

type FinancialReport = {
  campaignId: string;
  campaignName: string;
  campaignStatus: string;
  commercialStatus?: string;
  currency?: string;
  approvedRecipients: number;
  recipientObligations: number;
  providerAccepted: number;
  sent: number;
  delivered: number;
  read: number;
  failed: number;
  unknown: number;
  approvedAmountMinor: number;
  recipientVariance: number;
  reconciliationStatus: string;
  warnings: string[];
  generatedAt: string;
};

type OrganisationReport = {
  organisationId: string;
  organisationName: string;
  campaigns: Record<string, number>;
  recipients: Record<string, number>;
  delivery: Record<string, number>;
  commercial: Array<{ currency: string; campaigns: number; approvedRecipients: number; approvedAmountMinor: number }>;
  warnings: string[];
  breakdowns?: Record<string, Array<{ label: string; count?: number; suppressed: boolean }>>;
  privacy: PrivacyEvidence;
  generatedAt: string;
};

function humanise(value: string) {
  return value.replaceAll('_', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function MetricMap({ values }: { values: Record<string, number> }) {
  const entries = Object.entries(values);
  if (!entries.length) return <p className="muted">No metrics reported.</p>;
  return <div className="mini-metric-grid">{entries.map(([key, value]) => <div className="metric-tile" key={key}><span>{humanise(key)}</span><strong>{Number(value).toLocaleString()}</strong></div>)}</div>;
}

export function ReportsConsole() {
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [organisations, setOrganisations] = useState<Organisation[]>([]);
  const [campaignId, setCampaignId] = useState('');
  const [organisationId, setOrganisationId] = useState('');
  const [campaignReport, setCampaignReport] = useState<CampaignReport>();
  const [financialReport, setFinancialReport] = useState<FinancialReport>();
  const [organisationReport, setOrganisationReport] = useState<OrganisationReport>();
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState('');

  useEffect(() => {
    let cancelled = false;
    void Promise.all([
      collectBoundedPages<Campaign>((cursor) => apiRequest<ListEnvelope<Campaign>>('/v1/campaigns?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20),
      collectBoundedPages<Organisation>((cursor) => apiRequest<ListEnvelope<Organisation>>('/v1/organisations?limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '')), 20)
    ]).then(([campaignItems, organisationItems]) => {
      if (cancelled) return;
      setCampaigns(campaignItems);
      setOrganisations(organisationItems);
      setCampaignId((current) => current || campaignItems[0]?.id || '');
      setOrganisationId((current) => current || organisationItems[0]?.id || '');
    }).catch((cause) => {
      if (!cancelled) setMessage(cause instanceof Error ? cause.message : 'Reporting inventories could not be loaded.');
    });
    return () => { cancelled = true; };
  }, []);

  const selectedCampaign = useMemo(() => campaigns.find((item) => item.id === campaignId), [campaignId, campaigns]);

  async function loadCampaign(event?: FormEvent) {
    event?.preventDefault();
    if (!campaignId) return;
    setBusy('campaign');
    setMessage('');
    try {
      const [report, finance] = await Promise.all([
        apiRequest<CampaignReport>('/v1/campaigns/' + campaignId + '/report'),
        apiRequest<FinancialReport>('/v1/campaigns/' + campaignId + '/financial-reconciliation')
      ]);
      setCampaignReport(report);
      setFinancialReport(finance);
    } catch (cause) {
      setMessage(cause instanceof Error ? cause.message : 'Campaign report could not be loaded.');
    } finally {
      setBusy('');
    }
  }

  async function loadOrganisation(event?: FormEvent) {
    event?.preventDefault();
    if (!organisationId) return;
    setBusy('organisation');
    setMessage('');
    try {
      setOrganisationReport(await apiRequest<OrganisationReport>('/v1/organisations/' + organisationId + '/performance-report'));
    } catch (cause) {
      setMessage(cause instanceof Error ? cause.message : 'Organisation report could not be loaded.');
    } finally {
      setBusy('');
    }
  }

  return (
    <div className="workspace-stack">
      {message ? <div className="alert alert-danger" role="alert">{message}</div> : null}

      <section className="grid grid-2">
        <form className="card form-stack" onSubmit={loadCampaign}>
          <h2>Campaign reporting</h2>
          <label>Campaign<select value={campaignId} onChange={(event) => setCampaignId(event.target.value)} required><option value="">Select campaign</option>{campaigns.map((item) => <option value={item.id} key={item.id}>{item.name} · {item.status}</option>)}</select></label>
          <button className="primary" type="submit" disabled={!campaignId || Boolean(busy)}>{busy === 'campaign' ? 'Loading…' : 'Load campaign report'}</button>
        </form>

        <form className="card form-stack" onSubmit={loadOrganisation}>
          <h2>Organisation reporting</h2>
          <label>Organisation<select value={organisationId} onChange={(event) => setOrganisationId(event.target.value)} required><option value="">Select organisation</option>{organisations.map((item) => <option value={item.id} key={item.id}>{item.tradingName || item.legalName} · {item.status}</option>)}</select></label>
          <button className="primary" type="submit" disabled={!organisationId || Boolean(busy)}>{busy === 'organisation' ? 'Loading…' : 'Load organisation report'}</button>
        </form>
      </section>

      {campaignReport ? (
        <section className="workspace-stack">
          <article className="card workspace-hero">
            <div><h2>{campaignReport.name}</h2><p className="muted">Campaign report · generated {new Date(campaignReport.generatedAt).toLocaleString()}</p></div>
            <StatusBadge status={campaignReport.status} />
          </article>

          <section className="grid grid-2">
            <article className="card"><h2>Audience</h2><MetricMap values={campaignReport.audience} /></article>
            <article className="card"><h2>Delivery</h2><MetricMap values={campaignReport.delivery} /></article>
            <article className="card"><h2>Engagement</h2><MetricMap values={campaignReport.engagement} /></article>
            <article className="card"><h2>Exceptions</h2><MetricMap values={campaignReport.exceptions} /></article>
          </section>

          <section className="grid grid-2">
            <article className="card">
              <h2>Privacy evidence</h2>
              <dl className="definition-list">
                <div><dt>Policy</dt><dd>{campaignReport.privacy.policyId} v{campaignReport.privacy.policyVersion}</dd></div>
                <div><dt>Minimum cohort</dt><dd>{campaignReport.privacy.minimumCohortSize}</dd></div>
                <div><dt>Suppressed cells</dt><dd>{campaignReport.privacy.suppressedCellCount}</dd></div>
                <div><dt>Suppression label</dt><dd>{campaignReport.privacy.suppressionLabel}</dd></div>
              </dl>
            </article>
            <article className="card">
              <h2>Warnings</h2>
              {campaignReport.warnings.length ? <ul className="process-list">{campaignReport.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul> : <p className="muted">No report warnings.</p>}
            </article>
          </section>
        </section>
      ) : (
        <section className="card empty-state"><strong>No campaign report selected</strong><p>{selectedCampaign ? 'Load the authoritative report for ' + selectedCampaign.name + '.' : 'Choose a campaign above.'}</p></section>
      )}

      {financialReport ? (
        <section className="card">
          <div className="section-heading"><div><h2>Financial reconciliation</h2><p className="muted">{financialReport.campaignName}</p></div><StatusBadge status={financialReport.reconciliationStatus} /></div>
          <div className="mini-metric-grid">
            <div className="metric-tile"><span>Approved recipients</span><strong>{financialReport.approvedRecipients.toLocaleString()}</strong></div>
            <div className="metric-tile"><span>Recipient obligations</span><strong>{financialReport.recipientObligations.toLocaleString()}</strong></div>
            <div className="metric-tile"><span>Provider accepted</span><strong>{financialReport.providerAccepted.toLocaleString()}</strong></div>
            <div className="metric-tile"><span>Unknown</span><strong>{financialReport.unknown.toLocaleString()}</strong></div>
          </div>
          {financialReport.warnings.length ? <ul className="process-list">{financialReport.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul> : null}
        </section>
      ) : null}

      {organisationReport ? (
        <section className="workspace-stack">
          <article className="card workspace-hero"><div><h2>{organisationReport.organisationName}</h2><p className="muted">Organisation performance · generated {new Date(organisationReport.generatedAt).toLocaleString()}</p></div></article>
          <section className="grid grid-2">
            <article className="card"><h2>Campaigns</h2><MetricMap values={organisationReport.campaigns} /></article>
            <article className="card"><h2>Recipients</h2><MetricMap values={organisationReport.recipients} /></article>
            <article className="card"><h2>Delivery</h2><MetricMap values={organisationReport.delivery} /></article>
            <article className="card"><h2>Commercial</h2>{organisationReport.commercial.length ? <div className="table-wrap"><table><thead><tr><th>Currency</th><th>Campaigns</th><th>Recipients</th><th>Approved amount</th></tr></thead><tbody>{organisationReport.commercial.map((item) => <tr key={item.currency}><td>{item.currency}</td><td>{item.campaigns}</td><td>{item.approvedRecipients.toLocaleString()}</td><td>{item.approvedAmountMinor.toLocaleString()} minor units</td></tr>)}</tbody></table></div> : <p className="muted">No approved commercial totals.</p>}</article>
          </section>
          <article className="card">
            <h2>Organisation privacy evidence</h2>
            <p className="muted">Minimum cohort {organisationReport.privacy.minimumCohortSize}; {organisationReport.privacy.suppressedCellCount} cells suppressed under policy {organisationReport.privacy.policyId} v{organisationReport.privacy.policyVersion}.</p>
          </article>
        </section>
      ) : null}
    </div>
  );
}
