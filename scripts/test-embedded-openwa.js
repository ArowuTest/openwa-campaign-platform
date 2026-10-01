#!/usr/bin/env node
const assert = require('node:assert/strict');
// This suite isolates other behavior; real boot ownership is covered separately.
const ownedRuntime = { assertSessionOwned() {}, onSessionOwnershipLost() { return () => {}; } };
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
  triggerDisconnected(reason = 'transport lost') { this.status = 'disconnected'; this.callbacks.onDisconnected?.(reason); }
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
  const service = new EmbeddedOpenWAEngineService(identity, publisher, inbound, observability, ownedRuntime);
  await service.onModuleInit();

  const created = await service.createSession('session-1');
  assert.equal(created.status, 'disconnected');
  const proxy = { url: 'socks5://proxy-user:proxy-secret@proxy.example:1080', type: 'socks5' };
  const runtime = {
    reconnectMode: 'BOUNDED', reconnectMaxAttempts: 4, reconnectBaseDelayMs: 7000,
    reconnectStabilityResetMs: 240000, watchdogProbeTimeoutMs: 9000, watchdogFailureThreshold: 3,
    engineTeardownTimeoutMs: 45000, source: 'GOVERNED_CONFIGURATION', configurationId: 'config-1',
    scopeType: 'SENDER_SESSION', scopeId: 'session-1', version: 7,
  };
  const started = await service.startSession('session-1', { proxy, runtime });
  assert.equal(started.status, 'initializing');
  const engine = fakeEngines.at(-1);
  assert(engine, 'start must instantiate an engine');
  assert.equal(engine.config.proxyUrl, proxy.url);
  assert.equal(engine.config.proxyType, proxy.type);
  assert.equal(engine.config.reconnectMode, 'BOUNDED', 'governed reconnect mode must reach the retained Baileys adapter');
  assert.equal(engine.config.reconnectMaxAttempts, 4, 'governed reconnect budget must reach the retained Baileys adapter');
  assert.equal(engine.config.reconnectBaseDelayMs, 7000, 'governed reconnect base delay must reach the retained Baileys adapter');
  assert.equal(engine.config.reconnectStabilityResetMs, 240000, 'governed stability reset must reach the retained Baileys adapter');
  const registryFiles = await fs.readdir(process.env.OPENWA_SESSION_REGISTRY_DIR);
  const registryText = (await Promise.all(registryFiles.map(file => fs.readFile(path.join(process.env.OPENWA_SESSION_REGISTRY_DIR, file), 'utf8')))).join('\n');
  assert.equal(registryText.includes('proxy-secret'), false, 'proxy credentials must never persist in the worker session registry');
  assert.equal(registryText.includes('proxy.example'), false, 'proxy endpoint must never persist in the worker session registry');
  assert.equal(registryText.includes('config-1'), false, 'governed recovery authority must not persist in the worker session registry');
  const governedHealth = await service.health('session-1');
  assert.equal(governedHealth.runtimeConfiguration?.source, 'GOVERNED_CONFIGURATION');
  assert.equal(governedHealth.runtimeConfiguration?.configurationId, 'config-1');
  assert.equal(governedHealth.runtimeConfiguration?.watchdogFailureThreshold, 3);
  await assert.rejects(() => service.startSession('session-1', { proxy, runtime: { ...runtime, reconnectBaseDelayMs: 8000 } }), ConflictException);
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

  await service.createSession('session-3');
  const runtime3 = { ...runtime, configurationId: 'config-3', scopeId: 'session-3' };
  await service.startSession('session-3', { proxy, runtime: runtime3 });
  const logoutProxyEngine = fakeEngines.at(-1);
  await service.logoutSession('session-3');
  assert.equal(logoutProxyEngine.calls.includes('logout'), true);
  await service.startSession('session-3');
  const afterLogoutEngine = fakeEngines.at(-1);
  assert.equal(afterLogoutEngine.config.proxyUrl, undefined, 'logout must clear in-memory proxy authority before a later start');
  assert.equal(afterLogoutEngine.config.proxyType, undefined, 'logout must not retain proxy type after control-plane authority can change');
  assert.equal((await service.health('session-3')).runtimeConfiguration?.source, 'DEPLOYMENT_BOOTSTRAP', 'logout must clear governed recovery authority before a later start');
  await service.deleteSession('session-3');

  const serviceAfterRestart = new EmbeddedOpenWAEngineService(identity, publisher, inbound, observability, ownedRuntime);
  await serviceAfterRestart.onModuleInit();
  const reloaded = await serviceAfterRestart.getSession('session-1');
  assert.equal(reloaded.status, 'disconnected');
  assert.equal(reloaded.engineLoaded, false);

  await serviceAfterRestart.startSession('session-1');
  const restartedEngine = fakeEngines.at(-1);
  assert.equal(restartedEngine.config.proxyUrl, undefined, 'proxy credentials must be supplied again by the control plane after worker restart');
  assert.equal(restartedEngine.config.proxyType, undefined, 'proxy type must not survive through the persistent session registry');
  assert.equal((await serviceAfterRestart.health('session-1')).runtimeConfiguration?.source, 'DEPLOYMENT_BOOTSTRAP', 'governed recovery authority must be supplied again after worker restart');
  assert.equal(restartedEngine.callbacks.claimStuckAuthRecovery?.(), true, 'explicit restart must re-arm stuck-auth recovery');
  await serviceAfterRestart.logoutSession('session-1');
  assert.equal(restartedEngine.calls.includes('logout'), true);
  await serviceAfterRestart.deleteSession('session-1');
  await assert.rejects(() => serviceAfterRestart.getSession('session-1'), NotFoundException);
  await service.onModuleDestroy();
  await serviceAfterRestart.onModuleDestroy();
  await fs.rm(root, { recursive: true, force: true });
}

async function testExplicitRestartUsesCurrentProxyConfiguration() {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'embedded-openwa-proxy-reset-'));
  process.env.OPENWA_SESSION_REGISTRY_DIR = path.join(root, 'registry');
  process.env.OPENWA_SESSION_DATA_DIR = path.join(root, 'auth');
  process.env.OPENWA_RECONNECT_BASE_DELAY_MS = '5000';
  fakeEngines.length = 0;
  const identity = { engine: 'WHATSAPP_WEB_JS', gatewayPoolId: 'gateway-1' };
  const publisher = { create: input => ({ schemaVersion: '1.0', ...input }), queue: async () => {} };
  const inbound = { queue: async () => {} };
  const observability = { increment() {}, gauge() {}, observe() {} };
  const { EmbeddedOpenWAEngineService } = serviceModule();
  const service = new EmbeddedOpenWAEngineService(identity, publisher, inbound, observability, ownedRuntime);
  await service.onModuleInit();
  await service.createSession('proxy-reset-session');
  await service.startSession('proxy-reset-session', { proxy: { url: 'socks5://proxy.example:1080', type: 'socks5' } });
  const first = fakeEngines.at(-1);
  assert.equal(first.config.proxy?.url, 'socks5://proxy.example:1080', 'first engine must prove the proxy was active');
  first.triggerDisconnected();
  assert.equal((await service.health('proxy-reset-session')).status, 'DISCONNECTED', 'disconnect must detach the old engine before restart');
  await service.startSession('proxy-reset-session');
  const second = fakeEngines.at(-1);
  assert.notEqual(second, first, 'explicit start after disconnect must create a fresh engine');
  assert.equal(second.config.proxy, undefined, 'explicit start without proxy must not reuse the previous proxy');
  await service.stopSession('proxy-reset-session');
  await service.deleteSession('proxy-reset-session');
  await service.onModuleDestroy();
  delete process.env.OPENWA_RECONNECT_BASE_DELAY_MS;
  await fs.rm(root, { recursive: true, force: true });
}

async function testEngineSpecificDeploymentRecoveryDefaults() {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'embedded-openwa-engine-defaults-'));
  process.env.OPENWA_SESSION_REGISTRY_DIR = path.join(root, 'registry');
  process.env.OPENWA_SESSION_DATA_DIR = path.join(root, 'auth');
  delete process.env.OPENWA_RECONNECT_BASE_DELAY_MS;
  fakeEngines.length = 0;
  const publisher = { create: input => ({ schemaVersion: '1.0', ...input }), queue: async () => {} };
  const inbound = { queue: async () => {} };
  const observability = { increment() {}, gauge() {}, observe() {} };
  const { EmbeddedOpenWAEngineService } = serviceModule();

  const baileys = new EmbeddedOpenWAEngineService({ engine: 'BAILEYS', gatewayPoolId: 'gateway-1' }, publisher, inbound, observability, ownedRuntime);
  await baileys.onModuleInit();
  await baileys.createSession('baileys-defaults');
  await baileys.startSession('baileys-defaults');
  assert.equal((await baileys.health('baileys-defaults')).runtimeConfiguration?.reconnectBaseDelayMs, 1000, 'Baileys deployment evidence must match its native one-second reconnect base');
  assert.equal(fakeEngines.at(-1).config.reconnectBaseDelayMs, undefined, 'Baileys native reconnect base must remain unset when deployment does not override it');
  await baileys.stopSession('baileys-defaults');
  await baileys.deleteSession('baileys-defaults');
  await baileys.onModuleDestroy();

  const wwjs = new EmbeddedOpenWAEngineService({ engine: 'WHATSAPP_WEB_JS', gatewayPoolId: 'gateway-1' }, publisher, inbound, observability, ownedRuntime);
  await wwjs.onModuleInit();
  await wwjs.createSession('wwjs-defaults');
  await wwjs.startSession('wwjs-defaults');
  assert.equal((await wwjs.health('wwjs-defaults')).runtimeConfiguration?.reconnectBaseDelayMs, 5000, 'whatsapp-web.js deployment evidence must retain the five-second worker fallback');
  await wwjs.stopSession('wwjs-defaults');
  await wwjs.deleteSession('wwjs-defaults');
  await wwjs.onModuleDestroy();
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
  const service = new EmbeddedOpenWAEngineService(identity, publisher, inbound, observability, ownedRuntime);
  await service.onModuleInit();
  await service.createSession('watchdog-session');
  const runtime = {
    reconnectMode: 'UNBOUNDED', reconnectMaxAttempts: 0, reconnectBaseDelayMs: 5000,
    reconnectStabilityResetMs: 300000, watchdogProbeTimeoutMs: 100, watchdogFailureThreshold: 3,
    engineTeardownTimeoutMs: 30000, source: 'GOVERNED_CONFIGURATION', configurationId: 'watchdog-config',
    scopeType: 'SENDER_SESSION', scopeId: 'watchdog-session', version: 2,
  };
  await service.startSession('watchdog-session', { runtime });
  const engine = fakeEngines.at(-1);
  engine.triggerReady();
  engine.probeAlive = false;
  await new Promise(resolve => setTimeout(resolve, 2300));
  assert.equal(engine.calls.includes('forceDestroy'), false, 'governed watchdog threshold must delay recovery until the configured failure count');
  await new Promise(resolve => setTimeout(resolve, 1200));
  assert.equal(engine.calls.includes('forceDestroy'), true, 'watchdog must tear down a silently dead READY engine at the governed threshold');
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
  const startOptions = { proxy: { url: 'http://proxy.example:8080', type: 'http' } };
  await provider.startSession('session-1', startOptions);
  assert.deepEqual(calls.find(call => call[0] === 'startSession'), ['startSession', 'session-1', startOptions]);
  await provider.stopSession('session-1');
  await provider.logoutSession('session-1');
  await provider.deleteSession('session-1');
  assert.deepEqual(calls.map(call => call[0]), ['health', 'createSession', 'startSession', 'stopSession', 'logoutSession', 'deleteSession']);
}

async function testBootOwnershipStopsActivationAndRetiresEngine() {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'embedded-openwa-owned-'));
  const originalRegistry = process.env.OPENWA_SESSION_REGISTRY_DIR;
  const originalAuth = process.env.OPENWA_SESSION_DATA_DIR;
  process.env.OPENWA_SESSION_REGISTRY_DIR = path.join(root, 'registry');
  process.env.OPENWA_SESSION_DATA_DIR = path.join(root, 'auth');
  let owned = false;
  let onLoss;
  const ownership = {
    assertSessionOwned() { if (!owned) throw new ServiceUnavailableException('current process does not own session'); },
    onSessionOwnershipLost(listener) { onLoss = listener; return () => { onLoss = undefined; }; },
  };
  const { EmbeddedOpenWAEngineService } = serviceModule();
  const service = new EmbeddedOpenWAEngineService({ engine: 'BAILEYS' }, { create: x => x, queue: async () => {} }, { queue: async () => {} }, { increment() {}, gauge() {}, observe() {} }, ownership);
  try {
    await service.onModuleInit();
    await service.createSession('boot-owned');
    const before = fakeEngines.length;
    await assert.rejects(() => service.startSession('boot-owned'), /does not own/);
    assert.equal(fakeEngines.length, before, 'unowned process instantiated a transport');
    owned = true;
    await service.startSession('boot-owned');
    const engine = fakeEngines.at(-1);
    assert.equal(typeof engine.config.assertSessionOwnership, 'function', 'retained reconnect did not receive the ownership guard');
    engine.triggerReady();
    assert.equal((await service.health('boot-owned')).ready, true);
    const registryFiles = await fs.readdir(process.env.OPENWA_SESSION_REGISTRY_DIR);
    const persisted = await fs.readFile(path.join(process.env.OPENWA_SESSION_REGISTRY_DIR, registryFiles[0]), 'utf8');
    owned = false;
    onLoss('boot-owned');
    assert.equal(service.sessions.get('boot-owned').engine, undefined, 'ownership loss did not synchronously detach engine');
    engine.triggerReady();
    assert.equal((await service.health('boot-owned')).ready, false, 'retired callbacks resurrected READY');
    await waitTurn();
    assert.ok(engine.calls.includes('forceDestroy'), 'ownership loss did not tear down the old engine');
    const afterLoss = fakeEngines.length;
    await assert.rejects(() => service.startSession('boot-owned'), /does not own/);
    assert.equal(fakeEngines.length, afterLoss, 'unowned manual restart instantiated another transport');
    assert.equal(await fs.readFile(path.join(process.env.OPENWA_SESSION_REGISTRY_DIR, registryFiles[0]), 'utf8'), persisted, 'ownership loss deleted or rewrote session persistence');
    assert.throws(() => engine.config.assertSessionOwnership(), /does not own/);
    owned = true;
    await service.startSession('boot-owned');
    assert.equal(fakeEngines.length, afterLoss + 1, 'controlled recovery did not create a new engine');
    console.log('Embedded boot ownership activation/retirement/recovery test passed.');
  } finally {
    await service.onModuleDestroy();
    if (originalRegistry === undefined) delete process.env.OPENWA_SESSION_REGISTRY_DIR; else process.env.OPENWA_SESSION_REGISTRY_DIR = originalRegistry;
    if (originalAuth === undefined) delete process.env.OPENWA_SESSION_DATA_DIR; else process.env.OPENWA_SESSION_DATA_DIR = originalAuth;
    await fs.rm(root, { recursive: true, force: true });
  }
}

async function testOwnershipRetirementPublishesRecoverableTelemetry() {
  for (const engineName of ['BAILEYS', 'WHATSAPP_WEB_JS']) {
    const root = await fs.mkdtemp(path.join(os.tmpdir(), 'ownership-retirement-'));
    const previousRegistry = process.env.OPENWA_SESSION_REGISTRY_DIR;
    const previousAuth = process.env.OPENWA_SESSION_DATA_DIR;
    process.env.OPENWA_SESSION_REGISTRY_DIR = path.join(root, 'registry');
    process.env.OPENWA_SESSION_DATA_DIR = path.join(root, 'auth');
    let owned = true;
    let onLoss;
    let finishRetirement;
    const retiring = new Promise(resolve => { finishRetirement = resolve; });
    const runtime = {
      assertSessionOwned() { if (!owned) throw new ServiceUnavailableException('current process does not own session'); },
      onSessionOwnershipLost(listener) { onLoss = listener; return () => { onLoss = undefined; }; },
    };
    const metrics = { increment() {}, gauge() {}, observe() {} };
    const { EmbeddedOpenWAEngineService } = serviceModule();
    const service = new EmbeddedOpenWAEngineService({ engine: engineName }, { create: x => x, queue: async () => {} }, { queue: async () => {} }, metrics, runtime);
    const { SessionHeartbeatPublisherService } = loadModule('services/openwa-gateway/src/session-heartbeat-publisher.service.ts');
    const reported = [];
    const heartbeat = new SessionHeartbeatPublisherService(
      { nodeId: 'retirement-node', adapterVersion: 'retirement-test' },
      { listSessions: () => service.listSessions(), health: async id => ({ ...(await service.health(id)), pipeline: {} }), synchronizeSentToday: (id, count) => service.synchronizeSentToday(id, count) },
      { isRuntimeRegistered: () => true, bootIdentity: () => 'retirement-boot', publishSessionHeartbeat: async (_id, report) => { reported.push(report.status); return { sentToday: report.sentToday }; } },
      metrics,
    );
    try {
      await service.onModuleInit();
      await service.createSession('retiring-session');
      await service.startSession('retiring-session');
      const engine = fakeEngines.at(-1);
      engine.triggerReady();
      engine.forceDestroy = () => retiring;
      owned = false;
      onLoss('retiring-session');
      await heartbeat.publishNow();
      assert.deepEqual(reported, ['DRAINING'], `${engineName}: routine retirement must not publish sticky RESTRICTED`);
      assert.equal((await service.health('retiring-session')).ready, false);
      // A second loss may follow another heartbeat while teardown remains pending.
      onLoss('retiring-session');
      await heartbeat.publishNow();
      assert.deepEqual(reported, ['DRAINING', 'DRAINING'], 'repeat loss cleared or promoted the pending retirement');
      // An operator can also stop the already detached session. That must not
      // orphan the original teardown completion or report recovery prematurely.
      await service.stopSession('retiring-session');
      await heartbeat.publishNow();
      assert.equal(reported.at(-1), 'DRAINING', 'manual stop hid unfinished retirement');
      finishRetirement();
      await waitTurn();
      await heartbeat.publishNow();
      assert.equal(reported.at(-1), 'DISCONNECTED', 'completed teardown did not restore a non-sending recovery state');
      owned = true;
      await service.startSession('retiring-session');
      const replacement = fakeEngines.at(-1);
      assert.notEqual(replacement, engine, 'controlled start reused the retired engine');
      replacement.triggerReady();
      replacement.forceDestroy = async () => { throw new Error('teardown proof unavailable'); };
      owned = false;
      onLoss('retiring-session');
      await waitTurn();
      await heartbeat.publishNow();
      assert.equal(reported.at(-1), 'RESTRICTED', 'genuine teardown failure must remain fail-closed');
      assert.equal((await service.health('retiring-session')).ready, false);
    } finally {
      finishRetirement();
      await waitTurn();
      heartbeat.onModuleDestroy();
      await service.onModuleDestroy();
      if (previousRegistry === undefined) delete process.env.OPENWA_SESSION_REGISTRY_DIR; else process.env.OPENWA_SESSION_REGISTRY_DIR = previousRegistry;
      if (previousAuth === undefined) delete process.env.OPENWA_SESSION_DATA_DIR; else process.env.OPENWA_SESSION_DATA_DIR = previousAuth;
      await fs.rm(root, { recursive: true, force: true });
    }
  }
  console.log('Ownership retirement telemetry and failure policy tests passed for both engines.');
}

(async () => {
  await testOwnershipRetirementPublishesRecoverableTelemetry();
  await testBootOwnershipStopsActivationAndRetiresEngine();
  await testLifecycleAndEvents();
  await testExplicitRestartUsesCurrentProxyConfiguration();
  await testEngineSpecificDeploymentRecoveryDefaults();
  await testWatchdogRecoversSilentDeadReadySession();
  await testProviderDelegatesWithoutFetch();
  console.log('Embedded OpenWA engine tests passed.');
})().catch(error => {
  console.error(error?.stack ?? error);
  process.exit(1);
});
