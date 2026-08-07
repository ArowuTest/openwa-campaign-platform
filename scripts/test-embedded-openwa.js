#!/usr/bin/env node
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');

function loadTypeScript() {
  for (const candidate of [
    'typescript',
    path.join(process.cwd(), 'services/openwa-gateway/node_modules/typescript'),
    path.join(process.cwd(), 'apps/admin-web/node_modules/typescript'),
  ]) {
    try { return require(candidate); } catch {}
  }
  throw new Error('TypeScript is required for embedded OpenWA tests');
}

const ts = loadTypeScript();
class HttpException extends Error {}
class BadRequestException extends HttpException {}
class ConflictException extends HttpException {}
class NotFoundException extends HttpException {}
class ServiceUnavailableException extends HttpException {}
const nestMock = {
  Injectable: () => target => target,
  Logger: class { log() {} warn() {} error() {} debug() {} },
  BadRequestException,
  ConflictException,
  NotFoundException,
  ServiceUnavailableException,
};

const fakeEngines = [];
class FakeEngine {
  constructor(config) {
    this.config = config;
    this.status = 'disconnected';
    this.qr = null;
    this.phone = null;
    this.pushName = null;
    this.probeAlive = true;
    this.calls = [];
    fakeEngines.push(this);
  }
  async initialize(callbacks) {
    this.callbacks = callbacks;
    this.status = 'initializing';
    callbacks.onStateChanged?.('initializing');
  }
  async disconnect() { this.calls.push('disconnect'); this.status = 'disconnected'; }
  async logout() { this.calls.push('logout'); this.status = 'disconnected'; }
  async destroy() { this.calls.push('destroy'); this.status = 'disconnected'; }
  async forceDestroy() { this.calls.push('forceDestroy'); this.status = 'disconnected'; }
  getStatus() { return this.status; }
  async probeLiveness() { return this.status === 'ready' && this.probeAlive; }
  getQRCode() { return this.qr; }
  async requestPairingCode(phone) { this.calls.push(`pair:${phone}`); return '12345678'; }
  getPhoneNumber() { return this.phone; }
  getPushName() { return this.pushName; }
  async sendTextMessage(chatId, text) { this.calls.push(`text:${chatId}:${text}`); return { id: 'msg-1', timestamp: 1700000000 }; }
  async sendImageMessage(chatId, media) { this.calls.push(`image:${chatId}:${media.data}`); return { id: 'msg-2', timestamp: 1700000001 }; }
  async sendVideoMessage(chatId, media) { this.calls.push(`video:${chatId}:${media.data}`); return { id: 'msg-3', timestamp: 1700000002 }; }
  async sendDocumentMessage(chatId, media) { this.calls.push(`document:${chatId}:${media.filename}`); return { id: 'msg-4', timestamp: 1700000003 }; }
  triggerQR(value) { this.qr = value; this.status = 'qr_ready'; this.callbacks.onQRCode?.(value); }
  triggerReady(phone = '2348000000000', pushName = 'Test') {
    this.phone = phone; this.pushName = pushName; this.status = 'ready';
    this.callbacks.onReady?.(phone, pushName); this.callbacks.onStateChanged?.('ready');
  }
  triggerAck(messageId, status) { this.callbacks.onMessageAck?.(messageId, status); }
  triggerInbound(message) { this.callbacks.onMessage?.(message); }
}
function loadModule(relativePath, overrides = {}) {
  const filename = path.join(process.cwd(), relativePath);
  const source = require('node:fs').readFileSync(filename, 'utf8');
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
    if (overrides[specifier]) return overrides[specifier];
    return require(specifier);
  };
  new Function('exports', 'require', 'module', '__filename', '__dirname', output)(
    module.exports, localRequire, module, filename, path.dirname(filename),
  );
  return module.exports;
}
function serviceModule() {
  const engineStatus = {
    DISCONNECTED: 'disconnected', INITIALIZING: 'initializing', QR_READY: 'qr_ready',
    AUTHENTICATING: 'authenticating', READY: 'ready', ACTION_REQUIRED: 'action_required', FAILED: 'failed',
  };
  return loadModule('services/openwa-gateway/src/provider/embedded-openwa-engine.service.ts', {
    '../gateway-identity.service': { GatewayIdentityService: class {} },
    '../provider-event-publisher.service': { ProviderEventPublisherService: class {} },
    '../inbound-message-publisher.service': { InboundMessagePublisherService: class {} },
    '../observability.service': { GatewayObservabilityService: class {} },
    '../retained-openwa/engine/adapters/whatsapp-web-js.adapter': { WhatsAppWebJsAdapter: FakeEngine },
    '../retained-openwa/engine/adapters/baileys.adapter': { BaileysAdapter: FakeEngine },
    '../retained-openwa/engine/interfaces/whatsapp-engine.interface': { EngineStatus: engineStatus },
  });
}

function providerModule() {
  return loadModule('services/openwa-gateway/src/provider/openwa.provider.ts', {
    './embedded-openwa-engine.service': { EmbeddedOpenWAEngineService: class {} },
  });
}

const waitTurn = () => new Promise(resolve => setTimeout(resolve, 20));
async function testLifecycleAndEvents() {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'embedded-openwa-'));
  process.env.OPENWA_SESSION_REGISTRY_DIR = path.join(root, 'registry');
  process.env.OPENWA_SESSION_DATA_DIR = path.join(root, 'auth');
  process.env.PUPPETEER_EXECUTABLE_PATH = '/usr/bin/chromium';
  process.env.PUPPETEER_ARGS = '--no-sandbox,--disable-dev-shm-usage';
  fakeEngines.length = 0;
  const providerEvents = [];
  const inboundEvents = [];
  const identity = { engine: 'BAILEYS' };
  const publisher = {
    create: input => ({ schemaVersion: '1.0', occurredAt: input.occurredAt ?? new Date().toISOString(), ...input }),
    queue: async event => { providerEvents.push(event); },
  };
  const inbound = { queue: async event => { inboundEvents.push(event); } };
  const observability = { increment() {}, gauge() {}, observe() {} };
  const { EmbeddedOpenWAEngineService } = serviceModule();
  const service = new EmbeddedOpenWAEngineService(identity, publisher, inbound, observability);
  await service.onModuleInit();

  const created = await service.createSession('session-1');
  assert.equal(created.status, 'disconnected');
  const started = await service.startSession('session-1');
  assert.equal(started.status, 'initializing');
  const engine = fakeEngines.at(-1);
  assert(engine, 'start must instantiate an engine');
  assert.equal(engine.callbacks.claimStuckAuthRecovery?.(), true, 'first stuck-auth recovery claim must be accepted');
  assert.equal(engine.callbacks.claimStuckAuthRecovery?.(), false, 'stuck-auth recovery must be one-shot per start episode');

  await service.createSession('session-2');
  process.env.OPENWA_MAX_CONCURRENT_SESSIONS = '1';
  await assert.rejects(() => service.startSession('session-2'), ConflictException);
  delete process.env.OPENWA_MAX_CONCURRENT_SESSIONS;

  engine.triggerQR('data:image/png;base64,qr');
  assert.equal((await service.qr('session-1')).qr, 'data:image/png;base64,qr');
  assert.equal((await service.health('session-1')).status, 'PAIRING');
  assert.equal((await service.pairingCode('session-1', '2348000000000')).pairingCode, '12345678');

  engine.triggerReady();
  assert.equal((await service.health('session-1')).ready, true);
  assert.equal((await service.getSession('session-1')).phone, '2348000000000');

  const send = await service.send({
    sessionId: 'session-1', recipientMsisdn: '+2348111111111', messageType: 'text', body: 'hello',
    clientReference: 'client-1', engine: 'BAILEYS', provider: 'OPENWA', idempotencyKey: 'idem-1',
  });
  assert.equal(send.providerMessageId, 'msg-1');
  assert.equal(providerEvents.some(event => event.eventType === 'message.sent' && event.providerMessageId === 'msg-1'), true);

  engine.triggerAck('msg-1', 'delivered');
  await waitTurn();
  assert.equal(providerEvents.some(event => event.eventType === 'message.delivered' && event.providerMessageId === 'msg-1'), true);
  engine.triggerInbound({
    id: 'in-1', from: '2348222222222@c.us', to: '', chatId: '2348222222222@c.us', body: 'STOP',
    type: 'text', timestamp: 1700000010, fromMe: false, isGroup: false, kind: 'direct',
  });
  await waitTurn();
  assert.equal(inboundEvents.length, 1);
  assert.equal(inboundEvents[0].senderMsisdn, '+2348222222222');
  assert.equal(inboundEvents[0].messageText, 'STOP');

  const stopped = await service.stopSession('session-1');
  assert.equal(stopped.status, 'disconnected');
  assert.equal(engine.calls.includes('disconnect'), true);

  const serviceAfterRestart = new EmbeddedOpenWAEngineService(identity, publisher, inbound, observability);
  await serviceAfterRestart.onModuleInit();
  const reloaded = await serviceAfterRestart.getSession('session-1');
  assert.equal(reloaded.status, 'disconnected');
  assert.equal(reloaded.engineLoaded, false);

  await serviceAfterRestart.startSession('session-1');
  const restartedEngine = fakeEngines.at(-1);
  assert.equal(restartedEngine.callbacks.claimStuckAuthRecovery?.(), true, 'explicit restart must re-arm stuck-auth recovery');
  await serviceAfterRestart.logoutSession('session-1');
  assert.equal(restartedEngine.calls.includes('logout'), true);
  await serviceAfterRestart.deleteSession('session-1');
  await assert.rejects(() => serviceAfterRestart.getSession('session-1'), NotFoundException);
  await service.onModuleDestroy();
  await serviceAfterRestart.onModuleDestroy();
  await fs.rm(root, { recursive: true, force: true });
}

async function testWatchdogRecoversSilentDeadReadySession() {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'embedded-openwa-watchdog-'));
  process.env.OPENWA_SESSION_REGISTRY_DIR = path.join(root, 'registry');
  process.env.OPENWA_SESSION_DATA_DIR = path.join(root, 'auth');
  process.env.OPENWA_WATCHDOG_INTERVAL_MS = '1000';
  process.env.OPENWA_WATCHDOG_PROBE_TIMEOUT_MS = '100';
  process.env.OPENWA_RECONNECT_BASE_DELAY_MS = '5000';
  fakeEngines.length = 0;
  const identity = { engine: 'WHATSAPP_WEB_JS' };
  const publisher = { create: input => ({ schemaVersion: '1.0', ...input }), queue: async () => {} };
  const inbound = { queue: async () => {} };
  const observability = { increment() {}, gauge() {}, observe() {} };
  const { EmbeddedOpenWAEngineService } = serviceModule();
  const service = new EmbeddedOpenWAEngineService(identity, publisher, inbound, observability);
  await service.onModuleInit();
  await service.createSession('watchdog-session');
  await service.startSession('watchdog-session');
  const engine = fakeEngines.at(-1);
  engine.triggerReady();
  engine.probeAlive = false;
  await new Promise(resolve => setTimeout(resolve, 2300));
  assert.equal(engine.calls.includes('forceDestroy'), true, 'watchdog must tear down a silently dead READY engine');
  assert.equal((await service.health('watchdog-session')).status, 'DISCONNECTED');
  await service.onModuleDestroy();
  delete process.env.OPENWA_WATCHDOG_INTERVAL_MS;
  delete process.env.OPENWA_WATCHDOG_PROBE_TIMEOUT_MS;
  delete process.env.OPENWA_RECONNECT_BASE_DELAY_MS;
  await fs.rm(root, { recursive: true, force: true });
}

async function testProviderDelegatesWithoutFetch() {
  const calls = [];
  const embedded = new Proxy({}, {
    get(_target, property) {
      return async (...args) => { calls.push([property, ...args]); return property === 'health' ? { ready: true, status: 'READY', checkedAt: new Date().toISOString() } : { accepted: true, acceptedAt: new Date().toISOString() }; };
    },
  });
  const { OpenWAProvider } = providerModule();
  const provider = new OpenWAProvider(embedded);
  await provider.health('session-1');
  await provider.createSession('session-1');
  await provider.startSession('session-1');
  await provider.stopSession('session-1');
  await provider.logoutSession('session-1');
  await provider.deleteSession('session-1');
  assert.deepEqual(calls.map(call => call[0]), ['health', 'createSession', 'startSession', 'stopSession', 'logoutSession', 'deleteSession']);
}

(async () => {
  await testLifecycleAndEvents();
  await testWatchdogRecoversSilentDeadReadySession();
  await testProviderDelegatesWithoutFetch();
  console.log('Embedded OpenWA engine tests passed.');
})().catch(error => {
  console.error(error?.stack ?? error);
  process.exit(1);
});
