import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { InboundReviewWorkbench } from '../app/operations/inbound/inbound-review-workbench';

const nav = vi.hoisted(() => ({ push: vi.fn() }));
const auth = vi.hoisted(() => ({
  session: {
    id: 'operator-one',
    sessionId: 'session-one',
    permissions: ['inbound.read', 'inbound.review', 'inbound.content.read', 'consent.read', 'consent.write', 'privacy.admin']
  }
}));
vi.mock('next/navigation', () => ({ useRouter: () => nav }));
vi.mock('../components/auth-provider', () => ({ useAuth: () => ({ session: auth.session }) }));

afterEach(() => {
  auth.session = {
    id: 'operator-one', sessionId: 'session-one',
    permissions: ['inbound.read', 'inbound.review', 'inbound.content.read', 'consent.read', 'consent.write', 'privacy.admin']
  };
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  nav.push.mockReset();
});

const contactId = '00000000-0000-4000-8000-000000000010';
const reply = {
  id: '00000000-0000-4000-8000-000000000011',
  eventId: 'event-one',
  recipientId: 'recipient-one',
  contactId,
  campaignId: 'campaign-one',
  sessionId: 'sender-one',
  classification: 'UNREVIEWED',
  escalated: false,
  messageText: 'PRIVATE REPLY DO NOT RENDER',
  occurredAt: '2026-10-13T09:00:00Z',
  createdAt: '2026-10-13T09:00:01Z',
  contentRetainUntil: '2026-11-13T00:00:00Z',
  legalHold: false,
  version: 1
};

function result(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'content-type': 'application/json' } });
}

test('lists only masked metadata and loads cursor pages without automatically revealing reply content', async () => {
  const calls: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    calls.push(path);
    if (path === '/v1/inbound-replies?limit=50') {
      return result({ items: [reply], count: 1, hasMore: true, nextCursor: 'older-cursor' });
    }
    if (path === '/v1/inbound-replies?limit=50&cursor=older-cursor') {
      return result({ items: [{ ...reply, id: '00000000-0000-4000-8000-000000000012', eventId: 'event-two', version: 2 }], count: 1, hasMore: false });
    }
    if (path.startsWith('/v1/consent-events?contactId=')) {
      return result({ items: [], count: 0, hasMore: false });
    }
    throw new Error('Unexpected request ' + path);
  });

  render(<InboundReviewWorkbench />);
  expect(await screen.findByRole('button', { name: /Review reply event-one/ })).toBeTruthy();
  expect(screen.queryByText('PRIVATE REPLY DO NOT RENDER')).toBeNull();
  expect(calls.some((path) => path.includes('/content'))).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Load older replies' }));
  expect(await screen.findByRole('button', { name: /Review reply event-two/ })).toBeTruthy();
  expect(screen.queryByText('PRIVATE REPLY DO NOT RENDER')).toBeNull();
});

test('governed review classifies and escalates with expected version, never claiming classification makes suppression', async () => {
  const posts: Array<{path:string; body:Record<string,unknown>}> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    if (path.startsWith('/v1/inbound-replies?')) {
      return result({ items: [reply], count: 1, hasMore: false });
    }
    if (path.startsWith('/v1/consent-events?')) {
      return result({ items: [{
        id: 'consent-event-one', suppressionId: 'suppression-one',
        eventType: 'SUPPRESSION_CREATED', occurredAt: '2026-10-13T09:00:02Z'
      }], count: 1, hasMore: false });
    }
    if (path.endsWith('/review') && init.method === 'POST') {
      const body = JSON.parse(String(init.body));
      posts.push({path,body});
      return result({ ...reply, classification: 'OPT_OUT', escalated: true, escalationReason: 'Ambiguous multilingual request', version: 2 });
    }
    throw new Error('Unexpected request '+path);
  });
  render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  expect(await screen.findByText('SUPPRESSION_CREATED')).toBeTruthy();
  expect(screen.getByText(/Classification does not itself create a suppression/)).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Classification'), { target: { value: 'OPT_OUT' } });
  fireEvent.click(screen.getByLabelText('Escalate for review'));
  fireEvent.change(screen.getByLabelText('Escalation reason'), { target: { value: 'Ambiguous multilingual request' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save classification' }));
  await waitFor(() => expect(posts).toHaveLength(1));
  expect(posts[0].body).toEqual({
    expectedVersion: 1,
    classification: 'OPT_OUT',
    escalated: true,
    escalationReason: 'Ambiguous multilingual request'
  });
  expect(posts[0].path).toBe('/v1/inbound-replies/' + reply.id + '/review');
});

test('content reveal is explicit, ephemeral and cleared immediately on session change or permission loss', async () => {
  const paths: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    paths.push(path);
    if (path.startsWith('/v1/inbound-replies?')) return result({ items: [reply], count: 1, hasMore: false });
    if (path.startsWith('/v1/consent-events?')) return result({ items: [], count: 0, hasMore: false });
    if (path.endsWith('/content')) return result({
      id: reply.id, messageText: 'SENSITIVE EXPLICIT REVEAL', fingerprint: 'hash',
      retainUntil: '2026-11-13T00:00:00Z', legalHold: false
    });
    throw new Error('Unexpected request '+path);
  });
  const rendered = render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  expect(paths.some((value) => value.includes('/content'))).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Reveal retained reply content (audited)' }));
  expect(await screen.findByText('SENSITIVE EXPLICIT REVEAL')).toBeTruthy();
  auth.session = { id: 'operator-two', sessionId: 'session-two', permissions: ['inbound.read'] };
  rendered.rerender(<InboundReviewWorkbench />);
  expect(screen.queryByText('SENSITIVE EXPLICIT REVEAL')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Reveal retained reply content (audited)' })).toBeNull();
  auth.session = { id: 'operator-two', sessionId: 'session-two', permissions: [] };
  rendered.rerender(<InboundReviewWorkbench />);
  expect(screen.getByText('Inbound review requires inbound.read permission.')).toBeTruthy();
  expect(screen.queryByText('SENSITIVE EXPLICIT REVEAL')).toBeNull();
});

test('manual global suppression is an independent consent.write action with idempotency and attributable contact source', async () => {
  const calls: Array<{ path: string; method: string; options: RequestInit; body?: Record<string, unknown> }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({path,method:init.method??'GET',options:init,body:init.body ? JSON.parse(String(init.body)) : undefined});
    if (path.startsWith('/v1/inbound-replies?')) return result({ items: [reply], count: 1, hasMore: false });
    if (path.startsWith('/v1/consent-events?')) return result({ items: [], count: 0, hasMore: false });
    if (path === '/v1/suppressions' && init.method === 'POST') {
      return result({ created: true, suppression: { id:'suppression-created',active:true,contactId,scope:'GLOBAL',version:1 } },201);
    }
    throw new Error('Unexpected request '+path);
  });
  render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  const form = screen.getByRole('heading', { name: 'Independent suppression action' }).closest('form');
  const controls=within(form as HTMLElement);
  fireEvent.change(controls.getByLabelText('Suppression reason'), { target: { value: 'Recipient expressly requested no further messages' } });
  fireEvent.click(controls.getByLabelText('I have verified the contact reference and permanent global scope'));
  fireEvent.click(controls.getByRole('button', {name:'Record global suppression'}));
  await waitFor(() => expect(calls.some((call) => call.path === '/v1/suppressions' && call.method === 'POST')).toBe(true));
  const post=calls.find((call)=>call.path==='/v1/suppressions');
  expect(post?.body).toMatchObject({
    contactId,
    scope:'GLOBAL',
    reason:'Recipient expressly requested no further messages',
    sourceReference:'inbound:event-one'
  });
  expect(new Headers(post?.options.headers).get('Idempotency-Key')).toMatch(/^[a-z0-9-]{16,}$/i);
  expect(await screen.findByText(/Global suppression recorded/)).toBeTruthy();
});


test('unreviewed evidence cannot be saved without an explicit classification decision', async () => {
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    if (path.startsWith('/v1/inbound-replies?')) return result({items:[reply],count:1,hasMore:false});
    if (path.startsWith('/v1/consent-events?')) return result({items:[],count:0,hasMore:false});
    throw new Error('Unexpected '+path);
  });
  render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button',{name:/Review reply event-one/}));
  expect((screen.getByLabelText('Classification') as HTMLSelectElement).value).toBe('');
  expect((screen.getByRole('button',{name:'Save classification'}) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText('Classification'), {target:{value:'COMPLAINT'}});
  expect((screen.getByRole('button',{name:'Save classification'}) as HTMLButtonElement).disabled).toBe(false);
});

test('lost suppression response is not automatically retried and an exact manual replay reuses the idempotency key', async () => {
  let posts = 0;
  const keys: string[] = [];
  const bodies: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit={}) => {
    const path = String(input).replace('/api','');
    if(path.startsWith('/v1/inbound-replies?')) return result({items:[reply],count:1,hasMore:false});
    if(path.startsWith('/v1/consent-events?')) return result({items:[],count:0,hasMore:false});
    if(path==='/v1/suppressions' && init.method==='POST'){
      posts++;
      keys.push(String(new Headers(init.headers).get('Idempotency-Key')));
      bodies.push(String(init.body));
      if(posts===1) throw new Error('Network response lost');
      return result({created:false,suppression:{id:'suppression-one',active:true,scope:'GLOBAL',version:1}});
    }
    throw new Error('Unexpected '+path);
  });
  render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button',{name:/Review reply event-one/}));
  const form=screen.getByRole('heading',{name:'Independent suppression action'}).closest('form');
  const view=within(form as HTMLElement);
  fireEvent.change(view.getByLabelText('Suppression reason'),{target:{value:'Recipient explicitly requested global do not contact'}});
  fireEvent.click(view.getByLabelText('I have verified the contact reference and permanent global scope'));
  fireEvent.click(view.getByRole('button',{name:'Record global suppression'}));
  expect(await screen.findByText(/Suppression outcome is unconfirmed/)).toBeTruthy();
  expect(posts).toBe(1);
  // A refresh and re-selection may never mint a second key for an uncertain
  // suppression. Exact user-triggered replay must remain idempotent.
  fireEvent.click(screen.getByRole('button', {name:'Refresh replies'}));
  fireEvent.click(await screen.findByRole('button', {name:/Review reply event-one/}));
  const renewed=within(screen.getByRole('heading',{name:'Independent suppression action'}).closest('form') as HTMLElement);
  fireEvent.change(renewed.getByLabelText('Suppression reason'),{target:{value:'Recipient explicitly requested global do not contact'}});
  fireEvent.click(renewed.getByLabelText('I have verified the contact reference and permanent global scope'));
  fireEvent.click(renewed.getByRole('button',{name:'Record global suppression'}));
  await waitFor(()=>expect(posts).toBe(2));
  expect(keys[0]).toBe(keys[1]);
  expect(bodies[0]).toBe(bodies[1]);
  expect(await screen.findByText(/Existing identical suppression request confirmed/)).toBeTruthy();
});

test('a repeated page cursor fails closed instead of reading an endless inventory', async () => {
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path=String(input).replace('/api','');
    if(path==='/v1/inbound-replies?limit=50') return result({items:[reply],count:1,hasMore:true,nextCursor:'cursor-one'});
    if(path==='/v1/inbound-replies?limit=50&cursor=cursor-one') return result({items:[reply],count:1,hasMore:true,nextCursor:'cursor-one'});
    throw new Error('Unexpected '+path);
  });
  render(<InboundReviewWorkbench />);
  expect(await screen.findByRole('button',{name:/Review reply event-one/})).toBeTruthy();
  fireEvent.click(screen.getByRole('button',{name:'Load older replies'}));
  expect(await screen.findByText(/Repeated inbound-reply cursor/)).toBeTruthy();
});

test('late revealed content is discarded after an operator session change', async () => {
  let releaseContent: ((response: Response) => void) | undefined;
  let fetchStarted = false;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    if (path.startsWith('/v1/inbound-replies?')) return result({ items: [reply], count: 1, hasMore: false });
    if (path.startsWith('/v1/consent-events?')) return result({ items: [], count: 0, hasMore: false });
    if (path.endsWith('/content')) {
      fetchStarted = true;
      return new Promise<Response>((resolve) => { releaseContent = resolve; });
    }
    throw new Error('Unexpected request: ' + path);
  });
  const rendered = render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Reveal retained reply content (audited)' }));
  await waitFor(() => expect(fetchStarted).toBe(true));
  auth.session = { id: 'another-operator', sessionId: 'another-session', permissions: ['inbound.read'] };
  rendered.rerender(<InboundReviewWorkbench />);
  await act(async () => {
    releaseContent?.(result({ id: reply.id, messageText: 'LATE PRIVATE CONTENT MUST BE DISCARDED', legalHold: false }));
    await Promise.resolve();
  });
  expect(screen.queryByText('LATE PRIVATE CONTENT MUST BE DISCARDED')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Reveal retained reply content (audited)' })).toBeNull();
});

test('optimistic review conflict stays blocked until a refresh observes a newer server version', async () => {
  let submissions = 0;
  let currentVersion = 1;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    if (path.startsWith('/v1/inbound-replies?')) return result({ items: [{ ...reply, version: currentVersion }], count: 1, hasMore: false });
    if (path.startsWith('/v1/consent-events?')) return result({ items: [], count: 0, hasMore: false });
    if (path.endsWith('/review') && init.method === 'POST') {
      submissions += 1;
      return result({ code: 'INBOUND_REPLY_CONFLICT', message: 'Reply changed' }, 409);
    }
    throw new Error('Unexpected request: ' + path);
  });
  render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  fireEvent.change(screen.getByLabelText('Classification'), { target: { value: 'QUESTION' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save classification' }));
  expect(await screen.findByText(/This reply was changed by another operator/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Save classification' }));
  expect(submissions).toBe(1);
  expect((screen.getByRole('button', { name: 'Save classification' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: /Review reply event-one/ }));
  expect((screen.getByRole('button', { name: 'Save classification' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Refresh replies' }));
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  expect((screen.getByRole('button', { name: 'Save classification' }) as HTMLButtonElement).disabled).toBe(true);
  currentVersion = 2;
  fireEvent.click(screen.getByRole('button', { name: 'Refresh replies' }));
  await waitFor(() => expect(screen.getByText('No reply selected')).toBeTruthy());
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  fireEvent.change(screen.getByLabelText('Classification'), { target: { value: 'QUESTION' } });
  expect((screen.getByRole('button', { name: 'Save classification' }) as HTMLButtonElement).disabled).toBe(false);
});

test('a definitively rejected suppression can be corrected without falsely preserving an uncertain request', async () => {
  const requests: Array<{ payload: string; key: string }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    if (path.startsWith('/v1/inbound-replies?')) return result({ items: [reply], count: 1, hasMore: false });
    if (path.startsWith('/v1/consent-events?')) return result({ items: [], count: 0, hasMore: false });
    if (path === '/v1/suppressions' && init.method === 'POST') {
      requests.push({ payload: String(init.body), key: String(new Headers(init.headers).get('Idempotency-Key')) });
      if (requests.length === 1) return result({ code: 'CONSENT_LEDGER_REJECTED', message: 'Invalid suppression evidence' }, 422);
      return result({ created: true, suppression: { id: 'suppression-fixed', active: true, scope: 'GLOBAL', version: 1 } }, 201);
    }
    throw new Error('Unexpected request: ' + path);
  });
  render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  const view = within(screen.getByRole('heading', { name: 'Independent suppression action' }).closest('form') as HTMLElement);
  fireEvent.change(view.getByLabelText('Suppression reason'), { target: { value: 'Original valid reason' } });
  fireEvent.click(view.getByLabelText('I have verified the contact reference and permanent global scope'));
  fireEvent.click(view.getByRole('button', { name: 'Record global suppression' }));
  expect(await screen.findByText(/The server rejected this suppression/)).toBeTruthy();
  fireEvent.change(view.getByLabelText('Suppression reason'), { target: { value: 'Corrected explicit opt-out and global suppression basis' } });
  fireEvent.click(view.getByRole('button', { name: 'Record global suppression' }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[0].payload).not.toBe(requests[1].payload);
  expect(requests[0].key).not.toBe(requests[1].key);
  expect(await screen.findByText(/Global suppression recorded/)).toBeTruthy();
});

test('manual suppression fails closed when the browser cannot generate cryptographic idempotency keys', async () => {
  const posts: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    if (path.startsWith('/v1/inbound-replies?')) return result({ items: [reply], count: 1, hasMore: false });
    if (path.startsWith('/v1/consent-events?')) return result({ items: [], count: 0, hasMore: false });
    if (path === '/v1/suppressions') posts.push(path);
    throw new Error('Unexpected request ' + path);
  });
  render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button', { name: /Review reply event-one/ }));
  const form = screen.getByRole('heading', { name: 'Independent suppression action' }).closest('form');
  const view = within(form as HTMLElement);
  fireEvent.change(view.getByLabelText('Suppression reason'), { target: { value: 'Explicit no further contact instruction' } });
  fireEvent.click(view.getByLabelText('I have verified the contact reference and permanent global scope'));
  vi.stubGlobal('crypto', {});
  fireEvent.click(view.getByRole('button', { name: 'Record global suppression' }));
  expect(await screen.findByText(/cannot generate a secure idempotency key/)).toBeTruthy();
  expect(posts).toHaveLength(0);
});

test('unprivileged operations users never request or render the inbound workbench', () => {
  auth.session={id:'operator-one',sessionId:'session-one',permissions:['operations.read']};
  const mock=vi.fn();
  vi.stubGlobal('fetch',mock);
  render(<InboundReviewWorkbench />);
  expect(screen.getByText('Inbound review requires inbound.read permission.')).toBeTruthy();
  expect(mock).not.toHaveBeenCalled();
});

test('legal hold action carries exact version and reason; reviewers without privacy.admin cannot access hold controls', async () => {
  const calls: Array<{path:string;body:Record<string,unknown>}> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path=String(input).replace('/api','');
    if(path.startsWith('/v1/inbound-replies?'))return result({items:[reply],count:1,hasMore:false});
    if(path.startsWith('/v1/consent-events?'))return result({items:[],count:0,hasMore:false});
    if(path.endsWith('/legal-hold') && init.method==='POST'){
      const body=JSON.parse(String(init.body)); calls.push({path,body});
      return result({...reply,legalHold:true,legalHoldReason:'Preserve evidence under legal request',version:2});
    }
    throw new Error('Unexpected request '+path);
  });
  const rendered=render(<InboundReviewWorkbench />);
  fireEvent.click(await screen.findByRole('button', {name:/Review reply event-one/}));
  fireEvent.change(screen.getByLabelText('Legal hold reason'), {target:{value:'Preserve evidence under legal request'}});
  fireEvent.click(screen.getByRole('button', {name:'Apply legal hold'}));
  await waitFor(()=>expect(calls).toHaveLength(1));
  expect(calls[0].body).toEqual({expectedVersion:1,legalHold:true,reason:'Preserve evidence under legal request'});
  auth.session={id:'operator-one',sessionId:'session-new',permissions:['inbound.read','inbound.review']};
  rendered.rerender(<InboundReviewWorkbench />);
  expect(screen.queryByRole('button', {name:'Apply legal hold'})).toBeNull();
});
