import { afterEach, expect, test, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { ConsentReviewManager } from '../app/consent-reviews/review-manager';

const auth = vi.hoisted(() => ({ session: { id: 'maker', sessionId: 'session-a', permissions: ['consent.read', 'consent.write', 'consent.review'] } }));
vi.mock('../components/auth-provider', () => ({ useAuth: () => auth }));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

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
