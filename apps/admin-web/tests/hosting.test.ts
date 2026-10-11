import assert from 'node:assert/strict';
import test from 'node:test';

import {
  buildControlAPIURL,
  proxyControlAPI,
  resolveControlAPIBase
} from '../lib/control-api-proxy.ts';

const productionEnv = {
  NODE_ENV: 'production',
  ADMIN_WEB_CONTROL_API_URL: 'http://control-api.railway.internal:8080'
};

function railwayHeaders(extra: Record<string, string> = {}) {
  return {
    'x-railway-edge': 'ams1',
    'x-real-ip': '203.0.113.200',
    ...extra
  };
}

test('production control API upstream must be an explicit Railway-private origin', () => {
  assert.throws(() => resolveControlAPIBase({ NODE_ENV: 'production' }), /ADMIN_WEB_CONTROL_API_URL/);
  assert.throws(
    () => resolveControlAPIBase({ NODE_ENV: 'production', ADMIN_WEB_CONTROL_API_URL: 'https://control.example.com' }),
    /railway\.internal/
  );
  assert.throws(
    () => resolveControlAPIBase({ NODE_ENV: 'production', ADMIN_WEB_CONTROL_API_URL: 'http://user:pass@control-api.railway.internal:8080' }),
    /credentials/
  );
  assert.throws(
    () => resolveControlAPIBase({ NODE_ENV: 'production', ADMIN_WEB_CONTROL_API_URL: 'http://control-api.railway.internal:8080/base' }),
    /origin/
  );
  assert.equal(resolveControlAPIBase(productionEnv).toString(), 'http://control-api.railway.internal:8080/');
});

test('development upstream defaults only to loopback', () => {
  assert.equal(resolveControlAPIBase({ NODE_ENV: 'development' }).toString(), 'http://127.0.0.1:8080/');
});

test('proxy URL encodes route segments and preserves query parameters', () => {
  const target = buildControlAPIURL(
    'https://operator.example/api/campaigns/a?view=full&limit=20',
    ['v1', 'campaigns', 'a/b'],
    productionEnv
  );
  assert.equal(
    target.toString(),
    'http://control-api.railway.internal:8080/api/v1/campaigns/a%2Fb?view=full&limit=20'
  );
});

test('production proxy fails closed when Railway client network context is absent or malformed', async () => {
  let attempts = 0;
  const invalidHeaders: Array<Record<string, string>> = [
    { 'x-real-ip': '203.0.113.200' },
    { 'x-railway-edge': 'ams1', 'x-real-ip': 'not-an-ip' },
    { 'x-railway-edge': 'ams1', 'x-forwarded-for': '203.0.113.200' }
  ];
  for (const headers of invalidHeaders) {
    const response = await proxyControlAPI(
      new Request('https://operator.example/api/v1/auth/me', { headers }),
      ['v1', 'auth', 'me'],
      async () => {
        attempts += 1;
        return new Response('{}');
      },
      productionEnv
    );
    assert.equal(response.status, 403);
    assert.match(await response.text(), /NETWORK_CONTEXT_UNAVAILABLE/);
  }
  assert.equal(attempts, 0);
});

test('unsafe proxy requests are attempted once and forward only sanitized Railway client context', async () => {
  let attempts = 0;
  let forwardedHeaders: Headers | undefined;
  const request = new Request('https://operator.example/api/v1/campaigns', {
    method: 'POST',
    headers: railwayHeaders({
      host: 'evil.example',
      cookie: 'campaign_session=s; campaign_csrf=c',
      'x-csrf-token': 'c',
      'idempotency-key': 'idem-1',
      'x-forwarded-for': '10.1.2.3, fd12::10',
      'x-forwarded-proto': 'http',
      'content-type': 'application/json'
    }),
    body: JSON.stringify({ name: 'fixture' })
  });

  const response = await proxyControlAPI(
    request,
    ['v1', 'campaigns'],
    async (_url, init) => {
      attempts += 1;
      forwardedHeaders = new Headers(init?.headers);
      throw new TypeError('upstream unavailable');
    },
    productionEnv
  );

  assert.equal(attempts, 1);
  assert.ok(forwardedHeaders);
  assert.equal(forwardedHeaders.get('cookie'), 'campaign_session=s; campaign_csrf=c');
  assert.equal(forwardedHeaders.get('x-csrf-token'), 'c');
  assert.equal(forwardedHeaders.get('idempotency-key'), 'idem-1');
  assert.equal(forwardedHeaders.get('host'), null);
  assert.equal(forwardedHeaders.get('x-railway-edge'), 'ams1');
  assert.equal(forwardedHeaders.get('x-real-ip'), '203.0.113.200');
  assert.equal(forwardedHeaders.get('x-forwarded-for'), null);
  assert.equal(forwardedHeaders.get('x-forwarded-proto'), null);
  assert.equal(response.status, 503);
  assert.match(await response.text(), /CONTROL_API_UNAVAILABLE/);
});

test('proxy preserves backend cookies and status without exposing hop-by-hop headers', async () => {
  const request = new Request('https://operator.example/api/v1/auth/login', {
    method: 'POST',
    headers: railwayHeaders({ 'content-type': 'application/json' }),
    body: JSON.stringify({ email: 'fixture@example.test', password: 'not-a-real-secret' })
  });

  const response = await proxyControlAPI(
    request,
    ['v1', 'auth', 'login'],
    async (_url, init) => {
      const headers = new Headers(init?.headers);
      assert.equal(headers.get('x-real-ip'), '203.0.113.200');
      assert.equal(headers.get('x-forwarded-for'), null);
      assert.equal(headers.get('x-railway-edge'), 'ams1');
      return new Response(JSON.stringify({ ok: true }), {
        status: 201,
        headers: [
          ['content-type', 'application/json'],
          ['connection', 'keep-alive'],
          ['set-cookie', 'campaign_session=abc; HttpOnly; Path=/; SameSite=Strict'],
          ['set-cookie', 'campaign_csrf=xyz; Path=/; SameSite=Strict']
        ]
      });
    },
    productionEnv
  );

  assert.equal(response.status, 201);
  assert.equal(response.headers.get('connection'), null);
  const getSetCookie = (response.headers as Headers & { getSetCookie?: () => string[] }).getSetCookie;
  assert.equal(typeof getSetCookie, 'function');
  assert.equal(getSetCookie?.call(response.headers).length, 2);
});
