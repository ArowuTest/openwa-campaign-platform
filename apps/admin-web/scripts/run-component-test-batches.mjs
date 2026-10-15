// CI-safe component regression runner.
//
// The single Vitest process can exhaust its Node heap after many jsdom test
// files, despite maxWorkers=1. Fresh processes per bounded batch retain exact
// *.component.test.tsx coverage without weakening assertions or increasing the
// runner's global memory allowance.
import { spawnSync } from 'node:child_process';
import { readdirSync } from 'node:fs';
import { resolve } from 'node:path';

const files = readdirSync('tests')
  .filter((name) => name.endsWith('.component.test.tsx'))
  .sort()
  .map((name) => 'tests/' + name);
const batchSize = 6;

if (files.length === 0) {
  console.error('[admin-web] No component test files discovered; failing closed.');
  process.exit(1);
}
const batches = [];
for (let i = 0; i < files.length; i += batchSize) {
  batches.push(files.slice(i, i + batchSize));
}
console.log('[admin-web] ' + files.length + ' component test files across ' + batches.length + ' isolated batches.');
if (process.argv.includes('--dry-run')) {
  for (const [index, batch] of batches.entries()) {
    console.log('[admin-web] batch ' + (index + 1) + ': ' + batch.join(', '));
  }
  process.exit(0);
}
for (const [index, batch] of batches.entries()) {
  console.log('[admin-web] running component batch ' + (index + 1) + '/' + batches.length + ' (' + batch.length + ' files)');
  const test = spawnSync(process.execPath, [
    resolve('node_modules/vitest/vitest.mjs'),
    'run',
    '--maxWorkers=1',
    ...batch
  ], {
    stdio: 'inherit',
    env: process.env,
    windowsHide: true
  });
  if (test.error || test.status !== 0) {
    if (test.error) console.error('[admin-web] failed to launch Vitest:', test.error.message);
    console.error('[admin-web] component batch ' + (index + 1) + ' FAILED; remaining batches were not skipped as a success.');
    process.exit(test.status && test.status > 0 ? test.status : 1);
  }
}
console.log('[admin-web] all ' + files.length + ' component test files passed across all batches.');
