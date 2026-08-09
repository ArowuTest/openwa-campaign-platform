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
  const idempotency = { execute: async (_request, operation) => operation() };
  const pipelines = { run: async (_sessionId, operation) => operation(), status: () => ({}) };
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

  await fs.rm(directory, { recursive: true, force: true });
  delete process.env.GATEWAY_SESSION_AUTHORITY_DIR;
  console.log('Session authority fencing test passed.');
}

run().catch(error => {
  console.error(error?.stack ?? error);
  process.exit(1);
});
