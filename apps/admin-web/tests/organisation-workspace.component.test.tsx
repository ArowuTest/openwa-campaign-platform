import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { OrganisationWorkspace } from '../app/organisations/[id]/organisation-workspace';

const auth = vi.hoisted(() => ({
  session: {
    id: 'ops-one', sessionId: 'session-one',
    permissions: ['organisation.read', 'campaign.read', 'consent.read', 'audience.read', 'finance.read', 'operations.read']
  }
}));
vi.mock('../components/auth-provider', () => ({ useAuth: () => ({ session: auth.session }) }));

const orgId = '00000000-0000-4000-8000-000000000121';
const otherOrgId = '00000000-0000-4000-8000-000000000122';
const org = {
  id: orgId, legalName: 'Alpha Media Limited', tradingName: 'Alpha Media', countryISO2: 'NG',
  status: 'ACTIVE', primaryContactName: 'Client Operations', primaryContactEmail: 'ops@example.test',
  internalNotes: 'PRIVATE_ORGANISATION_NOTES_DO_NOT_RENDER',
  createdAt: '2026-10-01T10:00:00Z', updatedAt: '2026-10-12T10:00:00Z', version: 5
};
const campaign = {
  id: 'campaign-one', organisationId: orgId, name: 'Autumn welcome', status: 'RUNNING',
  version: 8, updatedAt: '2026-10-13T11:00:00Z', createdAt: '2026-10-01T08:00:00Z'
};
const consent = {
  id: 'review-one', organisationId: orgId, name: 'Subscriber sign-up', scope: 'ORGANISATION',
  status: 'APPROVED', version: 3, expiresAt: '2026-12-01T00:00:00Z',
  exactConsentWording: 'PRIVATE_CONSENT_WORDING_DO_NOT_RENDER',
  updatedAt: '2026-10-05T12:00:00Z'
};
const policy = {
  id: 'policy-one', organisationId: orgId, status: 'ACTIVE', version: 2,
  allowedPurposeIds: ['PROMOTIONAL'], prohibitedPurposeIds: ['POLITICAL'],
  reason: 'PRIVATE_GOVERNANCE_CASE_NOTES_DO_NOT_RENDER',
  contactRetentionDays: 30, campaignRetentionDays: 90,
  updatedAt: '2026-10-10T07:00:00Z'
};
const audit = {
  id: 'audit-one', organisationId: orgId, version: 5, eventType: 'ORGANISATION_STATUS_CHANGED',
  actorId: 'staff-secret-actor', reason: 'PRIVATE_AUDIT_REASON_DO_NOT_RENDER',
  beforeStatus: 'UNDER_REVIEW', afterStatus: 'ACTIVE', occurredAt: '2026-10-12T10:00:00Z'
};
const sourceTrust = {
  organisationId: orgId, sourceSystem: 'REGISTRATION_FORM',
  trustLevel: 85, reason: 'PRIVATE_SOURCE_NOTES_DO_NOT_RENDER',
  version: 1, updatedAt: '2026-10-11T08:00:00Z'
};
function reply(payload: unknown, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { 'content-type': 'application/json' } });
}
function page(items: unknown[], hasMore = false, nextCursor?: string) {
  return reply({ items, count: items.length, hasMore, nextCursor });
}
function mockStandardFetch(paths: string[]) {
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    paths.push(path);
    if (path === '/v1/organisations/' + orgId) return reply(org);
    if (path === '/v1/organisations/' + orgId + '/events?limit=50') return page([audit]);
    if (path === '/v1/organisations/' + orgId + '/policies?limit=50') return page([policy]);
    if (path === '/v1/consent-reviews?organisationId=' + orgId + '&limit=50') return page([consent]);
    if (path === '/v1/campaigns?organisationId=' + orgId + '&limit=50') return page([campaign]);
    if (path === '/v1/organisations/' + orgId + '/audience-source-trust') return page([sourceTrust]);
    throw new Error('Unexpected read: ' + path);
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  auth.session = {
    id: 'ops-one', sessionId: 'session-one',
    permissions: ['organisation.read', 'campaign.read', 'consent.read', 'audience.read', 'finance.read', 'operations.read']
  };
});

test('shows a permission-scoped organisation profile with real server-filtered related records and deep links', async () => {
  const paths: string[] = [];
  mockStandardFetch(paths);
  render(<OrganisationWorkspace organisationId={orgId} />);
  expect(await screen.findByRole('heading', { name: 'Alpha Media' })).toBeTruthy();
  expect(await screen.findByText('Autumn welcome')).toBeTruthy();
  expect(await screen.findByText('Subscriber sign-up')).toBeTruthy();
  expect(await screen.findByText('REGISTRATION_FORM')).toBeTruthy();
  expect(await screen.findByText('ORGANISATION_STATUS_CHANGED')).toBeTruthy();

  expect(screen.getByRole('link', { name: 'Open campaign Autumn welcome' }).getAttribute('href')).toBe('/campaigns/campaign-one');
  expect(screen.getByRole('link', { name: 'Open consent review Subscriber sign-up' }).getAttribute('href')).toBe('/consent-reviews?reviewId=review-one');
  expect(screen.getByRole('link', { name: 'Review commercial record for Autumn welcome' }).getAttribute('href')).toBe('/work/finance/campaign-one');
  expect(screen.getByRole('link', { name: 'Edit this organisation in register' }).getAttribute('href')).toBe('/organisations?organisationId=' + orgId);
  expect(paths).toHaveLength(6);
  expect(paths.every((path) => !path.includes('limit=') || path.includes('organisationId=' + orgId) || path.includes('/organisations/' + orgId + '/'))).toBe(true);
  for (const privateText of ['PRIVATE_ORGANISATION_NOTES_DO_NOT_RENDER','PRIVATE_CONSENT_WORDING_DO_NOT_RENDER','PRIVATE_GOVERNANCE_CASE_NOTES_DO_NOT_RENDER','PRIVATE_AUDIT_REASON_DO_NOT_RENDER','PRIVATE_SOURCE_NOTES_DO_NOT_RENDER']) {
    expect(screen.queryByText(privateText)).toBeNull();
  }
});

test('organisation.read-only viewer never requests or sees campaign, consent or source-trust data', async () => {
  auth.session = { id: 'reader', sessionId: 'reader-session', permissions: ['organisation.read'] };
  const paths: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    paths.push(path);
    if (path === '/v1/organisations/' + orgId) return reply(org);
    if (path === '/v1/organisations/' + orgId + '/events?limit=50') return page([audit]);
    if (path === '/v1/organisations/' + orgId + '/policies?limit=50') return page([policy]);
    throw new Error('Unauthorised data request: ' + path);
  });
  render(<OrganisationWorkspace organisationId={orgId} />);
  expect(await screen.findByRole('heading', { name: 'Alpha Media' })).toBeTruthy();
  expect(paths.sort()).toEqual([
    '/v1/organisations/' + orgId,
    '/v1/organisations/' + orgId + '/events?limit=50',
    '/v1/organisations/' + orgId + '/policies?limit=50'
  ].sort());
  expect(screen.queryByText('Autumn welcome')).toBeNull();
  expect(screen.queryByText('Subscriber sign-up')).toBeNull();
  expect(screen.queryByText('REGISTRATION_FORM')).toBeNull();
});

test('rejects unexpected cross-organisation records rather than displaying another client data', async () => {
  const paths: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    paths.push(path);
    if (path === '/v1/organisations/' + orgId) return reply(org);
    if (path.includes('/events?') || path.includes('/policies?')) return page([]);
    if (path === '/v1/consent-reviews?organisationId=' + orgId + '&limit=50') return page([{ ...consent, organisationId: otherOrgId }]);
    if (path === '/v1/campaigns?organisationId=' + orgId + '&limit=50') return page([{ ...campaign, organisationId: otherOrgId }]);
    if (path.endsWith('/audience-source-trust')) return page([]);
    throw new Error('Unexpected ' + path);
  });
  render(<OrganisationWorkspace organisationId={orgId} />);
  expect(await screen.findAllByText(/The server returned a record for a different organisation/)).toHaveLength(2);
  expect(screen.queryByText('Autumn welcome')).toBeNull();
  expect(screen.queryByText('Subscriber sign-up')).toBeNull();
});

test('paginates immutable events with exact cursor and fails closed if the server repeats a cursor', async () => {
  auth.session = { id: 'reader', sessionId: 'session-reader', permissions: ['organisation.read'] };
  const paths: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    paths.push(path);
    if (path === '/v1/organisations/' + orgId) return reply(org);
    if (path === '/v1/organisations/' + orgId + '/policies?limit=50') return page([]);
    if (path === '/v1/organisations/' + orgId + '/events?limit=50') return page([audit], true, 'event-cursor');
    if (path === '/v1/organisations/' + orgId + '/events?limit=50&cursor=event-cursor')
      return page([{ ...audit, id:'audit-two',eventType:'ORGANISATION_UPDATED'}], true, 'event-cursor');
    throw new Error('Unexpected ' + path);
  });
  render(<OrganisationWorkspace organisationId={orgId} />);
  fireEvent.click(await screen.findByRole('button', { name: 'Load older organisation events' }));
  expect(await screen.findByText(/The server repeated a pagination cursor/)).toBeTruthy();
  expect(paths.filter(path => path.includes('cursor=event-cursor'))).toHaveLength(1);
});

test('discard previous operator data and late responses after session or permission change', async () => {
  auth.session = {id:'reader-1',sessionId:'session-1',permissions:['organisation.read']};
  let release!: (value: Response) => void;
  let requests = 0;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path=String(input).replace('/api','');
    if(path === '/v1/organisations/' + orgId) {
      requests++;
      if (requests === 1) return new Promise<Response>((resolve) => { release = resolve; });
      return reply({ ...org, legalName:'Other operator safe view', tradingName:'Other operator company' });
    }
    if(path.endsWith('/events?limit=50') || path.endsWith('/policies?limit=50')) return page([]);
    throw new Error('Unexpected '+path);
  });
  const view=render(<OrganisationWorkspace organisationId={orgId}/>);
  await waitFor(()=>expect(requests).toBe(1));
  auth.session={id:'reader-2',sessionId:'session-2',permissions:['organisation.read']};
  view.rerender(<OrganisationWorkspace organisationId={orgId}/>);
  await act(async()=>{ release(reply(org)); await Promise.resolve(); });
  expect(screen.queryByText('Alpha Media')).toBeNull();
  expect(await screen.findByRole('heading',{name:'Other operator company'})).toBeTruthy();
  auth.session={id:'reader-2',sessionId:'session-2',permissions:[]};
  view.rerender(<OrganisationWorkspace organisationId={orgId}/>);
  expect(screen.getByText('Organisation workspace requires organisation.read permission.')).toBeTruthy();
  expect(screen.queryByText('Other operator company')).toBeNull();
});

test('invalid or missing organisation references are rejected without API access', () => {
  const calls=vi.fn();
  vi.stubGlobal('fetch',calls);
  render(<OrganisationWorkspace organisationId={'..%2Fsecret'} />);
  expect(screen.getByText('An organisation must be selected using a valid internal reference.')).toBeTruthy();
  expect(calls).not.toHaveBeenCalled();
});

test('a missing organisation is rejected before requesting any related client evidence', async () => {
  const requests:string[]=[];
  vi.stubGlobal('fetch', async (input:RequestInfo|URL) => {
    const path=String(input).replace('/api','');
    requests.push(path);
    if(path==='/v1/organisations/'+orgId) return reply({code:'ORGANISATION_NOT_FOUND',message:'Not found'},404);
    throw new Error('Related evidence must not be requested for a missing client: '+path);
  });
  render(<OrganisationWorkspace organisationId={orgId}/>);
  expect(await screen.findByText('The requested organisation or evidence was not found.')).toBeTruthy();
  expect(requests).toEqual(['/v1/organisations/'+orgId]);
});

test('non-paginated source-trust API fails closed on an incomplete or oversized result', async () => {
  const paths:string[]=[];
  vi.stubGlobal('fetch', async (input:RequestInfo|URL) => {
    const path=String(input).replace('/api','');
    paths.push(path);
    if(path==='/v1/organisations/'+orgId)return reply(org);
    if(path.endsWith('/events?limit=50') || path.endsWith('/policies?limit=50'))return page([]);
    if(path==='/v1/organisations/'+orgId+'/audience-source-trust')
      return page([sourceTrust], true, 'fake-cursor-unsupported');
    throw new Error('Unexpected ' + path);
  });
  auth.session={id:'source-reader',sessionId:'source-session',permissions:['organisation.read','audience.read']};
  render(<OrganisationWorkspace organisationId={orgId}/>);
  expect(await screen.findByText(/Audience source trust cannot be paginated safely/)).toBeTruthy();
  expect(screen.queryByText('REGISTRATION_FORM')).toBeNull();
  expect(paths.filter(p=>p.includes('audience-source-trust'))).toHaveLength(1);
  expect(screen.queryByRole('button',{name:/Load more trusted audience sources/})).toBeNull();
});
