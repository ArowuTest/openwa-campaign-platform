'use client';

import Link from 'next/link';
import { useCallback, useEffect, useRef, useState } from 'react';

import { useAuth } from '../../components/auth-provider';
import { StatusBadge } from '../../components/status-badge';
import { apiRequest, APIError, type ListEnvelope } from '../../lib/api';
import { hasPermission } from '../../lib/session';

type Source = 'consent' | 'finance' | 'message' | 'final';

type InventoryItem = {
  id: string;
  name?: string;
  campaignId?: string;
  organisationId?: string;
  status?: string;
  updatedAt?: string;
  createdAt?: string;
  submittedBy?: string;
  createdBy?: string;
  version?: number;
};

type WorkItem = {
  id: string;
  title: string;
  href: string | null;
  organisationId?: string;
  status: string;
  updatedAt?: string;
  submittingActor?: string;
};

type SectionState = {
  items: WorkItem[];
  loaded: boolean;
  loading: boolean;
  loadingMore: boolean;
  hasMore: boolean;
  nextCursor: string;
  pages: number;
  error: string;
};

const sources: Source[] = ['consent', 'finance', 'message', 'final'];
const maxPages = 20;

const config: Record<Source, {
  title: string;
  description: string;
  empty: string;
  readPermission: string;
  decisionPermission: string;
  url: string;
  pendingStatus: string;
  linkPrefix: string;
}> = {
  consent: {
    title: 'Consent awaiting checker review',
    description: 'Submitted consent evidence must be assessed by a separate authorised checker.',
    empty: 'No pending consent work on this page',
    readPermission: 'consent.read',
    decisionPermission: 'consent.review',
    url: '/v1/consent-reviews?limit=50',
    pendingStatus: 'PENDING_REVIEW',
    linkPrefix: 'Open consent review'
  },
  finance: {
    title: 'Commercial evidence awaiting finance approval',
    description: 'Finance evidence is approved only in the campaign commercial-governance workspace.',
    empty: 'No pending finance records on this page',
    readPermission: 'finance.read',
    decisionPermission: 'finance.approve',
    url: '/v1/commercial-records?limit=50',
    pendingStatus: 'PENDING_APPROVAL',
    linkPrefix: 'Open commercial review'
  },
  message: {
    title: 'Message versions awaiting review',
    description: 'Open the campaign composer to inspect immutable message versions and consent evidence.',
    empty: 'No messages awaiting review on this page',
    readPermission: 'campaign.read',
    decisionPermission: 'campaign.approve',
    url: '/v1/campaigns?limit=50&status=MESSAGE_REVIEW_PENDING',
    pendingStatus: 'MESSAGE_REVIEW_PENDING',
    linkPrefix: 'Open message review'
  },
  final: {
    title: 'Final campaign approval',
    description: 'Final approval requires the controlled test, commercial evidence and capacity checks.',
    empty: 'No final approvals on this page',
    readPermission: 'campaign.read',
    decisionPermission: 'campaign.approve',
    url: '/v1/campaigns?limit=50&status=FINAL_APPROVAL_PENDING',
    pendingStatus: 'FINAL_APPROVAL_PENDING',
    linkPrefix: 'Open final approval'
  }
};

function emptyState(): SectionState {
  return {
    items: [], loaded: false, loading: false, loadingMore: false,
    hasMore: false, nextCursor: '', pages: 0, error: ''
  };
}

function initialState(): Record<Source, SectionState> {
  return { consent: emptyState(), finance: emptyState(), message: emptyState(), final: emptyState() };
}

function dateLabel(value?: string) {
  if (!value) return 'Not recorded';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? 'Invalid date' : date.toLocaleString();
}

function reference(value?: string) {
  return value ? '…' + value.slice(-8) : 'Not supplied';
}

function sourceItems(source: Source, items: InventoryItem[]): WorkItem[] {
  const expected = config[source].pendingStatus;
  return items.flatMap((item): WorkItem[] => {
    if (!item || !item.id || item.status !== expected) return [];
    const title = source === 'consent'
      ? item.name || item.id
      : source === 'finance'
        ? item.id
        : item.name || item.id;
    let href: string | null = null;
    if (source === 'consent') href = '/consent-reviews?reviewId=' + encodeURIComponent(item.id);
    if (source === 'finance') href = item.campaignId ? '/work/finance/' + encodeURIComponent(item.campaignId) : null;
    if (source === 'message' || source === 'final') href = '/campaigns/' + encodeURIComponent(item.id);
    return [{
      id: item.id, title, href, status: item.status,
      organisationId: item.organisationId,
      submittingActor: source === 'message' || source === 'final' ? item.createdBy : item.submittedBy,
      updatedAt: item.updatedAt || item.createdAt
    }];
  });
}

function messageFor(error: unknown) {
  if (error instanceof APIError && error.status === 403 && error.code.includes('STEP_UP')) {
    return 'A recent MFA challenge is required for this operation. Return through the secure step-up journey.';
  }
  return error instanceof Error ? error.message : 'The governed work register could not be loaded.';
}

export function WorkApprovalBoard() {
  const { session } = useAuth();
  const generation = JSON.stringify([session?.id, session?.sessionId, [...(session?.permissions ?? [])].sort()]);
  const readable = sources.some((source) => hasPermission(session?.permissions, config[source].readPermission));
  if (!readable) return <p role="status">My Work requires at least one campaign.read, consent.read or finance.read permission.</p>;
  return <WorkApprovalGeneration key={generation} />;
}

function WorkApprovalGeneration() {
  const { session } = useAuth();
  const readable = sources.filter((source) => hasPermission(session?.permissions, config[source].readPermission));

  const mounted = useRef(false);
  const requests = useRef<Record<Source, number>>({ consent: 0, finance: 0, message: 0, final: 0 });
  const cursors = useRef<Record<Source, Set<string>>>({
    consent: new Set(), finance: new Set(), message: new Set(), final: new Set()
  });
  const [state, setState] = useState<Record<Source, SectionState>>(initialState);

  const load = useCallback(async (source: Source, cursor = '', reset = false) => {
    const request = ++requests.current[source];
    if (reset) {
      cursors.current[source].clear();
      setState((prior) => ({ ...prior, [source]: { ...emptyState(), loading: true } }));
    } else {
      if (state[source].pages >= maxPages) {
        setState((prior) => ({
          ...prior,
          [source]: { ...prior[source], hasMore: false, error: 'The displayed source is capped at 1,000 records; refresh for current work.' }
        }));
        return;
      }
      setState((prior) => ({ ...prior, [source]: { ...prior[source], loadingMore: true, error: '' } }));
    }

    const path = config[source].url + (cursor ? '&cursor=' + encodeURIComponent(cursor) : '');
    try {
      const page = await apiRequest<ListEnvelope<InventoryItem>>(path);
      if (!mounted.current || request !== requests.current[source]) return;
      if (!page || !Array.isArray(page.items) || typeof page.hasMore !== 'boolean' || (page.hasMore && !page.nextCursor)) {
        throw new Error('The work source returned an invalid continuation page.');
      }
      const nextCursor = page.nextCursor || '';
      if (page.hasMore && (nextCursor === cursor || cursors.current[source].has(nextCursor))) {
        throw new Error('The work source repeated its cursor. Further requests have been blocked.');
      }
      if (page.hasMore) cursors.current[source].add(nextCursor);
      const received = sourceItems(source, page.items);
      setState((prior) => {
        const previous = prior[source];
        const items = reset ? received : [...previous.items, ...received.filter((item) =>
          !previous.items.some((current) => current.id === item.id)
        )];
        const pages = reset ? 1 : previous.pages + 1;
        return {
          ...prior,
          [source]: {
            items,
            loaded: true,
            loading: false,
            loadingMore: false,
            hasMore: page.hasMore && pages < maxPages,
            nextCursor,
            pages,
            error: ''
          }
        };
      });
    } catch (error) {
      if (!mounted.current || request !== requests.current[source]) return;
      setState((prior) => ({
        ...prior,
        [source]: {
          ...prior[source],
          loaded: true, loading: false, loadingMore: false, hasMore: false,
          error: messageFor(error)
        }
      }));
    }
  }, [state]);

  useEffect(() => {
    mounted.current = true;
    // This is the initial synchronisation with four existing server-owned queues.
    for (const source of readable) void load(source, '', true);
    const currentRequests = requests.current;
    return () => {
      mounted.current = false;
      for (const source of sources) currentRequests[source] += 1;
    };
  // The available sources cannot change inside this key-bound session generation.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function refresh() {
    for (const source of readable) void load(source, '', true);
  }

  return (
    <div className="workspace-stack">
      <section className="card">
        <div className="section-heading">
          <div>
            <h2>Pending decision register</h2>
            <p className="muted">This is an operator routing aid, not an approval engine. No approval decisions are taken on this page; all decisions and audit evidence remain in their source workspaces.</p>
          </div>
          <button type="button" onClick={refresh} disabled={readable.some((source) => state[source].loading || state[source].loadingMore)}>Refresh available work</button>
        </div>
        <p className="muted">Loaded results are not the complete backlog. The consent and finance APIs currently paginate without server-side status filtering; additional pending items may exist on later pages.</p>
      </section>
      <div className="grid grid-2">
        {readable.map((source) => {
          const opts = config[source];
          const section = state[source];
          const canApprove = hasPermission(session?.permissions, opts.decisionPermission);
          return (
            <section key={source} className="card form-stack" aria-label={opts.title}>
              <div className="section-heading">
                <div><h2>{opts.title}</h2><p className="muted">{opts.description}</p></div>
                <span className="pill">{section.items.length} loaded</span>
              </div>
              {!canApprove ? <p className="muted">Read-only — decisions require {opts.decisionPermission} permission.</p> : null}
              {section.loading ? <p role="status">Loading {source} work…</p> : null}
              {section.error ? <p role="alert">{section.error}</p> : null}
              {section.loaded && !section.loading && section.items.length === 0 && !section.error
                ? <div className="empty-state"><strong>{opts.empty}</strong><p>More pending records may exist outside the loaded page or in another organisation scope.</p></div>
                : null}
              {section.items.length > 0 ? (
                <div className="table-wrap">
                  <table>
                    <thead><tr><th>Pending work</th><th>Status</th><th>Detail</th></tr></thead>
                    <tbody>{section.items.map((item) => (
                      <tr key={item.id}>
                        <td>
                          <strong>{item.title}</strong>
                          <small>Organisation {reference(item.organisationId)} · {dateLabel(item.updatedAt)}</small>
                          {item.submittingActor === session?.id
                            ? <small>Submitted by you — independent checker required</small>
                            : !canApprove ? <small>View only — approval role not held</small>
                              : <small>Subject to independent checker and server evidence validation</small>}
                        </td>
                        <td><StatusBadge status={item.status} /></td>
                        <td>{item.href
                          ? <Link href={item.href} aria-label={opts.linkPrefix + ' ' + item.title}>Open</Link>
                          : <span>Campaign reference unavailable; escalate to Finance administrator</span>}</td>
                      </tr>
                    ))}</tbody>
                  </table>
                </div>
              ) : null}
              {section.hasMore
                ? <button type="button" disabled={section.loading || section.loadingMore} onClick={() => void load(source, section.nextCursor)}>
                  {section.loadingMore ? 'Loading…' : 'Load more ' + (source === 'consent' ? 'consent reviews' : source === 'finance' ? 'finance evidence' : source === 'message' ? 'message approvals' : 'final approvals')}
                </button> : null}
            </section>
          );
        })}
      </div>
    </div>
  );
}
