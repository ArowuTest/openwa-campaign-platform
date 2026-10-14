import { cleanup, fireEvent, render, screen, waitFor, within, act } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { AlertCentre } from '../app/operations/alerts/alert-centre';

const nav = vi.hoisted(() => ({ push: vi.fn() }));
const auth = vi.hoisted(() => ({
  session: { id: 'operator-1', sessionId: 'session-1', permissions: ['operations.read', 'operations.write'] }
}));
vi.mock('next/navigation', () => ({ useRouter: () => nav }));
vi.mock('../components/auth-provider', () => ({ useAuth: () => ({ session: auth.session }) }));

const alert = {
  id: 'alert-1', policyId: 'policy-1', status: 'ACTIVE',
  severity: 'CRITICAL', observedValue: 77, threshold: 50,
  occurrenceCount: 3, escalationIndex: 1, version: 4,
  firstTriggeredAt: '2026-10-13T09:00:00Z',
  lastObservedAt: '2026-10-13T10:00:00Z',
  updatedAt: '2026-10-13T10:00:00Z',
  linkedIncidentId: 'incident-1'
};

function ok(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  nav.push.mockReset();
  auth.session = { id: 'operator-1', sessionId: 'session-1', permissions: ['operations.read', 'operations.write'] };
});

test('loads an active, server-filtered alert register and continues bounded cursor pages', async () => {
  const paths: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    paths.push(path);
    if (path === '/v1/operations/alerts?limit=50&status=ACTIVE')
      return ok({ items: [alert], count: 1, hasMore: true, nextCursor: 'older-cursor' });
    if (path === '/v1/operations/alerts?limit=50&status=ACTIVE&cursor=older-cursor')
      return ok({ items: [{...alert, id:'alert-2',version:1}], count:1, hasMore:false });
    if (path === '/v1/operations/alerts?limit=50&status=ACTIVE&severity=WARNING')
      return ok({ items: [], count:0, hasMore:false });
    throw new Error('Unexpected GET ' + path);
  });
  render(<AlertCentre />);
  expect(await screen.findByRole('button', {name:/Inspect alert alert-1/})).toBeTruthy();
  expect(screen.getByText(/Only the loaded records are counted/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', {name:'Load older alerts'}));
  expect(await screen.findByRole('button', {name:/Inspect alert alert-2/})).toBeTruthy();
  expect(paths).toContain('/v1/operations/alerts?limit=50&status=ACTIVE&cursor=older-cursor');
  fireEvent.change(screen.getByLabelText('Severity'), {target:{value:'WARNING'}});
  expect(await screen.findByText('No alerts matched the selected server filters')).toBeTruthy();
});

test('selecting an alert loads bounded audit events but never exposes raw event evidence blobs', async () => {
  const paths: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path=String(input).replace('/api','');
    paths.push(path);
    if(path.startsWith('/v1/operations/alerts?'))return ok({items:[alert],count:1,hasMore:false});
    if(path==='/v1/operations/alerts/alert-1/events?limit=50') return ok({
      items:[{id:'event-1',alertId:'alert-1',eventType:'ALERT_TRIGGERED',detail:'Queue breached threshold',actorId:'user-secret-ref',evidence:{token:'PRIVATE_EVENT_EVIDENCE'},occurredAt:'2026-10-13T09:00:00Z'}],
      count:1,hasMore:false
    });
    throw new Error('Unexpected GET '+path);
  });
  render(<AlertCentre />);
  expect(paths.some(x=>x.includes('/events'))).toBe(false);
  fireEvent.click(await screen.findByRole('button',{name:/Inspect alert alert-1/}));
  expect(await screen.findByText('ALERT_TRIGGERED')).toBeTruthy();
  expect(screen.getByText('Queue breached threshold')).toBeTruthy();
  expect(screen.queryByText('PRIVATE_EVENT_EVIDENCE')).toBeNull();
});

test('acknowledgement is explicitly reasoned, versioned and never misrepresented as resolution', async () => {
  const writes: Array<{path:string;body:unknown}> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api','');
    if(path.startsWith('/v1/operations/alerts?'))return ok({items:[alert],count:1,hasMore:false});
    if(path.endsWith('/events?limit=50'))return ok({items:[],count:0,hasMore:false});
    if(path==='/v1/operations/alerts/alert-1/acknowledge' && init.method==='POST'){
      writes.push({path,body:JSON.parse(String(init.body))});
      return ok({...alert,status:'ACKNOWLEDGED',version:5,acknowledgedAt:'2026-10-13T11:00:00Z'});
    }
    throw new Error('Unexpected '+path);
  });
  render(<AlertCentre />);
  fireEvent.click(await screen.findByRole('button',{name:/Inspect alert alert-1/}));
  const form=screen.getByRole('heading',{name:'Acknowledge active alert'}).closest('form');
  const view=within(form as HTMLElement);
  expect((view.getByRole('button',{name:'Acknowledge alert'}) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(view.getByLabelText('Acknowledgement reason'),{target:{value:'Investigating queue pressure with on-call engineer'}});
  fireEvent.click(view.getByRole('button',{name:'Acknowledge alert'}));
  await waitFor(()=>expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({path:'/v1/operations/alerts/alert-1/acknowledge',body:{
    expectedVersion:4,reason:'Investigating queue pressure with on-call engineer'
  }});
  expect(await screen.findByText(/Acknowledged successfully; this does not resolve the underlying condition/)).toBeTruthy();
});

test('optimistic version conflict fails closed without retrying acknowledgement', async () => {
  let writes=0;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path=String(input).replace('/api','');
    if(path.startsWith('/v1/operations/alerts?'))return ok({items:[alert],count:1,hasMore:false});
    if(path.endsWith('/events?limit=50'))return ok({items:[],count:0,hasMore:false});
    if(path.endsWith('/acknowledge')&&init.method==='POST'){
      writes++;
      return ok({code:'ALERT_CONFLICT',message:'Updated elsewhere'},409);
    }
    throw new Error('Unexpected '+path);
  });
  render(<AlertCentre />);
  fireEvent.click(await screen.findByRole('button',{name:/Inspect alert alert-1/}));
  const form=screen.getByRole('heading',{name:'Acknowledge active alert'}).closest('form');
  const view=within(form as HTMLElement);
  fireEvent.change(view.getByLabelText('Acknowledgement reason'),{target:{value:'Investigating this critical condition'}});
  fireEvent.click(view.getByRole('button',{name:'Acknowledge alert'}));
  expect(await screen.findByText(/changed since it was loaded/)).toBeTruthy();
  expect((view.getByRole('button',{name:'Acknowledge alert'}) as HTMLButtonElement).disabled).toBe(true);
  expect(writes).toBe(1);
});

test('permission loss and operator switch discard selected alerts and late private operational evidence', async () => {
  let completeEvents: ((v:Response)=>void) | undefined;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path=String(input).replace('/api','');
    if(path.startsWith('/v1/operations/alerts?'))return ok({items:[alert],count:1,hasMore:false});
    if(path.endsWith('/events?limit=50'))return new Promise<Response>((resolve)=>{completeEvents=resolve;});
    throw new Error('Unexpected '+path);
  });
  const view=render(<AlertCentre />);
  fireEvent.click(await screen.findByRole('button',{name:/Inspect alert alert-1/}));
  await waitFor(()=>expect(completeEvents).toBeTruthy());
  auth.session={id:'different-operator',sessionId:'different-session',permissions:['operations.read']};
  view.rerender(<AlertCentre />);
  await act(async ()=>{
    completeEvents?.(ok({items:[{id:'event-1',eventType:'PRIVATE_OLD_SESSION',detail:'PRIVATE_OLD_SESSION',occurredAt:'2026-10-13T09:00:00Z'}],count:1,hasMore:false}));
    await Promise.resolve();
  });
  expect(screen.queryByText('PRIVATE_OLD_SESSION')).toBeNull();
  auth.session={id:'different-operator',sessionId:'different-session',permissions:[]};
  view.rerender(<AlertCentre />);
  expect(screen.getByText('Alert Centre requires operations.read permission.')).toBeTruthy();
  expect(screen.queryByRole('button',{name:/Inspect alert/})).toBeNull();
});
