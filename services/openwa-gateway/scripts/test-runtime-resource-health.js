'use strict';

const assert = require('node:assert/strict');
const { collectContainerResourceHealth } = require('../dist/runtime-registration.service.js');

(async () => {
  assert.equal(typeof collectContainerResourceHealth, 'function', 'runtime resource collector is not exported');
  const value = await collectContainerResourceHealth('/workspace');
  assert.equal(value.scope, 'CONTAINER');
  assert.equal(value.filesystemPath, '/workspace');
  assert.ok(Number.isSafeInteger(value.diskTotalBytes) && value.diskTotalBytes > 0);
  assert.ok(Number.isSafeInteger(value.diskFreeBytes) && value.diskFreeBytes >= 0);
  assert.ok(Number.isSafeInteger(value.diskAvailableBytes) && value.diskAvailableBytes >= 0);
  assert.ok(Number.isSafeInteger(value.inodesTotal) && value.inodesTotal > 0);
  assert.ok(Number.isSafeInteger(value.inodesFree) && value.inodesFree >= 0);
  assert.ok(Number.isSafeInteger(value.processId) && value.processId > 0);
  assert.ok(Number.isSafeInteger(value.processUptimeSeconds) && value.processUptimeSeconds >= 0);
  assert.ok(Number.isSafeInteger(value.openFileDescriptorCount) && value.openFileDescriptorCount >= 0);
  assert.ok(Number.isSafeInteger(value.networkRxBytes) && value.networkRxBytes >= 0);
  assert.ok(Number.isSafeInteger(value.networkTxBytes) && value.networkTxBytes >= 0);
  assert.ok(Number.isSafeInteger(value.networkInterfaceCount) && value.networkInterfaceCount >= 1);
  console.log('Gateway container resource-health collector test passed.');
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
