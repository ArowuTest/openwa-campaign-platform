#!/usr/bin/env node
const assert = require('node:assert/strict');
// This suite isolates other behavior; real boot ownership is covered separately.
const ownedRuntime = { assertSessionOwned() {}, onSessionOwnershipLost() { return () => {}; } };
const { createHash, createHmac } = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

function loadTypeScript() {
  for (const candidate of ['typescript', path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript')]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for gateway session-heartbeat tests');
}

const nestMock = {
  Injectable: () => target => target,
  Logger: class { warn() {} },
};

function loadModule(relativePath, overrides = {}) {
  const filename = path.join(process.cwd(), 'services/openwa-gateway/src', relativePath);
  const source = fs.readFileSync(filename, 'utf8');
  const ts = loadTypeScript();
  const output = ts.transpileModule(source, {
    fileName: filename,
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
      experimentalDecorators: true,
      esModuleInterop: true,
    },
  }).outputText;
  const module = { exports: {} };
  const localRequire = specifier => {
    if (specifier === '@nestjs/common') return nestMock;
    if (Object.prototype.hasOwnProperty.call(overrides, specifier)) return overrides[specifier];
    if (specifier.startsWith('./')) {
      const candidate = path.resolve(path.dirname(filename), `${specifier}.ts`);
      const sourceRoot = path.join(process.cwd(), 'services/openwa-gateway/src');
      if (fs.existsSync(candidate) && candidate.startsWith(sourceRoot)) {
        return loadModule(path.relative(sourceRoot, candidate), overrides);
      }
    }
    return require(specifier);
  };
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(
    module.exports, localRequire, module, filename, path.dirname(filename),
  );
  return module.exports;
}

function observabilityStub() {
  return { increment() {}, gauge() {}, observe() {} };
}

function embeddedUsageService() {
  const { EmbeddedOpenWAEngineService } = loadModule('provider/embedded-openwa-engine.service.ts', {
    '../gateway-identity.service': { GatewayIdentityService: class {} },
    '../provider-event-publisher.service': { ProviderEventPublisherService: class {} },
    '../inbound-message-publisher.service': { InboundMessagePublisherService: class {} },
    '../observability.service': { GatewayObservabilityService: class {} },
    '../retained-openwa/engine/adapters/whatsapp-web-js.adapter': { WhatsAppWebJsAdapter: class {} },
    '../retained-openwa/engine/adapters/baileys.adapter': { BaileysAdapter: class {} },
    '../retained-openwa/engine/interfaces/whatsapp-engine.interface': {
      EngineStatus: { DISCONNECTED: 'disconnected', READY: 'ready' },
    },
  });
  const service = new EmbeddedOpenWAEngineService({ engine: 'BAILEYS' }, {}, {}, observabilityStub(), ownedRuntime);
  // Seed runtime state only; list/get/synchronization below execute the real implementation.
  service.sessions.set('session-1', {
    persistent: { id: 'session-1', name: 'session-1', engine: 'BAILEYS', schemaVersion: 1 },
    status: 'ready', generation: 0, updatedAt: '2026-09-19T23:59:00.000Z',
    sentToday: 37, sentTodayDate: '2026-09-19',
  });
  return service;
}

async function withUsageClock(initial, run) {
  const OriginalDate = global.Date;
  let now = OriginalDate.parse(initial);
  global.Date = class extends OriginalDate {
    constructor(...args) { super(...(args.length ? args : [now])); }
    static now() { return now; }
  };
  try { await run(iso => { now = OriginalDate.parse(iso); }); }
  finally { global.Date = OriginalDate; }
}

test('idle session rolls UTC usage over before the first new-day heartbeat', async () => {
  await withUsageClock('2026-09-19T23:59:59.000Z', async advance => {
    const service = embeddedUsageService();
    assert.equal((await service.listSessions())[0].sentToday, 37);
    advance('2026-09-20T00:00:01.000Z');
    assert.equal((await service.listSessions())[0].sentToday, 0,
      'yesterday\'s usage must not be published as new-day consumption');
    assert.equal((await service.getSession('session-1')).sentToday, 0);
    service.synchronizeSentToday('session-1', 5);
    service.synchronizeSentToday('session-1', 3);
    assert.equal((await service.listSessions())[0].sentToday, 5,
      'authoritative usage must remain monotonic within the new UTC day');
  });
});

test('session detail also rolls idle UTC usage over without a preceding list', async () => {
  await withUsageClock('2026-09-20T00:00:01.000Z', async () => {
    const service = embeddedUsageService();
    assert.equal((await service.getSession('session-1')).sentToday, 0);
    service.synchronizeSentToday('session-1', 8);
    assert.equal((await service.getSession('session-1')).sentToday, 8);
  });
});

test('late prior-day provider acceptance cannot erase current-day usage', async () => {
  await withUsageClock('2026-09-20T00:00:05.000Z', async () => {
    const service = embeddedUsageService();
    service.synchronizeSentToday('session-1', 17);
    service.publisher = { create: value => value, queue: async () => undefined };
    const timestamps = ['2026-09-19T23:59:59.000Z', '2026-09-20T00:00:04.000Z'];
    let sequence = 0;
    service.sessions.get('session-1').engine = {
      getStatus: () => 'ready',
      sendTextMessage: async () => ({
        id: `usage-message-${++sequence}`, timestamp: Date.parse(timestamps.shift()) / 1000,
      }),
    };
    const request = {
      sessionId: 'session-1', recipientMsisdn: '+2348111111111',
      messageType: 'text', body: 'usage regression', engine: 'BAILEYS', provider: 'OPENWA',
    };
    const late = await service.send(request);
    assert.equal(late.acceptedAt, '2026-09-19T23:59:59.000Z');
    assert.equal((await service.getSession('session-1')).sentToday, 17,
      'late prior-day evidence must not reset the active UTC-day counter');
    await service.send(request);
    assert.equal((await service.getSession('session-1')).sentToday, 18);
  });
});

function usagePublisher(gateway, publishSessionHeartbeat) {
  const { SessionHeartbeatPublisherService } = loadModule('session-heartbeat-publisher.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './gateway-messaging.service': { GatewayMessagingService: class {} },
    './runtime-registration.service': { RuntimeRegistrationService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  return new SessionHeartbeatPublisherService(
    { nodeId: 'node-1', adapterVersion: '0.13.0' }, gateway,
    { isRuntimeRegistered: () => true, bootIdentity: () => 'boot-1', publishSessionHeartbeat },
    observabilityStub(),
  );
}

test('prior-day heartbeat response cannot seed the new UTC day', async () => {
  await withUsageClock('2026-09-19T23:59:59.000Z', async advance => {
    const engine = embeddedUsageService();
    const gateway = {
      listSessions: () => engine.listSessions(),
      health: async () => ({ status: 'READY', pipeline: {} }),
      synchronizeSentToday: (id, value) => engine.synchronizeSentToday(id, value),
    };
    const publisher = usagePublisher(gateway, async () => {
      advance('2026-09-20T00:00:01.000Z');
      engine.synchronizeSentToday('session-1', 2);
      return { sentToday: 37 };
    });
    await publisher.publishNow();
    assert.equal((await engine.getSession('session-1')).sentToday, 2,
      'late yesterday response must not overwrite two accepted sends from today');
  });
});

test('heartbeat defers a usage snapshot that crosses UTC midnight during health lookup', async () => {
  await withUsageClock('2026-09-19T23:59:59.000Z', async advance => {
    const engine = embeddedUsageService();
    const reports = [];
    const gateway = {
      listSessions: () => engine.listSessions(),
      health: async () => {
        advance('2026-09-20T00:00:01.000Z');
        return { status: 'READY', pipeline: {} };
      },
      synchronizeSentToday: (id, value) => engine.synchronizeSentToday(id, value),
    };
    const publisher = usagePublisher(gateway, async (_id, report) => {
      reports.push(report);
      return { sentToday: report.sentToday };
    });
    await publisher.publishNow();
    assert.equal(reports.length, 0, 'do not timestamp yesterday usage as a new-day report');
    await publisher.publishNow();
    assert.equal(reports.length, 1, 'the next scan can publish a fresh new-day snapshot');
    assert.equal(reports[0].sentToday, 0);
    assert.equal((await engine.getSession('session-1')).sentToday, 0);
  });
});

test('session heartbeat publisher binds node boot identity and seeds authoritative usage', async () => {
  const { SessionHeartbeatPublisherService } = loadModule('session-heartbeat-publisher.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './gateway-messaging.service': { GatewayMessagingService: class {} },
    './runtime-registration.service': { RuntimeRegistrationService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const published = [];
  const synced = [];
  const gateway = {
    listSessions: async () => [{ id: 'session-1', sentToday: 7 }],
    health: async () => ({ ready: true, status: 'READY', checkedAt: new Date().toISOString(), pipeline: { draining: false, starting: false, tornDown: false } }),
    synchronizeSentToday: (id, value) => synced.push([id, value]),
  };
  const runtime = {
    isRuntimeRegistered: () => true,
    bootIdentity: () => 'boot-1',
    publishSessionHeartbeat: async (id, report) => {
      published.push([id, report]);
      return { sentToday: 11 };
    },
  };
  const service = new SessionHeartbeatPublisherService(
    { nodeId: 'node-1', adapterVersion: '0.13.0' },
    gateway,
    runtime,
    observabilityStub(),
  );
  await service.publishNow();
  assert.equal(published.length, 1);
  assert.deepEqual(published[0], ['session-1', {
    nodeId: 'node-1',
    sessionId: 'session-1',
    bootId: 'boot-1',
    status: 'READY',
    engineVersion: '0.13.0',
    sentToday: 7,
  }]);
  assert.deepEqual(synced, [['session-1', 11]]);
});

test('session heartbeat publisher reports pipeline recovery states instead of cached engine READY', async () => {
  const { SessionHeartbeatPublisherService } = loadModule('session-heartbeat-publisher.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './gateway-messaging.service': { GatewayMessagingService: class {} },
    './runtime-registration.service': { RuntimeRegistrationService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const statuses = [];
  const gateway = {
    listSessions: async () => [{ id: 'drain-1', sentToday: 1 }, { id: 'start-1', sentToday: 2 }],
    health: async id => ({
      ready: true, status: 'READY', checkedAt: new Date().toISOString(),
      pipeline: id === 'drain-1'
        ? { draining: true, starting: false, tornDown: false }
        : { draining: false, starting: true, tornDown: false },
    }),
    synchronizeSentToday: () => undefined,
  };
  const runtime = {
    isRuntimeRegistered: () => true,
    bootIdentity: () => 'boot-2',
    publishSessionHeartbeat: async (_id, report) => { statuses.push(report.status); return { sentToday: report.sentToday }; },
  };
  const service = new SessionHeartbeatPublisherService(
    { nodeId: 'node-2', adapterVersion: '0.13.0' },
    gateway,
    runtime,
    observabilityStub(),
  );
  await service.publishNow();
  assert.deepEqual(statuses, ['DRAINING', 'CONNECTING']);
});

test('session heartbeat publisher never claims session ownership before node runtime registration succeeds', async () => {
  const { SessionHeartbeatPublisherService } = loadModule('session-heartbeat-publisher.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './gateway-messaging.service': { GatewayMessagingService: class {} },
    './runtime-registration.service': { RuntimeRegistrationService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  let listed = 0;
  const service = new SessionHeartbeatPublisherService(
    { nodeId: 'node-3', adapterVersion: '0.13.0' },
    { listSessions: async () => { listed += 1; return []; } },
    { isRuntimeRegistered: () => false, bootIdentity: () => 'boot-3' },
    observabilityStub(),
  );
  await service.publishNow();
  assert.equal(listed, 0);
});

test('session heartbeat transport reuses runtime HMAC and never follows redirects', async () => {
  const originalFetch = global.fetch;
  const previous = {
    NODE_ENV: process.env.NODE_ENV,
    CONTROL_API_INTERNAL_URL: process.env.CONTROL_API_INTERNAL_URL,
    GATEWAY_RUNTIME_SECRET: process.env.GATEWAY_RUNTIME_SECRET,
  };
  const secret = 'r'.repeat(32);
  process.env.NODE_ENV = 'test';
  process.env.CONTROL_API_INTERNAL_URL = 'http://control-api.internal';
  process.env.GATEWAY_RUNTIME_SECRET = secret;
  let captured;
  global.fetch = async (url, init) => {
    captured = { url: String(url), init: init ?? {} };
    const report = JSON.parse(Buffer.from(init.body).toString());
    return new Response(JSON.stringify({ sentToday: 12, status: 'READY', ownership: {
      nodeId: report.nodeId, sessionId: report.sessionId, bootId: report.bootId, leaseVersion: 1,
      serverNow: new Date().toISOString(), leaseExpiresAt: new Date(Date.now() + 90000).toISOString(),
    } }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  };
  try {
    const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
      './gateway-identity.service': { GatewayIdentityService: class {} },
      './observability.service': { GatewayObservabilityService: class {} },
    });
    const runtime = new RuntimeRegistrationService(
      { nodeId: 'node-1' },
      { traceparent: () => undefined, increment() {}, gauge() {}, observe() {} },
    );
    runtime.runtimeRegistered = true;
    const bootId = runtime.bootIdentity();
    const report = {
      nodeId: 'node-1',
      sessionId: 'session-1',
      bootId,
      status: 'READY',
      engineVersion: '0.13.0',
      sentToday: 5,
    };
    // A report is attributed to when usage was sampled, not when fetch begins.
    const observedAt = new Date(Date.now() - 2000);
    const accepted = await runtime.publishSessionHeartbeat('session-1', report, observedAt);
    assert.equal(captured.init.headers['x-gateway-runtime-timestamp'], String(Math.floor(observedAt.getTime() / 1000)));
    assert.equal(accepted.sentToday, 12);
    assert.equal(captured.url, 'http://control-api.internal/api/v1/internal/sender-sessions/session-1/heartbeat');
    assert.equal(captured.init.redirect, 'manual');
    const headers = captured.init.headers;
    const timestamp = headers['x-gateway-runtime-timestamp'];
    const nonce = headers['x-gateway-runtime-nonce'];
    const signature = headers['x-gateway-runtime-signature'];
    assert.match(timestamp, /^\d+$/);
    assert.ok(typeof nonce === 'string' && nonce.length >= 16);
    const body = Buffer.from(captured.init.body);
    const digest = createHash('sha256').update(body).digest('hex');
    const expected = `sha256=${createHmac('sha256', secret).update(timestamp).update('\n').update(nonce).update('\n').update(digest).digest('hex')}`;
    assert.equal(signature, expected);
    assert.deepEqual(JSON.parse(body.toString('utf8')), report);
  } finally {
    global.fetch = originalFetch;
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) delete process.env[key];
      else process.env[key] = value;
    }
  }
});
