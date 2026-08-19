#!/usr/bin/env node
const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of [
    'typescript',
    path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript'),
  ]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for gateway drain tests');
}

class ServiceUnavailableException extends Error {}
class NotImplementedException extends Error {}
const nestMock = {
  Injectable: () => target => target,
  Inject: () => () => undefined,
  ServiceUnavailableException,
  NotImplementedException,
};
function loadModule(relative, overrides = {}) {
  const filename = path.join(process.cwd(), 'services/openwa-gateway/src', relative);
  const source = fs.readFileSync(filename, 'utf8');
  const ts = loadTypeScript();
  const output = ts.transpileModule(source, {
    fileName: filename,
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
      experimentalDecorators: true,
      emitDecoratorMetadata: false,
      esModuleInterop: true,
    },
  }).outputText;
  const module = { exports: {} };
  const localRequire = specifier => {
    if (specifier === '@nestjs/common') return nestMock;
    if (overrides[specifier]) return overrides[specifier];
    return require(specifier);
  };
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(
    module.exports, localRequire, module, filename, path.dirname(filename),
  );
  return module.exports;
}
const pipelineModule = loadModule('session-pipeline.service.ts');
const messagingModule = loadModule('gateway-messaging.service.ts', {
  './session-pipeline.service': pipelineModule,
  './idempotency.service': { IdempotencyService: class {} },
  './provider/provider.token': { MESSAGING_PROVIDER: Symbol('provider') },
  './session-authority.service': { SessionAuthorityService: class {} },
});
const lifecycleAuthorities = { runIfNotTombstoned: async (_id, operation) => operation(), tombstone: async () => undefined };

test('session rate window remains serialized when concurrent sends are enabled', async () => {
  process.env.SESSION_IN_FLIGHT_LIMIT = '2';
  process.env.SESSION_MESSAGES_PER_MINUTE = '600';
  process.env.SESSION_MIN_MESSAGES_PER_MINUTE = '600';
  try {
    const pipelines = new pipelineModule.SessionPipelineService();
    await pipelines.run('session-rate', async () => undefined);
    const submittedAt = [];
    await Promise.all([
      pipelines.run('session-rate', async () => { submittedAt.push(Date.now()); }),
      pipelines.run('session-rate', async () => { submittedAt.push(Date.now()); }),
    ]);
    submittedAt.sort((a, b) => a - b);
    assert.ok(
      submittedAt[1] - submittedAt[0] >= 70,
      `concurrent sends bypassed the configured rate window: ${submittedAt[1] - submittedAt[0]}ms`,
    );
  } finally {
    delete process.env.SESSION_IN_FLIGHT_LIMIT;
    delete process.env.SESSION_MESSAGES_PER_MINUTE;
    delete process.env.SESSION_MIN_MESSAGES_PER_MINUTE;
  }
});

test('session drain blocks a send that is already waiting for its rate window', async () => {
  process.env.SESSION_IN_FLIGHT_LIMIT = '2';
  process.env.SESSION_MESSAGES_PER_MINUTE = '600';
  process.env.SESSION_MIN_MESSAGES_PER_MINUTE = '600';
  try {
    const pipelines = new pipelineModule.SessionPipelineService();
    await pipelines.run('session-rate-drain', async () => undefined);
    let submitted = false;
    const waiting = pipelines.run('session-rate-drain', async () => { submitted = true; });
    await new Promise(resolve => setTimeout(resolve, 10));
    pipelines.drain('session-rate-drain');
    await assert.rejects(waiting, ServiceUnavailableException);
    assert.equal(submitted, false, 'provider submission occurred after session drain began');
  } finally {
    delete process.env.SESSION_IN_FLIGHT_LIMIT;
    delete process.env.SESSION_MESSAGES_PER_MINUTE;
    delete process.env.SESSION_MIN_MESSAGES_PER_MINUTE;
  }
});

for (const lifecycle of ['stop', 'logout', 'delete']) {
  test(`${lifecycle} waits for active sends after entering drain`, async () => {
    const pipelines = new pipelineModule.SessionPipelineService();
    let signalStarted;
    let releaseSend;
    const started = new Promise(resolve => { signalStarted = resolve; });
    const release = new Promise(resolve => { releaseSend = resolve; });
    const active = pipelines.run('session-1', async () => {
      signalStarted();
      await release;
      return 'sent';
    });
    await started;
    let teardownCalled = false;
    const provider = {
      stopSession: async () => { teardownCalled = true; return { id: 'session-1' }; },
      logoutSession: async () => { teardownCalled = true; return { id: 'session-1' }; },
      deleteSession: async () => { teardownCalled = true; },
    };
    const gateway = new messagingModule.GatewayMessagingService(
      provider, {}, pipelines, lifecycleAuthorities,
    );
    const operation = lifecycle === 'stop'
      ? gateway.stopSession('session-1')
      : lifecycle === 'logout'
        ? gateway.logoutSession('session-1')
        : gateway.deleteSession('session-1');
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(
      teardownCalled, false,
      `${lifecycle} tore down the provider while a send was still active`,
    );
    releaseSend();
    await active;
    await operation;
    assert.equal(teardownCalled, true);
  });
}


test('successful session restart exits teardown drain before new sends', async () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  const provider = {
    stopSession: async () => ({ id: 'session-restart' }),
    startSession: async () => ({ id: 'session-restart' }),
  };
  const gateway = new messagingModule.GatewayMessagingService(provider, {}, pipelines, lifecycleAuthorities);
  await gateway.stopSession('session-restart');
  assert.equal(pipelines.status('session-restart').draining, true);
  await gateway.startSession('session-restart');
  assert.equal(
    pipelines.status('session-restart').draining, false,
    'successful provider start left the session pipeline permanently draining',
  );
  let submitted = false;
  await pipelines.run('session-restart', async () => { submitted = true; });
  assert.equal(submitted, true);
});

test('failed session restart preserves teardown drain', async () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  const provider = {
    stopSession: async () => ({ id: 'session-failed-restart' }),
    startSession: async () => { throw new Error('provider start failed'); },
  };
  const gateway = new messagingModule.GatewayMessagingService(provider, {}, pipelines, lifecycleAuthorities);  await gateway.stopSession('session-failed-restart');
  await assert.rejects(() => gateway.startSession('session-failed-restart'), /provider start failed/);
  assert.equal(
    pipelines.status('session-failed-restart').draining, true,
    'failed provider start reopened a drained session',
  );
});

test('teardown is rejected while a provider start is in progress', async () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  let startEntered;
  let releaseStart;
  const entered = new Promise(resolve => { startEntered = resolve; });
  const release = new Promise(resolve => { releaseStart = resolve; });
  const provider = {
    startSession: async () => { startEntered(); await release; return { id: 'session-start-race' }; },
    stopSession: async () => ({ id: 'session-start-race' }),
  };
  const gateway = new messagingModule.GatewayMessagingService(provider, {}, pipelines, lifecycleAuthorities);
  const starting = gateway.startSession('session-start-race');
  await entered;
  await assert.rejects(
    () => gateway.stopSession('session-start-race'),
    ServiceUnavailableException,
    'teardown raced a provider start instead of failing closed',
  );
  releaseStart();
  await starting;
});

test('send is rejected for the full duration of a provider start', async () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  let enteredStart; let releaseStart;
  const entered = new Promise(resolve => { enteredStart = resolve; });
  const release = new Promise(resolve => { releaseStart = resolve; });
  let providerSends = 0;
  const provider = {
    startSession: async () => { enteredStart(); await release; return { id: 'session-start-send' }; },
    health: async () => ({ ready: true, status: 'READY' }),
    send: async () => { providerSends += 1; return { accepted: true }; },
  };
  const idempotency = { execute: async (_request, operation) => operation() };
  const authorities = { validate: async () => undefined, runIfNotTombstoned: async (_id, operation) => operation() };
  const gateway = new messagingModule.GatewayMessagingService(provider, idempotency, pipelines, authorities);
  const starting = gateway.startSession('session-start-send');
  await entered;
  await assert.rejects(() => gateway.send({ sessionId: 'session-start-send' }), ServiceUnavailableException);
  assert.equal(providerSends, 0, 'provider send interleaved with provider start');
  releaseStart(); await starting;
});

test('status inspection does not allocate unbounded session pipeline state', () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  for (let i = 0; i < 1000; i += 1) pipelines.status(`unknown-${i}`);
  assert.equal(pipelines.states.size, 0, 'status/health lookups allocated persistent pipeline state');
});

test('teardown cannot be reopened by resume without a successful start', async () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  const provider = { stopSession: async () => ({ id: 'session-terminal' }) };
  const gateway = new messagingModule.GatewayMessagingService(provider, {}, pipelines, lifecycleAuthorities);
  await gateway.stopSession('session-terminal');
  assert.throws(() => gateway.resume('session-terminal'), ServiceUnavailableException);
});

test('delete durably tombstones session authority', async () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  let tombstoned = '';
  const provider = { deleteSession: async () => ({ id: 'session-delete-tombstone' }) };
  const authorities = { tombstone: async id => { tombstoned = id; } };
  const gateway = new messagingModule.GatewayMessagingService(provider, {}, pipelines, authorities);
  await gateway.deleteSession('session-delete-tombstone');
  assert.equal(tombstoned, 'session-delete-tombstone');
});

test('delete publishes durable tombstone before provider deletion begins', async () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  let tombstoned = false;
  const authorities = {
    tombstone: async () => { tombstoned = true; },
    assertNotTombstoned: async () => undefined,
  };
  const provider = {
    deleteSession: async () => {
      assert.equal(tombstoned, true, 'provider deletion began before durable authority tombstone');
      throw new Error('simulated provider delete failure after tombstone');
    },
  };
  const gateway = new messagingModule.GatewayMessagingService(provider, {}, pipelines, authorities);
  await assert.rejects(() => gateway.deleteSession('session-delete-order'), /simulated provider delete failure/);
  assert.equal(tombstoned, true, 'failed provider deletion lost durable delete fence');
});


test('tombstoned session blocks create and pairing side effects', async () => {
  const pipelines = new pipelineModule.SessionPipelineService();
  let providerCalls = 0;
  const provider = {
    createSession: async () => { providerCalls += 1; return { id: 'session-deleted' }; },
    qr: async () => { providerCalls += 1; return { qr: 'must-not-return' }; },
    pairingCode: async () => { providerCalls += 1; return { code: 'must-not-return' }; },
  };
  const authorities = {
    runIfNotTombstoned: async () => { throw new Error('session authority is permanently tombstoned'); },
    tombstone: async () => undefined,
  };
  const gateway = new messagingModule.GatewayMessagingService(provider, {}, pipelines, authorities);
  await assert.rejects(() => gateway.createSession('session-deleted'), /permanently tombstoned/);
  await assert.rejects(() => gateway.qr('session-deleted'), /permanently tombstoned/);
  await assert.rejects(() => gateway.pairingCode('session-deleted', '2348012345678'), /permanently tombstoned/);
  assert.equal(providerCalls, 0, 'tombstoned session reached provider create/pairing operations');
});


test('pipeline state map fails closed at its configured bound', () => {
  process.env.SESSION_PIPELINE_MAX_STATES = '2';
  try {
    const pipelines = new pipelineModule.SessionPipelineService();
    pipelines.drain('bounded-state-1');
    pipelines.drain('bounded-state-2');
    assert.throws(() => pipelines.drain('bounded-state-3'), ServiceUnavailableException);
    assert.equal(pipelines.states.size, 2, 'pipeline state bound was exceeded');
  } finally {
    delete process.env.SESSION_PIPELINE_MAX_STATES;
  }
});
test('pre-network control rejection does not reduce provider send rate', async () => {
  process.env.SESSION_MESSAGES_PER_MINUTE = '20';
  process.env.SESSION_MIN_MESSAGES_PER_MINUTE = '1';
  try {
    const pipelines = new pipelineModule.SessionPipelineService();
    let healthCalls = 0;
    let idempotencyCalls = 0;
    let providerSends = 0;
    const provider = {
      health: async () => (++healthCalls === 1 ? { ready: true, status: 'READY' } : { ready: false, status: 'UNAVAILABLE' }),
      send: async () => { providerSends += 1; return { accepted: true }; },
    };
    const idempotency = { execute: async (_request, operation) => { idempotencyCalls += 1; return operation(); } };
    const authorities = { validate: async () => undefined, submitIfCurrent: async (_request, operation) => operation() };
    const gateway = new messagingModule.GatewayMessagingService(provider, idempotency, pipelines, authorities);
    await assert.rejects(() => gateway.send({ sessionId: 'session-control-reject' }), ServiceUnavailableException);
    const status = pipelines.status('session-control-reject');
    assert.equal(status.currentMessagesPerMinute, 20, 'pre-network health rejection reduced provider send rate');
    assert.equal(idempotencyCalls, 0, 'pre-network health rejection opened idempotency boundary');
    assert.equal(providerSends, 0, 'pre-network health rejection reached provider send');
  } finally {
    delete process.env.SESSION_MESSAGES_PER_MINUTE;
    delete process.env.SESSION_MIN_MESSAGES_PER_MINUTE;
  }
});
test('permanently deleted session releases bounded pipeline state', async () => {
  process.env.SESSION_PIPELINE_MAX_STATES = '1';
  try {
    const pipelines = new pipelineModule.SessionPipelineService();
    const authorities = { tombstone: async () => undefined };
    const provider = { deleteSession: async () => undefined };
    const gateway = new messagingModule.GatewayMessagingService(provider, {}, pipelines, authorities);
    await gateway.deleteSession('session-deleted-state');
    assert.equal(pipelines.states.size, 0, 'durably deleted session retained terminal pipeline state');
    assert.doesNotThrow(() => pipelines.drain('session-replacement'), 'terminal state exhausted the bounded session map');
  } finally {
    delete process.env.SESSION_PIPELINE_MAX_STATES;
  }
});
test('provider send failure still reduces adaptive provider rate', async () => {
  process.env.SESSION_MESSAGES_PER_MINUTE = '20';
  process.env.SESSION_MIN_MESSAGES_PER_MINUTE = '1';
  try {
    const pipelines = new pipelineModule.SessionPipelineService();
    const provider = {
      health: async () => ({ ready: true, status: 'READY' }),
      send: async () => { throw new Error('provider submission failed'); },
    };
    const idempotency = { execute: async (_request, operation) => operation() };
    const authorities = { validate: async () => undefined, submitIfCurrent: async (_request, operation) => operation() };
    const gateway = new messagingModule.GatewayMessagingService(provider, idempotency, pipelines, authorities);
    await assert.rejects(() => gateway.send({ sessionId: 'session-provider-failure' }), /provider submission failed/);
    assert.equal(pipelines.status('session-provider-failure').currentMessagesPerMinute, 10, 'provider failure did not reduce adaptive send rate');
  } finally {
    delete process.env.SESSION_MESSAGES_PER_MINUTE;
    delete process.env.SESSION_MIN_MESSAGES_PER_MINUTE;
  }
});
