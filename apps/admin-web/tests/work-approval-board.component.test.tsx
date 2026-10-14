import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { WorkApprovalBoard } from '../app/work/work-approval-board';

const auth = vi.hoisted(() => ({
  session: {
    id: 'checker-one',
    sessionId: 'session-1',
    permissions: ['campaign.read','campaign.approve','consent.read','consent.review','finance.read','finance.approve']
  }
}));
vi.mock('../components/auth-provider', () => ({ useAuth: () => ({ session: auth.session }) }));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  auth.session = { id:'checker-one',sessionId:'session-1',permissions:['campaign.read','campaign.approve','consent.read','consent.review','finance.read','finance.approve'] };
});

function ok(body: unknown, status=200) {
  return new Response(JSON.stringify(body), { status, headers:{ 'content-type':'application/json' } });
}
const cid = '00000000-0000-4000-8000-000000000101';
const consent = {
  id:cid, organisationId:'org-1',name:'Community registration',status:'PENDING_REVIEW',
  version:3,updatedAt:'2026-10-13T08:30:00Z',submittedBy:'maker-1'
};
const finance = {
  id:'finance-record-1',campaignId:'campaign-finance-1',organisationId:'org-2',
  status:'PENDING_APPROVAL',version:2,updatedAt:'2026-10-13T09:00:00Z',submittedBy:'finance-maker-1'
};
const message = {
  id:'campaign-message-1',name:'Community offer',organisationId:'org-1',
  status:'MESSAGE_REVIEW_PENDING',createdBy:'maker-2',version:4,updatedAt:'2026-10-13T09:30:00Z'
};
const final = {
  ...message,id:'campaign-final-1',name:'Final send consent',status:'FINAL_APPROVAL_PENDING',createdBy:'maker-3'
};

test('builds a read-only pending-work queue across four independently governed sources', async () => {
  const requests:string[]=[];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit={}) => {
    const path=String(input).replace('/api','');
    requests.push(path);
    expect((init.method||'GET').toUpperCase()).toBe('GET');
    if(path==='/v1/consent-reviews?limit=50') return ok({items:[consent],count:1,hasMore:false});
    if(path==='/v1/commercial-records?limit=50')return ok({items:[finance],count:1,hasMore:false});
    if(path==='/v1/campaigns?limit=50&status=MESSAGE_REVIEW_PENDING')return ok({items:[message],count:1,hasMore:false});
    if(path==='/v1/campaigns?limit=50&status=FINAL_APPROVAL_PENDING')return ok({items:[final],count:1,hasMore:false});
    throw new Error('Unexpected '+path);
  });

  render(<WorkApprovalBoard />);
  for(const heading of ['Consent awaiting checker review','Commercial evidence awaiting finance approval','Message versions awaiting review','Final campaign approval']) {
    expect(await screen.findByRole('heading',{name:heading})).toBeTruthy();
  }
  expect(await screen.findByText('Community registration')).toBeTruthy();
  expect(await screen.findByText('Community offer')).toBeTruthy();
  expect(await screen.findByText('Final send consent')).toBeTruthy();
  expect(requests).toHaveLength(4);
  expect(screen.getByRole('link',{name:/Open consent review.*Community registration/}).getAttribute('href')).toBe('/consent-reviews?reviewId='+cid);
  expect(screen.getByRole('link',{name:/Open commercial review.*finance-record-1/}).getAttribute('href')).toBe('/work/finance/campaign-finance-1');
  expect(screen.getByRole('link',{name:/Open message review.*Community offer/}).getAttribute('href')).toBe('/campaigns/campaign-message-1');
  expect(screen.getByRole('link',{name:/Open final approval.*Final send consent/}).getAttribute('href')).toBe('/campaigns/campaign-final-1');
  expect(screen.getByText(/No approval decisions are taken on this page/)).toBeTruthy();
});

test('an operator with only consent permissions never calls commercial or campaign APIs', async () => {
  auth.session={id:'consent-only',sessionId:'session-a',permissions:['consent.read']};
  const paths:string[]=[];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path=String(input).replace('/api','');paths.push(path);
    if(path==='/v1/consent-reviews?limit=50')return ok({items:[consent],count:1,hasMore:false});
    throw new Error('Unauthorized or unexpected '+path);
  });
  render(<WorkApprovalBoard />);
  expect(await screen.findByText('Community registration')).toBeTruthy();
  expect(paths).toEqual(['/v1/consent-reviews?limit=50']);
  expect(screen.queryByRole('heading',{name:'Commercial evidence awaiting finance approval'})).toBeNull();
  expect(screen.queryByRole('heading',{name:'Final campaign approval'})).toBeNull();
});

test('own submitted work is not advertised as independently approvable', async () => {
  auth.session={id:'maker-1',sessionId:'session-a',permissions:['consent.read','consent.review']};
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path=String(input).replace('/api','');
    if(path==='/v1/consent-reviews?limit=50')return ok({items:[consent],count:1,hasMore:false});
    throw new Error('Unexpected '+path);
  });
  render(<WorkApprovalBoard />);
  expect(await screen.findByText('Community registration')).toBeTruthy();
  expect(screen.getByText('Submitted by you — independent checker required')).toBeTruthy();
});

test('uses bounded cursor continuation, and never mistakes a partial work queue for the complete backlog', async () => {
  let pages=0;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path=String(input).replace('/api','');
    if(path==='/v1/consent-reviews?limit=50'){
      pages++;
      return ok({items:[consent],count:1,hasMore:true,nextCursor:'cursor-1'});
    }
    if(path==='/v1/consent-reviews?limit=50&cursor=cursor-1'){
      pages++;
      return ok({items:[{...consent,id:'consent-2',name:'Other event consent'}],count:1,hasMore:false});
    }
    throw new Error('Unexpected '+path);
  });
  auth.session={id:'checker-one',sessionId:'session-a',permissions:['consent.read','consent.review']};
  render(<WorkApprovalBoard />);
  expect(await screen.findByText('Community registration')).toBeTruthy();
  expect(screen.getByText(/Loaded results are not the complete backlog/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button',{name:'Load more consent reviews'}));
  expect(await screen.findByText('Other event consent')).toBeTruthy();
  expect(pages).toBe(2);
});

test('session change discards previous operator backlog even if an old read resolves late', async () => {
  let release:((response:Response)=>void)|undefined;
  let calls=0;
  auth.session={id:'first-reviewer',sessionId:'one',permissions:['consent.read']};
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path=String(input).replace('/api','');
    if(path!=='/v1/consent-reviews?limit=50')throw new Error('Unexpected '+path);
    calls++;
    if(calls===1)return new Promise<Response>((resolve)=>{release=resolve;});
    return ok({items:[],count:0,hasMore:false});
  });
  const view=render(<WorkApprovalBoard />);
  await waitFor(()=>expect(calls).toBe(1));
  auth.session={id:'new-reviewer',sessionId:'two',permissions:['consent.read']};
  view.rerender(<WorkApprovalBoard />);
  release?.(ok({items:[consent],count:1,hasMore:false}));
  expect(await screen.findByText('No pending consent work on this page')).toBeTruthy();
  expect(screen.queryByText('Community registration')).toBeNull();
});
