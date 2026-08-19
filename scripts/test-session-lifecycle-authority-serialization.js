#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of ['typescript', path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript')]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for lifecycle authority tests');
}
const ts = loadTypeScript();
class ConflictException extends Error {}
const nestMock = {
  Injectable: () => target => target,
  Inject: () => () => undefined,
  ConflictException,
  UnauthorizedException: class extends Error {},
  ServiceUnavailableException: class extends Error {},
  NotImplementedException: class extends Error {},
};
function loadModule(relativePath, overrides = {}) {
  const filename = path.join(process.cwd(), relativePath);
  const source = require('node:fs').readFileSync(filename, 'utf8');
  const output = ts.transpileModule(source, {
    fileName: filename,
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, experimentalDecorators: true, esModuleInterop: true },
  }).outputText;
  const module = { exports: {} };
  const localRequire = specifier => {
    if (specifier === '@nestjs/common') return nestMock;
    if (Object.prototype.hasOwnProperty.call(overrides, specifier)) return overrides[specifier];
    return require(specifier);
  };
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(
    module.exports, localRequire, module, filename, path.dirname(filename),
  );
  return module.exports;
}

async function verifyAuthoritySerialization() {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'openwa-lifecycle-authority-'));
  process.env.GATEWAY_SESSION_AUTHORITY_DIR = directory;
  const { SessionAuthorityService } = loadModule('services/openwa-gateway/src/session-authority.service.ts', {
    './gateway-identity.service': { GatewayIdentityService: class {} },
  });
  const authority = new SessionAuthorityService({
    provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1', gatewayPoolVersion: 1,
    adapterVersion: '0.8.28', nodeId: 'node-1', nodeVersion: 1,
  });
  await authority.onModuleInit();
  assert.equal(typeof authority.runIfNotTombstoned, 'function', 'session lifecycle side effects need a tombstone-serialised authority guard');
  let entered;
  let release;
  const operationEntered = new Promise(resolve => { entered = resolve; });
  const operationRelease = new Promise(resolve => { release = resolve; });
  const guarded = authority.runIfNotTombstoned('session-race', async () => {
    entered();
    await operationRelease;
    return 'completed';
  });
  await operationEntered;
  let tombstoneFinished = false;
  const deleting = authority.tombstone('session-race').then(() => { tombstoneFinished = true; });
  await new Promise(resolve => setTimeout(resolve, 20));
  assert.equal(tombstoneFinished, false, 'tombstone overtook an already-authorised lifecycle side effect');
  release();
  assert.equal(await guarded, 'completed');
  await deleting;
  await assert.rejects(
    () => authority.runIfNotTombstoned('session-race', async () => 'must-not-run'),
    ConflictException,
    'lifecycle side effect reopened after durable tombstone',
  );
  await fs.rm(directory, { recursive: true, force: true });
  delete process.env.GATEWAY_SESSION_AUTHORITY_DIR;
}

async function verifyGatewayUsesSerialGuard() {
  const { GatewayMessagingService } = loadModule('services/openwa-gateway/src/gateway-messaging.service.ts', {
    './idempotency.service': { IdempotencyService: class {} },
    './session-pipeline.service': { SessionPipelineService: class {} },
    './provider/provider.token': { MESSAGING_PROVIDER: Symbol('provider') },
    './session-authority.service': { SessionAuthorityService: class {} },
  });
  const guarded = [];
  const authorities = {
    runIfNotTombstoned: async (sessionId, operation) => { guarded.push(sessionId); return operation(); },
    assertNotTombstoned: async () => { throw new Error('non-serial tombstone check used'); },
    tombstone: async () => undefined,
  };
  const provider = {
    createSession: async id => ({ id }),
    startSession: async id => ({ id }),
    qr: async () => ({ qr: 'ok' }),
    pairingCode: async () => ({ code: 'ok' }),
  };
  const pipelines = {
    beginStart: () => undefined,
    finishStart: () => undefined,
    drainForTeardown: async () => undefined,
    finishTeardown: () => undefined,
  };
  const gateway = new GatewayMessagingService(provider, {}, pipelines, authorities);
  await gateway.createSession('session-create');
  await gateway.startSession('session-start');
  await gateway.qr('session-qr');
  await gateway.pairingCode('session-pair', '2348012345678');
  assert.deepEqual(guarded, ['session-create', 'session-start', 'session-qr', 'session-pair']);
}

(async () => {
  await verifyAuthoritySerialization();
  await verifyGatewayUsesSerialGuard();
  console.log('Session lifecycle authority serialization test passed.');
})().catch(error => {
  console.error(error?.stack ?? error);
  process.exit(1);
});
