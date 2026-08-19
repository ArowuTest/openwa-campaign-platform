#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of ['typescript', path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript')]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for session-authority fencing tests');
}
const ts = loadTypeScript();

class HttpException extends Error {}
class ConflictException extends HttpException {}
class UnauthorizedException extends HttpException {}
class ServiceUnavailableException extends HttpException {}
class NotImplementedException extends HttpException {}
const nestMock = {
  Injectable: () => target => target,
  Inject: () => () => {},
  ConflictException,
  UnauthorizedException,
  ServiceUnavailableException,
  NotImplementedException,
};
function loadModule(relativePath, overrides = {}) {
  const filename = path.join(process.cwd(), relativePath);
  const source = require('node:fs').readFileSync(filename, 'utf8');
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
    return require(specifier);
  };
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(
    module.exports, localRequire, module, filename, path.dirname(filename),
  );
  return module.exports;
}

function request(version) {
  return {    idempotencyKey: `fence-${version}-0000000000000000`,
    provider: 'OPENWA', engine: 'BAILEYS',
    gatewayPoolId: 'pool-1', gatewayPoolVersion: 4,
    gatewayAdapterVersion: '0.8.28',
    gatewayNodeId: 'node-1', gatewayNodeVersion: 9,
    sessionId: 'session-1', sessionLeaseVersion: version,
    sessionConfigurationVersion: 2,
    authorityExpiresAt: new Date(Date.now() + 60_000).toISOString(),
    routeReference: 'campaign-1:recipient-1',
    recipientMsisdn: '+2348000000000', messageType: 'TEXT',
    body: 'governed message', clientReference: 'client-1',
  };
}

async function run() {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'openwa-authority-fence-'));
  process.env.GATEWAY_SESSION_AUTHORITY_DIR = directory;
  const identity = {
    provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1',
    gatewayPoolVersion: 4, adapterVersion: '0.8.28', nodeId: 'node-1', nodeVersion: 9,
  };
  const { SessionAuthorityService } = loadModule(
    'services/openwa-gateway/src/session-authority.service.ts',
    { './gateway-identity.service': { GatewayIdentityService: class {} } },
  );
  const authority = new SessionAuthorityService(identity);
  await authority.onModuleInit();
  let providerSends = 0;
  const provider = {
    health: async () => ({ ready: true, status: 'READY' }),
    send: async () => {
      providerSends += 1;
      return { accepted: true, providerMessageId: `provider-${providerSends}`, acceptedAt: new Date().toISOString() };
    },
  };
  let idempotencyExecutions = 0;
  const idempotency = { execute: async (_request, operation) => { idempotencyExecutions += 1; return operation(); } };
  const pipelines = { run: async (_sessionId, operation) => operation(), status: () => ({}), recordProviderSuccess: () => undefined, recordProviderFailure: () => undefined };
  const { GatewayMessagingService } = loadModule(
    'services/openwa-gateway/src/gateway-messaging.service.ts',
    {
      './idempotency.service': { IdempotencyService: class {} },
      './session-pipeline.service': { SessionPipelineService: class {} },
      './provider/provider.token': { MESSAGING_PROVIDER: Symbol('provider') },
      './session-authority.service': { SessionAuthorityService: class {} },
    },
  );
  const messaging = new GatewayMessagingService(provider, idempotency, pipelines, authority);

  const current = request(2);
  const sent = await messaging.send(current);
  assert.equal(sent.accepted, true);
  assert.equal(providerSends, 1, 'current fenced owner should reach provider once');
  const stale = { ...request(1), idempotencyKey: 'stale-fence-0000000000000001' };
  await assert.rejects(() => messaging.send(stale), ConflictException);
  assert.equal(providerSends, 1, 'stale fenced owner must not reach provider send');

  const conflicting = { ...request(2), gatewayNodeVersion: 8, idempotencyKey: 'conflicting-fence-000000001' };
  await assert.rejects(() => messaging.send(conflicting), UnauthorizedException);
  assert.equal(providerSends, 1, 'conflicting authority must not reach provider send');

  const reboundIdentity = { ...identity, gatewayPoolId: 'pool-2', adapterVersion: '0.8.29' };
  const reboundAuthority = new SessionAuthorityService(reboundIdentity);
  await reboundAuthority.onModuleInit();
  const rebound = { ...request(2), gatewayPoolId: 'pool-2', gatewayAdapterVersion: '0.8.29', idempotencyKey: 'rebound-fence-000000000000001' };
  await assert.rejects(() => reboundAuthority.validate(rebound), ConflictException, 'same lease must not rebind durable pool/adapter authority');

  const siblingIdentity = { ...identity, engine: 'WHATSAPP_WEB_JS' };
  const siblingAuthority = new SessionAuthorityService(siblingIdentity);
  await siblingAuthority.onModuleInit();
  const siblingEngine = { ...request(2), engine: 'WHATSAPP_WEB_JS', idempotencyKey: 'sibling-engine-fence-000000001' };
  await assert.rejects(() => siblingAuthority.validate(siblingEngine), ConflictException, 'same lease must not rebind durable provider/engine authority');

  let signalPipeline; let releasePipeline;
  const pipelineEntered = new Promise(resolve => { signalPipeline = resolve; });
  const pipelineRelease = new Promise(resolve => { releasePipeline = resolve; });
  const delayedPipelines = {
    run: async (_sessionId, operation) => { signalPipeline(); await pipelineRelease; return operation(); },
    status: () => ({}), recordProviderSuccess: () => undefined, recordProviderFailure: () => undefined,
  };
  const delayedMessaging = new GatewayMessagingService(provider, idempotency, delayedPipelines, authority);
  const lease10 = { ...request(10), sessionId: 'session-delayed', idempotencyKey: 'delayed-fence-00000000000010' };
  const idempotencyBeforeDelayed = idempotencyExecutions;
  const delayedSend = delayedMessaging.send(lease10);
  await pipelineEntered;
  const lease11 = { ...lease10, sessionLeaseVersion: 11, idempotencyKey: 'delayed-fence-00000000000011' };
  await authority.validate(lease11);
  releasePipeline();
  await assert.rejects(delayedSend, ConflictException, 'superseded lease reached provider submission after queue wait');
  assert.equal(providerSends, 1, 'superseded delayed lease reached provider send');
  assert.equal(idempotencyExecutions, idempotencyBeforeDelayed, 'superseded lease opened the idempotency ambiguity boundary before provider submission');
  const deleted = { ...request(20), sessionId: 'session-deleted', idempotencyKey: 'deleted-fence-00000000000020' };
  await authority.validate(deleted);
  await authority.tombstone('session-deleted');
  const tombstoneFile = (await fs.readdir(directory)).find(name => name.endsWith('.deleted.json'));
  assert.ok(tombstoneFile, 'durable delete tombstone was not published');
  const originalTombstone = await fs.readFile(path.join(directory, tombstoneFile), 'utf8');
  await new Promise(resolve => setTimeout(resolve, 5));
  await authority.tombstone('session-deleted');
  const repeatedTombstone = await fs.readFile(path.join(directory, tombstoneFile), 'utf8');
  assert.equal(repeatedTombstone, originalTombstone, 'repeated delete rewrote immutable tombstone evidence');
  const restartedAuthority = new SessionAuthorityService(identity);
  await restartedAuthority.onModuleInit();
  await assert.rejects(
    () => restartedAuthority.validate({ ...deleted, sessionLeaseVersion: 21, idempotencyKey: 'deleted-fence-00000000000021' }),
    ConflictException,
    'deleted session authority was resurrected after gateway restart',
  );
  let finalHealthChecks = 0;
  let finalHealthIdempotencyExecutions = 0;
  let finalHealthProviderSends = 0;
  const finalHealthProvider = {
    health: async () => {
      finalHealthChecks += 1;
      return finalHealthChecks === 1 ? { ready: true, status: 'READY' } : { ready: false, status: 'UNAVAILABLE' };
    },
    send: async () => { finalHealthProviderSends += 1; return { accepted: true }; },
  };
  const finalHealthIdempotency = { execute: async (_request, operation) => { finalHealthIdempotencyExecutions += 1; return operation(); } };
  const passPipelines = { run: async (_sessionId, operation) => operation(), status: () => ({}), recordProviderSuccess: () => undefined, recordProviderFailure: () => undefined };
  const passAuthorities = { validate: async () => undefined, submitIfCurrent: async (_request, operation) => operation() };
  const finalHealthMessaging = new GatewayMessagingService(finalHealthProvider, finalHealthIdempotency, passPipelines, passAuthorities);
  await assert.rejects(() => finalHealthMessaging.send({ ...request(30), sessionId: 'session-health-drop', idempotencyKey: 'health-drop-0000000000000030' }), ServiceUnavailableException);
  assert.equal(finalHealthIdempotencyExecutions, 0, 'final health failure opened idempotency ambiguity boundary');
  assert.equal(finalHealthProviderSends, 0, 'final health failure reached provider send');
  await fs.rm(directory, { recursive: true, force: true });
  delete process.env.GATEWAY_SESSION_AUTHORITY_DIR;
  console.log('Session authority fencing test passed.');
}

run().catch(error => {
  console.error(error?.stack ?? error);
  process.exit(1);
});
