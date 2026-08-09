#!/usr/bin/env node
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of ['typescript', path.join(process.cwd(), 'apps/admin-web/node_modules/typescript'), path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript')]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for gateway durability tests');
}

const ts = loadTypeScript();
class HttpException extends Error {}
class ConflictException extends HttpException {}
class ServiceUnavailableException extends HttpException {}
class UnauthorizedException extends HttpException {}
const nestMock = {
  Injectable: () => target => target,
  ConflictException,
  ServiceUnavailableException,
  UnauthorizedException
};

function loadModule(relativePath) {
  const filename = path.join(process.cwd(), relativePath);
  const source = require('node:fs').readFileSync(filename, 'utf8');
  const output = ts.transpileModule(source, {
    fileName: filename,
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
      experimentalDecorators: true,
      esModuleInterop: true
    }
  }).outputText;
  const module = { exports: {} };
  const localRequire = specifier => specifier === '@nestjs/common' ? nestMock : require(specifier);
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(module.exports, localRequire, module, filename, path.dirname(filename));
  return module.exports;
}

async function testReplayEvidence() {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'openwa-replay-'));
  process.env.GATEWAY_COMMAND_NONCE_DIR = directory;
  process.env.GATEWAY_COMMAND_NONCE_CLEANUP_INTERVAL_MS = '5000';
  const { CommandReplayService } = loadModule('services/openwa-gateway/src/command-replay.service.ts');
  const service = new CommandReplayService();
  await service.onModuleInit();

  const now = Math.floor(Date.now() / 1000);
  await service.claim('nonce-0000000001', 'a'.repeat(64), now, now + 300);
  await assert.rejects(() => service.claim('nonce-0000000001', 'a'.repeat(64), now, now + 300), UnauthorizedException);

  const corruptNonce = 'nonce-0000000002';
  const corruptPath = path.join(directory, `${crypto.createHash('sha256').update(corruptNonce).digest('hex')}.json`);
  await fs.writeFile(corruptPath, '{corrupt', { mode: 0o600 });
  const restarted = new CommandReplayService();
  await restarted.onModuleInit();
  await fs.access(corruptPath);
  await assert.rejects(() => restarted.claim(corruptNonce, 'b'.repeat(64), now, now + 300), UnauthorizedException);
  await fs.rm(directory, { recursive: true, force: true });
}

function sendRequest(key) {
  return {
    idempotencyKey: key,
    provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1', gatewayPoolVersion: 1,
    gatewayAdapterVersion: '0.8.28', gatewayNodeId: 'node-1', gatewayNodeVersion: 1,
    sessionId: 'session-1', sessionLeaseVersion: 1, sessionConfigurationVersion: 1,
    authorityExpiresAt: new Date(Date.now() + 60_000).toISOString(), routeReference: 'route-1',
    recipientMsisdn: '+2348000000000', messageType: 'TEXT', body: 'hello', clientReference: 'client-1'
  };
}


async function testObservabilitySanitisation() {
  const { GatewayObservabilityService, SanitisedJsonLogger } = loadModule('services/openwa-gateway/src/observability.service.ts');
  const logger = new SanitisedJsonLogger();
  const writes = [];
  const originalWrite = process.stdout.write;
  process.stdout.write = value => { writes.push(String(value)); return true; };
  try {
    logger.error('recipient=+2348012345678 qrPayload=pairing-secret session_token=session-secret message_content=approved-message DATABASE_URL=postgres://user:password@example/db');
  } finally {
    process.stdout.write = originalWrite;
  }
  const output = writes.join('');
  for (const forbidden of ['+2348012345678', 'pairing-secret', 'session-secret', 'approved-message', 'user:password']) {
    assert.equal(output.includes(forbidden), false, `sanitised logger leaked ${forbidden}`);
  }

  const observability = new GatewayObservabilityService();
  const rawMsisdn = '+2348012345678';
  observability.observeHttp('POST', '/v1/messages', 202, 0.01);
  observability.increment('gateway_test_total', { 'bad-key': 'first', 'bad key': 'discarded', '9code': 'value' });
  observability.gauge('gateway_runtime_last_success_seconds', 123, { node: 'node-1' });
  observability.observe('gateway_runtime_duration_seconds', 0.25, { node: 'node-1' });
  observability.observe('gateway_runtime_duration_seconds', 0.75, { node: 'node-1' });
  const metrics = observability.prometheus();
  assert.equal(metrics.includes(rawMsisdn), false, 'Prometheus output leaked recipient MSISDN');
  assert.equal(metrics.includes('/v1/messages'), true, 'static send route missing from HTTP metrics');
  assert.equal(metrics.includes('bad-key='), false);
  assert.equal(metrics.includes('bad key='), false);
  assert.equal((metrics.match(/bad_key=/g) ?? []).length, 1);
  assert.equal(metrics.includes('_9code="value"'), true);
  assert.equal(metrics.includes('# TYPE gateway_runtime_last_success_seconds gauge'), true);
  assert.equal(metrics.includes('gateway_runtime_last_success_seconds{node="node-1",service="openwa-gateway"} 123'), true);
  assert.equal(metrics.includes('# TYPE gateway_runtime_duration_seconds summary'), true);
  assert.equal(metrics.includes('gateway_runtime_duration_seconds_sum{node="node-1",service="openwa-gateway"} 1'), true);
  assert.equal(metrics.includes('gateway_runtime_duration_seconds_count{node="node-1",service="openwa-gateway"} 2'), true);
}

async function testIdempotencyDurability() {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'openwa-idempotency-'));
  process.env.GATEWAY_IDEMPOTENCY_DIR = directory;
  process.env.IDEMPOTENCY_CLEANUP_INTERVAL_MS = '5000';
  const { IdempotencyService } = loadModule('services/openwa-gateway/src/idempotency.service.ts');
  const service = new IdempotencyService();
  await service.onModuleInit();
  const request = sendRequest('idem-key-0000000000000001');
  let submissions = 0;
  const operation = async () => {
    submissions += 1;
    await new Promise(resolve => setTimeout(resolve, 30));
    return { accepted: true, providerMessageId: 'provider-1', acceptedAt: new Date().toISOString(), duplicate: false };
  };
  const [first, second] = await Promise.all([service.execute(request, operation), service.execute(request, operation)]);
  assert.equal(submissions, 1, 'identical concurrent requests must submit once');
  assert.equal(first.duplicate, false);
  assert.equal(second.duplicate, true);

  const restarted = new IdempotencyService();
  await restarted.onModuleInit();
  const replay = await restarted.execute(request, async () => { throw new Error('provider must not be called'); });
  assert.equal(replay.duplicate, true);
  assert.equal(replay.providerMessageId, 'provider-1');

  const persisted = (await Promise.all((await fs.readdir(directory)).map(name => fs.readFile(path.join(directory, name), 'utf8')))).join('\n');
  for (const forbidden of [request.recipientMsisdn, request.body, request.routeReference, request.clientReference]) {
    assert.equal(persisted.includes(forbidden), false, `durable gateway idempotency storage leaked send payload field: ${forbidden}`);
  }

  const unknownRequest = sendRequest('idem-key-0000000000000002');
  await assert.rejects(() => service.execute(unknownRequest, async () => { throw new Error('ambiguous provider failure'); }), /ambiguous provider failure/);
  const status = await service.describe(unknownRequest.idempotencyKey);
  assert.equal(status?.state, 'UNKNOWN');
  await assert.rejects(() => service.execute(unknownRequest, operation), ConflictException);
  await fs.rm(directory, { recursive: true, force: true });
}

(async () => {
  await testReplayEvidence();
  await testIdempotencyDurability();
  await testObservabilitySanitisation();
  console.log('Gateway durability tests passed.');
})().catch(error => {
  console.error(error?.stack ?? error);
  process.exit(1);
});
