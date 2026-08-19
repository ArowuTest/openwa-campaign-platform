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
  throw new Error('TypeScript is required for gateway outbox tests');
}

const nestMock = {
  Injectable: () => target => target,
  Logger: class { warn() {} },
};

function loadOutbox(relativePath) {
  const filename = path.join(process.cwd(), relativePath);
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

async function assertEvidenceCannotBeReplaced({
  modulePath,
  className,
  environmentName,
  original,
  replacement,
}) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'gateway-outbox-'));
  process.env[environmentName] = root;
  const Outbox = loadOutbox(modulePath)[className];
  const outbox = new Outbox();
  await outbox.onModuleInit();
  try {
    await outbox.enqueue(original);
    await outbox.enqueue(original);
    await assert.rejects(
      () => outbox.enqueue(replacement),
      /identifier was reused with different evidence/,
    );
    const files = (await fs.readdir(root)).filter(name => name.endsWith('.json'));
    assert.equal(files.length, 1);
    const stored = JSON.parse(await fs.readFile(path.join(root, files[0]), 'utf8'));
    assert.deepEqual(stored, original);
  } finally {
    await outbox.onModuleDestroy();
    delete process.env[environmentName];
    await fs.rm(root, { recursive: true, force: true });
  }
}

test('provider event outbox rejects event-id evidence replacement', async () => {
  const original = {
    schemaVersion: '1.0',
    eventId: 'provider-event-1',
    eventType: 'message.sent',
    sessionId: 'session-1',
    providerMessageId: 'provider-message-1',
    occurredAt: '2099-10-03T12:00:00.000Z',
  };
  await assertEvidenceCannotBeReplaced({
    modulePath: 'services/openwa-gateway/src/provider-event-outbox.service.ts',
    className: 'ProviderEventOutboxService',
    environmentName: 'GATEWAY_EVENT_OUTBOX_DIR',
    original,
    replacement: { ...original, providerMessageId: 'provider-message-replaced' },
  });
});

test('inbound event outbox rejects event-id evidence replacement', async () => {
  const original = {
    schemaVersion: '1.0',
    eventId: 'inbound-event-1',
    sessionId: 'session-1',
    senderMsisdn: '+2348012345678',
    messageText: 'original inbound evidence',
    occurredAt: '2099-10-03T12:00:00.000Z',
  };
  await assertEvidenceCannotBeReplaced({
    modulePath: 'services/openwa-gateway/src/inbound-message-outbox.service.ts',
    className: 'InboundMessageOutboxService',
    environmentName: 'GATEWAY_INBOUND_OUTBOX_DIR',
    original,
    replacement: { ...original, messageText: 'replacement inbound evidence' },
  });
});

async function assertShutdownPreservesFailedEvidence({ modulePath, className, environmentName, event }) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'gateway-shutdown-outbox-'));
  process.env[environmentName] = root;
  const Outbox = loadOutbox(modulePath)[className];
  const outbox = new Outbox();
  await outbox.onModuleInit();
  try {
    outbox.setSender(async () => { throw new Error('control plane unavailable'); });
    await outbox.enqueue(event);
    await outbox.onModuleDestroy();
    const files = (await fs.readdir(root)).filter(name => name.endsWith('.json'));
    assert.equal(files.length, 1, 'failed shutdown flush must leave durable evidence for restart');
  } finally {
    delete process.env[environmentName];
    await fs.rm(root, { recursive: true, force: true });
  }
}

test('provider outbox preserves undelivered evidence during shutdown', async () => {
  await assertShutdownPreservesFailedEvidence({
    modulePath: 'services/openwa-gateway/src/provider-event-outbox.service.ts',
    className: 'ProviderEventOutboxService', environmentName: 'GATEWAY_EVENT_OUTBOX_DIR',
    event: { schemaVersion: '1.0', eventId: 'shutdown-provider-1', eventType: 'message.sent',
      sessionId: 'session-1', providerMessageId: 'provider-1', occurredAt: '2099-10-03T12:00:00.000Z' },
  });
});

test('inbound outbox preserves undelivered evidence during shutdown', async () => {
  await assertShutdownPreservesFailedEvidence({
    modulePath: 'services/openwa-gateway/src/inbound-message-outbox.service.ts',
    className: 'InboundMessageOutboxService', environmentName: 'GATEWAY_INBOUND_OUTBOX_DIR',
    event: { schemaVersion: '1.0', eventId: 'shutdown-inbound-1', sessionId: 'session-1',
      senderMsisdn: '+2348012345678', messageText: 'stop', occurredAt: '2099-10-03T12:00:00.000Z' },
  });
});


async function assertPoisonDoesNotBlockValidEvidence({ modulePath, className, environmentName, event }) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'gateway-poison-outbox-'));
  process.env[environmentName] = root;
  const Outbox = loadOutbox(modulePath)[className];
  const outbox = new Outbox();
  await outbox.onModuleInit();
  const sent = [];
  try {
    outbox.setSender(async value => { sent.push(value); });
    await outbox.enqueue(event);
    await fs.writeFile(path.join(root, '000-poison.json'), '{not-json', { mode: 0o600 });
    await outbox.flush();
    assert.deepEqual(sent.map(value => value.eventId), [event.eventId], 'poison evidence head-of-line blocked a valid event');
    const quarantine = path.join(root, '.quarantine');
    const quarantined = await fs.readdir(quarantine);
    assert.equal(quarantined.length, 1, 'poison evidence was not retained in quarantine');
    assert.match(quarantined[0], /000-poison\.json/);
  } finally {
    await outbox.onModuleDestroy();
    delete process.env[environmentName];
    await fs.rm(root, { recursive: true, force: true });
  }
}

test('provider outbox quarantines poison JSON without blocking valid evidence', async () => {
  await assertPoisonDoesNotBlockValidEvidence({
    modulePath: 'services/openwa-gateway/src/provider-event-outbox.service.ts',
    className: 'ProviderEventOutboxService', environmentName: 'GATEWAY_EVENT_OUTBOX_DIR',    event: { schemaVersion: '1.0', eventId: 'provider-poison-valid', eventType: 'message.sent',
      sessionId: 'session-1', providerMessageId: 'provider-valid', occurredAt: '2099-10-03T12:00:00.000Z' },
  });
});

test('inbound outbox quarantines poison JSON without blocking valid evidence', async () => {
  await assertPoisonDoesNotBlockValidEvidence({
    modulePath: 'services/openwa-gateway/src/inbound-message-outbox.service.ts',
    className: 'InboundMessageOutboxService', environmentName: 'GATEWAY_INBOUND_OUTBOX_DIR',
    event: { schemaVersion: '1.0', eventId: 'inbound-poison-valid', sessionId: 'session-1',
      senderMsisdn: '+2348012345678', messageText: 'stop', occurredAt: '2099-10-03T12:00:00.000Z' },
  });
});

async function assertMismatchedFileCannotRetargetAcknowledgement({
  modulePath, className, environmentName, valid, rogue,
}) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), 'gateway-mismatch-outbox-'));
  process.env[environmentName] = root;
  const Outbox = loadOutbox(modulePath)[className];
  const outbox = new Outbox();
  await outbox.onModuleInit();
  const sent = [];
  try {
    outbox.setSender(async value => { sent.push(value); });
    await outbox.enqueue(valid);
    await fs.writeFile(path.join(root, '000-mismatch.json'), JSON.stringify(rogue), { mode: 0o600 });
    await outbox.flush();
    assert.deepEqual(sent, [valid], 'mismatched filename/payload evidence reached the control plane');    const quarantine = path.join(root, '.quarantine');
    const quarantined = await fs.readdir(quarantine);
    assert.equal(quarantined.length, 1, 'mismatched evidence was not quarantined');
    assert.match(quarantined[0], /000-mismatch\.json/);
  } finally {
    await outbox.onModuleDestroy();
    delete process.env[environmentName];
    await fs.rm(root, { recursive: true, force: true });
  }
}

test('provider outbox binds acknowledgement to the exact durable file', async () => {
  const valid = { schemaVersion: '1.0', eventId: 'provider-file-bind', eventType: 'message.sent',
    sessionId: 'session-1', providerMessageId: 'provider-good', occurredAt: '2099-10-03T12:00:00.000Z' };
  await assertMismatchedFileCannotRetargetAcknowledgement({
    modulePath: 'services/openwa-gateway/src/provider-event-outbox.service.ts',
    className: 'ProviderEventOutboxService', environmentName: 'GATEWAY_EVENT_OUTBOX_DIR',
    valid, rogue: { ...valid, providerMessageId: 'provider-rogue' },
  });
});

test('inbound outbox binds delivery to the exact durable file identity', async () => {
  const valid = { schemaVersion: '1.0', eventId: 'inbound-file-bind', sessionId: 'session-1',
    senderMsisdn: '+2348012345678', messageText: 'legitimate', occurredAt: '2099-10-03T12:00:00.000Z' };
  await assertMismatchedFileCannotRetargetAcknowledgement({
    modulePath: 'services/openwa-gateway/src/inbound-message-outbox.service.ts',
    className: 'InboundMessageOutboxService', environmentName: 'GATEWAY_INBOUND_OUTBOX_DIR',
    valid, rogue: { ...valid, messageText: 'rogue' },
  });
});

test('gateway outboxes publish final evidence atomically instead of writing final JSON directly', async () => {
  for (const relative of [
    'services/openwa-gateway/src/provider-event-outbox.service.ts',
    'services/openwa-gateway/src/inbound-message-outbox.service.ts',
  ]) {
    const source = await fs.readFile(path.join(process.cwd(), relative), 'utf8');
    assert.match(source, /\blink\s*\(/, `${relative} does not atomically publish a completed temp file`);
    assert.match(source, /\.tmp/, `${relative} does not use a non-consumable temporary file`);
    assert.doesNotMatch(
      source,
      /open\(path,\s*['"]wx['"]/, 
      `${relative} still writes directly into the final consumable JSON path`,
    );
  }
});