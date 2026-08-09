import { ConflictException, Injectable, OnModuleInit, ServiceUnavailableException } from '@nestjs/common';
import { createHash } from 'node:crypto';
import { mkdir, open, readFile, readdir, rename, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import type { SendRequest, SendResult } from './provider/messaging-provider';

type StoredEntry = {
  fingerprint: string;
  state: 'PENDING' | 'COMPLETED' | 'UNKNOWN';
  createdAt: string;
  completedAt?: string;
  result?: SendResult;
  reconciliation?: { operatorReference: string; evidenceHash: string; reconciledAt: string };
};

type MemoryEntry = { fingerprint: string; promise: Promise<SendResult> };

@Injectable()
export class IdempotencyService implements OnModuleInit {
  private readonly entries = new Map<string, MemoryEntry>();
  private readonly directory = String(process.env.GATEWAY_IDEMPOTENCY_DIR ?? '/data/gateway-idempotency');
  private readonly maximumEntries = positiveInteger(process.env.IDEMPOTENCY_MAX_ENTRIES, 1_000_000);
  private readonly completedRetentionDays = boundedInteger(process.env.IDEMPOTENCY_COMPLETED_RETENTION_DAYS, 30, 1, 3650);
  private readonly pendingMaximumMinutes = boundedInteger(process.env.IDEMPOTENCY_PENDING_MAXIMUM_MINUTES, 60, 5, 10_080);
  private readonly cleanupIntervalMs = boundedInteger(process.env.IDEMPOTENCY_CLEANUP_INTERVAL_MS, 60_000, 5_000, 3_600_000);
  private lastCleanup = 0;

  async onModuleInit() {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    await this.cleanup(Date.now(), true);
  }

  async execute(request: SendRequest, operation: () => Promise<SendResult>): Promise<SendResult> {
    const fingerprint = requestFingerprint(request);
    const existingMemory = this.entries.get(request.idempotencyKey);
    if (existingMemory) {
      if (existingMemory.fingerprint !== fingerprint) throw new ConflictException('idempotency key was already used for a different request');
      return existingMemory.promise.then(result => ({ ...result, duplicate: true }));
    }

    // Register the promise before the first asynchronous filesystem operation.
    // JavaScript runs this section synchronously, so identical concurrent calls
    // in one process share the same provider submission rather than racing to
    // create the durable record and returning a spurious conflict.
    const promise = this.executeOnce(request, fingerprint, operation);
    this.entries.set(request.idempotencyKey, { fingerprint, promise });
    try {
      return await promise;
    } finally {
      if (this.entries.get(request.idempotencyKey)?.promise === promise) this.entries.delete(request.idempotencyKey);
    }
  }

  private async executeOnce(request: SendRequest, fingerprint: string, operation: () => Promise<SendResult>): Promise<SendResult> {
    const now = Date.now();
    if (now - this.lastCleanup >= this.cleanupIntervalMs) await this.cleanup(now);
    const stored = await this.read(request.idempotencyKey);
    if (stored) {
      if (stored.fingerprint !== fingerprint) throw new ConflictException('idempotency key was already used for a different request');
      if (stored.state === 'COMPLETED' && stored.result) return { ...stored.result, duplicate: true };
      throw new ConflictException('an earlier gateway submission has an unresolved outcome and will not be repeated automatically');
    }
    if (await this.durableCount() >= this.maximumEntries) throw new ServiceUnavailableException('idempotency store capacity is exhausted');

    const pending: StoredEntry = { fingerprint, state: 'PENDING', createdAt: new Date().toISOString() };
    await this.create(request.idempotencyKey, pending);
    try {
      const result = await operation();
      const complete: StoredEntry = { ...pending, state: 'COMPLETED', completedAt: new Date().toISOString(), result: { ...result, duplicate: false } };
      await this.replace(request.idempotencyKey, complete);
      return { ...result, duplicate: false };
    } catch (error) {
      const unknown: StoredEntry = { ...pending, state: 'UNKNOWN', completedAt: new Date().toISOString() };
      await this.replace(request.idempotencyKey, unknown).catch(() => undefined);
      throw error;
    }
  }

  async describe(idempotencyKey: string): Promise<Omit<StoredEntry, 'result'> & { hasResult: boolean } | undefined> {
    const stored = await this.read(idempotencyKey);
    if (!stored) return undefined;
    const { result: _result, ...safe } = stored;
    return { ...safe, hasResult: Boolean(stored.result) };
  }

  async reconcileUnknown(idempotencyKey: string, expectedFingerprint: string, result: SendResult, operatorReference: string, evidenceHash: string): Promise<void> {
    if (!/^[A-Za-z0-9._:-]{3,200}$/.test(operatorReference)) throw new ConflictException('operator reconciliation reference is invalid');
    if (!/^[a-f0-9]{64}$/.test(evidenceHash)) throw new ConflictException('reconciliation evidence hash is invalid');
    const stored = await this.read(idempotencyKey);
    if (!stored || stored.state !== 'UNKNOWN') throw new ConflictException('only unresolved idempotency records may be reconciled');
    if (stored.fingerprint !== expectedFingerprint) throw new ConflictException('reconciliation evidence does not match the original submission');
    await this.replace(idempotencyKey, {
      ...stored,
      state: 'COMPLETED',
      completedAt: new Date().toISOString(),
      result: { ...result, duplicate: false },
      reconciliation: { operatorReference, evidenceHash, reconciledAt: new Date().toISOString() }
    });
  }

  private async durableCount(): Promise<number> {
    const entries = await readdir(this.directory);
    return entries.filter(name => /^[A-Za-z0-9_-]+\.json$/.test(name)).length;
  }

  private async cleanup(nowMilliseconds: number, recoverPendingAfterRestart = false): Promise<void> {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const completedBefore = nowMilliseconds - this.completedRetentionDays * 86_400_000;
    const pendingBefore = nowMilliseconds - this.pendingMaximumMinutes * 60_000;
    const entries = await readdir(this.directory);
    for (const name of entries.filter(value => /^[A-Za-z0-9_-]+\.json$/.test(value))) {
      const path = join(this.directory, name);
      let stored: StoredEntry;
      try {
        stored = JSON.parse(await readFile(path, 'utf8')) as StoredEntry;
      } catch {
        // Retain corrupt evidence fail-closed: its filename continues to block reuse.
        continue;
      }
      const created = Date.parse(stored.createdAt);
      const completed = Date.parse(stored.completedAt ?? '');
      if (stored.state === 'COMPLETED' && Number.isFinite(completed) && completed <= completedBefore) {
        await rm(path, { force: true });
        await syncDirectory(this.directory);
      } else if (stored.state === 'PENDING' && Number.isFinite(created) && (recoverPendingAfterRestart || created <= pendingBefore)) {
        await replacePath(path, { ...stored, state: 'UNKNOWN', completedAt: new Date(nowMilliseconds).toISOString() });
      }
      // UNKNOWN is never deleted automatically; only governed reconciliation can close it.
    }
    this.lastCleanup = nowMilliseconds;
  }

  private async create(key: string, entry: StoredEntry) {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const path = this.path(key);
    try {
      await writeDurableExclusive(path, entry);
    } catch (error) {
      if ((error as NodeJS.ErrnoException)?.code === 'EEXIST') throw new ConflictException('idempotency record already exists');
      throw error;
    }
  }
  private async replace(key: string, entry: StoredEntry) { await replacePath(this.path(key), entry); }
  private async read(key: string): Promise<StoredEntry | undefined> {
    try { return JSON.parse(await readFile(this.path(key), 'utf8')) as StoredEntry; }
    catch (error) { if ((error as NodeJS.ErrnoException)?.code === 'ENOENT') return undefined; throw error; }
  }
  private path(key: string): string { return join(this.directory, `${safeName(key)}.json`); }
}

async function replacePath(path: string, entry: StoredEntry): Promise<void> {
  const temporary = `${path}.${process.pid}.${Date.now()}.${Math.random().toString(16).slice(2)}.tmp`;
  try {
    await writeDurableExclusive(temporary, entry);
    await rename(temporary, path);
    await syncDirectory(dirname(path));
  } catch (error) {
    await rm(temporary, { force: true }).catch(() => undefined);
    throw error;
  }
}

async function writeDurableExclusive(path: string, entry: StoredEntry): Promise<void> {
  const handle = await open(path, 'wx', 0o600);
  try {
    await handle.writeFile(JSON.stringify(entry), { encoding: 'utf8' });
    await handle.sync();
  } finally {
    await handle.close();
  }
  await syncDirectory(dirname(path));
}

async function syncDirectory(directory: string): Promise<void> {
  const handle = await open(directory, 'r');
  try { await handle.sync(); } finally { await handle.close(); }
}

function requestFingerprint(request: SendRequest): string {
  const canonical = JSON.stringify({
    provider: request.provider,
    engine: request.engine,
    gatewayPoolId: request.gatewayPoolId,
    gatewayPoolVersion: request.gatewayPoolVersion,
    gatewayAdapterVersion: request.gatewayAdapterVersion,
    gatewayNodeId: request.gatewayNodeId,
    gatewayNodeVersion: request.gatewayNodeVersion,
    sessionId: request.sessionId,
    sessionLeaseVersion: request.sessionLeaseVersion,
    sessionConfigurationVersion: request.sessionConfigurationVersion,
    authorityExpiresAt: request.authorityExpiresAt,
    routeReference: request.routeReference,
    recipientMsisdn: request.recipientMsisdn,
    messageType: request.messageType,
    body: request.body ?? '',
    mediaUrl: request.mediaUrl ?? '',
    clientReference: request.clientReference ?? ''
  });
  return createHash('sha256').update(canonical).digest('hex');
}
function safeName(value: string): string {
  if (!/^[A-Za-z0-9:_-]{16,200}$/.test(value)) throw new TypeError('idempotency key format is invalid');
  return Buffer.from(value).toString('base64url');
}
function positiveInteger(value: string | undefined, fallback: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : fallback;
}
function boundedInteger(value: string | undefined, fallback: number, minimum: number, maximum: number): number {
  const parsed = Number.parseInt(value ?? '', 10);
  return Number.isInteger(parsed) && parsed >= minimum && parsed <= maximum ? parsed : fallback;
}
