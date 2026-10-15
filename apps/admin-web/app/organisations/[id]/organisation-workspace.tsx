'use client';

import Link from 'next/link';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { useAuth } from '../../../components/auth-provider';
import { StatusBadge } from '../../../components/status-badge';
import { APIError, apiRequest, type ListEnvelope } from '../../../lib/api';
import { hasPermission } from '../../../lib/session';

type Feed = 'events' | 'policies' | 'consent' | 'campaigns' | 'trust';

type Organisation = {
  id: string;
  legalName: string;
  tradingName?: string;
  countryISO2?: string;
  primaryContactName?: string;
  primaryContactEmail?: string;
  status: string;
  createdAt: string;
  updatedAt?: string;
  version: number;
};

type RelatedRecord = {
  id?: string;
  organisationId: string;
  name?: string;
  status?: string;
  eventType?: string;
  sourceSystem?: string;
  trustLevel?: number;
  scope?: string;
  channel?: string;
  expiresAt?: string;
  createdAt?: string;
  updatedAt?: string;
  occurredAt?: string;
  version?: number;
  beforeStatus?: string;
  afterStatus?: string;
  contactRetentionDays?: number;
  campaignRetentionDays?: number;
  allowedPurposeIds?: string[];
  prohibitedPurposeIds?: string[];
  internalNotes?: string;
  exactConsentWording?: string;
  reason?: string;
  actorId?: string;
};

type Row = {
  id: string;
  label: string;
  status?: string;
  date?: string;
  organisationId: string;
  version?: number;
  beforeStatus?: string;
  afterStatus?: string;
  scope?: string;
  channel?: string;
  expiresAt?: string;
  trustLevel?: number;
  retentionDays?: number;
  campaignRetentionDays?: number;
  allowedPurposes?: number;
  prohibitedPurposes?: number;
};

type FeedState = {
  items: Row[];
  loaded: boolean;
  loading: boolean;
  loadingMore: boolean;
  error: string;
  hasMore: boolean;
  nextCursor: string;
};

const feeds: Feed[] = ['events', 'policies', 'consent', 'campaigns', 'trust'];
const maxPages = 20;
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

function initialFeed(): FeedState {
  return { items: [], loaded: false, loading: false, loadingMore: false, error: '', hasMore: false, nextCursor: '' };
}

function initialFeeds(): Record<Feed, FeedState> {
  return { events: initialFeed(), policies: initialFeed(), consent: initialFeed(), campaigns: initialFeed(), trust: initialFeed() };
}

function when(value?: string) {
  if (!value) return 'Not recorded';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? 'Invalid timestamp' : date.toLocaleString();
}

function safeName(value?: string) {
  return value?.trim() || 'Not recorded';
}

function readableError(error: unknown) {
  if (error instanceof APIError && error.status === 404) return 'The requested organisation or evidence was not found.';
  if (error instanceof APIError && error.status === 403) return 'The server denied access to this governed evidence.';
  return error instanceof Error ? error.message : 'The governed organisation evidence could not be loaded.';
}

function projectedOrganisation(raw: Organisation): Organisation {
  return {
    id: raw.id,
    legalName: raw.legalName,
    tradingName: raw.tradingName,
    countryISO2: raw.countryISO2,
    primaryContactName: raw.primaryContactName,
    primaryContactEmail: raw.primaryContactEmail,
    status: raw.status,
    createdAt: raw.createdAt,
    updatedAt: raw.updatedAt,
    version: raw.version
  };
}

function projectRow(source: Feed, raw: RelatedRecord): Row {
  const id = source === 'trust' ? raw.sourceSystem : raw.id;
  if (!id?.trim() || !raw.organisationId) throw new Error('Invalid related record returned for this organisation.');
  const label = source === 'events' ? raw.eventType : source === 'trust' ? raw.sourceSystem : raw.name || raw.id;
  if (!label?.trim()) throw new Error('Invalid label returned for this organisation.');
  return {
    id,
    organisationId: raw.organisationId,
    label,
    status: raw.status,
    date: raw.occurredAt || raw.updatedAt || raw.createdAt,
    version: raw.version,
    beforeStatus: raw.beforeStatus,
    afterStatus: raw.afterStatus,
    scope: raw.scope,
    channel: raw.channel,
    expiresAt: raw.expiresAt,
    trustLevel: raw.trustLevel,
    retentionDays: raw.contactRetentionDays,
    campaignRetentionDays: raw.campaignRetentionDays,
    allowedPurposes: raw.allowedPurposeIds?.length,
    prohibitedPurposes: raw.prohibitedPurposeIds?.length
  };
}

const titles: Record<Feed, string> = {
  events: 'Organisation event audit',
  policies: 'Governed organisation policies',
  consent: 'Consent reviews',
  campaigns: 'Campaigns',
  trust: 'Trusted audience sources'
};

export function OrganisationWorkspace({ organisationId }: { organisationId: string }) {
  const { session } = useAuth();
  const canRead = hasPermission(session?.permissions, 'organisation.read');
  const valid = uuid.test(organisationId);
  const generation = JSON.stringify([organisationId, session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  if (!canRead) return <p role="status">Organisation workspace requires organisation.read permission.</p>;
  if (!valid) return <p role="alert">An organisation must be selected using a valid internal reference.</p>;
  return <OrganisationWorkspaceGeneration key={generation} organisationId={organisationId} />;
}

function OrganisationWorkspaceGeneration({ organisationId }: { organisationId: string }) {
  const { session } = useAuth();
  const canConsent = hasPermission(session?.permissions, 'consent.read');
  const canCampaign = hasPermission(session?.permissions, 'campaign.read');
  const canSource = hasPermission(session?.permissions, 'audience.read');
  const canFinance = hasPermission(session?.permissions, 'finance.read');
  const canOperations = hasPermission(session?.permissions, 'operations.read');

  const enabled = useMemo<Feed[]>(() => [
    'events', 'policies',
    ...(canConsent ? ['consent' as Feed] : []),
    ...(canCampaign ? ['campaigns' as Feed] : []),
    ...(canSource ? ['trust' as Feed] : [])
  ], [canConsent, canCampaign, canSource]);

  const mounted = useRef(false);
  const mainRequest = useRef(0);
  const feedRequests = useRef<Record<Feed, number>>({ events: 0, policies: 0, consent: 0, campaigns: 0, trust: 0 });
  const feedPages = useRef<Record<Feed, number>>({ events: 0, policies: 0, consent: 0, campaigns: 0, trust: 0 });
  const feedCursors = useRef<Record<Feed, Set<string>>>({
    events: new Set(), policies: new Set(), consent: new Set(), campaigns: new Set(), trust: new Set()
  });

  const [organisation, setOrganisation] = useState<Organisation>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [updatedAt, setUpdatedAt] = useState('');
  const [states, setStates] = useState<Record<Feed, FeedState>>(initialFeeds);

  const loadOrganisation = useCallback(async () => {
    const seq = ++mainRequest.current;
    setLoading(true);
    setError('');
    try {
      const raw = await apiRequest<Organisation>('/v1/organisations/' + encodeURIComponent(organisationId));
      if (!mounted.current || seq !== mainRequest.current) return false;
      if (!raw || raw.id !== organisationId || !raw.legalName || !Number.isSafeInteger(raw.version)) {
        throw new Error('The server returned an invalid or mismatched organisation record.');
      }
      setOrganisation(projectedOrganisation(raw));
      setUpdatedAt(new Date().toISOString());
      return true;
    } catch (cause) {
      if (!mounted.current || seq !== mainRequest.current) return false;
      setOrganisation(undefined);
      setError(readableError(cause));
      return false;
    } finally {
      if (mounted.current && seq === mainRequest.current) setLoading(false);
    }
  }, [organisationId]);

  const loadFeed = useCallback(async (feed: Feed, cursor = '', reset = false) => {
    const seq = ++feedRequests.current[feed];
    if (reset) {
      feedPages.current[feed] = 0;
      feedCursors.current[feed].clear();
      setStates((prev) => ({ ...prev, [feed]: { ...initialFeed(), loading: true } }));
    } else {
      if (feedPages.current[feed] >= maxPages) {
        setStates((prev) => ({ ...prev, [feed]: { ...prev[feed], hasMore: false, error: 'The displayed history is capped at 1,000 records. Refresh for the latest data.' } }));
        return;
      }
      setStates((prev) => ({ ...prev, [feed]: { ...prev[feed], loadingMore: true, error: '' } }));
    }

    const prefix = '/v1/organisations/' + encodeURIComponent(organisationId);
    const url = feed === 'events' ? prefix + '/events?limit=50'
      : feed === 'policies' ? prefix + '/policies?limit=50'
      : feed === 'consent' ? '/v1/consent-reviews?organisationId=' + encodeURIComponent(organisationId) + '&limit=50'
      : feed === 'campaigns' ? '/v1/campaigns?organisationId=' + encodeURIComponent(organisationId) + '&limit=50'
      : prefix + '/audience-source-trust';
    try {
      const page = await apiRequest<ListEnvelope<RelatedRecord>>(url + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''));
      if (!mounted.current || seq !== feedRequests.current[feed]) return;
      if (!page || !Array.isArray(page.items) || typeof page.hasMore !== 'boolean' ||
        (page.hasMore && !page.nextCursor)) throw new Error('The server returned an invalid bounded evidence page.');
      // Source trust uses a legacy non-paginated API. Do not invent cursor
      // support or render an unbounded list if that contract changes.
      if (feed === 'trust' && (page.hasMore || page.items.length > 50)) {
        throw new Error('Audience source trust cannot be paginated safely or exceeds the 50-record display limit. Use the governed source register instead.');
      }
      if (page.items.some((record) => !record || record.organisationId !== organisationId)) {
        throw new Error('The server returned a record for a different organisation. Data was not displayed.');
      }
      if (page.hasMore && (page.nextCursor === cursor || feedCursors.current[feed].has(page.nextCursor || ''))) {
        throw new Error('The server repeated a pagination cursor. Further requests were stopped.');
      }
      const rows = page.items.map((record) => projectRow(feed, record));
      feedPages.current[feed] += 1;
      if (page.hasMore && page.nextCursor) feedCursors.current[feed].add(page.nextCursor);
      setStates((prev) => {
        const prior = prev[feed];
        const seen = new Set(prior.items.map((item) => item.id));
        return {
          ...prev,
          [feed]: {
            items: reset ? rows : [...prior.items, ...rows.filter((item) => !seen.has(item.id))],
            loaded: true, loading: false, loadingMore: false, error: '',
            hasMore: page.hasMore && feedPages.current[feed] < maxPages,
            nextCursor: page.nextCursor || ''
          }
        };
      });
    } catch (cause) {
      if (!mounted.current || seq !== feedRequests.current[feed]) return;
      setStates((prev) => ({
        ...prev,
        [feed]: {
          ...prev[feed], loaded: true, loading: false, loadingMore: false,
          hasMore: false, error: readableError(cause)
        }
      }));
    }
  }, [organisationId]);

  useEffect(() => {
    mounted.current = true;
    // Verify the organisation exists and is accessible before requesting
    // any related records, so a stale/missing route cannot fan out API reads.
    let cancelled = false;
    void (async () => {
      const verified = await loadOrganisation();
      if (!verified || cancelled || !mounted.current) return;
      for (const feed of enabled) void loadFeed(feed, '', true);
    })();
    const activeRequests = feedRequests.current;
    return () => {
      cancelled = true;
      mounted.current = false;
      mainRequest.current += 1;
      for (const feed of feeds) activeRequests[feed] += 1;
    };
  }, [loadOrganisation, loadFeed, enabled]);

  function refresh() {
    void (async () => {
      const verified = await loadOrganisation();
      if (!verified || !mounted.current) return;
      for (const feed of enabled) void loadFeed(feed, '', true);
    })();
  }

  return (
    <div className="workspace-stack">
      <section className="card form-stack">
        <div className="section-heading">
          <div>
            <h2>Client organisation profile</h2>
            <p className="muted">Authoritative internal client record. Workspaces below display bounded pages, not proof that all related records have been loaded.</p>
          </div>
          <Link href="/organisations">Return to organisation register</Link>
        </div>
        <button type="button" onClick={refresh} disabled={loading || enabled.some((feed) => states[feed].loading)}>
          Refresh organisation workspace
        </button>
        {loading ? <p role="status">Loading client organisation…</p> : null}
        {error ? <p role="alert">{error}</p> : null}
        {organisation ? (
          <>
            <div className="section-heading">
              <h3>{safeName(organisation.tradingName || organisation.legalName)}</h3>
              <StatusBadge status={organisation.status} />
            </div>
            {organisation.status !== 'ACTIVE' ? (
              <p role="status">This organisation is {organisation.status}. Consult the approved lifecycle and campaign release records before taking action; this screen does not override dispatch eligibility.</p>
            ) : null}
            <dl className="definition-list">
              <div><dt>Legal name</dt><dd>{organisation.legalName}</dd></div>
              <div><dt>Internal organisation reference</dt><dd>{organisation.id}</dd></div>
              <div><dt>Country</dt><dd>{safeName(organisation.countryISO2)}</dd></div>
              <div><dt>Primary contact</dt><dd>{safeName(organisation.primaryContactName)}</dd></div>
              <div><dt>Contact email</dt><dd>{safeName(organisation.primaryContactEmail)}</dd></div>
              <div><dt>Current version</dt><dd>{organisation.version}</dd></div>
              <div><dt>Record updated</dt><dd>{when(organisation.updatedAt)}</dd></div>
              <div><dt>Viewed</dt><dd>{when(updatedAt)}</dd></div>
            </dl>
            <p className="muted">Internal notes and unneeded case narratives are deliberately omitted from this summary.</p>
            <Link href={'/organisations?organisationId=' + encodeURIComponent(organisation.id)} aria-label="Edit this organisation in register">
              Open governed organisation edit and status controls
            </Link>
          </>
        ) : !loading && !error ? <p>No organisation detail is available.</p> : null}
      </section>

      {organisation ? (
        <>
          <div className="grid grid-2">
            {enabled.map((feed) => {
              const current = states[feed];
              return (
                <section key={feed} className="card form-stack" aria-label={titles[feed]}>
                  <div className="section-heading">
                    <h3>{titles[feed]}</h3>
                    <span className="pill">{current.items.length} loaded</span>
                  </div>
                  {feed === 'events' ? <p className="muted">Immutable event types, versions and lifecycle transitions. Audit reasons and staff identifiers are not copied into this overview.</p> : null}
                  {feed === 'policies' ? <p className="muted">Policies are displayed as summary controls; only a formally approved active policy governs use.</p> : null}
                  {feed === 'trust' ? <p className="muted">Trust levels are operational indicators, not consent authority or proof of eligibility to message.</p> : null}
                  {current.loading ? <p role="status">Loading {titles[feed].toLowerCase()}…</p> : null}
                  {current.error ? <p role="alert">{current.error}</p> : null}
                  {current.loaded && !current.loading && current.items.length === 0 && !current.error ? (
                    <p>No {titles[feed].toLowerCase()} on the loaded page. Older records or separately governed evidence may exist.</p>
                  ) : null}
                  {current.items.length ? (
                    <div className="table-wrap">
                      <table>
                        <thead><tr><th>Record</th><th>Details</th><th>Action</th></tr></thead>
                        <tbody>
                          {current.items.map((item) => (
                            <tr key={item.id}>
                              <td>
                                <strong>{item.label}</strong>
                                <small>{when(item.date)}</small>
                              </td>
                              <td>
                                {item.status ? <StatusBadge status={item.status} /> : null}
                                {feed === 'events' ? <small>Version {item.version ?? 'unknown'}; {safeName(item.beforeStatus)} → {safeName(item.afterStatus)}</small> : null}
                                {feed === 'consent' ? <small>{safeName(item.scope)} · expiry {when(item.expiresAt)}</small> : null}
                                {feed === 'policies' ? (
                                  <small>{item.retentionDays ?? '?'} contact-retention days; {item.campaignRetentionDays ?? '?'} campaign-retention days; {item.allowedPurposes ?? 0} allowed and {item.prohibitedPurposes ?? 0} restricted purpose references</small>
                                ) : null}
                                {feed === 'trust' ? <small>Trust level {item.trustLevel ?? 'unavailable'} (server-reported)</small> : null}
                                {feed === 'campaigns' ? <small>Version {item.version ?? '?'}</small> : null}
                              </td>
                              <td>
                                {feed === 'campaigns' ? (
                                  <>
                                    <Link href={'/campaigns/' + encodeURIComponent(item.id)} aria-label={'Open campaign ' + item.label}>Open campaign</Link>
                                    {canFinance ? <Link href={'/work/finance/' + encodeURIComponent(item.id)} aria-label={'Review commercial record for ' + item.label}>Commercial review</Link> : null}
                                  </>
                                ) : feed === 'consent'
                                  ? <Link href={'/consent-reviews?reviewId=' + encodeURIComponent(item.id)} aria-label={'Open consent review ' + item.label}>Open consent review</Link>
                                  : <span>Server-owned evidence</span>}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    </div>
                  ) : null}
                  {current.hasMore ? (
                    <button type="button" disabled={current.loading || current.loadingMore}
                      onClick={() => void loadFeed(feed, current.nextCursor)}>
                      {current.loadingMore ? 'Loading…' : feed === 'events' ? 'Load older organisation events' : 'Load more ' + titles[feed].toLowerCase()}
                    </button>
                  ) : null}
                  {current.items.length ? <p className="muted">{current.items.length} records on loaded pages; no full-count or completeness claim.</p> : null}
                </section>
              );
            })}
          </div>

          {(canFinance || canOperations) ? (
            <section className="card">
              <h3>Related operational evidence</h3>
              <p className="muted">Financial approvals and alert triage are separately permissioned. The links below are not filtered incident or finance totals for this organisation.</p>
              {canFinance ? <p><Link href="/work">Open finance approvals</Link> — filter by campaign in the governed work register.</p> : null}
              {canOperations ? <p><Link href="/operations/alerts">Open global Alert Centre</Link> — verify affected references before attributing an incident to this organisation.</p> : null}
            </section>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
