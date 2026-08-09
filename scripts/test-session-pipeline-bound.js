#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of ['typescript', path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript')]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for session pipeline tests');
}

const ts = loadTypeScript();
class ServiceUnavailableException extends Error {}
const nestMock = { Injectable: () => target => target, ServiceUnavailableException };

function loadPipeline() {
  const filename = path.join(process.cwd(), 'services/openwa-gateway/src/session-pipeline.service.ts');
  const source = fs.readFileSync(filename, 'utf8');
  const output = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, experimentalDecorators: true },
    fileName: filename,
  }).outputText;
  const module = { exports: {} };
  const localRequire = specifier => specifier === '@nestjs/common' ? nestMock : require(specifier);
  new Function('require', 'module', 'exports', '__filename', '__dirname', output)(localRequire, module, module.exports, filename, path.dirname(filename));
  return module.exports.SessionPipelineService;
}

(async () => {
  process.env.SESSION_IN_FLIGHT_LIMIT = '3';
  process.env.SESSION_QUEUE_LIMIT = '100';
  process.env.SESSION_MESSAGES_PER_MINUTE = '100000';
  process.env.SESSION_MIN_MESSAGES_PER_MINUTE = '100000';
  const SessionPipelineService = loadPipeline();
  const pipeline = new SessionPipelineService();
  let active = 0;
  let maxObserved = 0;
  const tasks = Array.from({ length: 30 }, (_, index) => pipeline.run('session-load-test', async () => {
    active += 1;
    maxObserved = Math.max(maxObserved, active);
    await new Promise(resolve => setTimeout(resolve, 5 + (index % 3)));
    active -= 1;
    return index;
  }));
  const results = await Promise.all(tasks);
  assert.equal(results.length, 30);
  assert.equal(pipeline.status('session-load-test').inFlightLimit, 3);
  assert.ok(maxObserved <= 3, `observed ${maxObserved} concurrent operations with configured limit 3`);
  assert.equal(maxObserved, 3, 'load did not exercise the configured concurrency bound');
  assert.equal(pipeline.status('session-load-test').active, 0);
  assert.equal(pipeline.status('session-load-test').queued, 0);
  console.log(`Session pipeline bound test passed; max observed in-flight=${maxObserved}.`);
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
