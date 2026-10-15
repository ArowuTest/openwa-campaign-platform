import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { ConsentReviewManager } from '../app/consent-reviews/review-manager';

const auth = vi.hoisted(() => ({ session: { id: 'maker', sessionId: 'session-a', permissions: ['consent.read', 'consent.write', 'consent.review'] } }));
vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); auth.session = { id:'maker', sessionId:'session-a', permissions:['consent.read','consent.write','consent.review'] }; });

function review(overrides: Record<string, unknown> = {}) {
  return { id: 'review-1', organisationId: 'org-1', scope: 'SOURCE', sourceSystem: 'registration-form',
    name: 'Festival consent', purposeDescription: 'Event promotions', purposeCode: 'PROMOTIONAL_MESSAGING',
    channel: 'WHATSAPP', consentSource: 'website', collectionMethod: 'explicit-checkbox',
    controllerRole: 'data-controller', wordingVersion: 'wording-v3', exactConsentWording: 'I agree to receive event promotions.',
    privacyNoticeVersion: 'privacy-v2', evidenceAssetIds: ['asset-1'], externalEvidenceReferences: [],
    privacyNoticeReviewed: true, optOutProcessReviewed: true, sampleRecordsReviewed: true, sampleReviewNotes: 'Checked a minimised sample.',
    permittedCountries: ['NG'], permittedMessageCategory: 'event-promotions', restrictions: 'Named event series.',
    status: 'DRAFT', outcome: 'PENDING', createdAt: '2026-10-01T00:00:00.000Z', updatedAt: '2026-10-01T00:00:00.000Z',
    version: 1, ...overrides };
}

test('creates aligned draft and submits with current version', async () => {
  let current = review();
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, init });
    if (path.startsWith('/v1/consent-reviews?')) return new Response(JSON.stringify({ items: [current], count: 1, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    if (path === '/v1/consent-reviews' && init.method === 'POST') {
      current = review(JSON.parse(String(init.body)));
      return new Response(JSON.stringify(current), { status: 201, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/consent-reviews/review-1/submit' && init.method === 'POST') {
      const body = JSON.parse(String(init.body));
      current = review({ ...current, status: 'PENDING_REVIEW', submittedBy: 'maker', version: body.expectedVersion + 1 });
      return new Response(JSON.stringify(current), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });
  render(<ConsentReviewManager />);
  await screen.findByRole('heading', { name: 'Record consent assessment' });
  fireEvent.change(screen.getByLabelText('Review scope'), { target: { value: 'SOURCE' } });
  fireEvent.change(screen.getByLabelText('Source system'), { target: { value: 'registration-form' } });
  const values = [['Organisation ID', 'org-1'], ['Review name', 'Festival consent'], ['Permitted purpose description', 'Event promotions'],
    ['Purpose code', 'PROMOTIONAL_MESSAGING'], ['Consent collection source', 'website'], ['Collection method', 'explicit-checkbox'],
    ['Controller role', 'data-controller'], ['Consent wording version', 'wording-v3'], ['Exact consent wording', 'I agree to receive event promotions.'],
    ['Privacy notice version', 'privacy-v2'], ['Permitted message category', 'event-promotions'], ['Sample review notes', 'Checked a minimised sample.'],
    ['Restrictions', 'Named event series.']] as const;
  for (const [label, value] of values) fireEvent.change(screen.getByLabelText(label), { target: { value } });
  fireEvent.change(screen.getByPlaceholderText('UUID per line'), { target: { value: 'asset-1' } });
  for (const label of ['Nigeria', 'Privacy notice reviewed', 'Opt-out process reviewed', 'Sample consent records reviewed']) fireEvent.click(screen.getByLabelText(label));
  fireEvent.submit(screen.getByRole('button', { name: 'Create draft review' }).closest('form')!);
  await waitFor(() => expect(calls.some((call) => call.path === '/v1/consent-reviews' && call.init.method === 'POST')).toBe(true));
  const body = JSON.parse(String(calls.find((call) => call.path === '/v1/consent-reviews' && call.init.method === 'POST')?.init.body));
  expect(body).toMatchObject({ scope: 'SOURCE', purposeCode: 'PROMOTIONAL_MESSAGING', collectionMethod: 'explicit-checkbox',
    controllerRole: 'data-controller', wordingVersion: 'wording-v3', privacyNoticeVersion: 'privacy-v2', evidenceAssetIds: ['asset-1'], permittedCountries: ['NG'] });
  fireEvent.change(screen.getByLabelText('Submission reason'), { target: { value: 'Evidence checked' } });
  fireEvent.click(screen.getByRole('button', { name: 'Submit for checker review' }));
  await screen.findByText('Review submitted. An independent checker must make the decision.');
  const submitBody = JSON.parse(String(calls.find((call) => call.path.endsWith('/submit'))?.init.body));
  expect(submitBody).toMatchObject({ expectedVersion: 1, reason: 'Evidence checked' });
});


test('revises an incomplete draft by creating a superseding governed draft', async () => {
  let current = review();
  const calls: Array<{ path: string; init: RequestInit }> = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).replace('/api', '');
    calls.push({ path, init });
    if (path.startsWith('/v1/consent-reviews?')) {
      return new Response(JSON.stringify({ items: [current], count: 1, hasMore: false }), {
        status: 200, headers: { 'content-type': 'application/json' }
      });
    }
    if (path === '/v1/consent-reviews/review-1' && (!init.method || init.method === 'GET')) {
      return new Response(JSON.stringify(current), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/consent-reviews/review-1/revisions' && init.method === 'POST') {
      const body = JSON.parse(String(init.body));
      current = review({
        ...body.review,
        id: 'review-2',
        parentReviewId: 'review-1',
        organisationId: 'org-1',
        status: 'DRAFT',
        outcome: 'PENDING',
        version: 1,
        createdAt: '2026-10-09T18:00:00.000Z',
        updatedAt: '2026-10-09T18:00:00.000Z'
      });
      return new Response(JSON.stringify(current), { status: 201, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path + ' ' + String(init.method));
  });

  render(<ConsentReviewManager />);
  await screen.findByRole('heading', { name: 'Record consent assessment' });
  fireEvent.click(await screen.findByRole('button', { name: 'Festival consent' }));
  await screen.findByRole('button', { name: 'Revise draft evidence' });
  fireEvent.click(screen.getByRole('button', { name: 'Revise draft evidence' }));

  const revisionHeading = await screen.findByRole('heading', { name: 'Revise consent assessment' });
  const form = revisionHeading.closest('form');
  expect(form).not.toBeNull();
  const revisionForm = within(form as HTMLFormElement);
  expect((revisionForm.getByLabelText('Organisation ID') as HTMLInputElement).value).toBe('org-1');
  expect((revisionForm.getByLabelText('Consent wording version') as HTMLInputElement).value).toBe('wording-v3');
  expect((revisionForm.getByLabelText('Privacy notice reviewed') as HTMLInputElement).checked).toBe(true);
  expect((revisionForm.getByLabelText('Nigeria') as HTMLInputElement).checked).toBe(true);

  fireEvent.change(revisionForm.getByLabelText('Review name'), { target: { value: 'Festival consent corrected' } });
  fireEvent.change(revisionForm.getByLabelText('Consent wording version'), { target: { value: 'wording-v4' } });
  fireEvent.change(revisionForm.getByLabelText('Revision reason'), { target: { value: 'Correct incomplete draft evidence' } });
  fireEvent.click(revisionForm.getByRole('button', { name: 'Create corrected draft' }));

  await screen.findByText('Corrected draft created. The previous draft is preserved as superseded evidence.');
  const revisionCall = calls.find((call) => call.path === '/v1/consent-reviews/review-1/revisions');
  expect(revisionCall).toBeTruthy();
  expect(JSON.parse(String(revisionCall?.init.body))).toMatchObject({
    expectedVersion: 1,
    reason: 'Correct incomplete draft evidence',
    review: {
      organisationId: 'org-1',
      scope: 'SOURCE',
      name: 'Festival consent corrected',
      wordingVersion: 'wording-v4',
      evidenceAssetIds: ['asset-1'],
      permittedCountries: ['NG']
    }
  });
});

test('an approval-queue consent deep link opens its exact governed review without scanning for it', async () => {
  const calls: string[] = [];
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    calls.push(path);
    if (path === '/v1/consent-reviews?limit=100') {
      return new Response(JSON.stringify({ items: [], count: 0, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path === '/v1/consent-reviews/review-1') {
      return new Response(JSON.stringify(review({ status: 'PENDING_REVIEW', submittedBy: 'different-maker' })), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected path ' + path);
  });
  render(<ConsentReviewManager initialReviewId="review-1" />);
  expect(await screen.findByText('Independent checker decision')).toBeTruthy();
  expect(calls).toContain('/v1/consent-reviews/review-1');
  expect(screen.getByRole('button', { name: 'Record approval' })).toBeTruthy();
});

test('consent deep links and privileged content are reset when the operator or route changes', async () => {
  let details = 0;
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    if (path === '/v1/consent-reviews?limit=100') {
      return new Response(JSON.stringify({ items: [], count: 0, hasMore: false }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    if (path.startsWith('/v1/consent-reviews/review-')) {
      details++;
      const id = path.split('/').at(-1);
      return new Response(JSON.stringify(review({ id, name: id === 'review-2' ? 'Other consent evidence' : 'Old operator consent evidence', status: 'PENDING_REVIEW' })), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected path ' + path);
  });
  const view = render(<ConsentReviewManager initialReviewId="review-1" />);
  expect(await screen.findByText(/Old operator consent evidence/)).toBeTruthy();
  view.rerender(<ConsentReviewManager initialReviewId="review-2" />);
  expect(screen.queryByText(/Old operator consent evidence/)).toBeNull();
  expect(await screen.findByText(/Other consent evidence/)).toBeTruthy();
  expect(details).toBe(2);
  auth.session = { id: 'other-operator', sessionId: 'other-session', permissions: [] };
  view.rerender(<ConsentReviewManager initialReviewId="review-2" />);
  expect(screen.getByText('Consent review requires consent.read permission.')).toBeTruthy();
  expect(screen.queryByText(/Other consent evidence/)).toBeNull();
  expect(details).toBe(2);
});

test('selected consent review opens governed trusted-asset metadata inspection without downloading evidence', async () => {
  const calls: string[] = [];
  const digest = 'abcdef0123456789'.repeat(4);
  vi.stubGlobal('fetch', async (input: RequestInfo | URL) => {
    const path = String(input).replace('/api', '');
    calls.push(path);
    if (path === '/v1/consent-reviews?limit=100') {
      return new Response(JSON.stringify({ items: [review()], count: 1, hasMore: false }), {
        status: 200, headers: { 'content-type': 'application/json' }
      });
    }
    if (path === '/v1/consent-reviews/review-1') {
      return new Response(JSON.stringify(review()), {
        status: 200, headers: { 'content-type': 'application/json' }
      });
    }
    if (path === '/v1/assets/asset-1') {
      return new Response(JSON.stringify({
        id: 'asset-1', purpose: 'CONSENT_EVIDENCE', originalFilename: 'signed-consent.png',
        mediaType: 'image/png', size: 512, sha256: digest,
        status: 'CLEAN', version: 2, createdAt: '2026-10-01T00:00:00Z', updatedAt: '2026-10-02T00:00:00Z'
      }), { status: 200, headers: { 'content-type': 'application/json' } });
    }
    throw new Error('Unexpected request: ' + path);
  });

  render(<ConsentReviewManager />);
  fireEvent.click(await screen.findByRole('button', { name: 'Festival consent' }));
  expect(await screen.findByRole('heading', { name: 'Consent evidence verification' })).toBeTruthy();
  expect(calls.some((path) => path.startsWith('/v1/assets/'))).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Inspect metadata for asset-1' }));
  expect(await screen.findByText('signed-consent.png')).toBeTruthy();
  expect(calls.filter((path) => path === '/v1/assets/asset-1')).toHaveLength(1);
  expect(screen.getByText(/Clean scan status alone does not approve consent/)).toBeTruthy();
});
