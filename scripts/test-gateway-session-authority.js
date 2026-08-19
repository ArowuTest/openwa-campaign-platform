#!/usr/bin/env node
const assert = require('node:assert/strict');
const test = require('node:test');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of [
    'typescript',
    path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript'),
  ]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for gateway authority tests');
}

class HttpException extends Error {}
class ConflictException extends HttpException {}
class UnauthorizedException extends HttpException {}
const nestMock = {
  Injectable: () => target => target,
  ConflictException,
  UnauthorizedException,
};
function loadSessionAuthority(overrides = {}) {
  const filename = path.join(
    process.cwd(),
    'services/openwa-gateway/src/session-authority.service.ts',
  );
  const source = require('node:fs').readFileSync(filename, 'utf8');
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
    if (specifier === './gateway-identity.service') {
      return { GatewayIdentityService: class {} };
    }
    if (overrides[specifier]) return overrides[specifier];
    return require(specifier);
  };
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(
    module.exports,
    localRequire,
    module,
    filename,
    path.dirname(filename),
  );
  return module.exports;
}
test('session authority never regresses a concurrent lease fence', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'gateway-authority-'));
  process.env.GATEWAY_SESSION_AUTHORITY_DIR = root;
  const realFs = require('node:fs/promises');
  let signalLeaseTwo;
  let signalLeaseThree;
  const leaseTwoAtRename = new Promise(resolve => { signalLeaseTwo = resolve; });
  const leaseThreeRenamed = new Promise(resolve => { signalLeaseThree = resolve; });
  const delayedFs = {
    ...realFs,
    rename: async (source, target) => {
      const record = JSON.parse(await realFs.readFile(source, 'utf8'));
      if (record.sessionLeaseVersion === 2) {
        signalLeaseTwo();
        await Promise.race([
          leaseThreeRenamed,
          new Promise(resolve => setTimeout(resolve, 250)),
        ]);
      }
      await realFs.rename(source, target);
      if (record.sessionLeaseVersion === 3) signalLeaseThree();
    },
  };
  const identity = {
    provider: 'OPENWA',
    engine: 'BAILEYS',
    gatewayPoolId: 'pool-1',
    gatewayPoolVersion: 1,
    adapterVersion: '0.13.0',
    nodeId: 'node-1',
    nodeVersion: 1,
  };
  const { SessionAuthorityService } = loadSessionAuthority({
    'node:fs/promises': delayedFs,
  });
  const service = new SessionAuthorityService(identity);
  await service.onModuleInit();
  const request = {
    provider: 'OPENWA',
    engine: 'BAILEYS',
    gatewayPoolId: 'pool-1',
    gatewayPoolVersion: 1,
    gatewayAdapterVersion: '0.13.0',
    gatewayNodeId: 'node-1',
    gatewayNodeVersion: 1,
    sessionId: 'session-1',
    sessionLeaseVersion: 1,
    sessionConfigurationVersion: 1,
    authorityExpiresAt: new Date(Date.now() + 5 * 60_000).toISOString(),
    routeReference: 'route-1',
  };
  await service.validate(request);
  const leaseTwo = service.validate({
    ...request,
    sessionLeaseVersion: 2,
    routeReference: 'route-2',
  });
  await leaseTwoAtRename;
  const leaseThree = service.validate({
    ...request,
    sessionLeaseVersion: 3,
    routeReference: 'route-3',
  });
  await Promise.all([leaseTwo, leaseThree]);

  const files = await fs.readdir(root);
  assert.equal(files.length, 1);
  const stored = JSON.parse(await fs.readFile(path.join(root, files[0]), 'utf8'));
  assert.equal(
    stored.sessionLeaseVersion,
    3,
    'durable session authority must retain the highest accepted lease fence',
  );
  delete process.env.GATEWAY_SESSION_AUTHORITY_DIR;
  await fs.rm(root, { recursive: true, force: true });
});

test('session authority fsyncs the accepted fence before returning success', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'gateway-authority-sync-'));
  process.env.GATEWAY_SESSION_AUTHORITY_DIR = root;
  const realFs = require('node:fs/promises');
  const synced = [];
  const durableFs = {
    ...realFs,
    open: async (target, flags, mode) => {
      const handle = await realFs.open(target, flags, mode);
      return {
        writeFile: (...args) => handle.writeFile(...args),
        sync: async () => { synced.push(String(target)); await handle.sync(); },
        close: () => handle.close(),
      };
    },
  };
  const identity = {
    provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1', gatewayPoolVersion: 1,
    adapterVersion: '0.13.0', nodeId: 'node-1', nodeVersion: 1,
  };
  const { SessionAuthorityService } = loadSessionAuthority({ 'node:fs/promises': durableFs });
  const service = new SessionAuthorityService(identity);
  await service.onModuleInit();
  await service.validate({
    provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1', gatewayPoolVersion: 1,
    gatewayAdapterVersion: '0.13.0', gatewayNodeId: 'node-1', gatewayNodeVersion: 1,
    sessionId: 'session-sync', sessionLeaseVersion: 9, sessionConfigurationVersion: 3,
    authorityExpiresAt: new Date(Date.now() + 5 * 60_000).toISOString(), routeReference: 'route-sync',
  });
  assert.ok(synced.some(target => target.endsWith('.tmp')), 'authority file contents were not fsynced');
  assert.ok(synced.includes(root), 'authority directory rename was not fsynced');
  delete process.env.GATEWAY_SESSION_AUTHORITY_DIR;
  await fs.rm(root, { recursive: true, force: true });
});

test('session authority rejects expired and excessive horizons without persisting evidence', async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'gateway-authority-expiry-'));
  process.env.GATEWAY_SESSION_AUTHORITY_DIR = root;
  process.env.GATEWAY_AUTHORITY_MAX_HORIZON_SECONDS = '900';
  const identity = {
    provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1', gatewayPoolVersion: 1,
    adapterVersion: '0.13.0', nodeId: 'node-1', nodeVersion: 1,
  };
  const { SessionAuthorityService } = loadSessionAuthority();
  const service = new SessionAuthorityService(identity);
  await service.onModuleInit();
  const base = {
    provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1', gatewayPoolVersion: 1,
    gatewayAdapterVersion: '0.13.0', gatewayNodeId: 'node-1', gatewayNodeVersion: 1,
    sessionId: 'session-expiry', sessionLeaseVersion: 1, sessionConfigurationVersion: 1,
    routeReference: 'route-expiry',
  };
  await assert.rejects(
    () => service.validate({ ...base, authorityExpiresAt: new Date(Date.now() - 1000).toISOString() }),
    UnauthorizedException,
  );
  await assert.rejects(
    () => service.validate({ ...base, authorityExpiresAt: new Date(Date.now() + 901_000).toISOString() }),
    UnauthorizedException,
  );
  assert.equal((await fs.readdir(root)).length, 0);
  delete process.env.GATEWAY_SESSION_AUTHORITY_DIR;
  delete process.env.GATEWAY_AUTHORITY_MAX_HORIZON_SECONDS;
  await fs.rm(root, { recursive: true, force: true });
});
