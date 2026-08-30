#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

function loadTypeScript() {
  for (const candidate of ['typescript', path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript')]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for gateway control-plane tests');
}

class ServiceUnavailableException extends Error {}
class UnauthorizedException extends Error {}
const nestMock = {
  Injectable: () => target => target,
  ServiceUnavailableException,
  UnauthorizedException,
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
  return {
    traceparent: () => undefined,
    increment: () => undefined,
    gauge: () => undefined,
    observe: () => undefined,
  };
}

async function withCapturedFetch(run, responseFactory = () => ({ ok: true, status: 200, text: async () => '' })) {
  const original = global.fetch;
  let captured;
  global.fetch = async (url, init) => {
    captured = { url: String(url), init: init ?? {} };
    return responseFactory();
  };
  try {
    await run();
    return captured;
  } finally {
    global.fetch = original;
  }
}

function streamingErrorResponse() {
  let textCalled = false;
  let cancelled = false;
  const payload = new TextEncoder().encode('X'.repeat(1024));
  let chunks = 0;
  const body = new ReadableStream({
    pull(controller) {
      controller.enqueue(payload);
      chunks += 1;
      if (chunks >= 32) controller.close();
    },
    cancel() { cancelled = true; },
  });
  return {
    response: { ok: false, status: 502, body, text: async () => { textCalled = true; throw new Error('unbounded response.text() called'); } },
    state: () => ({ textCalled, cancelled }),
  };
}
test('signed provider callbacks never follow redirects', async () => {
  process.env.CONTROL_API_CALLBACK_URL = 'http://control-api.internal/provider-events';
  process.env.GATEWAY_CALLBACK_SECRET = 'x'.repeat(32);
  let sender;
  const outbox = {
    setSender: value => { sender = value; },
    enqueue: async () => undefined,
    acknowledge: async () => undefined,
  };
  const { ProviderEventPublisherService } = loadModule('provider-event-publisher.service.ts', {
    './provider-event-outbox.service': { ProviderEventOutboxService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const publisher = new ProviderEventPublisherService(outbox, observabilityStub());
  assert.equal(typeof sender, 'function');
  const captured = await withCapturedFetch(() => publisher.publish({
    schemaVersion: '1.0', eventId: 'event-1', eventType: 'message.sent',
    sessionId: 'session-1', providerMessageId: 'provider-1',
    occurredAt: new Date().toISOString(),
  }));
  assert.equal(captured.init.redirect, 'manual', 'signed provider callback may follow a control-plane redirect');
});
test('signed runtime registration never follows redirects', async () => {
  process.env.GATEWAY_RUNTIME_REGISTRATION_URL = 'http://control-api.internal/runtime';
  process.env.GATEWAY_RUNTIME_SECRET = 'r'.repeat(32);
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const identity = {
    nodeId: 'node-1',
    describe: () => ({
      provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1',
      gatewayPoolVersion: 1, adapterVersion: '0.13.0', nodeId: 'node-1',
      nodeVersion: 1, capabilities: ['SEND_TEXT'],
    }),
  };
  const service = new RuntimeRegistrationService(identity, observabilityStub());
  service.report = async () => ({ nodeId: 'node-1', observedAt: new Date().toISOString() });
  const captured = await withCapturedFetch(() => service.publish());
  assert.equal(captured.init.redirect, 'manual', 'signed runtime heartbeat may follow a control-plane redirect');
});

test('provider callback error responses are consumed with a bounded stream read', async () => {
  process.env.CONTROL_API_CALLBACK_URL = 'http://control-api.internal/provider-events';
  process.env.GATEWAY_CALLBACK_SECRET = 'x'.repeat(32);
  const outbox = { setSender: () => undefined, enqueue: async () => undefined, acknowledge: async () => undefined };
  const { ProviderEventPublisherService } = loadModule('provider-event-publisher.service.ts', {
    './provider-event-outbox.service': { ProviderEventOutboxService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const publisher = new ProviderEventPublisherService(outbox, observabilityStub());
  const failure = streamingErrorResponse();
  await assert.rejects(
    () => withCapturedFetch(() => publisher.publish({
      schemaVersion: '1.0', eventId: 'event-bounded', eventType: 'message.sent',
      sessionId: 'session-1', providerMessageId: 'provider-1', occurredAt: new Date().toISOString(),
    }), () => failure.response),
    error => error instanceof ServiceUnavailableException && /HTTP 502/.test(error.message),
  );
  assert.equal(failure.state().textCalled, false, 'provider callback used unbounded response.text()');
  assert.equal(failure.state().cancelled, true, 'provider callback did not stop the oversized response stream');
});

test('inbound callback error responses are consumed with a bounded stream read', async () => {
  process.env.CONTROL_API_INBOUND_URL = 'http://control-api.internal/inbound';
  process.env.GATEWAY_CALLBACK_SECRET = 'x'.repeat(32);
  const outbox = { setSender: () => undefined, enqueue: async () => undefined, acknowledge: async () => undefined };
  const { InboundMessagePublisherService } = loadModule('inbound-message-publisher.service.ts', {
    './inbound-message-outbox.service': { InboundMessageOutboxService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const publisher = new InboundMessagePublisherService(outbox, observabilityStub());
  const failure = streamingErrorResponse();
  await assert.rejects(
    () => withCapturedFetch(() => publisher.publish({
      schemaVersion: '1.0', eventId: 'inbound-bounded', sessionId: 'session-1',
      senderMsisdn: '+447700900123', messageText: 'bounded response test', occurredAt: new Date().toISOString(),
    }), () => failure.response),
    error => error instanceof ServiceUnavailableException && /HTTP 502/.test(error.message),
  );
  assert.equal(failure.state().textCalled, false, 'inbound callback used unbounded response.text()');
  assert.equal(failure.state().cancelled, true, 'inbound callback did not stop the oversized response stream');
});

test('production gateway does not receive unused control-plane secrets', () => {
  const main = fs.readFileSync(
    path.join(process.cwd(), 'services/openwa-gateway/src/main.ts'),
    'utf8',
  );
  const compose = fs.readFileSync(
    path.join(process.cwd(), 'infrastructure/compose/compose.production.yaml'),
    'utf8',
  );
  const gatewayBlock = compose.split(/\n  openwa-gateway:\s*\n/)[1]?.split(/\nnetworks:\s*\n/)[0] ?? '';
  for (const secret of ['PROFILING_TOKEN', 'MEDIA_DOWNLOAD_SECRET']) {
    assert.equal(
      main.includes(secret), false,
      `gateway bootstrap still loads unused ${secret}`,
    );
    assert.equal(
      gatewayBlock.includes(secret), false,
      `production gateway still receives unused ${secret}`,
    );
  }
});

test('gateway command replay window rejects stale and future requests before nonce persistence', async () => {
  process.env.GATEWAY_COMMAND_SECRET = 'x'.repeat(32);
  process.env.GATEWAY_COMMAND_REPLAY_WINDOW_SECONDS = '300';
  let claims = 0;
  const replay = { claim: async () => { claims += 1; } };
  const identity = { describe: () => ({}) };
  const { InternalAuthMiddleware } = loadModule('internal-auth.middleware.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './command-replay.service': { CommandReplayService: class {} },
  });
  const middleware = new InternalAuthMiddleware(identity, replay);
  const now = Math.floor(Date.now() / 1000);
  const request = timestamp => ({
    method: 'POST', originalUrl: '/v1/messages', path: '/v1/messages', rawBody: Buffer.from('{}'),
    header: name => ({
      'x-gateway-timestamp': String(timestamp),
      'x-gateway-nonce': 'nonce-0123456789abcdef',
      'x-gateway-signature': `sha256=${'0'.repeat(64)}`,
    })[String(name).toLowerCase()] ?? '',
  });
  await assert.rejects(() => middleware.use(request(now - 301), {}, () => undefined), UnauthorizedException);
  await assert.rejects(() => middleware.use(request(now + 301), {}, () => undefined), UnauthorizedException);
  assert.equal(claims, 0, 'out-of-window commands must not consume durable nonce capacity');
  delete process.env.GATEWAY_COMMAND_SECRET;
  delete process.env.GATEWAY_COMMAND_REPLAY_WINDOW_SECONDS;
});
test('declared command body cannot authenticate when raw-body evidence is absent', async () => {
  const crypto = require('node:crypto');
  const secret = 'x'.repeat(32);
  process.env.GATEWAY_COMMAND_SECRET = secret;
  let claims = 0;
  const replay = { claim: async () => { claims += 1; } };
  const identity = { describe: () => ({}) };
  const { InternalAuthMiddleware } = loadModule('internal-auth.middleware.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './command-replay.service': { CommandReplayService: class {} },
  });
  const middleware = new InternalAuthMiddleware(identity, replay);
  const timestamp = String(Math.floor(Date.now() / 1000));
  const nonce = 'nonce-raw-body-00000001';
  const emptyHash = crypto.createHash('sha256').update(Buffer.alloc(0)).digest('hex');
  const canonical = ['POST', '/v1/messages', timestamp, nonce, emptyHash].join('\n');
  const signature = `sha256=${crypto.createHmac('sha256', secret).update(canonical).digest('hex')}`;
  const headers = {
    'content-length': '17',
    'x-gateway-timestamp': timestamp,
    'x-gateway-nonce': nonce,
    'x-gateway-signature': signature,
  };
  const request = {
    method: 'POST', originalUrl: '/v1/messages', path: '/v1/messages', body: { message: 'real' },
    header: name => headers[String(name).toLowerCase()] ?? '',
  };
  await assert.rejects(
    () => middleware.use(request, {}, () => undefined),
    UnauthorizedException,
    'middleware authenticated a declared JSON body using an empty-body signature',
  );
  assert.equal(claims, 0, 'missing raw-body evidence consumed replay nonce capacity');
  delete process.env.GATEWAY_COMMAND_SECRET;
});
test('session command MAC rejects target-identity retargeting across workers', async () => {
  const crypto = require('node:crypto');
  const secret = 'y'.repeat(32);
  process.env.GATEWAY_COMMAND_SECRET = secret;
  let claims = 0;
  const replay = { claim: async () => { claims += 1; } };
  const targetB = { nodeId: 'node-b', nodeVersion: 7, gatewayPoolId: 'pool-1', provider: 'OPENWA', engine: 'BAILEYS', adapterVersion: '0.13.0' };
  const identity = { describe: () => targetB };
  const { InternalAuthMiddleware } = loadModule('internal-auth.middleware.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './command-replay.service': { CommandReplayService: class {} },
  });
  const middleware = new InternalAuthMiddleware(identity, replay);
  const timestamp = String(Math.floor(Date.now() / 1000));
  const nonce = 'nonce-target-bind-0000001';
  const bodyHash = crypto.createHash('sha256').update(Buffer.alloc(0)).digest('hex');
  const signedTargetA = ['node-a', '7', 'pool-1', 'OPENWA', 'BAILEYS', '0.13.0'];
  const canonical = ['GET', '/v1/sessions/session-1/qr', timestamp, nonce, bodyHash, ...signedTargetA].join('\n');
  const signature = `sha256=${crypto.createHmac('sha256', secret).update(canonical).digest('hex')}`;
  const headers = {
    'x-gateway-timestamp': timestamp, 'x-gateway-nonce': nonce, 'x-gateway-signature': signature,
    'x-gateway-target-node-id': targetB.nodeId,
    'x-gateway-target-node-version': String(targetB.nodeVersion),
    'x-gateway-target-pool-id': targetB.gatewayPoolId,
    'x-gateway-target-provider': targetB.provider,
    'x-gateway-target-engine': targetB.engine,
    'x-gateway-target-adapter-version': targetB.adapterVersion,
  };
  const request = {
    method: 'GET', originalUrl: '/v1/sessions/session-1/qr', path: '/v1/sessions/session-1/qr',
    header: name => headers[String(name).toLowerCase()] ?? '',
  };
  await assert.rejects(
    () => middleware.use(request, {}, () => undefined),
    UnauthorizedException,
    'a command MAC for node A remained valid after retargeting unsigned headers to node B',
  );
  assert.equal(claims, 0, 'retargeted command consumed replay nonce capacity');
  delete process.env.GATEWAY_COMMAND_SECRET;
});

test('production runtime registration requires an explicit governed gateway internal URL', async () => {
  process.env.NODE_ENV = 'production';
  process.env.CONTROL_API_INTERNAL_URL = 'https://control.internal.example';
  process.env.GATEWAY_BIND_ADDRESS = '10.20.30.40';
  process.env.GATEWAY_RUNTIME_SECRET = 'r'.repeat(32);
  delete process.env.GATEWAY_INTERNAL_URL;
  delete process.env.GATEWAY_RUNTIME_REGISTRATION_URL;
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const identity = { nodeId: 'node-1', describe: () => ({ nodeId: 'node-1' }) };
  const service = new RuntimeRegistrationService(identity, observabilityStub());
  service.publish = async () => undefined;
  await assert.rejects(() => service.onModuleInit(), /GATEWAY_INTERNAL_URL|internal URL/i);
  delete process.env.NODE_ENV;
  delete process.env.CONTROL_API_INTERNAL_URL;
  delete process.env.GATEWAY_BIND_ADDRESS;
  delete process.env.GATEWAY_RUNTIME_SECRET;
});

test('production gateway rejects hostname that merely mimics a private IPv4 prefix', () => {
  process.env.NODE_ENV = 'production';
  process.env.GATEWAY_INTERNAL_URL = 'http://10.attacker.example:2785';
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const service = new RuntimeRegistrationService({}, observabilityStub());
  assert.throws(
    () => service.advertisedInternalURL(),
    /private network|IP address|GATEWAY_INTERNAL_URL/i,
    'hostname prefix bypassed production private-address validation',
  );
  delete process.env.NODE_ENV;
  delete process.env.GATEWAY_INTERNAL_URL;
});

test('production gateway rejects Docker-local cross-provider control hostname', async () => {
  process.env.NODE_ENV = 'production';
  process.env.GATEWAY_INTERNAL_URL = 'https://gateway.private.example';
  process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS = 'gateway.private.example';
  process.env.GATEWAY_BIND_ADDRESS = '10.20.30.40';
  process.env.CONTROL_API_INTERNAL_URL = 'https://control-api';
  process.env.CONTROL_API_CALLBACK_URL = 'https://control-api/api/v1/internal/gateway/events';
  process.env.CONTROL_API_INBOUND_URL = 'https://control-api/api/v1/internal/gateway/inbound';
  process.env.MEDIA_DOWNLOAD_BASE_URL = 'https://control-api/api/v1/internal/media';
  process.env.SSRF_ALLOWED_HOSTS = 'control-api';
  process.env.GATEWAY_RUNTIME_SECRET = 'r'.repeat(32);
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  const service = new RuntimeRegistrationService({ nodeId: 'node-1' }, observabilityStub());
  service.publish = async () => undefined;
  await assert.rejects(() => service.onModuleInit(), /Docker-local|control hostname|cross-provider/i);
  for (const key of ['NODE_ENV','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','GATEWAY_BIND_ADDRESS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET']) delete process.env[key];
});

test('production gateway rejects advertised gateway aliasing the Railway control hostname', async () => {
  const keys = ['NODE_ENV','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','GATEWAY_BIND_ADDRESS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET'];
  try {
    process.env.NODE_ENV = 'production';
    process.env.GATEWAY_INTERNAL_URL = 'https://control.internal.example:2785';
    process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS = 'control.internal.example';
    process.env.GATEWAY_BIND_ADDRESS = '10.20.30.40';
    process.env.CONTROL_API_INTERNAL_URL = 'https://control.internal.example';
    process.env.CONTROL_API_CALLBACK_URL = 'https://control.internal.example/api/v1/internal/gateway/events';
    process.env.CONTROL_API_INBOUND_URL = 'https://control.internal.example/api/v1/internal/gateway/inbound';
    process.env.MEDIA_DOWNLOAD_BASE_URL = 'https://control.internal.example/api/v1/internal/media';
    process.env.SSRF_ALLOWED_HOSTS = 'control.internal.example';
    process.env.GATEWAY_RUNTIME_SECRET = 'r'.repeat(32);
    const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
      './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
    });
    const service = new RuntimeRegistrationService({ nodeId: 'node-1' }, observabilityStub());
    service.publish = async () => undefined;
    await assert.rejects(() => service.onModuleInit(), /control hostname|gateway authority/i);
  } finally {
    for (const key of keys) delete process.env[key];
  }
});

test('production gateway rejects Docker special and loopback cross-provider endpoints', async () => {
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  for (const host of ['host.docker.internal', 'bridge.docker.internal', '127.0.0.1', '[::1]', '169.254.10.20', '10.20.30.50', '172.16.0.50', '192.168.1.50', '[fd00::1]', '[::ffff:10.20.30.50]']) {
    process.env.NODE_ENV = 'production';
    process.env.GATEWAY_INTERNAL_URL = 'https://gateway.private.example';
    process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS = 'gateway.private.example';
    process.env.GATEWAY_BIND_ADDRESS = '10.20.30.40';
    process.env.CONTROL_API_INTERNAL_URL = 'https://' + host;
    process.env.CONTROL_API_CALLBACK_URL = 'https://' + host + '/api/v1/internal/gateway/events';
    process.env.CONTROL_API_INBOUND_URL = 'https://' + host + '/api/v1/internal/gateway/inbound';
    process.env.MEDIA_DOWNLOAD_BASE_URL = 'https://' + host + '/api/v1/internal/media';
    process.env.SSRF_ALLOWED_HOSTS = host.replace(/^\[|\]$/g, '');
    process.env.GATEWAY_RUNTIME_SECRET = 'r'.repeat(32);
    const service = new RuntimeRegistrationService({ nodeId: 'node-1' }, observabilityStub());
    service.publish = async () => undefined;
    await assert.rejects(() => service.onModuleInit(), /Docker-local|loopback|link-local|production endpoint|cross-provider/i, 'accepted ' + host);
  }
  for (const key of ['NODE_ENV','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','GATEWAY_BIND_ADDRESS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET']) delete process.env[key];
});

test('production gateway rejects Docker special and loopback advertised HTTPS node URL', () => {
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  for (const raw of ['https://host.docker.internal:2785', 'https://bridge.docker.internal:2785', 'https://127.0.0.1:2785', 'https://[::1]:2785', 'https://169.254.10.20:2785']) {
    process.env.NODE_ENV = 'production';
    process.env.GATEWAY_INTERNAL_URL = raw;
    const service = new RuntimeRegistrationService({}, observabilityStub());
    assert.throws(() => service.advertisedInternalURL(), /Docker-local|loopback|link-local|production endpoint|GATEWAY_INTERNAL_URL/i, 'accepted ' + raw);
  }
  delete process.env.NODE_ENV;
  delete process.env.GATEWAY_INTERNAL_URL;
});

test('production HTTP advertised gateway URL is rejected', () => {
  process.env.NODE_ENV = 'production';
  process.env.GATEWAY_BIND_ADDRESS = '10.20.30.40';
  process.env.GATEWAY_INTERNAL_URL = 'http://10.20.30.99:2785';
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  const service = new RuntimeRegistrationService({}, observabilityStub());
  assert.throws(() => service.advertisedInternalURL(), /HTTPS|GATEWAY_INTERNAL_URL/i);
  for (const key of ['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL']) delete process.env[key];
});

test('Hostinger gateway passes bind address into production runtime validation', () => {
  const text = fs.readFileSync(path.join(process.cwd(), 'infrastructure/compose/compose.hostinger-openwa-gateway.yaml'), 'utf8');
  const matches = text.match(/GATEWAY_BIND_ADDRESS/g) ?? [];
  assert.ok(matches.length >= 2, 'GATEWAY_BIND_ADDRESS is used for host port binding but is not passed into the gateway environment');
});

test('production gateway rejects runtime registration URL override outside governed Railway URL derivation', async () => {
  const keys = ['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_REGISTRATION_URL','GATEWAY_RUNTIME_SECRET'];
  try {
    process.env.NODE_ENV = 'production';
    process.env.GATEWAY_BIND_ADDRESS = '10.20.30.40';
    process.env.GATEWAY_INTERNAL_URL = 'https://gateway.private.example';
    process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS = 'gateway.private.example';
    process.env.CONTROL_API_INTERNAL_URL = 'https://control.internal.example';
    process.env.CONTROL_API_CALLBACK_URL = 'https://control.internal.example/api/v1/internal/gateway/events';
    process.env.CONTROL_API_INBOUND_URL = 'https://control.internal.example/api/v1/internal/gateway/inbound';
    process.env.MEDIA_DOWNLOAD_BASE_URL = 'https://control.internal.example/api/v1/internal/media';
    process.env.SSRF_ALLOWED_HOSTS = 'control.internal.example';
    process.env.GATEWAY_RUNTIME_REGISTRATION_URL = 'http://127.0.0.1:9999/runtime';
    process.env.GATEWAY_RUNTIME_SECRET = 'r'.repeat(32);
    const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
      './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
    });
    const service = new RuntimeRegistrationService({ nodeId: 'node-1' }, observabilityStub());
    service.publish = async () => undefined;
    await assert.rejects(() => service.onModuleInit(), /GATEWAY_RUNTIME_REGISTRATION_URL|runtime registration.*production/i);
  } finally {
    for (const key of keys) delete process.env[key];
  }
});

test('gateway shutdown publishes DRAINING instead of refreshing READY liveness', async () => {
  process.env.GATEWAY_RUNTIME_REGISTRATION_URL = 'http://control-api.internal/runtime';
  process.env.GATEWAY_RUNTIME_SECRET = 'r'.repeat(32);
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const service = new RuntimeRegistrationService({ nodeId: 'node-1' }, observabilityStub());
  service.report = async runtimeState => ({
    nodeId: 'node-1', runtimeState: runtimeState ?? 'READY', observedAt: new Date().toISOString(),
  });
  const captured = await withCapturedFetch(() => service.onModuleDestroy());
  const body = JSON.parse(Buffer.from(captured.init.body).toString('utf8'));
  assert.equal(body.runtimeState, 'DRAINING', 'shutdown refreshed READY selected-node liveness');
  delete process.env.GATEWAY_RUNTIME_REGISTRATION_URL;
  delete process.env.GATEWAY_RUNTIME_SECRET;
});


test('production gateway rejects IPv4-mapped special cross-provider endpoints', async () => {
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  for (const host of ['[::ffff:127.0.0.1]', '[::ffff:169.254.169.254]', '[::ffff:0.0.0.0]']) {
    process.env.NODE_ENV='production'; process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example'; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40';
    process.env.CONTROL_API_INTERNAL_URL='https://'+host; process.env.CONTROL_API_CALLBACK_URL='https://'+host+'/api/v1/internal/gateway/events';
    process.env.CONTROL_API_INBOUND_URL='https://'+host+'/api/v1/internal/gateway/inbound'; process.env.MEDIA_DOWNLOAD_BASE_URL='https://'+host+'/api/v1/internal/media';
    process.env.SSRF_ALLOWED_HOSTS=new URL('https://'+host).hostname.replace(/^\[|\]$/g, ''); process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32);
    const service = new RuntimeRegistrationService({ nodeId:'node-1' }, observabilityStub()); service.publish=async()=>undefined;
    await assert.rejects(() => service.onModuleInit(), /loopback|link-local|production endpoint|cross-provider/i, 'accepted '+host);
  }
  for (const key of ['NODE_ENV','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','GATEWAY_BIND_ADDRESS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET']) delete process.env[key];
});


test('production gateway rejects globally routable advertised HTTPS gateway URL', () => {
  process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40'; process.env.GATEWAY_INTERNAL_URL='https://8.8.8.8:2785';
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  const service = new RuntimeRegistrationService({}, observabilityStub());
  assert.throws(() => service.advertisedInternalURL(), /global|private|public|GATEWAY_INTERNAL_URL/i);
  for (const key of ['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL']) delete process.env[key];
});

test('production gateway rejects IPv4-mapped special advertised node URLs', () => {
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  for (const raw of ['https://[::ffff:127.0.0.1]:2785','https://[::ffff:169.254.169.254]:2785','https://[::ffff:0.0.0.0]:2785']) {
    process.env.NODE_ENV='production'; process.env.GATEWAY_INTERNAL_URL=raw;
    const service=new RuntimeRegistrationService({},observabilityStub());
    assert.throws(() => service.advertisedInternalURL(), /loopback|link-local|production endpoint|GATEWAY_INTERNAL_URL/i, 'accepted '+raw);
  }
  delete process.env.NODE_ENV; delete process.env.GATEWAY_INTERNAL_URL;
});

test('production advertised gateway URL requires HTTPS and governed host authority', () => {
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', { './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} } });
  const keys=['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS'];
  try {
    for (const raw of ['https://gateway.attacker.example:2785','http://10.20.30.40:2785','https://deploy:secret@gateway.private.example:2785','https://gateway.private.example:2785/runtime','https://gateway.private.example:2785?token=secret','https://gateway.private.example:0','https://gateway.private.example:65536']) {
      process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40'; process.env.GATEWAY_INTERNAL_URL=raw; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example';
      const service=new RuntimeRegistrationService({},observabilityStub());
      assert.throws(() => service.advertisedInternalURL(), /GATEWAY_INTERNAL_URL|HTTPS|approved|allow/i, 'accepted '+raw);
    }
  } finally { for (const key of keys) delete process.env[key]; }
});

test('production gateway requires a canonical authority-only Railway control origin', async () => {
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const keys=['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET','GATEWAY_CAPACITY','OPENWA_MAX_CONCURRENT_SESSIONS'];
  try {
    process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40'; process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example'; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example';
    process.env.CONTROL_API_CALLBACK_URL='https://control.internal.example/api/v1/internal/gateway/events'; process.env.CONTROL_API_INBOUND_URL='https://control.internal.example/api/v1/internal/gateway/inbound'; process.env.MEDIA_DOWNLOAD_BASE_URL='https://control.internal.example/api/v1/internal/media';
    process.env.SSRF_ALLOWED_HOSTS='control.internal.example'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32); process.env.GATEWAY_CAPACITY='10'; process.env.OPENWA_MAX_CONCURRENT_SESSIONS='10';
    for (const raw of ['https://deploy:secret@control.internal.example','https://control.internal.example/api','https://control.internal.example?token=secret','https://control.internal.example#fragment','https://control.internal.example:0']) {
      process.env.CONTROL_API_INTERNAL_URL=raw;
      const service=new RuntimeRegistrationService({nodeId:'node-control-origin'},observabilityStub()); service.publish=async()=>undefined;
      await assert.rejects(()=>service.onModuleInit(),/CONTROL_API_INTERNAL_URL|authority|cross-provider|port/i,'accepted '+raw);
    }
    process.env.CONTROL_API_INTERNAL_URL='https://Control.Internal.Example.:443/';
    const service=new RuntimeRegistrationService({nodeId:'node-control-origin'},observabilityStub()); service.publish=async()=>undefined;
    await service.onModuleInit();
    assert.equal(service.runtimeURL(),'https://control.internal.example/api/v1/internal/gateway-nodes/node-control-origin/runtime');
  } finally { for(const key of keys) delete process.env[key]; }
});

test('production gateway rejects credentials, fragments, and invalid ports in Railway endpoint URLs', async () => {
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const keys=['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET','GATEWAY_CAPACITY','OPENWA_MAX_CONCURRENT_SESSIONS'];
  try {
    process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40'; process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example'; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example'; process.env.CONTROL_API_INTERNAL_URL='https://control.internal.example';
    process.env.SSRF_ALLOWED_HOSTS='control.internal.example'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32); process.env.GATEWAY_CAPACITY='10'; process.env.OPENWA_MAX_CONCURRENT_SESSIONS='10';
    const valid={CONTROL_API_CALLBACK_URL:'https://control.internal.example/api/v1/internal/gateway/events',CONTROL_API_INBOUND_URL:'https://control.internal.example/api/v1/internal/gateway/inbound',MEDIA_DOWNLOAD_BASE_URL:'https://control.internal.example/api/v1/internal/media'};
    for (const [key,good] of Object.entries(valid)) {
      Object.assign(process.env,valid);
      for (const raw of [good.replace('https://','https://deploy:secret@'),good+'#fragment',good.replace('control.internal.example','control.internal.example:0')]) {
        process.env[key]=raw;
        const service=new RuntimeRegistrationService({nodeId:'node-control-endpoint'},observabilityStub()); service.publish=async()=>undefined;
        await assert.rejects(()=>service.onModuleInit(),new RegExp(key+'|cross-provider|port','i'),'accepted '+key+'='+raw);
      }
    }
  } finally { for(const key of keys) delete process.env[key]; }
});

test('queued heartbeat cannot publish READY after shutdown DRAINING', async () => {
  delete process.env.NODE_ENV; process.env.GATEWAY_RUNTIME_REGISTRATION_URL='http://control-api.internal/runtime'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32);
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const service=new RuntimeRegistrationService({nodeId:'node-1'},observabilityStub()); service.report=async state=>({nodeId:'node-1',runtimeState:state??'READY',observedAt:new Date().toISOString()});
  const original=global.fetch, states=[]; global.fetch=async (_url,init)=>{states.push(JSON.parse(Buffer.from(init.body).toString('utf8')).runtimeState); return {ok:true,status:200,text:async()=>''};};
  try { const queuedHeartbeat=()=>service.publish(); await service.onModuleDestroy(); await queuedHeartbeat(); assert.deepEqual(states,['DRAINING']); } finally { global.fetch=original; delete process.env.GATEWAY_RUNTIME_REGISTRATION_URL; delete process.env.GATEWAY_RUNTIME_SECRET; }
});

test('failed DRAINING publication is retried without publishing READY', async () => {
  delete process.env.NODE_ENV; process.env.GATEWAY_RUNTIME_REGISTRATION_URL='http://control-api.internal/runtime'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32);
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const service=new RuntimeRegistrationService({nodeId:'node-1'},observabilityStub()); service.report=async state=>({nodeId:'node-1',runtimeState:state??'READY',observedAt:new Date().toISOString()});
  const original=global.fetch, states=[]; let calls=0;
  global.fetch=async (_url,init)=>{calls++; states.push(JSON.parse(Buffer.from(init.body).toString('utf8')).runtimeState); return {ok:calls>=2,status:calls>=2?200:503,text:async()=>''};};
  try { await service.onModuleDestroy(); assert.equal(calls,2); assert.deepEqual(states,['DRAINING','DRAINING']); }
  finally { global.fetch=original; delete process.env.GATEWAY_RUNTIME_REGISTRATION_URL; delete process.env.GATEWAY_RUNTIME_SECRET; }
});
test('runtime report carries a stable bootStartedAt across heartbeats', async () => {
  process.env.GATEWAY_CAPACITY = '1';
  process.env.GATEWAY_RESOURCE_FILESYSTEM_PATH = '/';
  delete process.env.NODE_ENV; delete process.env.GATEWAY_INTERNAL_URL;
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const identity = { nodeId: 'node-boot-origin', describe: () => ({ nodeId: 'node-boot-origin', nodeVersion: 1, gatewayPoolId: 'pool-boot-origin', gatewayPoolVersion: 1, provider: 'OPENWA', engine: 'BAILEYS', adapterVersion: '0.13.0', capabilities: ['SEND_TEXT'] }) };
  const service = new RuntimeRegistrationService(identity, observabilityStub());
  const first = await service.report();
  await new Promise(resolve => setTimeout(resolve, 5));
  const second = await service.report();
  assert.equal(first.bootId, second.bootId);
  assert.equal(first.bootStartedAt, second.bootStartedAt, 'bootStartedAt drifted between heartbeats');
  assert.ok(new Date(first.bootStartedAt).getTime() <= new Date(first.observedAt).getTime(), 'bootStartedAt is after observedAt');
  delete process.env.GATEWAY_CAPACITY; delete process.env.GATEWAY_RESOURCE_FILESYSTEM_PATH;
});

test('gateway runtime capacity cannot exceed control-plane maximum', async () => {
  process.env.GATEWAY_CAPACITY = '1001';
  process.env.GATEWAY_RESOURCE_FILESYSTEM_PATH = '/';
  delete process.env.NODE_ENV; delete process.env.GATEWAY_INTERNAL_URL;
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
    './observability.service': { GatewayObservabilityService: class {} },
  });
  const identity = { nodeId: 'node-capacity', describe: () => ({ nodeId: 'node-capacity', nodeVersion: 1, gatewayPoolId: 'pool-capacity', gatewayPoolVersion: 1, provider: 'OPENWA', engine: 'BAILEYS', adapterVersion: '0.13.0', capabilities: ['SEND_TEXT'] }) };
  const service = new RuntimeRegistrationService(identity, observabilityStub());
  await assert.rejects(() => service.report(), /GATEWAY_CAPACITY|1000/i);
  delete process.env.GATEWAY_CAPACITY; delete process.env.GATEWAY_RESOURCE_FILESYSTEM_PATH;
});

test('production gateway fails startup when runtime capacity is invalid', async () => {
  const keys = ['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET','GATEWAY_CAPACITY'];
  try {
    process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40';
    process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example'; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example';
    process.env.CONTROL_API_INTERNAL_URL='https://control.internal.example'; process.env.CONTROL_API_CALLBACK_URL='https://control.internal.example/api/v1/internal/gateway/events';
    process.env.CONTROL_API_INBOUND_URL='https://control.internal.example/api/v1/internal/gateway/inbound'; process.env.MEDIA_DOWNLOAD_BASE_URL='https://control.internal.example/api/v1/internal/media';
    process.env.SSRF_ALLOWED_HOSTS='control.internal.example'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32); process.env.GATEWAY_CAPACITY='1500';
    const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
    const service=new RuntimeRegistrationService({nodeId:'node-1'},observabilityStub()); service.publish=async()=>true;
    await assert.rejects(()=>service.onModuleInit(),/GATEWAY_CAPACITY|1000/i);
  } finally { for (const key of keys) delete process.env[key]; }
});

test('production gateway requires explicit capacity coupled to the engine session limit', async () => {
  const keys = ['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET','GATEWAY_CAPACITY','OPENWA_MAX_CONCURRENT_SESSIONS'];
  try {
    process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40';
    process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example'; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example';
    process.env.CONTROL_API_INTERNAL_URL='https://control.internal.example'; process.env.CONTROL_API_CALLBACK_URL='https://control.internal.example/api/v1/internal/gateway/events';
    process.env.CONTROL_API_INBOUND_URL='https://control.internal.example/api/v1/internal/gateway/inbound'; process.env.MEDIA_DOWNLOAD_BASE_URL='https://control.internal.example/api/v1/internal/media';
    process.env.SSRF_ALLOWED_HOSTS='control.internal.example'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32); process.env.OPENWA_MAX_CONCURRENT_SESSIONS='10';
    const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
    let service=new RuntimeRegistrationService({nodeId:'node-1'},observabilityStub()); service.publish=async()=>true;
    await assert.rejects(()=>service.onModuleInit(),/GATEWAY_CAPACITY|required|explicit/i);
    process.env.GATEWAY_CAPACITY='9';
    service=new RuntimeRegistrationService({nodeId:'node-1'},observabilityStub()); service.publish=async()=>true;
    await assert.rejects(()=>service.onModuleInit(),/GATEWAY_CAPACITY|OPENWA_MAX_CONCURRENT_SESSIONS|match|capacity/i);
  } finally { for (const key of keys) delete process.env[key]; }
});

test('production gateway rejects a reusable configured boot ID', async () => {
  const keys = ['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET','GATEWAY_CAPACITY','OPENWA_MAX_CONCURRENT_SESSIONS','GATEWAY_BOOT_ID'];
  try {
    process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40'; process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example'; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example';
    process.env.CONTROL_API_INTERNAL_URL='https://control.internal.example'; process.env.CONTROL_API_CALLBACK_URL='https://control.internal.example/api/v1/internal/gateway/events'; process.env.CONTROL_API_INBOUND_URL='https://control.internal.example/api/v1/internal/gateway/inbound'; process.env.MEDIA_DOWNLOAD_BASE_URL='https://control.internal.example/api/v1/internal/media'; process.env.SSRF_ALLOWED_HOSTS='control.internal.example'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32); process.env.GATEWAY_CAPACITY='10'; process.env.OPENWA_MAX_CONCURRENT_SESSIONS='10'; process.env.GATEWAY_BOOT_ID='pinned-production-boot';
    const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
    const service=new RuntimeRegistrationService({nodeId:'node-1'},observabilityStub()); service.publish=async()=>true;
    await assert.rejects(()=>service.onModuleInit(),/GATEWAY_BOOT_ID|fresh|boot/i);
  } finally { for (const key of keys) delete process.env[key]; }
});

test('shutdown aborts in-flight READY before publishing DRAINING', async () => {
  delete process.env.NODE_ENV; process.env.GATEWAY_RUNTIME_REGISTRATION_URL='http://control-api.internal/runtime'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32);
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const service=new RuntimeRegistrationService({nodeId:'node-1'},observabilityStub()); service.report=async state=>({nodeId:'node-1',runtimeState:state??'READY',observedAt:new Date().toISOString()});
  const original=global.fetch, states=[], signals=[]; let releaseReady;
  global.fetch=async (_url,init)=>{ const state=JSON.parse(Buffer.from(init.body).toString('utf8')).runtimeState; states.push(state); signals.push(init.signal); if(state==='READY') return await new Promise((resolve,reject)=>{releaseReady=()=>resolve({ok:true,status:200,text:async()=>''}); init.signal.addEventListener('abort',()=>reject(new Error('aborted')), {once:true});}); return {ok:true,status:200,text:async()=>''}; };
  try { const heartbeat=service.publish(); while(states.length===0) await new Promise(r=>setTimeout(r,1)); const destroying=service.onModuleDestroy(); await new Promise(r=>setTimeout(r,20)); const aborted=signals[0].aborted; if(!aborted) releaseReady(); await Promise.all([heartbeat,destroying]); assert.equal(aborted,true,'shutdown did not abort in-flight READY heartbeat'); assert.deepEqual(states,['READY','DRAINING']); }
  finally { global.fetch=original; delete process.env.GATEWAY_RUNTIME_REGISTRATION_URL; delete process.env.GATEWAY_RUNTIME_SECRET; }
});

test('production gateway canonicalizes valid trailing-dot FQDN and rejects dotted reserved names', async () => {
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const keys=['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET','GATEWAY_CAPACITY','OPENWA_MAX_CONCURRENT_SESSIONS'];
  try { process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40'; process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example.:443/'; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example.'; process.env.CONTROL_API_INTERNAL_URL='https://control.internal.example'; process.env.CONTROL_API_CALLBACK_URL='https://control.internal.example/api/v1/internal/gateway/events'; process.env.CONTROL_API_INBOUND_URL='https://control.internal.example/api/v1/internal/gateway/inbound'; process.env.MEDIA_DOWNLOAD_BASE_URL='https://control.internal.example/api/v1/internal/media'; process.env.SSRF_ALLOWED_HOSTS='control.internal.example'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32); process.env.GATEWAY_CAPACITY='10'; process.env.OPENWA_MAX_CONCURRENT_SESSIONS='10'; const service=new RuntimeRegistrationService({nodeId:'node-1'},observabilityStub()); service.publish=async()=>undefined; await service.onModuleInit(); assert.equal(service.advertisedInternalURL(),'https://gateway.private.example'); for(const host of ['localhost.','host.docker.internal.']) { process.env.GATEWAY_INTERNAL_URL='https://'+host; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS=host; assert.throws(()=>service.advertisedInternalURL(),/Docker-local|loopback|approved|GATEWAY_INTERNAL_URL/i,host); } }
  finally { for(const key of keys) delete process.env[key]; }
});

test('production gateway rejects IP-like gateway allowlist entries even beside a valid DNS authority', async () => {
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const keys=['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET','GATEWAY_CAPACITY','OPENWA_MAX_CONCURRENT_SESSIONS'];
  try {
    process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='10.20.30.40'; process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example';
    process.env.CONTROL_API_INTERNAL_URL='https://control.internal.example'; process.env.CONTROL_API_CALLBACK_URL='https://control.internal.example/api/v1/internal/gateway/events'; process.env.CONTROL_API_INBOUND_URL='https://control.internal.example/api/v1/internal/gateway/inbound'; process.env.MEDIA_DOWNLOAD_BASE_URL='https://control.internal.example/api/v1/internal/media'; process.env.SSRF_ALLOWED_HOSTS='control.internal.example'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32); process.env.GATEWAY_CAPACITY='10'; process.env.OPENWA_MAX_CONCURRENT_SESSIONS='10';
    for(const host of ['10.20.30.50','fd00::1','010.020.030.050','0x0a.0x14.0x1e.0x28','167772161','10.1']) {
      process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example,'+host;
      const service=new RuntimeRegistrationService({nodeId:'node-allowlist'},observabilityStub()); service.publish=async()=>undefined;
      await assert.rejects(()=>service.onModuleInit(),/GATEWAY_RUNTIME_ALLOWED_HOSTS|gateway authority|production endpoint|DNS|IP/i,'accepted '+host);
    }
  } finally { for(const key of keys) delete process.env[key]; }
});

test('shutdown during READY report pre-fetch cannot publish READY after shutdown starts', async () => {
  delete process.env.NODE_ENV; process.env.GATEWAY_RUNTIME_REGISTRATION_URL='http://control-api.internal/runtime'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32);
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const service=new RuntimeRegistrationService({nodeId:'node-race'},observabilityStub());
  let releaseReadyReport, readyReportStarted=false;
  service.report=async state=>{ if(!state){ readyReportStarted=true; await new Promise(resolve=>{releaseReadyReport=resolve}); } return {nodeId:'node-race',runtimeState:state??'READY',observedAt:new Date().toISOString()}; };
  const original=global.fetch, states=[];
  global.fetch=async (_url,init)=>{states.push(JSON.parse(Buffer.from(init.body).toString('utf8')).runtimeState); return {ok:true,status:200,text:async()=>''};};
  try { const heartbeat=service.publish(); while(!readyReportStarted) await new Promise(r=>setTimeout(r,1)); const destroying=service.onModuleDestroy(); await new Promise(r=>setTimeout(r,5)); releaseReadyReport(); await Promise.all([heartbeat,destroying]); assert.deepEqual(states,['DRAINING']); }
  finally { global.fetch=original; delete process.env.GATEWAY_RUNTIME_REGISTRATION_URL; delete process.env.GATEWAY_RUNTIME_SECRET; }
});

test('production gateway rejects non-private GATEWAY_BIND_ADDRESS at startup', async () => {
  const { RuntimeRegistrationService }=loadModule('runtime-registration.service.ts',{ './gateway-identity.service':{GatewayIdentityService:class{}}, './observability.service':{GatewayObservabilityService:class{}} });
  const keys=['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL','GATEWAY_RUNTIME_ALLOWED_HOSTS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET'];
  try { process.env.NODE_ENV='production'; process.env.GATEWAY_BIND_ADDRESS='0.0.0.0'; process.env.GATEWAY_INTERNAL_URL='https://gateway.private.example'; process.env.GATEWAY_RUNTIME_ALLOWED_HOSTS='gateway.private.example'; process.env.CONTROL_API_INTERNAL_URL='https://control.internal.example'; process.env.CONTROL_API_CALLBACK_URL='https://control.internal.example/api/v1/internal/gateway/events'; process.env.CONTROL_API_INBOUND_URL='https://control.internal.example/api/v1/internal/gateway/inbound'; process.env.MEDIA_DOWNLOAD_BASE_URL='https://control.internal.example/api/v1/internal/media'; process.env.SSRF_ALLOWED_HOSTS='control.internal.example'; process.env.GATEWAY_RUNTIME_SECRET='r'.repeat(32); const service=new RuntimeRegistrationService({nodeId:'node-bind'},observabilityStub()); service.publish=async()=>true; await assert.rejects(()=>service.onModuleInit(),/GATEWAY_BIND_ADDRESS|private bind/i); }
  finally { for(const key of keys) delete process.env[key]; }
});
