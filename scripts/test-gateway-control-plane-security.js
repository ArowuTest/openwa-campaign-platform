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
  process.env.GATEWAY_INTERNAL_URL = 'http://10.20.30.40:2785';
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
  for (const key of ['NODE_ENV','GATEWAY_INTERNAL_URL','GATEWAY_BIND_ADDRESS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET']) delete process.env[key];
});

test('production gateway rejects Docker special and loopback cross-provider endpoints', async () => {
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  for (const host of ['host.docker.internal', 'bridge.docker.internal', '127.0.0.1', '[::1]', '169.254.10.20']) {
    process.env.NODE_ENV = 'production';
    process.env.GATEWAY_INTERNAL_URL = 'http://10.20.30.40:2785';
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
  for (const key of ['NODE_ENV','GATEWAY_INTERNAL_URL','GATEWAY_BIND_ADDRESS','CONTROL_API_INTERNAL_URL','CONTROL_API_CALLBACK_URL','CONTROL_API_INBOUND_URL','MEDIA_DOWNLOAD_BASE_URL','SSRF_ALLOWED_HOSTS','GATEWAY_RUNTIME_SECRET']) delete process.env[key];
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

test('production HTTP advertised gateway URL must equal configured host bind address', () => {
  process.env.NODE_ENV = 'production';
  process.env.GATEWAY_BIND_ADDRESS = '10.20.30.40';
  process.env.GATEWAY_INTERNAL_URL = 'http://10.20.30.99:2785';
  const { RuntimeRegistrationService } = loadModule('runtime-registration.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} }, './observability.service': { GatewayObservabilityService: class {} },
  });
  const service = new RuntimeRegistrationService({}, observabilityStub());
  assert.throws(() => service.advertisedInternalURL(), /bind address|GATEWAY_BIND_ADDRESS|advertised/i);
  for (const key of ['NODE_ENV','GATEWAY_BIND_ADDRESS','GATEWAY_INTERNAL_URL']) delete process.env[key];
});

test('Hostinger gateway passes bind address into production runtime validation', () => {
  const text = fs.readFileSync(path.join(process.cwd(), 'infrastructure/compose/compose.hostinger-openwa-gateway.yaml'), 'utf8');
  const matches = text.match(/GATEWAY_BIND_ADDRESS/g) ?? [];
  assert.ok(matches.length >= 2, 'GATEWAY_BIND_ADDRESS is used for host port binding but is not passed into the gateway environment');
});