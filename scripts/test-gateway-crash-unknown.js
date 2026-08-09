#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

function loadTypeScript() {
  for (const candidate of [
    'typescript',
    '/app/node_modules/typescript',
    path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript'),
  ]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for gateway crash tests');
}

const ts = loadTypeScript();
class HttpException extends Error {}
class ConflictException extends HttpException {}
class ServiceUnavailableException extends HttpException {}
const nestMock = {
  Injectable: () => target => target,
  ConflictException,
  ServiceUnavailableException,
};
function loadIdempotencyService() {
  const filename = path.join(process.cwd(), 'services/openwa-gateway/src/idempotency.service.ts');
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
  const localRequire = specifier => specifier === '@nestjs/common' ? nestMock : require(specifier);
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(
    module.exports, localRequire, module, filename, path.dirname(filename),
  );
  return module.exports.IdempotencyService;
}

function request() {
  return {
    idempotencyKey: 'crash-unknown-000000000001',
    provider: 'OPENWA', engine: 'BAILEYS', gatewayPoolId: 'pool-1', gatewayPoolVersion: 1,
    gatewayAdapterVersion: '0.13.0+platform.1', gatewayNodeId: 'node-1', gatewayNodeVersion: 1,
    sessionId: 'session-1', sessionLeaseVersion: 1, sessionConfigurationVersion: 1,
    authorityExpiresAt: new Date(Date.now() + 60_000).toISOString(),
    routeReference: 'campaign-1:recipient-1', recipientMsisdn: '+2348000000000',
    messageType: 'text', body: 'approved message', clientReference: 'client-1',
  };
}

async function childMode(directory) {
  process.env.GATEWAY_IDEMPOTENCY_DIR = directory;
  process.env.IDEMPOTENCY_PENDING_MAXIMUM_MINUTES = '60';
  const IdempotencyService = loadIdempotencyService();
  const service = new IdempotencyService();
  await service.onModuleInit();
  void service.execute(request(), async () => {
    process.stdout.write('OPERATION_STARTED\n');
    await new Promise(() => {});
  });
  await new Promise(() => {});
}

function killDuringProviderOperation(directory) {
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [__filename, 'child', directory], {
      cwd: process.cwd(), stdio: ['ignore', 'pipe', 'pipe'], env: process.env,
    });
    let output = '';
    let killed = false;
    const timer = setTimeout(() => {
      if (!killed) { child.kill('SIGKILL'); reject(new Error('child did not enter provider operation')); }
    }, 10_000);
    child.stdout.on('data', chunk => {
      output += chunk.toString();
      if (!killed && output.includes('OPERATION_STARTED')) {
        killed = true;
        child.kill('SIGKILL');
      }
    });
    child.stderr.on('data', chunk => { output += chunk.toString(); });
    child.on('error', error => { clearTimeout(timer); reject(error); });
    child.on('exit', () => {
      clearTimeout(timer);
      if (!killed) return reject(new Error(`child exited before provider operation: ${output}`));
      resolve();
    });
  });
}

async function parentMode() {
  const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'openwa-crash-unknown-'));
  process.env.GATEWAY_IDEMPOTENCY_DIR = directory;
  process.env.IDEMPOTENCY_PENDING_MAXIMUM_MINUTES = '60';
  try {
    await killDuringProviderOperation(directory);
    const IdempotencyService = loadIdempotencyService();
    const restarted = new IdempotencyService();
    await restarted.onModuleInit();
    const status = await restarted.describe(request().idempotencyKey);
    assert.equal(status?.state, 'UNKNOWN', 'crash-left PENDING submission must recover as UNKNOWN on restart');
    let resubmissions = 0;
    await assert.rejects(() => restarted.execute(request(), async () => {
      resubmissions += 1;
      return { accepted: true, providerMessageId: 'must-not-send', acceptedAt: new Date().toISOString() };
    }), ConflictException);
    assert.equal(resubmissions, 0, 'UNKNOWN crash outcome must never auto-resubmit');
    console.log('Gateway kill-during-send UNKNOWN recovery test passed.');
  } finally {
    await fs.rm(directory, { recursive: true, force: true });
  }
}

if (process.argv[2] === 'child') {
  childMode(process.argv[3]).catch(error => { console.error(error?.stack ?? error); process.exit(1); });
} else {
  parentMode().catch(error => { console.error(error?.stack ?? error); process.exit(1); });
}
