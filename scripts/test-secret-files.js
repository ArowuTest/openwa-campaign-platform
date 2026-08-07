#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of [
    'typescript',
    path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript'),
  ]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for secret-file tests');
}

function loadSecretModule() {
  const ts = loadTypeScript();
  const filename = path.join(process.cwd(), 'services/openwa-gateway/src/secret-file.ts');
  const source = fs.readFileSync(filename, 'utf8');
  const output = ts.transpileModule(source, {
    fileName: filename,
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
  }).outputText;
  const module = { exports: {} };
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(
    module.exports, require, module, filename, path.dirname(filename),
  );
  return module.exports;
}
function testRejectsUnsafeOrdinaryFile(resolveSecretFiles) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'openwa-secret-'));
  const secret = path.join(root, 'secret');
  fs.writeFileSync(secret, 'ordinary-secret-value', { mode: 0o644 });
  process.env.NODE_ENV = 'production';
  process.env.TEST_ORDINARY_SECRET = '';
  process.env.TEST_ORDINARY_SECRET_FILE = secret;
  assert.throws(
    () => resolveSecretFiles(['TEST_ORDINARY_SECRET']),
    /group or other users/,
  );
  fs.rmSync(root, { recursive: true, force: true });
}

function testAcceptsReadOnlyRuntimeSecret(resolveSecretFiles) {
  const secret = String(process.env.TEST_READONLY_RUNTIME_SECRET_FILE ?? '').trim();
  if (!secret) return;
  assert(secret.startsWith('/run/secrets/'), 'runtime secret fixture must live under /run/secrets');
  assert.throws(() => fs.openSync(secret, 'w'), /EACCES|EPERM|EROFS/);
  process.env.NODE_ENV = 'production';
  process.env.TEST_RUNTIME_SECRET = '';
  process.env.TEST_RUNTIME_SECRET_FILE = secret;
  resolveSecretFiles(['TEST_RUNTIME_SECRET']);
  assert.equal(process.env.TEST_RUNTIME_SECRET, 'runtime-secret-value');
}

const { resolveSecretFiles } = loadSecretModule();
testRejectsUnsafeOrdinaryFile(resolveSecretFiles);
testAcceptsReadOnlyRuntimeSecret(resolveSecretFiles);
console.log('Secret-file tests passed.');
