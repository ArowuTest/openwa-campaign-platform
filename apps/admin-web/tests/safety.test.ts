import test from 'node:test';
import assert from 'node:assert/strict';

import { safeReturnPath, hasPermission } from '../lib/session.ts';
import { collectBoundedPages, csrfTokenFromCookie, requestWithFetch } from '../lib/api.ts';

test('safeReturnPath accepts only local absolute paths', () => {
  assert.equal(safeReturnPath('/campaigns/abc?tab=message'), '/campaigns/abc?tab=message');
  for (const value of [
    'https://evil.example',
    '//evil.example/path',
    '/%2f%2fevil.example',
    '/\\\\evil.example',
    '/campaigns\\evil',
    '/%5cevil.example',
    '/%0aLocation:%20https://evil.example',
    ''
  ]) {
    assert.equal(safeReturnPath(value), '/');
  }
});

test('hasPermission fails closed and honours wildcard explicitly', () => {
  assert.equal(hasPermission(undefined, 'campaign.write'), false);
  assert.equal(hasPermission([], 'campaign.write'), false);
  assert.equal(hasPermission(['campaign.read'], 'campaign.write'), false);
  assert.equal(hasPermission(['campaign.write'], 'campaign.write'), true);
  assert.equal(hasPermission(['*'], 'campaign.write'), true);
});

test('collectBoundedPages follows valid cursors but refuses malformed/infinite inventory', async () => {
  const pages = new Map<string, unknown>([
    ['', { items: [{ id: '1' }], count: 1, hasMore: true, nextCursor: 'c2' }],
    ['c2', { items: [{ id: '2' }], count: 1, hasMore: false }]
  ]);
  const items = await collectBoundedPages<{id:string}>(async (cursor) => pages.get(cursor ?? '') as never, 10);
  assert.deepEqual(items.map((item) => item.id), ['1', '2']);

  await assert.rejects(
    collectBoundedPages(async () => ({ items: [], count: 0, hasMore: true } as never), 10),
    /continuation cursor/i
  );

  await assert.rejects(
    collectBoundedPages(async () => ({ items: [], count: 0, hasMore: true, nextCursor: 'same' } as never), 2),
    /page limit|repeated cursor/i
  );
});

test('csrfTokenFromCookie reads only the dedicated browser CSRF cookie', () => {
  assert.equal(csrfTokenFromCookie('theme=dark; campaign_csrf=abc%2D123; other=x'), 'abc-123');
  assert.equal(csrfTokenFromCookie('campaign_session=secret; other=x'), undefined);
  assert.equal(csrfTokenFromCookie('campaign_csrf=%E0%A4%A'), undefined);
});

test('requestWithFetch never retries unsafe mutations', async () => {
  let attempts = 0;
  const fakeFetch: typeof fetch = async () => {
    attempts += 1;
    throw new TypeError('network down');
  };
  await assert.rejects(
    requestWithFetch('/api/v1/campaigns', { method: 'POST', body: '{}' }, fakeFetch),
    /network down/
  );
  assert.equal(attempts, 1);
});

test('requestWithFetch may retry one safe read after a transient network failure', async () => {
  let attempts = 0;
  const fakeFetch: typeof fetch = async () => {
    attempts += 1;
    if (attempts === 1) throw new TypeError('network down');
    return new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' }
    });
  };
  const response = await requestWithFetch('/api/v1/auth/me', { method: 'GET' }, fakeFetch);
  assert.equal(response.status, 200);
  assert.equal(attempts, 2);
});
