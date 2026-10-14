import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';

import { ConsentEvidencePanel } from '../app/consent-reviews/consent-evidence-panel';

const auth = vi.hoisted(() => ({
  session: { id:'reviewer-one',sessionId:'session-one',permissions:['consent.read','consent.review'] }
}));
vi.mock('../components/auth-provider',()=>({useAuth:()=>({session:auth.session})}));

const digest='0123456789abcdef'.repeat(4);
const clean={
  id:'asset-1',purpose:'CONSENT_EVIDENCE',originalFilename:'consent-provenance.pdf',
  mediaType:'application/pdf',size:3041,sha256:digest,status:'CLEAN',version:3,
  createdAt:'2026-10-01T12:00:00Z',updatedAt:'2026-10-02T12:00:00Z'
};
const panel=(assets=['asset-1'], external:{reference:string;sha256Checksum:string}[] = [])=>
  <ConsentEvidencePanel reviewId="review-1" version={4} assetIds={assets} externalReferences={external} />;

function response(data:unknown,status=200) {
  return new Response(JSON.stringify(data),{status,headers:{'content-type':'application/json'}});
}
afterEach(()=>{
  cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();
  auth.session={id:'reviewer-one',sessionId:'session-one',permissions:['consent.read','consent.review']};
});

test('trusted asset details require explicit inspection and never retrieve file bytes',async()=>{
  const paths:string[]=[];
  vi.stubGlobal('fetch',async(input:RequestInfo|URL)=>{
    const path=String(input).replace('/api','');paths.push(path);
    if(path==='/v1/assets/asset-1')return response(clean);
    throw new Error('Unexpected '+path);
  });
  render(panel());
  expect(screen.getByRole('heading',{name:'Consent evidence verification'})).toBeTruthy();
  expect(paths).toHaveLength(0);
  expect(screen.queryByText('consent-provenance.pdf')).toBeNull();
  fireEvent.click(screen.getByRole('button',{name:'Inspect metadata for asset-1'}));
  expect(await screen.findByText('consent-provenance.pdf')).toBeTruthy();
  expect(paths).toEqual(['/v1/assets/asset-1']);
  expect(screen.getByText(digest)).toBeTruthy();
  expect(screen.getByText(/Clean scan status alone does not approve consent/)).toBeTruthy();
  expect(screen.queryByRole('link',{name:/Download/i})).toBeNull();
  expect(screen.queryByRole('button',{name:/Preview file/i})).toBeNull();
});

test('unsafe, revoked, missing-purpose and malformed trusted asset responses fail closed',async()=>{
  vi.stubGlobal('fetch',async(input:RequestInfo|URL)=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/assets/asset-1')return response({...clean,status:'REVOKED'});
    if(path==='/v1/assets/asset-2')return response({...clean,id:'asset-2',purpose:'CAMPAIGN_EVIDENCE'});
    if(path==='/v1/assets/asset-3')return response({...clean,id:'asset-3',sha256:'invalid'});
    throw new Error('Unexpected '+path);
  });
  render(panel(['asset-1','asset-2','asset-3']));
  const requests=['asset-1','asset-2','asset-3'];
  for(const id of requests)fireEvent.click(screen.getByRole('button',{name:'Inspect metadata for '+id}));
  expect(await screen.findByText(/Revoked evidence cannot be used to establish consent/)).toBeTruthy();
  expect(await screen.findByText(/Asset purpose does not match consent evidence/)).toBeTruthy();
  expect(await screen.findByText(/Invalid SHA-256 metadata/)).toBeTruthy();
  expect(screen.queryByText('All evidence verified')).toBeNull();
});

test('external references are unverified checksum claims and are not fetched or turned into links',()=>{
  const fetchMock=vi.fn();
  vi.stubGlobal('fetch',fetchMock);
  render(panel([],[
    {reference:'https://external.example/proof?a=1',sha256Checksum:digest},
    {reference:'missing-document',sha256Checksum:'bad-hash'}
  ]));
  expect(screen.getByText('https://external.example/proof?a=1')).toBeTruthy();
  expect(screen.getByText(digest)).toBeTruthy();
  expect(screen.getByText(/External checksum has not been independently validated/)).toBeTruthy();
  expect(screen.getByText(/Invalid external SHA-256 reference/)).toBeTruthy();
  expect(screen.queryByRole('link',{name:'https://external.example/proof?a=1'})).toBeNull();
  expect(fetchMock).not.toHaveBeenCalled();
});

test('permission loss and operator replacement discard sensitive metadata and stale responses',async()=>{
  let resolveFirst:((response:Response)=>void)|undefined;
  let calls=0;
  vi.stubGlobal('fetch',async(input:RequestInfo|URL)=>{
    const path=String(input).replace('/api','');
    if(path==='/v1/assets/asset-1'){
      calls++;
      if(calls===1)return new Promise<Response>((resolve)=>{resolveFirst=resolve;});
      return response({...clean,originalFilename:'new-operator-file.pdf'});
    }
    throw new Error('Unexpected '+path);
  });
  const view=render(panel());
  fireEvent.click(screen.getByRole('button',{name:'Inspect metadata for asset-1'}));
  await waitFor(()=>expect(resolveFirst).toBeTruthy());
  auth.session={id:'reviewer-two',sessionId:'session-two',permissions:['consent.read']};
  view.rerender(panel());
  expect(screen.queryByText('consent-provenance.pdf')).toBeNull();
  await act(async()=>{resolveFirst?.(response(clean));await Promise.resolve();});
  expect(screen.queryByText('consent-provenance.pdf')).toBeNull();
  fireEvent.click(screen.getByRole('button',{name:'Inspect metadata for asset-1'}));
  expect(await screen.findByText('new-operator-file.pdf')).toBeTruthy();
  auth.session={id:'reviewer-two',sessionId:'session-two',permissions:[]};
  view.rerender(panel());
  expect(screen.getByText('Consent evidence requires consent.read permission.')).toBeTruthy();
  expect(screen.queryByText('new-operator-file.pdf')).toBeNull();
});

test('large evidence register is visibly bounded with no silent completeness claim',()=>{
  const fetchMock=vi.fn();
  vi.stubGlobal('fetch',fetchMock);
  const ids=Array.from({length:125},(_,i)=>'asset-'+(i+1));
  render(panel(ids));
  expect(screen.getByText(/Only the first 100 asset references can be inspected/)).toBeTruthy();
  expect(screen.getAllByRole('button',{name:/Inspect metadata for/})).toHaveLength(20);
  fireEvent.click(screen.getByRole('button',{name:'Show 20 more asset references'}));
  expect(screen.getAllByRole('button',{name:/Inspect metadata for/})).toHaveLength(40);
  expect(fetchMock).not.toHaveBeenCalled();
});
