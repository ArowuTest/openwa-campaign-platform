#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of ['typescript', path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript')]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for retained Baileys recovery tests');
}
const ts = loadTypeScript();

function loadLifecycle() {
  const filename = path.join(process.cwd(), 'services/openwa-gateway/src/retained-openwa/engine/adapters/baileys-lifecycle.ts');
  const source = fs.readFileSync(filename, 'utf8');
  const output = ts.transpileModule(source, { fileName: filename, compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, esModuleInterop: true } }).outputText;
  const module = { exports: {} };
  const engineStatus = { DISCONNECTED: 'disconnected', INITIALIZING: 'initializing', READY: 'ready', FAILED: 'failed', QR_READY: 'qr_ready' };
  const overrides = {
    'qrcode': { toDataURL: async () => 'data:qr' },
    'https-proxy-agent': { HttpsProxyAgent: class {} },
    'socks-proxy-agent': { SocksProxyAgent: class {} },
    '../interfaces/whatsapp-engine.interface': { EngineStatus: engineStatus },
    '../../common/errors/engine-not-ready.error': { EngineNotReadyError: class extends Error {} },
    './baileys-logger': { createBaileysLogger: () => ({}) },
  };
  const localRequire = specifier => overrides[specifier] ?? require(specifier);
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(module.exports, localRequire, module, filename, path.dirname(filename));
  return module.exports.BaileysLifecycle;
}
function host(config) {
  return {
    logger: { log() {}, warn() {}, error() {}, debug() {} }, authPath: '/tmp/auth', config,
    liveCalls: new Map(), extractPhone: () => null, upsertContacts: async () => {}, upsertChats: async () => {},
    addLidMappings: async () => {}, handleMessagesUpsert: async () => {}, handleMessagesUpdate: async () => {},
    logContactEvent() {}, handleGroupParticipantsUpdate() {}, handleGroupsUpdate() {}, handleCallEvents() {},
    captureHistoryMessages: async () => {}, hydrateNames: async () => {},
    getOnQRCode: () => undefined, getOnReady: () => undefined, getOnDisconnected: () => undefined,
    getOnError: () => undefined, getOnStateChanged: () => undefined, getOnCredentialTeardownStarted: () => undefined,
  };
}

(function run() {
  const BaileysLifecycle = loadLifecycle();
  const originalSetTimeout = global.setTimeout;
  const originalRandom = Math.random;
  try {
    const timers = [];
    global.setTimeout = (callback, delay) => { timers.push({ callback, delay }); return { fake: true }; };
    Math.random = () => 0;

    const disabled = new BaileysLifecycle(host({ reconnectMode: 'DISABLED', reconnectMaxAttempts: 0, reconnectBaseDelayMs: 7000, reconnectStabilityResetMs: 240000 }));
    disabled.scheduleReconnect();
    assert.equal(timers.length, 0, 'DISABLED Baileys recovery must not schedule a transient reconnect');

    const bounded = new BaileysLifecycle(host({ reconnectMode: 'BOUNDED', reconnectMaxAttempts: 2, reconnectBaseDelayMs: 7000, reconnectStabilityResetMs: 240000 }));
    bounded.reconnectAttempts = 2;
    bounded.scheduleReconnect();
    assert.equal(timers.length, 0, 'exhausted bounded Baileys recovery must not schedule another reconnect');

    const delayed = new BaileysLifecycle(host({ reconnectMode: 'BOUNDED', reconnectMaxAttempts: 2, reconnectBaseDelayMs: 7000, reconnectStabilityResetMs: 240000 }));
    delayed.scheduleReconnect();
    assert.equal(timers.length, 1, 'bounded Baileys recovery should schedule an available attempt');
    assert.equal(timers[0].delay, 7000, 'governed Baileys reconnect base delay must be applied');
    console.log('Retained Baileys governed recovery policy test passed.');
  } finally {
    global.setTimeout = originalSetTimeout;
    Math.random = originalRandom;
  }
})();
